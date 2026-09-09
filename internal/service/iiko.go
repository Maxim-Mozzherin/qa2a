package service

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"qa2a/internal/crypto"
	"qa2a/internal/models"
	"qa2a/internal/repository"

	"github.com/jmoiron/sqlx"
)

// IikoService обеспечивает интеграцию с REST/XML API iiko RMS:
// синхронизацию номенклатуры, складов, остатков и выгрузку списаний/перемещений.
type IikoService struct {
	repo          *repository.Repository
	encryptionKey string
	httpClient    *http.Client
}

// NewIikoService создает экземпляр сервиса iiko с настроенным HTTP-клиентом.
func NewIikoService(repo *repository.Repository, encryptionKey string) *IikoService {
	return &IikoService{
		repo:          repo,
		encryptionKey: encryptionKey,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        50,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// ============================================================================
// XML СТРУКТУРЫ ДЛЯ IIKO API V1 (/resto/api)
// ============================================================================

type XMLStore struct {
	ID   string `xml:"id"`
	Name string `xml:"name"`
	Type string `xml:"type"`
}

type XMLStores struct {
	List []XMLStore `xml:"corporateItemDto"`
}

type XMLProduct struct {
	ID               string `xml:"id"`
	Name             string `xml:"name"`
	MainUnit         string `xml:"mainUnit"`
	ProductType      string `xml:"productType"`
	Type             string `xml:"type"`
	ParentID         string `xml:"parentId"`
	ProductGroupType string `xml:"productGroupType"`
}

type XMLProducts struct {
	List []XMLProduct `xml:"productDto"`
}

type IikoV2Response struct {
	Result string   `json:"result"`
	Errors []string `json:"errors"`
}

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ МЕТОДЫ И БЕЗОПАСНОСТЬ
// ============================================================================

// cleanHost нормализует URL сервера iiko RMS (удаляет /resto и завершающие слэши).
func (s *IikoService) cleanHost(host string) string {
	h := strings.TrimSpace(host)
	h = strings.TrimSuffix(h, "/")
	h = strings.TrimSuffix(h, "/resto")
	h = strings.TrimSuffix(h, "/")
	if h != "" && !strings.HasPrefix(h, "http://") && !strings.HasPrefix(h, "https://") {
		h = "https://" + h
	}
	return h
}

// getDecryptedSettings загружает настройки компании и расшифровывает пароль API.
func (s *IikoService) getDecryptedSettings(companyID int) (*models.IikoSettings, error) {
	settings, err := s.repo.GetIikoSettings(companyID)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения настроек заведения #%d: %w", companyID, err)
	}

	settings.Host = s.cleanHost(settings.Host)

	if settings.Password != "" {
		decrypted, err := crypto.Decrypt(settings.Password, s.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("ошибка дешифрования пароля iiko RMS заведения #%d: %w", companyID, err)
		}
		settings.Password = decrypted
	}
	return settings, nil
}

// buildIikoComment форматирует комментарий документа с именами пользователей Telegram
// и строго обрезает длину до 255 символов (ограничение БД iiko RMS).
func (s *IikoService) buildIikoComment(opIDs []string, defaultPrefix, opDate string) string {
	var finalComment string
	rawComments, err := s.repo.GetWriteoffComments(opIDs)
	if err != nil {
		log.Printf("[iiko-export] ⚠️ Ошибка выборки комментариев списаний: %v", err)
	}

	if len(rawComments) > 0 {
		var commentParts []string
		for _, rc := range rawComments {
			userSuffix := ""
			if rc.Username != "" {
				userSuffix = " (@" + rc.Username + ")"
			}

			if rc.Comment != "" {
				commentParts = append(commentParts, fmt.Sprintf("%s-%s%s", rc.PositionName, rc.Comment, userSuffix))
			} else {
				commentParts = append(commentParts, fmt.Sprintf("%s%s", rc.PositionName, userSuffix))
			}
		}

		if len(commentParts) > 0 {
			rawList := strings.Join(commentParts, ", ")
			dateSuffix := ". (" + opDate + ")"

			maxListLen := 255 - len(dateSuffix)
			runes := []rune(rawList)
			if len(runes) > maxListLen {
				rawList = string(runes[:maxListLen-3]) + "..."
			}
			finalComment = rawList + dateSuffix
		}
	}

	if finalComment == "" {
		finalComment = fmt.Sprintf("%s (%s)", defaultPrefix, opDate)
	}
	return finalComment
}

// ============================================================================
// УПРАВЛЕНИЕ НАСТРОЙКАМИ IIKO RMS
// ============================================================================

// SaveSettings сохраняет хост, логин и пароль с шифрованием AES-256 (доступно только Владельцу).
func (s *IikoService) SaveSettings(companyID, userID int, host, login, pass string) error {
	member, err := s.repo.GetMembership(companyID, userID)
	if err != nil {
		return fmt.Errorf("ошибка доступа к компании: %w", err)
	}
	if strings.ToLower(member.Role) != "owner" {
		return fmt.Errorf("только Владелец может изменять параметры интеграции iiko RMS")
	}

	cleanHost := s.cleanHost(host)
	cleanLogin := strings.TrimSpace(login)

	encryptedPass := ""
	if pass != "" && pass != "********" {
		encryptedPass, err = crypto.Encrypt(pass, s.encryptionKey)
		if err != nil {
			return fmt.Errorf("ошибка шифрования мастер-пароля: %w", err)
		}
	} else if pass == "********" {
		encryptedPass = "" // Пустая строка говорит репозиторию сохранить старый пароль в БД
	}

	return s.repo.UpdateIikoSettings(companyID, cleanHost, cleanLogin, encryptedPass)
}

// GetSettings возвращает хост и логин, маскируя пароль звездочками.
func (s *IikoService) GetSettings(companyID, userID int) (*models.IikoSettings, error) {
	member, err := s.repo.GetMembership(companyID, userID)
	if err != nil || strings.ToLower(member.Role) != "owner" {
		return nil, fmt.Errorf("доступ запрещен: просматривать настройки может только Владелец")
	}

	settings, err := s.repo.GetIikoSettings(companyID)
	if err != nil {
		return nil, err
	}

	if settings.Password != "" {
		settings.Password = "********"
	}
	return settings, nil
}

// Auth выполняет авторизацию в iiko RMS API и возвращает сессионный токен (key).
func (s *IikoService) Auth(host, login, password string) (string, error) {
	cleanH := s.cleanHost(host)
	passHash := crypto.HashPasswordSHA1(password)

	authURL := fmt.Sprintf("%s/resto/api/auth?login=%s&pass=%s", cleanH, login, passHash)

	resp, err := s.httpClient.Get(authURL)
	if err != nil {
		return "", fmt.Errorf("сетевая ошибка при подключении к iiko (%s): %w", cleanH, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа авторизации iiko: %w", err)
	}
	token := strings.TrimSpace(string(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ошибка авторизации iiko RMS (HTTP %d): %s", resp.StatusCode, token)
	}

	return token, nil
}

// ============================================================================
// СИНХРОНИЗАЦИЯ СПРАВОЧНИКОВ (СКЛАДЫ И НОМЕНКЛАТУРА)
// ============================================================================

// SyncStores загружает список торговых складов из iiko и обновляет таблицу locations.
func (s *IikoService) SyncStores(companyID int, token, host string) error {
	cleanH := s.cleanHost(host)
	url := fmt.Sprintf("%s/resto/api/corporation/stores?key=%s", cleanH, token)

	resp, err := s.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("ошибка запроса складов: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("iiko вернул ошибку при запросе складов (код %d): %s", resp.StatusCode, string(body))
	}

	var stores XMLStores
	if err := xml.NewDecoder(resp.Body).Decode(&stores); err != nil {
		return fmt.Errorf("ошибка парсинга XML складов: %w", err)
	}

	return s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		for _, st := range stores.List {
			if strings.ToUpper(st.Type) != "STORE" {
				continue
			}

			var count int
			_ = tx.Get(&count, "SELECT count(*) FROM locations WHERE company_id = $1 AND external_id = $2", companyID, st.ID)

			if count == 0 {
				if _, err := tx.Exec("INSERT INTO locations (company_id, name, external_id) VALUES ($1, $2, $3)", companyID, st.Name, st.ID); err != nil {
					return err
				}
			} else {
				if _, err := tx.Exec("UPDATE locations SET name = $1 WHERE company_id = $2 AND external_id = $3", st.Name, companyID, st.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// SyncNomenclature синхронизирует товары, заготовки и структуру категорий (Conception).
func (s *IikoService) SyncNomenclature(companyID int, userID int) error {
	settings, err := s.getDecryptedSettings(companyID)
	if err != nil {
		return err
	}

	token, err := s.Auth(settings.Host, settings.Login, settings.Password)
	if err != nil {
		return err
	}

	// 1. Синхронизируем склады
	if err := s.SyncStores(companyID, token, settings.Host); err != nil {
		log.Printf("[iiko-sync] ⚠️ Предупреждение при синхронизации складов компании #%d: %v", companyID, err)
	}

	// 2. Скачиваем номенклатуру
	prodURL := fmt.Sprintf("%s/resto/api/products?key=%s", settings.Host, token)
	resp, err := s.httpClient.Get(prodURL)
	if err != nil {
		return fmt.Errorf("ошибка запроса товаров iiko: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ошибка загрузки товаров iiko (HTTP %d)", resp.StatusCode)
	}

	var products XMLProducts
	if err := xml.NewDecoder(resp.Body).Decode(&products); err != nil {
		return fmt.Errorf("ошибка разбора XML номенклатуры: %w", err)
	}
	log.Printf("[iiko-sync] Загружено %d позиций номенклатуры для компании #%d", len(products.List), companyID)

	// Построение карты иерархии категорий (Conception)
	parentMap := make(map[string]string, len(products.List))
	parentRelations := make(map[string]string, len(products.List))

	for _, p := range products.List {
		parentMap[p.ID] = p.Name
		if p.ParentID != "" {
			parentRelations[p.ID] = p.ParentID
		}
	}

	getFolderChain := func(parentID string) string {
		if parentID == "" {
			return ""
		}
		name, exists := parentMap[parentID]
		if !exists {
			return ""
		}
		ancestorID := parentRelations[parentID]
		if ancestorID != "" {
			ancestorName, existsAncestor := parentMap[ancestorID]
			if existsAncestor && ancestorName != "" {
				return ancestorName + " / " + name
			}
		}
		return name
	}

	// 3. Сохраняем позиции в базе данных
	return s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		for _, p := range products.List {
			pType := strings.ToUpper(strings.TrimSpace(p.ProductType))
			if pType == "" {
				pType = strings.ToUpper(strings.TrimSpace(p.Type))
			}

			if pType == "GOODS" || pType == "PREPARED" || pType == "DISH" || pType == "MODIFIER" {
				unitName := strings.TrimSpace(p.MainUnit)
				if unitName == "" {
					unitName = "ед."
				}
				pos := models.Position{
					CompanyID:  companyID,
					Name:       p.Name,
					Unit:       unitName,
					Supplier:   "iiko",
					ExternalID: p.ID,
					Type:       pType,
					Conception: getFolderChain(p.ParentID),
				}
				if err := s.repo.UpsertPositionTx(tx, pos); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// ============================================================================
// ОСТАТКИ И СПРАВОЧНИКИ (OLAP V2)
// ============================================================================

// FetchExpectedStock запрашивает расчетные остатки на складе через OLAP отчет транзакций.
func (s *IikoService) FetchExpectedStock(companyID int, storeExternalID string) (map[string]float64, error) {
	settings, err := s.getDecryptedSettings(companyID)
	if err != nil {
		return nil, err
	}

	token, err := s.Auth(settings.Host, settings.Login, settings.Password)
	if err != nil {
		return nil, err
	}

	olapPayload := models.OlapRequest{
		ReportType:       "TRANSACTIONS",
		GroupByRowFields: []string{"Product.Id", "Product.Name"},
		AggregateFields:  []string{"FinalBalance.Amount"},
		Filters: map[string]interface{}{
			"Account.Id": map[string]interface{}{
				"filterType": "IncludeValues",
				"values":     []string{storeExternalID},
			},
			"Account.Type": map[string]interface{}{
				"filterType": "IncludeValues",
				"values":     []string{"INVENTORY_ASSETS"},
			},
			"DateTime.DateTyped": map[string]interface{}{
				"filterType": "DateRange",
				"periodType": "CUSTOM",
				"from":       "2010-01-01",
				"to":         "2030-12-31",
			},
		},
	}

	payloadBytes, err := json.Marshal(olapPayload)
	if err != nil {
		return nil, err
	}

	olapURL := fmt.Sprintf("%s/resto/api/v2/reports/olap?key=%s", settings.Host, token)
	req, err := http.NewRequest("POST", olapURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("сетевая ошибка при запросе OLAP iiko: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ошибка отчета OLAP iiko (код %d): %s", resp.StatusCode, string(body))
	}

	var olapData models.OlapStockResponse
	if err := json.NewDecoder(resp.Body).Decode(&olapData); err != nil {
		return nil, fmt.Errorf("ошибка парсинга ответа OLAP: %w", err)
	}

	stockMap := make(map[string]float64, len(olapData.Data))
	for _, row := range olapData.Data {
		if row.ProductID != "" {
			stockMap[strings.ToUpper(row.ProductID)] = row.FinalBalance
		}
	}
	return stockMap, nil
}

// FetchGoodsCatalog возвращает карту UUID товаров с типом 'GOODS'.
func (s *IikoService) FetchGoodsCatalog(companyID int) (map[string]bool, error) {
	settings, err := s.getDecryptedSettings(companyID)
	if err != nil {
		return nil, err
	}

	token, err := s.Auth(settings.Host, settings.Login, settings.Password)
	if err != nil {
		return nil, err
	}

	prodURL := fmt.Sprintf("%s/resto/api/products?key=%s", settings.Host, token)
	resp, err := s.httpClient.Get(prodURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ошибка загрузки каталога товаров (код %d)", resp.StatusCode)
	}

	var products XMLProducts
	if err := xml.NewDecoder(resp.Body).Decode(&products); err != nil {
		return nil, fmt.Errorf("ошибка парсинга XML каталога: %w", err)
	}

	goodsMap := make(map[string]bool, len(products.List))
	for _, p := range products.List {
		pType := strings.ToUpper(strings.TrimSpace(p.ProductType))
		if pType == "" {
			pType = strings.ToUpper(strings.TrimSpace(p.Type))
		}
		if pType == "GOODS" {
			goodsMap[strings.ToLower(p.ID)] = true
		}
	}
	return goodsMap, nil
}

// ============================================================================
// ИНВЕНТАРИЗАЦИЯ И ВЫГРУЗКА БЛАНКОВ
// ============================================================================

// FetchDraftInventories загружает новые (статус NEW) бланки пересчета из iiko за последнюю неделю.
func (s *IikoService) FetchDraftInventories(companyID int, storeExternalID string) ([]models.IikoDraftInventory, error) {
	settings, err := s.getDecryptedSettings(companyID)
	if err != nil {
		return nil, err
	}

	token, err := s.Auth(settings.Host, settings.Login, settings.Password)
	if err != nil {
		return nil, err
	}

	dateFrom := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	dateTo := time.Now().Format("2006-01-02")

	url := fmt.Sprintf("%s/resto/api/v2/documents/incomingInventory?key=%s&dateFrom=%s&dateTo=%s", settings.Host, token, dateFrom, dateTo)
	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ошибка получения бланков iiko (код %d): %s", resp.StatusCode, string(respBody))
	}

	var drafts []models.IikoDraftInventory
	if err := json.NewDecoder(resp.Body).Decode(&drafts); err != nil {
		return nil, fmt.Errorf("ошибка парсинга бланков: %w", err)
	}

	var filtered []models.IikoDraftInventory
	for _, d := range drafts {
		if strings.ToUpper(d.Status) == "NEW" && d.StoreID == storeExternalID {
			filtered = append(filtered, d)
		}
	}

	return filtered, nil
}

// ExportInventoryAct отправляет заполненный акт инвентаризации в iiko RMS через XML импорт.
func (s *IikoService) ExportInventoryAct(companyID int, storeExternalID string, iikoDocID string, docNum string, items []models.InventoryItem) error {
	settings, err := s.getDecryptedSettings(companyID)
	if err != nil {
		return err
	}

	token, err := s.Auth(settings.Host, settings.Login, settings.Password)
	if err != nil {
		return err
	}

	var itemsXML strings.Builder
	for _, item := range items {
		if item.ExternalID != "" {
			itemsXML.WriteString(fmt.Sprintf(`
		<item>
			<productId>%s</productId>
			<amountContainer>%.3f</amountContainer>
		</item>`, item.ExternalID, item.ActualAmount))
		}
	}

	idTag := ""
	if iikoDocID != "" {
		idTag = fmt.Sprintf("<id>%s</id>", iikoDocID)
	}
	if docNum == "" {
		docNum = fmt.Sprintf("QA-INV-%d", time.Now().Unix())
	}

	xmlPayload := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<document>
	%s
	<documentNumber>%s</documentNumber>
	<dateIncoming>%s</dateIncoming>
	<status>NEW</status>
	<storeId>%s</storeId>
	<comment>Заполнено из QA2A</comment>
	<items>%s
	</items>
</document>`, idTag, docNum, time.Now().Format("2006-01-02T15:04:00"), storeExternalID, itemsXML.String())

	url := fmt.Sprintf("%s/resto/api/documents/import/incomingInventory?key=%s", settings.Host, token)
	req, err := http.NewRequest("POST", url, bytes.NewBufferString(xmlPayload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("сетевой сбой отправки инвентаризации в iiko: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("iiko отклонил акт инвентаризации (код %d): %s", res.StatusCode, string(respBody))
	}
	return nil
}

// ============================================================================
// ЕЖЕДНЕВНАЯ ВЫГРУЗКА ОПЕРАЦИЙ (СПИСАНИЯ, ПЕРЕМЕЩЕНИЯ, ПРИГОТОВЛЕНИЯ)
// ============================================================================

// ExportDailyOperations отправляет накопившиеся проводки в iiko (батчами, раздельно по типам).
func (s *IikoService) ExportDailyOperations(companyID int) error {
	log.Printf("[iiko-export] 🚀 Запуск выгрузки операций для заведения #%d", companyID)

	settings, err := s.getDecryptedSettings(companyID)
	if err != nil || settings.Host == "" {
		return fmt.Errorf("настройки iiko для компании #%d не заданы", companyID)
	}

	token, err := s.Auth(settings.Host, settings.Login, settings.Password)
	if err != nil {
		return fmt.Errorf("ошибка авторизации в iiko: %w", err)
	}

	// 1. ВЫГРУЗКА СПИСАНИЙ ТОВАРОВ (JSON v2)
	writeoffs, err := s.repo.GetGroupedWriteoffs(companyID)
	if err != nil {
		log.Printf("[iiko-export] ❌ Ошибка выборки списаний из БД: %v", err)
	} else if len(writeoffs) > 0 {
		log.Printf("[iiko-export] Найдено групп списаний: %d", len(writeoffs))
		storeGroups := make(map[string][]models.ExportOperationDTO)
		for _, w := range writeoffs {
			key := w.StoreFromID + "|" + w.AccountID + "|" + w.OpDate
			storeGroups[key] = append(storeGroups[key], w)
		}

		for key, items := range storeGroups {
			parts := strings.Split(key, "|")
			storeID := parts[0]
			accountID := parts[1]
			opDate := parts[2]

			const chunkSize = 15 // Оптимизированный размер пакета для списаний
			for i := 0; i < len(items); i += chunkSize {
				end := i + chunkSize
				if end > len(items) {
					end = len(items)
				}
				chunk := items[i:end]

				var iikoItems []map[string]interface{}
				var exportedOpIDs []string

				for _, item := range chunk {
					iikoItems = append(iikoItems, map[string]interface{}{
						"productId": item.ProductID,
						"amount":    item.TotalAmount,
					})
					exportedOpIDs = append(exportedOpIDs, strings.Split(item.OpIDs, ",")...)
				}

				finalComment := s.buildIikoComment(exportedOpIDs, "Списание из QA2A", opDate)

				payload := map[string]interface{}{
					"storeId":      storeID,
					"accountId":    accountID,
					"dateIncoming": opDate,
					"status":       "NEW",
					"comment":      finalComment,
					"items":        iikoItems,
				}

				payloadBytes, _ := json.Marshal(payload)
				url := fmt.Sprintf("%s/resto/api/v2/documents/writeoff?key=%s", settings.Host, token)
				req, _ := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
				req.Header.Set("Content-Type", "application/json")

				res, err := s.httpClient.Do(req)
				if err != nil {
					return fmt.Errorf("ошибка сети при выгрузке списания: %w", err)
				}
				respBody, _ := io.ReadAll(res.Body)
				res.Body.Close()

				if res.StatusCode != http.StatusOK {
					return fmt.Errorf("iiko отклонил списание (код %d): %s", res.StatusCode, string(respBody))
				}

				var iikoResult IikoV2Response
				if err := json.Unmarshal(respBody, &iikoResult); err == nil {
					if strings.ToUpper(iikoResult.Result) == "ERROR" {
						return fmt.Errorf("ошибка валидации списания iiko: %s", strings.Join(iikoResult.Errors, "; "))
					}
				}

				if err := s.repo.MarkOperationsExported(companyID, exportedOpIDs); err != nil {
					log.Printf("[iiko-export] ⚠️ Ошибка отметки списаний как выгруженных: %v", err)
				}
				log.Printf("[iiko-export] ✅ Пакет списания из %d позиций успешно выгружен в iiko", len(iikoItems))
			}
		}
	}

	// 2. ВЫГРУЗКА ПЕРЕМЕЩЕНИЙ ТОВАРОВ (JSON v2)
	transfers, err := s.repo.GetGroupedTransfers(companyID)
	if err != nil {
		log.Printf("[iiko-export] ❌ Ошибка выборки перемещений из БД: %v", err)
	} else if len(transfers) > 0 {
		log.Printf("[iiko-export] Найдено групп перемещений: %d", len(transfers))
		routeGroups := make(map[string][]models.ExportOperationDTO)
		for _, t := range transfers {
			route := t.StoreFromID + "|" + t.StoreToID + "|" + t.OpDate
			routeGroups[route] = append(routeGroups[route], t)
		}

		for key, items := range routeGroups {
			parts := strings.Split(key, "|")
			storeFrom := parts[0]
			storeTo := parts[1]
			opDate := parts[2]

			var iikoItems []map[string]interface{}
			var exportedOpIDs []string

			for _, item := range items {
				iikoItems = append(iikoItems, map[string]interface{}{
					"productId": item.ProductID,
					"amount":    item.TotalAmount,
				})
				exportedOpIDs = append(exportedOpIDs, strings.Split(item.OpIDs, ",")...)
			}

			finalComment := s.buildIikoComment(exportedOpIDs, "Перемещение из QA2A", opDate)

			payload := map[string]interface{}{
				"dateIncoming": opDate,
				"status":       "NEW",
				"storeFromId":  storeFrom,
				"storeToId":    storeTo,
				"comment":      finalComment,
				"items":        iikoItems,
			}

			payloadBytes, _ := json.Marshal(payload)
			url := fmt.Sprintf("%s/resto/api/v2/documents/internalTransfer?key=%s", settings.Host, token)
			req, _ := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
			req.Header.Set("Content-Type", "application/json")

			res, err := s.httpClient.Do(req)
			if err != nil {
				return fmt.Errorf("ошибка сети при выгрузке перемещения: %w", err)
			}
			respBody, _ := io.ReadAll(res.Body)
			res.Body.Close()

			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("iiko отклонил перемещение (код %d): %s", res.StatusCode, string(respBody))
			}

			var iikoResult IikoV2Response
			if err := json.Unmarshal(respBody, &iikoResult); err == nil {
				if strings.ToUpper(iikoResult.Result) == "ERROR" {
					return fmt.Errorf("ошибка спецификации перемещения iiko: %s", strings.Join(iikoResult.Errors, "; "))
				}
			}

			if err := s.repo.MarkOperationsExported(companyID, exportedOpIDs); err != nil {
				log.Printf("[iiko-export] ⚠️ Ошибка отметки перемещений: %v", err)
			}
			log.Printf("[iiko-export] ✅ Перемещение из %d позиций успешно выгружено", len(iikoItems))
		}
	}

	// 3. ВЫГРУЗКА АКТОВ ПРИГОТОВЛЕНИЯ ПФ (XML v1)
	assemblies, err := s.repo.GetGroupedAssemblies(companyID)
	if err != nil {
		log.Printf("[iiko-export] ❌ Ошибка выборки приготовлений ПФ: %v", err)
	} else if len(assemblies) > 0 {
		log.Printf("[iiko-export] Найдено групп приготовления: %d", len(assemblies))
		routeGroups := make(map[string][]models.ExportOperationDTO)
		for _, a := range assemblies {
			route := a.StoreFromID + "|" + a.StoreToID + "|" + a.OpDate
			routeGroups[route] = append(routeGroups[route], a)
		}

		for key, items := range routeGroups {
			parts := strings.Split(key, "|")
			storeFrom := parts[0]
			storeTo := parts[1]
			opDate := parts[2]

			var xmlItems strings.Builder
			var exportedOpIDs []string

			for idx, item := range items {
				xmlItems.WriteString(fmt.Sprintf(`
					<item>
						<num>%d</num>
						<product>%s</product>
						<amount>%.3f</amount>
					</item>`, idx+1, item.ProductID, item.TotalAmount))
				exportedOpIDs = append(exportedOpIDs, strings.Split(item.OpIDs, ",")...)
			}

			formattedDate := time.Now().Format("02.01.2006 15:04")
			if docDate, parseErr := time.Parse("2006-01-02T15:04", opDate); parseErr == nil {
				formattedDate = docDate.Format("02.01.2006 15:04")
			}

			finalComment := s.buildIikoComment(exportedOpIDs, "Акт приготовления из QA2A", opDate)

			xmlPayload := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
			<document>
				<storeFrom>%s</storeFrom>
				<storeTo>%s</storeTo>
				<dateIncoming>%s</dateIncoming>
				<comment>%s</comment>
				<status>NEW</status> 
				<items>%s
				</items>
			</document>`, storeFrom, storeTo, formattedDate, finalComment, xmlItems.String())

			url := fmt.Sprintf("%s/resto/api/documents/import/productionDocument?key=%s", settings.Host, token)
			req, _ := http.NewRequest("POST", url, bytes.NewBufferString(xmlPayload))
			req.Header.Set("Content-Type", "application/xml")

			res, err := s.httpClient.Do(req)
			if err != nil {
				return fmt.Errorf("ошибка сети при выгрузке акта приготовления: %w", err)
			}
			respBody, _ := io.ReadAll(res.Body)
			res.Body.Close()

			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("iiko отклонил XML акт приготовления (код %d): %s", res.StatusCode, string(respBody))
			}

			respStr := string(respBody)
			if !strings.Contains(respStr, "<valid>true</valid>") {
				return fmt.Errorf("ошибка валидации акта приготовления в iiko: %s", respStr)
			}

			if err := s.repo.MarkOperationsExported(companyID, exportedOpIDs); err != nil {
				log.Printf("[iiko-export] ⚠️ Ошибка отметки актов приготовления: %v", err)
			}
			log.Printf("[iiko-export] ✅ XML Акт приготовления полуфабрикатов успешно выгружен")
		}
	}

	log.Printf("[iiko-export] 🎉 Выгрузка заведения #%d успешно завершена", companyID)
	return nil
}

// ============================================================================
// ПОЛУЧЕНИЕ СЧЕТОВ И ПОСТАВЩИКОВ ИЗ IIKO RMS
// ============================================================================

// FetchIikoAccounts загружает доступные расходные статьи учета (EXPENSES, COST_OF_GOODS_SOLD).
func (s *IikoService) FetchIikoAccounts(companyID int) ([]models.WriteoffAccount, error) {
	settings, err := s.getDecryptedSettings(companyID)
	if err != nil {
		return nil, err
	}

	token, err := s.Auth(settings.Host, settings.Login, settings.Password)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/resto/api/v2/entities/accounts/list?key=%s&includeDeleted=false", settings.Host, token)
	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iiko вернул ошибку при запросе счетов (код %d)", resp.StatusCode)
	}

	var accountsData []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Deleted bool   `json:"deleted"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&accountsData); err != nil {
		return nil, fmt.Errorf("ошибка парсинга счетов: %w", err)
	}

	var result []models.WriteoffAccount
	for _, acc := range accountsData {
		if !acc.Deleted && (acc.Type == "EXPENSES" || acc.Type == "OTHER_EXPENSES" || acc.Type == "COST_OF_GOODS_SOLD") {
			result = append(result, models.WriteoffAccount{
				ExternalID: acc.ID,
				Name:       acc.Name,
			})
		}
	}

	if len(result) == 0 {
		result = append(result, models.WriteoffAccount{
			ExternalID: "97036ddb-b2e1-cd47-1669-c145daa9f9c5",
			Name:       "Расход продуктов (Стандарт)",
		})
	}

	return result, nil
}

// FetchIikoSuppliers загружает активных контрагентов-поставщиков из базы iiko.
func (s *IikoService) FetchIikoSuppliers(companyID int) ([]models.WriteoffAccount, error) {
	settings, err := s.getDecryptedSettings(companyID)
	if err != nil {
		return nil, err
	}

	token, err := s.Auth(settings.Host, settings.Login, settings.Password)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/resto/api/suppliers?key=%s", settings.Host, token)
	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iiko вернул статус %d при запросе поставщиков", resp.StatusCode)
	}

	var data struct {
		XMLName xml.Name `xml:"employees"`
		List    []struct {
			ID       string `xml:"id"`
			Name     string `xml:"name"`
			Supplier bool   `xml:"supplier"`
			Deleted  bool   `xml:"deleted"`
		} `xml:"employee"`
	}

	if err := xml.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("ошибка парсинга XML поставщиков: %w", err)
	}

	var results []models.WriteoffAccount
	for _, emp := range data.List {
		if emp.Supplier && !emp.Deleted {
			results = append(results, models.WriteoffAccount{
				ExternalID: emp.ID,
				Name:       emp.Name,
			})
		}
	}

	return results, nil
}

