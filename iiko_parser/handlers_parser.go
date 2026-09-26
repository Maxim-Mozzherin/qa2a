package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"iiko_parser/crypto"
	"iiko_parser/pkg/netutil"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type IikoIncomingInvoiceXML struct {
	XMLName                xml.Name                     `xml:"document"`
	IncomingDate           string                       `xml:"incomingDate"`
	Supplier               string                       `xml:"supplier"`
	DefaultStore           string                       `xml:"defaultStore"`
	DateIncoming           string                       `xml:"dateIncoming"`
	UseDefaultDocumentTime bool                         `xml:"useDefaultDocumentTime"`
	IncomingDocumentNumber string                       `xml:"incomingDocumentNumber"`
	Status                 string                       `xml:"status"`
	Items                  []IikoIncomingInvoiceItemXML `xml:"items>item"`
}

type IikoIncomingInvoiceItemXML struct {
	Amount       string `xml:"amount"`
	Product      string `xml:"product"`
	Num          int    `xml:"num"`
	Sum          string `xml:"sum"`
	VatPercent   string `xml:"vatPercent"`
	VatSum       string `xml:"vatSum"`
	Price        string `xml:"price"`
	Store        string `xml:"store"`
	ActualAmount string `xml:"actualAmount"`
}

// normalizeVendorName очищает наименование поставщика от организационно-правовых форм (ООО, ИП и др.),
// кавычек всех типов, знаков препинания и лишних пробелов для устойчивого сопоставления.
func normalizeVendorName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}

	s = strings.ReplaceAll(s, "ё", "е")

	quotes := []string{"\"", "«", "»", "'", "`", "“", "”", "„"}
	for _, q := range quotes {
		s = strings.ReplaceAll(s, q, " ")
	}

	if idx := strings.Index(s, "инн"); idx != -1 {
		s = s[:idx]
	}

	legalForms := []string{
		"общество с ограниченной ответственностью",
		"индивидуальный предприниматель",
		"акционерное общество",
		"публичное акционерное общество",
		"закрытое акционерное общество",
		"товарищество с ограниченной ответственностью",
		"ооо",
		"ип",
		"ао",
		"пао",
		"зао",
		"тоо",
	}
	for _, lf := range legalForms {
		s = strings.ReplaceAll(s, " "+lf+" ", " ")
		if strings.HasPrefix(s, lf+" ") {
			s = strings.TrimPrefix(s, lf+" ")
		}
		if strings.HasSuffix(s, " "+lf) {
			s = strings.TrimSuffix(s, " "+lf)
		}
	}

	punct := []string{",", ".", ";", ":", "!", "?", "/", "\\", "(", ")", "[", "]", "-"}
	for _, p := range punct {
		s = strings.ReplaceAll(s, p, " ")
	}

	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// normalizeItemName нормализует наименование товара: переводит в нижний регистр,
// убирает кавычки, неразрывные пробелы, спецсимволы и схлопывает множественные пробелы.
func normalizeItemName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "ё", "е")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")

	quotes := []string{"\"", "«", "»", "'", "`", "“", "”"}
	for _, q := range quotes {
		s = strings.ReplaceAll(s, q, " ")
	}

	punct := []string{"-", "/", "\\", ",", ".", ";", ":", "!", "?", "(", ")", "[", "]", "{", "}"}
	for _, p := range punct {
		s = strings.ReplaceAll(s, p, " ")
	}

	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

func handleParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		http.Error(w, "Файл слишком большой (максимум 100 MB)", http.StatusBadRequest)
		return
	}

	companyIDStr := r.FormValue("company_id")
	if companyIDStr == "" {
		http.Error(w, "Не передан обязательный параметр company_id", http.StatusBadRequest)
		return
	}

	var companyID int
	if _, err := fmt.Sscanf(companyIDStr, "%d", &companyID); err != nil || companyID <= 0 {
		http.Error(w, "Некорректный ID заведения", http.StatusBadRequest)
		return
	}

	if !checkAccountantAccessUser(GetAuthUser(r), companyID) {
		http.Error(w, "Доступ к данному заведению запрещен", http.StatusForbidden)
		return
	}

	files := r.MultipartForm.File["pdf"]
	if len(files) == 0 {
		http.Error(w, "Files not found", http.StatusBadRequest)
		return
	}

	isStreaming := r.FormValue("stream") == "true" || strings.Contains(r.Header.Get("Accept"), "application/x-ndjson")
	var flusher http.Flusher
	if isStreaming {
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		if f, ok := w.(http.Flusher); ok {
			flusher = f
		}
	}

	sendProgress := func(icon string, msg string, percent int) {
		if !isStreaming {
			return
		}
		event := map[string]interface{}{
			"type":     "log",
			"time":     time.Now().Format("15:04:05"),
			"icon":     icon,
			"message":  msg,
			"progress": percent,
		}
		b, _ := json.Marshal(event)
		w.Write(append(b, '\n'))
		if flusher != nil {
			flusher.Flush()
		}
	}

	sendProgress("📥", "Файлы приняты сервером, начало обработки...", 5)

	var textBytes []byte
	var imagesBase64 []string

	ctxCmd, cancelCmd := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancelCmd()

	for _, header := range files {
		err := func() error {
			file, err := header.Open()
			if err != nil {
				return err
			}
			defer file.Close()

			cleanFileName := filepath.Base(header.Filename)
			// Remove glob meta-characters to prevent filepath.Glob panics or misses
			cleanFileName = strings.Map(func(r rune) rune {
				if strings.ContainsRune("[]*?\"\\/", r) {
					return -1
				}
				return r
			}, cleanFileName)
			if cleanFileName == "" {
				cleanFileName = "document.pdf"
			}
			// Create isolated temporary directory for this upload
			tmpDir, err := os.MkdirTemp("", "pdf_parse_*")
			if err != nil {
				return fmt.Errorf("ошибка создания временной директории: %w", err)
			}
			defer os.RemoveAll(tmpDir)

			filePath := filepath.Join(tmpDir, cleanFileName)

			out, err := os.Create(filePath)
			if err != nil {
				return err
			}

			if _, err = io.Copy(out, file); err != nil {
				out.Close()
				return err
			}
			_ = out.Close()

			ext := strings.ToLower(filepath.Ext(cleanFileName))
			allowedExts := map[string]bool{
				".pdf": true, ".png": true, ".jpg": true, ".jpeg": true,
				".webp": true, ".txt": true, ".csv": true, ".xml": true,
			}
			if !allowedExts[ext] {
				return fmt.Errorf("недопустимый тип файла %s: разрешены только PDF, изображения и текстовые накладные", ext)
			}

			if ext == ".pdf" {
				txtPath := filepath.Join(tmpDir, "extracted.txt")
				cmdTxt := exec.CommandContext(ctxCmd, "pdftotext", "-layout", filePath, txtPath)
				_ = cmdTxt.Run()
				tb, _ := os.ReadFile(txtPath)
				textBytes = append(textBytes, tb...)
				textBytes = append(textBytes, []byte("\n\n")...)

				sendProgress("⚙️", "Рендеринг PDF страниц (pdftoppm, 2048px Crisp)...", 15)

				imgPrefix := filepath.Join(tmpDir, "img")
				cmdImg := exec.CommandContext(ctxCmd, "pdftoppm", "-jpeg", "-jpegopt", "quality=80", "-scale-to-x", "1600", "-scale-to-y", "-1", "-f", "1", "-l", "30", filePath, imgPrefix)
				if err := cmdImg.Run(); err != nil {
					log.Printf("pdftoppm error: %v", err)
				}

				matches, _ := filepath.Glob(imgPrefix + "-*.jpg")
				sort.Slice(matches, func(i, j int) bool {
					// Extract trailing page numbers before .jpg
					getPageNum := func(path string) int {
						base := filepath.Base(path)
						numPart := strings.TrimSuffix(base, ".jpg")
						idx := strings.LastIndex(numPart, "-")
						if idx != -1 {
							if n, err := strconv.Atoi(numPart[idx+1:]); err == nil {
								return n
							}
						}
						return 0
					}
					return getPageNum(matches[i]) < getPageNum(matches[j])
				})
				for _, m := range matches {
					imgBytes, err := os.ReadFile(m)
					if err == nil {
						imagesBase64 = append(imagesBase64, base64.StdEncoding.EncodeToString(imgBytes))
					}
				}
			} else if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" {
				imgBytes, err := os.ReadFile(filePath)
				if err == nil {
					imagesBase64 = append(imagesBase64, base64.StdEncoding.EncodeToString(imgBytes))
				}
			} else {
				tb, _ := os.ReadFile(filePath)
				textBytes = append(textBytes, tb...)
				textBytes = append(textBytes, []byte("\n\n")...)
			}
			return nil
		}()
		if err != nil {
			log.Printf("Error processing file %s: %v", header.Filename, err)
		}
	}

	if len(textBytes) == 0 && len(imagesBase64) == 0 {
		if isStreaming {
			sendProgress("❌", "Не удалось извлечь текст или изображения из файлов", 0)
			return
		}
		http.Error(w, "Не удалось извлечь текст или изображения из переданных файлов", http.StatusBadRequest)
		return
	}

	sendProgress("🖼️", fmt.Sprintf("Подготовлено %d страниц(ы) в высоком разрешении (2048px)", len(imagesBase64)), 25)

	customPrompt := strings.TrimSpace(r.FormValue("prompt"))
	aiData, err := parseWithClaude(string(textBytes), imagesBase64, customPrompt, sendProgress)
	if err != nil {
		if isStreaming {
			sendProgress("❌", "Сбой распознавания AI: "+err.Error(), 0)
			errPayload := map[string]interface{}{
				"type":  "error",
				"error": err.Error(),
			}
			b, _ := json.Marshal(errPayload)
			w.Write(append(b, '\n'))
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		http.Error(w, "Сбой распознавания AI: "+err.Error(), http.StatusInternalServerError)
		return
	}

	sendProgress("🔍", "Сопоставление товаров с номенклатурой iiko...", 95)

	mappedSupplierUUID := ""
	cleanVendorINN := strings.TrimSpace(aiData.VendorINN)
	normVendor := normalizeVendorName(aiData.VendorName)
	suppRows, err := db.Query("SELECT vendor_name, iiko_supplier_uuid, COALESCE(inn, '') FROM supplier_mappings WHERE company_id = $1", companyID)
	if err == nil {
		defer suppRows.Close()
		type supMap struct {
			vName    string
			suppUUID string
			inn      string
		}
		var list []supMap
		for suppRows.Next() {
			var sm supMap
			if err := suppRows.Scan(&sm.vName, &sm.suppUUID, &sm.inn); err == nil {
				list = append(list, sm)
			}
		}

		var matchedSM *supMap
		// 1. Приоритетное сопоставление по ИНН (если ИНН извлечен из накладной)
		if cleanVendorINN != "" {
			for _, sm := range list {
				if sm.inn != "" && sm.inn == cleanVendorINN {
					matchedSM = &sm
					break
				}
			}
		}

		// 2. Если по ИНН не найден — сопоставление по названию
		if matchedSM == nil {
			for _, sm := range list {
				if sm.vName == aiData.VendorName {
					matchedSM = &sm
					break
				}
				if normVendor != "" && normalizeVendorName(sm.vName) == normVendor {
					matchedSM = &sm
					break
				}
			}
		}

		if matchedSM != nil {
			mappedSupplierUUID = matchedSM.suppUUID
			// Приводим имя поставщика к единому каноническому виду из базы данных
			aiData.VendorName = matchedSM.vName
			if matchedSM.inn != "" && aiData.VendorINN == "" {
				aiData.VendorINN = matchedSM.inn
			}
		}
	}

	mappedStoreUUID := ""
	if aiData.Consignee != "" {
		_ = db.QueryRow("SELECT iiko_store_uuid FROM store_mappings WHERE company_id = $1 AND consignee = $2", companyID, aiData.Consignee).
			Scan(&mappedStoreUUID)
		if mappedStoreUUID == "" {
			_ = db.QueryRow("SELECT iiko_store_uuid FROM store_mappings WHERE company_id = $1 AND ($2 ILIKE '%' || consignee || '%' OR consignee ILIKE '%' || $2 || '%') LIMIT 1", companyID, strings.TrimSpace(aiData.Consignee)).
				Scan(&mappedStoreUUID)
		}
	}

	// 3. Загрузка маппингов товаров для активного заведения
	type candidateMapping struct {
		VendorName   string
		VendorItem   string
		InternalUUID string
		InternalName string
		Multiplier   float64
	}

	var allCompanyMappings []candidateMapping
	mapRows, err := db.Query("SELECT vendor_name, vendor_item_name, iiko_product_uuid, iiko_product_name, multiplier FROM product_mappings WHERE company_id = $1", companyID)
	if err == nil {
		defer mapRows.Close()
		for mapRows.Next() {
			var cm candidateMapping
			if err := mapRows.Scan(&cm.VendorName, &cm.VendorItem, &cm.InternalUUID, &cm.InternalName, &cm.Multiplier); err == nil {
				allCompanyMappings = append(allCompanyMappings, cm)
			}
		}
	}

	// Фильтруем маппинги текущего поставщика (прямое совпадение или через нормализацию ООО/кавычек)
	var vendorCandidates []candidateMapping
	for _, cm := range allCompanyMappings {
		if cm.VendorName == aiData.VendorName || (normVendor != "" && normalizeVendorName(cm.VendorName) == normVendor) {
			vendorCandidates = append(vendorCandidates, cm)
		}
	}

	type EnrichedItem struct {
		AiItem
		MappedUUID        string  `json:"mapped_uuid"`
		MappedName        string  `json:"mapped_name"`
		Multiplier        float64 `json:"multiplier"`
		IsAiGuessed       bool    `json:"is_ai_guessed"`
		IsWeightChanged   bool    `json:"is_weight_changed"`
		HistoryMultiplier float64 `json:"history_multiplier"`
	}

	var resultItems []EnrichedItem
	for _, item := range aiData.Items {
		enriched := EnrichedItem{
			AiItem:     item,
			Multiplier: 1.0,
		}

		itemNorm := normalizeItemName(item.Name)
		var matched *candidateMapping

		for _, cm := range vendorCandidates {
			if cm.VendorItem == item.Name {
				matched = &cm
				break
			}
		}

		if matched == nil && itemNorm != "" {
			for _, cm := range vendorCandidates {
				if normalizeItemName(cm.VendorItem) == itemNorm {
					matched = &cm
					break
				}
			}
		}

		if matched == nil && len(itemNorm) >= 4 {
			for _, cm := range vendorCandidates {
				cNorm := normalizeItemName(cm.VendorItem)
				if len(cNorm) >= 4 && (strings.HasPrefix(itemNorm, cNorm) || strings.HasPrefix(cNorm, itemNorm)) {
					matched = &cm
					break
				}
			}
		}

		if matched == nil {
			for _, cm := range allCompanyMappings {
				if cm.VendorItem == item.Name || (itemNorm != "" && normalizeItemName(cm.VendorItem) == itemNorm) {
					matched = &cm
					break
				}
			}
		}

		if matched != nil {
			enriched.MappedUUID = matched.InternalUUID
			enriched.MappedName = matched.InternalName
			enriched.Multiplier = matched.Multiplier
			enriched.IsAiGuessed = false

			if item.AiMultiplier > 0 && item.AiMultiplier != 1.0 && item.AiMultiplier != matched.Multiplier {
				enriched.IsWeightChanged = true
				enriched.HistoryMultiplier = matched.Multiplier
				enriched.Multiplier = item.AiMultiplier
			}
		} else {
			if item.AiMultiplier > 0 {
				enriched.Multiplier = item.AiMultiplier
				if item.AiMultiplier != 1.0 {
					enriched.IsAiGuessed = true
				}
			}
		}
		resultItems = append(resultItems, enriched)
	}

	respPayload := map[string]interface{}{
		"vendor_name":          aiData.VendorName,
		"vendor_inn":           aiData.VendorINN,
		"doc_number":           aiData.DocNumber,
		"doc_date":             aiData.DocDate,
		"consignee":            aiData.Consignee,
		"consignee_inn":        aiData.ConsigneeINN,
		"shipper":              aiData.Shipper,
		"mapped_supplier_uuid": mappedSupplierUUID,
		"mapped_store_uuid":    mappedStoreUUID,
		"used_model":           aiData.UsedModel,
		"items":                resultItems,
	}

	if isStreaming {
		sendProgress("🚀", "Парсинг успешно завершен!", 100)
		finalEvent := map[string]interface{}{
			"type":     "result",
			"progress": 100,
			"data":     respPayload,
		}
		b, _ := json.Marshal(finalEvent)
		w.Write(append(b, '\n'))
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(respPayload)
}

func handleParserHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"models": GetModelsHealthStatus(),
	})
}

func handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CompanyID     int     `json:"company_id"`
		StoreUUID     string  `json:"store_uuid"`
		SupplierUUID  string  `json:"supplier_uuid"`
		VendorName    string  `json:"vendor_name"`
		VendorINN     string  `json:"vendor_inn"`
		Consignee     string  `json:"consignee"`
		Shipper       string  `json:"shipper"`
		InvoiceNumber string  `json:"invoice_number"`
		InvoiceDate   string  `json:"invoice_date"`
		Items         []struct {
			Name          string  `json:"name"`
			CleanCategory string  `json:"clean_category"`
			Brand         string  `json:"brand"`
			Quantity      float64 `json:"quantity"`
			Unit          string  `json:"unit"`
			Price         float64 `json:"price"`
			Sum           float64 `json:"sum"`
			SumWithoutNds float64 `json:"sum_without_nds"`
			NdsPercent    float64 `json:"nds_percent"`
			MappedUUID    string  `json:"mapped_uuid"`
			MappedName    string  `json:"mapped_name"`
			Multiplier    float64 `json:"multiplier"`
		} `json:"items"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	if !checkAccountantAccessUser(GetAuthUser(r), req.CompanyID) {
		http.Error(w, "Доступ к данному заведению запрещен", http.StatusForbidden)
		return
	}

	// 1. Получаем реквизиты подключения к iiko RMS из БД
	var host, login, encryptedPass string
	err := db.QueryRow("SELECT iiko_host, iiko_api_login, iiko_api_password FROM companies WHERE id = $1", req.CompanyID).
		Scan(&host, &login, &encryptedPass)
	if err != nil {
		http.Error(w, "Заведение не найдено в базе данных", http.StatusNotFound)
		return
	}

	cleanHost := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(host), "/"), "/resto")
	if cleanHost == "" || strings.TrimSpace(login) == "" {
		http.Error(w, "В заведении не настроена интеграция с iiko RMS. Настройте подключение в Telegram-боте.", http.StatusBadRequest)
		return
	}

	if err := netutil.ValidateHost(cleanHost); err != nil {
		http.Error(w, "Недопустимый адрес сервера iiko RMS (заблокировано политикой безопасности SSRF): "+err.Error(), http.StatusBadRequest)
		return
	}

	// 2. Расшифровываем пароль и авторизуемся в iiko RMS для получения свежего токена
	password, err := crypto.Decrypt(encryptedPass, encryptionKey)
	if err != nil {
		http.Error(w, "Ошибка дешифрования пароля iiko RMS: "+err.Error(), http.StatusInternalServerError)
		return
	}

	iikoToken, err := authIiko(cleanHost, login, password)
	if err != nil {
		http.Error(w, "Сбой авторизации в iiko RMS: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// 3. Формируем тело XML-документа
	if len(req.Items) == 0 {
		http.Error(w, "В накладной нет позиций для отправки", http.StatusBadRequest)
		return
	}

	var invoiceItems []IikoIncomingInvoiceItemXML

	for i, item := range req.Items {
		if strings.TrimSpace(item.MappedUUID) == "" {
			http.Error(w, fmt.Sprintf("⛔ Отправка запрещена: позиция №%d (%s) не сопоставлена с номенклатурой iiko RMS. Все позиции накладной должны иметь соответствие в iiko.", i+1, item.Name), http.StatusBadRequest)
			return
		}

		finalQuantity := item.Quantity * item.Multiplier
		finalSum := item.Sum
		finalPrice := 0.0
		if finalQuantity > 0 {
			finalPrice = finalSum / finalQuantity
		}

		vatSum := 0.0
		if item.NdsPercent > 0 {
			vatSum = (finalSum * item.NdsPercent) / (100.0 + item.NdsPercent)
		}

		invoiceItems = append(invoiceItems, IikoIncomingInvoiceItemXML{
			Amount:       fmt.Sprintf("%.3f", finalQuantity),
			Product:      item.MappedUUID,
			Num:          i + 1,
			Sum:          fmt.Sprintf("%.2f", finalSum),
			VatPercent:   fmt.Sprintf("%.2f", item.NdsPercent),
			VatSum:       fmt.Sprintf("%.2f", vatSum),
			Price:        fmt.Sprintf("%.4f", finalPrice),
			Store:        req.StoreUUID,
			ActualAmount: fmt.Sprintf("%.3f", finalQuantity),
		})
	}

	docDate := req.InvoiceDate
	if docDate == "" {
		docDate = time.Now().Format("2006-01-02")
	}
	dateIncomingStr := time.Now().Format("02.01.2006")
	if pt, err := time.Parse("2006-01-02", docDate); err == nil {
		dateIncomingStr = pt.Format("02.01.2006")
	}

	doc := IikoIncomingInvoiceXML{
		IncomingDate:           docDate,
		Supplier:               req.SupplierUUID,
		DefaultStore:           req.StoreUUID,
		DateIncoming:           dateIncomingStr,
		UseDefaultDocumentTime: true,
		IncomingDocumentNumber: req.InvoiceNumber,
		Status:                 "NEW",
		Items:                  invoiceItems,
	}

	xmlBytes, err := xml.Marshal(doc)
	if err != nil {
		http.Error(w, "Ошибка сериализации XML накладной: "+err.Error(), http.StatusInternalServerError)
		return
	}
	payload := []byte(xml.Header + string(xmlBytes))

	// 4. СНАЧАЛА ОТПРАВЛЯЕМ В IIKO RMS
	url := fmt.Sprintf("%s/resto/api/documents/import/incomingInvoice?key=%s", cleanHost, iikoToken)
	iikoReq, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		http.Error(w, "Ошибка формирования запроса в iiko: "+err.Error(), http.StatusInternalServerError)
		return
	}
	iikoReq.Header.Set("Content-Type", "application/xml")

	res, err := iikoHTTPClient.Do(iikoReq)
	if err != nil {
		http.Error(w, "Сетевой сбой отправки накладной в iiko: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()

	respBody, _ := io.ReadAll(res.Body)

	respStr := string(respBody)
	if res.StatusCode != http.StatusOK || strings.Contains(respStr, "<valid>false</valid>") || strings.Contains(respStr, "<error>") {
		http.Error(w, fmt.Sprintf("iiko RMS отклонил документ (HTTP %d): %s", res.StatusCode, respStr), http.StatusBadRequest)
		return
	}

	// 5. И ТОЛЬКО ПРИ УСПЕХЕ В IIKO СОХРАНЯЕМ В ЛОКАЛЬНУЮ БД
	parsedDocDate := time.Now()
	if pt, err := time.Parse("2006-01-02", req.InvoiceDate); err == nil {
		parsedDocDate = pt
	}
	dbInvoiceDate := parsedDocDate.Format("2006-01-02")

	var storeName string
	_ = db.QueryRow(`
		SELECT name FROM locations 
		WHERE company_id = $1 AND external_id = $2 LIMIT 1`,
		req.CompanyID, req.StoreUUID).Scan(&storeName)
	if storeName == "" {
		storeName = "Основной склад"
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Ошибка БД при сохранении истории: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Приводим имя поставщика к каноническому виду из supplier_mappings (по UUID iiko или ИНН)
	var canonicalVendorName string
	if req.SupplierUUID != "" {
		_ = tx.QueryRow(`
			SELECT vendor_name FROM supplier_mappings 
			WHERE company_id = $1 AND iiko_supplier_uuid = $2 
			ORDER BY id ASC LIMIT 1`, req.CompanyID, req.SupplierUUID).Scan(&canonicalVendorName)
	}
	if canonicalVendorName == "" && strings.TrimSpace(req.VendorINN) != "" {
		_ = tx.QueryRow(`
			SELECT vendor_name FROM supplier_mappings 
			WHERE company_id = $1 AND inn = $2 
			ORDER BY id ASC LIMIT 1`, req.CompanyID, strings.TrimSpace(req.VendorINN)).Scan(&canonicalVendorName)
	}
	if canonicalVendorName != "" {
		req.VendorName = canonicalVendorName
	}

	for _, item := range req.Items {
		if item.MappedUUID != "" {
			_, err = tx.Exec(`
				INSERT INTO product_mappings (company_id, vendor_name, vendor_item_name, iiko_product_uuid, iiko_product_name, multiplier, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, now())
				ON CONFLICT (company_id, vendor_name, vendor_item_name)
				DO UPDATE SET iiko_product_uuid = EXCLUDED.iiko_product_uuid,
				              iiko_product_name = EXCLUDED.iiko_product_name,
				              multiplier = EXCLUDED.multiplier,
				              updated_at = now()`,
				req.CompanyID, req.VendorName, item.Name, item.MappedUUID, item.MappedName, item.Multiplier)
			if err != nil {
				log.Printf("⚠️ Ошибка сохранения маппинга: %v", err)
			}

			finalQty := item.Quantity * item.Multiplier
			pricePerUnit := 0.0
			if finalQty > 0 {
				pricePerUnit = item.Sum / finalQty
			}

			// Проверка скачка цены (Price Spike Sentinel) перед записью в историю
			var medianPrice sql.NullFloat64
			_ = tx.QueryRow(`
				SELECT PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY price_per_base_unit) 
				FROM purchase_history 
				WHERE company_id = $1 AND iiko_product_uuid = $2 AND invoice_date >= NOW() - INTERVAL '30 days'`,
				req.CompanyID, item.MappedUUID).Scan(&medianPrice)

			if medianPrice.Valid && medianPrice.Float64 > 0 && pricePerUnit > medianPrice.Float64*1.15 && item.Sum > 500 {
				companyID := req.CompanyID
				prodName := item.MappedName
				if prodName == "" {
					prodName = item.Name
				}
				med := medianPrice.Float64
				cur := pricePerUnit
				growth := ((cur - med) / med) * 100.0
				vendor := req.VendorName
				invNum := req.InvoiceNumber

				go func(cID int, pName string, growthPct, mPrice, cPrice float64, vName, iNum, iDate, sName string) {
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()

					var compName string
					_ = db.QueryRowContext(ctx, "SELECT name FROM companies WHERE id = $1", cID).Scan(&compName)
					if compName == "" {
						compName = fmt.Sprintf("ID %d", cID)
					}

					var tgIDs []int64
					rows, err := db.QueryContext(ctx, `
						SELECT u.tg_id 
						FROM memberships m 
						JOIN users u ON m.user_id = u.id 
						WHERE m.company_id = $1 AND m.role IN ('owner', 'admin', 'manager')`, cID)
					if err == nil {
						defer rows.Close()
						for rows.Next() {
							var tid int64
							if err := rows.Scan(&tid); err == nil && tid > 0 {
								tgIDs = append(tgIDs, tid)
							}
						}
					}

					formattedDate := iDate
					if pt, parseErr := time.Parse("2006-01-02", iDate); parseErr == nil {
						formattedDate = pt.Format("02.01.2006")
					}

					msg := fmt.Sprintf("🚨 <b>Скачок закупочной цены!</b>\n" +
						"🏢 Заведение: <b>%s</b>\n" +
						"📍 Склад: <b>%s</b>\n" +
						"📦 Товар: <b>%s</b>\n" +
						"📈 Рост: <b>+%.1f%%</b> (Медиана: %.2f ₽ ➡️ Новая: %.2f ₽)\n" +
						"🚚 Поставщик: <b>%s</b>\n" +
						"📄 Накладная: <b>№%s от %s</b>",
						html.EscapeString(compName),
						html.EscapeString(sName),
						html.EscapeString(pName),
						growthPct,
						mPrice,
						cPrice,
						html.EscapeString(vName),
						html.EscapeString(iNum),
						html.EscapeString(formattedDate),
					)
					for _, tid := range tgIDs {
						sendTelegramNotification(tid, msg)
					}
				}(companyID, prodName, growth, med, cur, vendor, invNum, dbInvoiceDate, storeName)
			}

			unit := item.Unit
			if unit == "" {
				unit = "кг/шт"
			}

			_, err = tx.Exec(`
				INSERT INTO purchase_history (
					company_id, invoice_date, invoice_number, supplier_uuid, supplier_name,
					iiko_product_uuid, iiko_product_name, product_name_in_invoice, clean_category, brand, quantity, unit, multiplier, total_sum, price_per_base_unit,
					consignee, shipper
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
				req.CompanyID, dbInvoiceDate, req.InvoiceNumber, req.SupplierUUID, req.VendorName,
				item.MappedUUID, item.MappedName, item.Name, item.CleanCategory, item.Brand, item.Quantity, unit, item.Multiplier, item.Sum, pricePerUnit,
				req.Consignee, req.Shipper)
			if err != nil {
				log.Printf("⚠️ Ошибка записи purchase_history: %v", err)
			}

			// Retroactive Unit Auto-healing
			cleanUnit := strings.TrimSpace(item.Unit)
			if cleanUnit != "" && cleanUnit != "ед." && cleanUnit != "кг/шт" {
				_, healErr := tx.Exec(`
					UPDATE purchase_history 
					SET unit = $1 
					WHERE company_id = $2 
					  AND iiko_product_uuid = $3 
					  AND unit IN ('ед.', 'кг/шт', '')
				`, cleanUnit, req.CompanyID, item.MappedUUID)
				
				if healErr != nil {
					log.Printf("⚠️ Ошибка ретроспективного обновления единиц измерения для %s: %v", item.MappedUUID, healErr)
				}
			}
		}
	}

	if req.SupplierUUID != "" && req.VendorName != "" {
		cleanINN := strings.TrimSpace(req.VendorINN)
		_, _ = tx.Exec(`
			INSERT INTO supplier_mappings (company_id, vendor_name, iiko_supplier_uuid, inn)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (company_id, vendor_name)
			DO UPDATE SET iiko_supplier_uuid = EXCLUDED.iiko_supplier_uuid,
			              inn = CASE WHEN EXCLUDED.inn <> '' THEN EXCLUDED.inn ELSE supplier_mappings.inn END`,
			req.CompanyID, req.VendorName, req.SupplierUUID, cleanINN)
	}

	if req.Consignee != "" && req.StoreUUID != "" {
		_, _ = tx.Exec(`
			INSERT INTO store_mappings (company_id, consignee, iiko_store_uuid)
			VALUES ($1, $2, $3)
			ON CONFLICT (company_id, consignee)
			DO UPDATE SET iiko_store_uuid = EXCLUDED.iiko_store_uuid`,
			req.CompanyID, req.Consignee, req.StoreUUID)
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Ошибка сохранения истории в локальную БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "success",
		"message":       "Накладная успешно проведена в iiko RMS и сохранена в истории",
		"iiko_response": string(respBody),
	})
}
