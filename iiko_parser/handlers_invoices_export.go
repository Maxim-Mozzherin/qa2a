package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"iiko_parser/crypto"
	"iiko_parser/pkg/netutil"
)

// handleIikoInvoicesExport выполняет прямую пакетную выгрузку накладных из iiko RMS за интервал дат.
// Маршрут: GET /api/iiko/invoices/export?company_id=13&from=2026-08-01&to=2026-09-30&sync_db=true
func handleIikoInvoicesExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается (ожидается GET)", http.StatusMethodNotAllowed)
		return
	}

	companyIDStr := r.URL.Query().Get("company_id")
	if companyIDStr == "" {
		http.Error(w, "Отсутствует обязательный параметр company_id", http.StatusBadRequest)
		return
	}

	var companyID int
	if _, err := fmt.Sscanf(companyIDStr, "%d", &companyID); err != nil || companyID <= 0 {
		http.Error(w, "Некорректный ID заведения (company_id)", http.StatusBadRequest)
		return
	}

	// Проверка прав доступа бухгалтера/пользователя
	user := GetAuthUser(r)
	if !checkAccountantAccessUser(user, companyID) {
		http.Error(w, "Доступ к накладным данного заведения запрещен", http.StatusForbidden)
		return
	}

	// Обработка параметров дат
	now := time.Now()
	to := r.URL.Query().Get("to")
	if to == "" {
		to = now.Format("2006-01-02")
	}

	from := r.URL.Query().Get("from")
	if from == "" {
		from = now.AddDate(0, 0, -30).Format("2006-01-02")
	}

	// Валидация формата дат YYYY-MM-DD
	fromDate, errFrom := time.Parse("2006-01-02", from)
	toDate, errTo := time.Parse("2006-01-02", to)
	if errFrom != nil || errTo != nil {
		http.Error(w, "Неверный формат дат (ожидается YYYY-MM-DD, например 2026-09-01)", http.StatusBadRequest)
		return
	}
	if fromDate.After(toDate) {
		http.Error(w, "Начальная дата (from) не может быть позже конечной (to)", http.StatusBadRequest)
		return
	}

	supplierID := strings.TrimSpace(r.URL.Query().Get("supplier_id"))
	syncDB := r.URL.Query().Get("sync_db") == "true" || r.URL.Query().Get("sync_db") == "1"

	// 1. Получение реквизитов подключения к iiko RMS из БД
	var host, login, encPass string
	err := db.QueryRow("SELECT iiko_host, iiko_api_login, iiko_api_password FROM companies WHERE id = $1", companyID).
		Scan(&host, &login, &encPass)
	if err != nil {
		http.Error(w, "Заведение не найдено в базе данных", http.StatusNotFound)
		return
	}

	cleanHost := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(host), "/"), "/resto")
	if cleanHost == "" || strings.TrimSpace(login) == "" {
		http.Error(w, "В заведении не настроено подключение к iiko RMS", http.StatusBadRequest)
		return
	}

	if err := netutil.ValidateHost(cleanHost); err != nil {
		http.Error(w, "Недопустимый адрес сервера iiko RMS (SSRF): "+err.Error(), http.StatusBadRequest)
		return
	}

	// 2. Расшифровываем пароль и получаем токен сессии iiko
	plainPass, err := crypto.Decrypt(encPass, encryptionKey)
	if err != nil {
		http.Error(w, "Ошибка расшифрования пароля iiko RMS: "+err.Error(), http.StatusInternalServerError)
		return
	}

	iikoToken, err := authIiko(cleanHost, login, plainPass)
	if err != nil {
		http.Error(w, "Ошибка авторизации в iiko RMS: "+err.Error(), http.StatusBadGateway)
		return
	}

	// 3. Параллельная выгрузка накладных и справочников (поставщики, склады, товары) для высокой скорости
	var (
		wg             sync.WaitGroup
		exportedDocXML *IikoExportedInvoicesXML
		suppliersList  []IikoSupplier
		storesList     []IikoStore
		catalogList    []IikoProduct
		errExport      error
	)

	wg.Add(4)

	// Накладные
	go func() {
		defer wg.Done()
		exportedDocXML, errExport = fetchIikoIncomingInvoices(cleanHost, iikoToken, from, to, supplierID)
	}()

	// Справочник поставщиков
	go func() {
		defer wg.Done()
		if s, err := fetchIikoSuppliers(cleanHost, iikoToken); err == nil {
			suppliersList = s
		}
	}()

	// Справочник складов
	go func() {
		defer wg.Done()
		if st, err := fetchIikoStores(cleanHost, iikoToken); err == nil {
			storesList = st
		}
	}()

	// Справочник номенклатуры
	go func() {
		defer wg.Done()
		if c, err := fetchIikoCatalog(cleanHost, iikoToken); err == nil {
			catalogList = c
		}
	}()

	wg.Wait()

	if errExport != nil {
		log.Printf("❌ [IikoExport] Ошибка выгрузки накладных: %v", errExport)
		http.Error(w, "Ошибка выгрузки приходных накладных из iiko RMS: "+errExport.Error(), http.StatusBadGateway)
		return
	}

	// Создаем быстрые in-memory кэш-таблицы справочников
	supplierMap := make(map[string]string, len(suppliersList))
	for _, s := range suppliersList {
		supplierMap[s.UUID] = s.Name
	}

	storeMap := make(map[string]string, len(storesList))
	for _, st := range storesList {
		storeMap[st.UUID] = st.Name
	}

	productMap := make(map[string]string, len(catalogList))
	for _, p := range catalogList {
		productMap[p.UUID] = p.Name
	}

	// 4. Обогащение выгруженных накладных названиями и подсчет сумм
	var totalInvoicesSum float64
	var totalItemsCount int

	docs := exportedDocXML.Documents
	for i := range docs {
		doc := &docs[i]
		if name, ok := supplierMap[doc.SupplierUUID]; ok {
			doc.SupplierName = name
		} else {
			doc.SupplierName = "Неизвестный поставщик"
		}

		if name, ok := storeMap[doc.DefaultStoreUUID]; ok {
			doc.DefaultStoreName = name
		}

		var docSum float64
		var docVatSum float64

		for j := range doc.Items {
			item := &doc.Items[j]
			if pName, ok := productMap[item.ProductUUID]; ok {
				item.ProductName = pName
			} else {
				item.ProductName = item.ProductArticle
			}

			if sName, ok := storeMap[item.StoreUUID]; ok {
				item.StoreName = sName
			} else {
				item.StoreName = doc.DefaultStoreName
			}

			docSum += item.Sum
			docVatSum += item.VatSum
			totalItemsCount++
		}

		doc.TotalSum = docSum
		doc.TotalVatSum = docVatSum
		totalInvoicesSum += docSum
	}

	// 5. Опциональная синхронизация с purchase_history
	var syncedCount int
	if syncDB && len(docs) > 0 {
		// Собираем уже существующие записи в purchase_history для предотвращения дублирования
		existingSet := make(map[string]bool)
		rows, err := db.Query(`
			SELECT invoice_number, TO_CHAR(invoice_date, 'YYYY-MM-DD'), iiko_product_uuid
			FROM purchase_history
			WHERE company_id = $1 AND invoice_date >= $2 AND invoice_date <= $3`,
			companyID, from, to)
		if err == nil {
			for rows.Next() {
				var invNum, invDate, prodUUID string
				if err := rows.Scan(&invNum, &invDate, &prodUUID); err == nil {
					key := fmt.Sprintf("%s#%s#%s", invNum, invDate, prodUUID)
					existingSet[key] = true
				}
			}
			rows.Close()
		}

		// Выполняем пакетную вставку новых строк
		tx, err := db.Begin()
		if err == nil {
			stmt, errStmt := tx.Prepare(`
				INSERT INTO purchase_history (
					company_id, invoice_date, invoice_number, supplier_uuid, supplier_name,
					iiko_product_uuid, iiko_product_name, product_name_in_invoice,
					quantity, unit, multiplier, total_sum, price_per_base_unit, created_at
				) VALUES (
					$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW()
				)`)
			if errStmt == nil {
				for _, doc := range docs {
					invDate := doc.IncomingDate
					if len(invDate) > 10 {
						invDate = invDate[:10]
					}
					if invDate == "" {
						invDate = doc.DateIncoming
						if len(invDate) > 10 {
							invDate = invDate[:10]
						}
					}

					invNum := doc.IncomingDocumentNumber
					if invNum == "" {
						invNum = doc.DocumentNumber
					}

					for _, item := range doc.Items {
						key := fmt.Sprintf("%s#%s#%s", invNum, invDate, item.ProductUUID)
						if existingSet[key] {
							continue // уже есть в базе
						}

						qty := item.ActualAmount
						if qty == 0 {
							qty = item.Amount
						}

						price := item.Price
						if price == 0 && qty > 0 {
							price = item.Sum / qty
						}

						_, errIns := stmt.Exec(
							companyID,
							invDate,
							invNum,
							doc.SupplierUUID,
							doc.SupplierName,
							item.ProductUUID,
							item.ProductName,
							item.ProductName,
							qty,
							item.Code, // код/базовая единица
							1.0,       // multiplier для данных из iiko
							item.Sum,
							price,
						)
						if errIns == nil {
							existingSet[key] = true
							syncedCount++
						}
					}
				}
				stmt.Close()
			}
			_ = tx.Commit()
		}
		log.Printf("📥 [IikoExport] Синхронизировано %d новых товарных позиций в purchase_history (company_id=%d)", syncedCount, companyID)
	}

	resp := IikoInvoicesExportResponse{
		Status:        "ok",
		TotalInvoices: len(docs),
		TotalItems:    totalItemsCount,
		TotalSum:      totalInvoicesSum,
		FromDate:      from,
		ToDate:        to,
		SyncedToDB:    syncedCount,
		Documents:     docs,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}
