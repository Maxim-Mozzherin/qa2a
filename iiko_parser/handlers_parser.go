package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

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

	if err := r.ParseMultipartForm(100 << 20); err != nil {
		http.Error(w, "Too large (max 100 MB per batch)", http.StatusBadRequest)
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

	files := r.MultipartForm.File["pdf"]
	if len(files) == 0 {
		http.Error(w, "Files not found", http.StatusBadRequest)
		return
	}

	var textBytes []byte
	var imagesBase64 []string

	ctxCmd, cancelCmd := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancelCmd()

	for i, header := range files {
		file, err := header.Open()
		if err != nil {
			log.Printf("Error opening file %s: %v", header.Filename, err)
			continue
		}

		cleanFileName := filepath.Base(header.Filename)
		tempBase := fmt.Sprintf("upd_%d_%d_%d_%s", companyID, time.Now().UnixNano(), i, cleanFileName)
		filePath := filepath.Join("temp", tempBase)

		out, err := os.Create(filePath)
		if err != nil {
			file.Close()
			http.Error(w, "Temp file creation error", http.StatusInternalServerError)
			return
		}
		if _, err = io.Copy(out, file); err != nil {
			out.Close()
			file.Close()
			http.Error(w, "Save error", http.StatusInternalServerError)
			return
		}
		out.Close()
		file.Close()

		ext := strings.ToLower(filepath.Ext(cleanFileName))

		if ext == ".pdf" {
			txtPath := filePath + ".txt"
			cmdTxt := exec.CommandContext(ctxCmd, "pdftotext", "-layout", filePath, txtPath)
			_ = cmdTxt.Run()
			tb, _ := os.ReadFile(txtPath)
			textBytes = append(textBytes, tb...)
			textBytes = append(textBytes, []byte("\n\n")...)

			imgPrefix := filePath + "_img"
			cmdImg := exec.CommandContext(ctxCmd, "pdftoppm", "-jpeg", "-f", "1", "-l", "10", filePath, imgPrefix)
			if err := cmdImg.Run(); err != nil {
				log.Printf("pdftoppm error: %v", err)
			}

			matches, _ := filepath.Glob(imgPrefix + "-*.jpg")
			for _, m := range matches {
				imgBytes, err := os.ReadFile(m)
				if err == nil {
					imagesBase64 = append(imagesBase64, base64.StdEncoding.EncodeToString(imgBytes))
				}
				os.Remove(m)
			}
			os.Remove(txtPath)
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

		os.Remove(filePath)
	}

	customPrompt := strings.TrimSpace(r.FormValue("prompt"))
	aiData, err := parseWithClaude(string(textBytes), imagesBase64, customPrompt)
	if err != nil {
		http.Error(w, "Сбой распознавания AI: "+err.Error(), http.StatusInternalServerError)
		return
	}

	mappedSupplierUUID := ""
	normVendor := normalizeVendorName(aiData.VendorName)
	suppRows, err := db.Query("SELECT vendor_name, iiko_supplier_uuid FROM supplier_mappings WHERE company_id = $1", companyID)
	if err == nil {
		defer suppRows.Close()
		for suppRows.Next() {
			var vName, suppUUID string
			if err := suppRows.Scan(&vName, &suppUUID); err == nil {
				if vName == aiData.VendorName {
					mappedSupplierUUID = suppUUID
					break
				}
				if normVendor != "" && normalizeVendorName(vName) == normVendor && mappedSupplierUUID == "" {
					mappedSupplierUUID = suppUUID
				}
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

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"vendor_name":          aiData.VendorName,
		"doc_number":           aiData.DocNumber,
		"doc_date":             aiData.DocDate,
		"consignee":            aiData.Consignee,
		"shipper":              aiData.Shipper,
		"mapped_supplier_uuid": mappedSupplierUUID,
		"mapped_store_uuid":    mappedStoreUUID,
		"items":                resultItems,
	})
}

func handleImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CompanyID     int    `json:"company_id"`
		Token         string `json:"token"`
		StoreUUID     string `json:"store_uuid"`
		SupplierUUID  string `json:"supplier_uuid"`
		VendorName    string `json:"vendor_name"`
		Consignee     string `json:"consignee"`
		Shipper       string `json:"shipper"`
		InvoiceNumber string `json:"invoice_number"`
		InvoiceDate   string `json:"invoice_date"`
		Items         []struct {
			Name          string  `json:"name"`
			CleanCategory string  `json:"clean_category"` // Новые поля из AI
			Brand         string  `json:"brand"`          // Новые поля из AI
			Quantity      float64 `json:"quantity"`
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

	var host string
	err := db.QueryRow("SELECT iiko_host FROM companies WHERE id = $1", req.CompanyID).Scan(&host)
	if err != nil {
		http.Error(w, "Заведение не найдено в базе данных", http.StatusNotFound)
		return
	}

	parsedDocDate := time.Now()
	if req.InvoiceDate != "" {
		if pt, err := time.Parse("2006-01-02", req.InvoiceDate); err == nil {
			parsedDocDate = pt
		}
	}
	dbInvoiceDate := parsedDocDate.Format("2006-01-02")

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Ошибка транзакции базы данных", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

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
				http.Error(w, "Ошибка сохранения маппинга товара: "+err.Error(), http.StatusInternalServerError)
				return
			}

			finalQty := item.Quantity * item.Multiplier
			pricePerUnit := 0.0
			if finalQty > 0 {
				pricePerUnit = item.Sum / finalQty
			}

			_, err = tx.Exec(`
				INSERT INTO purchase_history (
					company_id, invoice_date, invoice_number, supplier_uuid, supplier_name,
					iiko_product_uuid, iiko_product_name, product_name_in_invoice, clean_category, brand, quantity, multiplier, total_sum, price_per_base_unit,
					consignee, shipper
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
				req.CompanyID, dbInvoiceDate, req.InvoiceNumber, req.SupplierUUID, req.VendorName,
				item.MappedUUID, item.MappedName, item.Name, item.CleanCategory, item.Brand, item.Quantity, item.Multiplier, item.Sum, pricePerUnit,
				req.Consignee, req.Shipper)

			if err != nil {
				http.Error(w, "Ошибка записи истории закупок (Аналитика): "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	if req.SupplierUUID != "" && req.VendorName != "" {
		_, err = tx.Exec(`
			INSERT INTO supplier_mappings (company_id, vendor_name, iiko_supplier_uuid)
			VALUES ($1, $2, $3)
			ON CONFLICT (company_id, vendor_name)
			DO UPDATE SET iiko_supplier_uuid = EXCLUDED.iiko_supplier_uuid`,
			req.CompanyID, req.VendorName, req.SupplierUUID)
		if err != nil {
			http.Error(w, "Ошибка сохранения связи поставщика: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if req.Consignee != "" && req.StoreUUID != "" {
		_, err = tx.Exec(`
			INSERT INTO store_mappings (company_id, consignee, iiko_store_uuid)
			VALUES ($1, $2, $3)
			ON CONFLICT (company_id, consignee)
			DO UPDATE SET iiko_store_uuid = EXCLUDED.iiko_store_uuid`,
			req.CompanyID, req.Consignee, req.StoreUUID)
		if err != nil {
			http.Error(w, "Ошибка сохранения связи склада: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err = tx.Commit(); err != nil {
		http.Error(w, "Ошибка фиксации транзакции в БД", http.StatusInternalServerError)
		return
	}

	var itemsXML strings.Builder
	for i, item := range req.Items {
		if item.MappedUUID == "" {
			continue
		}

		finalQuantity := item.Quantity * item.Multiplier
		finalSum := item.Sum
		finalPrice := finalSum / finalQuantity

		vatSum := 0.0
		if item.NdsPercent > 0 {
			vatSum = (finalSum * item.NdsPercent) / (100.0 + item.NdsPercent)
		}

		itemXML := fmt.Sprintf(`
        <item>
            <amount>%.3f</amount>
            <product>%s</product>
            <num>%d</num>
            <sum>%.2f</sum>
            <vatPercent>%.2f</vatPercent>
            <vatSum>%.2f</vatSum>
            <price>%.4f</price>
            <store>%s</store>
            <actualAmount>%.3f</actualAmount>
        </item>`,
			finalQuantity, item.MappedUUID, i+1, finalSum, item.NdsPercent, vatSum, finalPrice, req.StoreUUID, finalQuantity)
		itemsXML.WriteString(itemXML)
	}

	docDate := req.InvoiceDate
	if docDate == "" {
		docDate = time.Now().Format("2006-01-02")
	}
	dateIncomingStr := ""
	parsedTime, err := time.Parse("2006-01-02", docDate)
	if err != nil {
		dateIncomingStr = time.Now().Format("02.01.2006")
		docDate = time.Now().Format("2006-01-02")
	} else {
		dateIncomingStr = parsedTime.Format("02.01.2006")
	}

	xmlPayload := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<document>
    <incomingDate>%s</incomingDate>
    <supplier>%s</supplier>
    <defaultStore>%s</defaultStore>
    <dateIncoming>%s</dateIncoming>
    <useDefaultDocumentTime>true</useDefaultDocumentTime>
    <incomingDocumentNumber>%s</incomingDocumentNumber>
    <status>NEW</status>
    <items>%s
    </items>
</document>`,
		docDate, req.SupplierUUID, req.StoreUUID, dateIncomingStr, req.InvoiceNumber, itemsXML.String())

	url := fmt.Sprintf("%s/resto/api/documents/import/incomingInvoice?key=%s", host, req.Token)
	iikoReq, err := http.NewRequest("POST", url, bytes.NewBufferString(xmlPayload))
	if err != nil {
		http.Error(w, "Ошибка создания HTTP-запроса", http.StatusInternalServerError)
		return
	}
	iikoReq.Header.Set("Content-Type", "application/xml")

	res, err := iikoHTTPClient.Do(iikoReq)
	if err != nil {
		http.Error(w, "Ошибка отправки накладной в iiko: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()

	respBody, _ := io.ReadAll(res.Body)

	if res.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("iiko RMS отклонил документ (HTTP %d): %s", res.StatusCode, string(respBody)), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "success",
		"message":       "Накладная успешно проведена в iiko RMS",
		"iiko_response": string(respBody),
	})
}
