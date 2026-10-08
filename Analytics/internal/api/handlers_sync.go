package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"analytics_service/internal/crypto"
	"analytics_service/internal/engine"
	"analytics_service/internal/iiko"
	"analytics_service/internal/queue"
)

// HandleSync запускает инжест накладных из iiko RMS за интервал дат
func (s *Server) HandleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Ожидается POST", http.StatusMethodNotAllowed)
		return
	}

	restIDStr := r.URL.Query().Get("restaurant_id")
	if restIDStr == "" {
		http.Error(w, "Параметр restaurant_id обязателен", http.StatusBadRequest)
		return
	}
	restID, _ := strconv.Atoi(restIDStr)

	from := r.URL.Query().Get("from")
	if from == "" {
		days := 60
		if daysStr := r.URL.Query().Get("days"); daysStr != "" {
			if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
				days = d
			}
		}
		from = time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	}
	to := r.URL.Query().Get("to")
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}

	importedInvoices, importedItems, err := s.SyncRestaurant(restID, from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "ok",
		"imported_invoices": importedInvoices,
		"imported_items":    importedItems,
	})
}

// SyncRestaurant выполняет выгрузку накладных заведения за интервал дат через iiko API
func (s *Server) SyncRestaurant(restID int, from, to string) (int, int, error) {
	var host, login, encPass string
	var companyID sql.NullInt64
	err := s.db.QueryRow(`
		SELECT iiko_host, iiko_login, iiko_password_enc, company_id 
		FROM analytics_restaurants 
		WHERE id = $1`, restID).Scan(&host, &login, &encPass, &companyID)
	if err != nil {
		return 0, 0, fmt.Errorf("заведение не найдено: %w", err)
	}

	targetCompanyID := 0
	if companyID.Valid && companyID.Int64 > 0 {
		targetCompanyID = int(companyID.Int64)
	} else {
		_ = s.db.QueryRow(`SELECT id FROM companies WHERE iiko_host = $1 LIMIT 1`, host).Scan(&targetCompanyID)
		if targetCompanyID > 0 {
			_, _ = s.db.Exec(`UPDATE analytics_restaurants SET company_id = $1 WHERE id = $2`, targetCompanyID, restID)
		}
	}

	plainPass, err := crypto.Decrypt(encPass, s.cfg.EncryptionKey)
	if err != nil {
		return 0, 0, fmt.Errorf("ошибка дешифрования пароля iiko: %w", err)
	}

	token, err := s.iikoClient.Auth(host, login, plainPass)
	if err != nil {
		return 0, 0, fmt.Errorf("ошибка авторизации в iiko RMS: %w", err)
	}
	defer s.iikoClient.Logout(host, token)

	// Предварительная загрузка существующих маппингов (product_mappings)
	// Приоритет: маппинги текущего заведения имеют наивысший приоритет
	type productMapping struct {
		vendorItemName string
		multiplier     float64
	}
	mappings := make(map[string]productMapping)
	posUnits := make(map[string]string)

	if targetCompanyID > 0 {
		mapRows, err := s.db.Query(`
			SELECT iiko_product_uuid, vendor_item_name, COALESCE(multiplier, 1.0)
			FROM product_mappings
			WHERE company_id = $1 OR company_id IS NULL
			ORDER BY (company_id = $1) ASC, updated_at ASC
		`, targetCompanyID)
		if err == nil {
			defer mapRows.Close()
			for mapRows.Next() {
				var pUUID, vName string
				var mult float64
				if errScan := mapRows.Scan(&pUUID, &vName, &mult); errScan == nil && pUUID != "" {
					mappings[pUUID] = productMapping{
						vendorItemName: strings.TrimSpace(vName),
						multiplier:     mult,
					}
				}
			}
		}

		// Загружаем эталонные единицы измерения из каталога positions
		pRows, errPos := s.db.Query(`
			SELECT external_id, unit 
			FROM positions 
			WHERE company_id = $1 AND unit IS NOT NULL AND unit != ''`, targetCompanyID)
		if errPos == nil {
			defer pRows.Close()
			for pRows.Next() {
				var extID, u string
				if errScan := pRows.Scan(&extID, &u); errScan == nil && extID != "" {
					posUnits[extID] = strings.TrimSpace(u)
				}
			}
		}
	}

	// Параллельная загрузка накладных, контрагентов, каталога номенклатуры и снимка остатков на начало периода
	var (
		wg          sync.WaitGroup
		invoicesXML *iiko.ExportedInvoicesXML
		suppliers   map[string]string
		catalog     map[string]string
		balances    []iiko.StoreBalanceItem
		errInv      error
		errSup      error
		errCat      error
		errBal      error
	)

	wg.Add(4)
	go func() {
		defer wg.Done()
		invoicesXML, errInv = s.iikoClient.FetchIncomingInvoices(host, token, from, to, "")
	}()
	go func() {
		defer wg.Done()
		suppliers, errSup = s.iikoClient.FetchSuppliers(host, token)
	}()
	go func() {
		defer wg.Done()
		catalog, errCat = s.iikoClient.FetchCatalog(host, token)
	}()
	go func() {
		defer wg.Done()
		balanceTimestamp := from + "T00:00:00"
		balances, errBal = s.iikoClient.FetchStoreBalances(host, token, balanceTimestamp)
	}()
	wg.Wait()

	if errInv != nil {
		return 0, 0, fmt.Errorf("ошибка получения накладных: %w", errInv)
	}
	if errSup != nil {
		suppliers = make(map[string]string)
	}
	if errCat != nil {
		catalog = make(map[string]string)
	}

	importedInvoices := 0
	importedItems := 0

	var docs []iiko.ExportedDocument
	if invoicesXML != nil {
		docs = invoicesXML.Documents
	}

	for _, doc := range docs {
		supplierName := suppliers[doc.SupplierUUID]
		docNum := strings.TrimSpace(doc.DocumentNumber)
		if docNum == "" {
			docNum = strings.TrimSpace(doc.IncomingDocumentNumber)
		}
		if docNum == "" {
			docNum = strings.TrimSpace(doc.TransportInvoiceNumber)
		}
		incomingNum := strings.TrimSpace(doc.IncomingDocumentNumber)

		docDate, _ := time.Parse("2006-01-02T15:04:05", doc.DateIncoming)
		if docDate.IsZero() {
			docDate, _ = time.Parse("2006-01-02", doc.DateIncoming)
		}
		if docDate.IsZero() {
			docDate, _ = time.Parse("2006-01-02T15:04:05", doc.IncomingDate)
		}
		if docDate.IsZero() {
			docDate, _ = time.Parse("2006-01-02", doc.IncomingDate)
		}
		if docDate.IsZero() {
			docDate = time.Now()
		}

		// Вычисляем суммарную стоимость накладной
		var totalSum float64
		for _, it := range doc.Items {
			totalSum += it.Sum
		}

		var supplierUUIDVal interface{}
		if strings.TrimSpace(doc.SupplierUUID) != "" {
			supplierUUIDVal = strings.TrimSpace(doc.SupplierUUID)
		}

		var invoiceDBID int
		err := s.db.QueryRow(`
			INSERT INTO analytics_invoices (restaurant_id, iiko_doc_id, doc_number, incoming_number, doc_date, supplier_uuid, supplier_name, total_sum, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (restaurant_id, iiko_doc_id) DO UPDATE 
			SET doc_number = EXCLUDED.doc_number,
			    incoming_number = EXCLUDED.incoming_number,
			    total_sum = EXCLUDED.total_sum,
			    supplier_name = EXCLUDED.supplier_name,
			    doc_date = EXCLUDED.doc_date
			RETURNING id`,
			restID, doc.ID, docNum, incomingNum, docDate, supplierUUIDVal, supplierName, totalSum, doc.Status,
		).Scan(&invoiceDBID)

		if err != nil {
			log.Printf("⚠️ [Sync] Ошибка сохранения накладной %s (%s): %v", doc.ID, docNum, err)
			continue
		}
		importedInvoices++

		// Очищаем старые позиции перед повторной вставкой для исключения дублирования
		_, _ = s.db.Exec(`DELETE FROM analytics_invoice_items WHERE invoice_id = $1`, invoiceDBID)

		// Очищаем старые позиции этой накладной в purchase_history перед записью (идемпотентность)
		if targetCompanyID > 0 && docNum != "" && !docDate.IsZero() {
			_, _ = s.db.Exec(`
				DELETE FROM purchase_history 
				WHERE company_id = $1 AND invoice_number = $2 AND invoice_date = $3`,
				targetCompanyID, docNum, docDate.Format("2006-01-02"))
		}

		// Сохраняем позиции с классификацией и обогащением
		for _, it := range doc.Items {
			prodName := catalog[it.ProductUUID]
			if prodName == "" {
				prodName = it.ProductArticle
			}

			// ПРИОРИТЕТ: если данный UUID ранее смапплен в product_mappings,
			// берем приоритетное реальное название из накладной через парсер, а не из iiko
			productNameInInvoice := prodName
			itemMultiplier := 1.0
			if m, ok := mappings[it.ProductUUID]; ok && m.vendorItemName != "" {
				productNameInInvoice = m.vendorItemName
				if m.multiplier > 0 {
					itemMultiplier = m.multiplier
				}
			}

			// Классификация новой детальной таксономией:
			// Сначала классифицируем по реальному названию из накладной, если оно есть
			cls := engine.ClassifyProduct(productNameInInvoice)
			if cls.CanonicalCategory == "Прочее" && productNameInInvoice != prodName {
				clsFallback := engine.ClassifyProduct(prodName)
				if clsFallback.CanonicalCategory != "Прочее" {
					cls = clsFallback
				}
			}

			qty := it.ActualAmount
			if qty == 0 {
				qty = it.Amount
			}

			price := it.Price
			if price == 0 && qty > 0 {
				price = it.Sum / qty
			}

			var prodUUIDVal interface{}
			if strings.TrimSpace(it.ProductUUID) != "" {
				prodUUIDVal = strings.TrimSpace(it.ProductUUID)
			}

			// Определяем единицу измерения: сначала из каталога positions, затем fallback
			unit := posUnits[it.ProductUUID]
			if unit == "" && !isNumericOrEmpty(it.Code) {
				unit = it.Code
			}
			if unit == "" {
				unit = "кг/шт"
			}

			// 1. Сохраняем в таблицу аналитического аудита
			_, errItem := s.db.Exec(`
				INSERT INTO analytics_invoice_items (
					invoice_id, restaurant_id, doc_date, product_uuid, product_name, product_article,
					supplier_product_name, is_commodity, detected_brand, canonical_category,
					quantity, unit, price_per_unit, total_sum, vat_sum
				) VALUES (
					$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
				)`,
				invoiceDBID, restID, docDate, prodUUIDVal, prodName, it.ProductArticle,
				productNameInInvoice, cls.IsCommodity, cls.DetectedBrand, cls.CanonicalCategory,
				qty, unit, price, it.Sum, it.VatSum,
			)
			if errItem != nil {
				log.Printf("⚠️ [Sync] Ошибка добавления позиции %s в analytics_invoice_items: %v", it.ProductUUID, errItem)
			}
			importedItems++

			// 2. Сохраняем в таблицу Godmode (purchase_history)
			if targetCompanyID > 0 {
				finalQty := qty * itemMultiplier
				pricePerBaseUnit := price
				if itemMultiplier > 0 && itemMultiplier != 1.0 {
					if finalQty > 0 {
						pricePerBaseUnit = it.Sum / finalQty
					}
				}

				_, errHist := s.db.Exec(`
					INSERT INTO purchase_history (
						company_id, invoice_date, invoice_number, supplier_uuid, supplier_name,
						iiko_product_uuid, iiko_product_name, product_name_in_invoice,
						clean_category, brand, quantity, unit, multiplier, total_sum, price_per_base_unit
					) VALUES (
						$1, $2, $3, $4, $5,
						$6, $7, $8,
						$9, $10, $11, $12, $13, $14, $15
					)`,
					targetCompanyID, docDate.Format("2006-01-02"), docNum, supplierUUIDVal, supplierName,
					prodUUIDVal, prodName, productNameInInvoice,
					cls.CanonicalCategory, cls.DetectedBrand, finalQty, unit, itemMultiplier, it.Sum, pricePerBaseUnit,
				)
				if errHist != nil {
					log.Printf("⚠️ [Sync] Ошибка добавления в purchase_history: %v", errHist)
				}
			}
		}
	}

	// 3. Синхронизация актов списания iiko (Write-offs)
	writeoffs, errW := s.iikoClient.FetchWriteoffs(host, token, from, to)
	if errW != nil {
		log.Printf("⚠️ [Sync Writeoffs] Заведение %d: акты списания не получены или не поддерживаются: %v", restID, errW)
	} else if len(writeoffs) > 0 {
		importedWriteoffs := 0
		for _, wo := range writeoffs {
			if wo.Status == "DELETED" {
				continue
			}
			wDate, _ := time.Parse("2006-01-02T15:04", wo.DateIncoming)
			if wDate.IsZero() {
				wDate, _ = time.Parse("2006-01-02", wo.DateIncoming)
			}
			if wDate.IsZero() {
				wDate = time.Now()
			}

			var totalCost float64
			for _, itm := range wo.Items {
				if itm.Cost != nil {
					totalCost += *itm.Cost
				}
			}

			var woDBID int
			var storeUUIDVal, accountUUIDVal interface{}
			if strings.TrimSpace(wo.StoreID) != "" {
				storeUUIDVal = strings.TrimSpace(wo.StoreID)
			}
			if strings.TrimSpace(wo.AccountID) != "" {
				accountUUIDVal = strings.TrimSpace(wo.AccountID)
			}

			errIns := s.db.QueryRow(`
				INSERT INTO analytics_writeoffs (restaurant_id, iiko_doc_id, doc_number, doc_date, date_incoming, status, store_id, account_id, comment, total_cost)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				ON CONFLICT (restaurant_id, iiko_doc_id) DO UPDATE
				SET doc_number = EXCLUDED.doc_number,
				    doc_date = EXCLUDED.doc_date,
				    status = EXCLUDED.status,
				    comment = EXCLUDED.comment,
				    total_cost = EXCLUDED.total_cost
				RETURNING id`,
				restID, wo.ID, strings.TrimSpace(wo.DocumentNumber), wDate, wo.DateIncoming, wo.Status,
				storeUUIDVal, accountUUIDVal, wo.Comment, totalCost,
			).Scan(&woDBID)

			if errIns != nil {
				log.Printf("⚠️ [Sync Writeoffs] Ошибка сохранения акта списания %s: %v", wo.ID, errIns)
				continue
			}
			importedWriteoffs++

			_, _ = s.db.Exec(`DELETE FROM analytics_writeoff_items WHERE writeoff_id = $1`, woDBID)

			for _, itm := range wo.Items {
				if itm.Amount <= 0 {
					continue
				}
				pName := catalog[itm.ProductID]
				if pName == "" {
					if len(itm.ProductID) >= 8 {
						pName = "Товар " + itm.ProductID[:8]
					} else {
						pName = "Товар " + itm.ProductID
					}
				}

				costVal := 0.0
				if itm.Cost != nil {
					costVal = *itm.Cost
				}

				unit := "кг"
				if u, ok := posUnits[itm.ProductID]; ok && u != "" {
					unit = u
				}

				_, _ = s.db.Exec(`
					INSERT INTO analytics_writeoff_items (
						writeoff_id, restaurant_id, doc_date, product_uuid, product_name, amount, unit, cost, store_id, account_id, comment
					) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
					woDBID, restID, wDate, itm.ProductID, pName, itm.Amount, unit, costVal,
					storeUUIDVal, accountUUIDVal, wo.Comment,
				)
			}
		}
		log.Printf("📥 [Analytics Sync] Заведение %d: синхронизировано %d актов списания", restID, importedWriteoffs)
	}

	// 4. Сохранение снимка складских остатков на начало периода (analytics_store_balances)
	if errBal != nil {
		log.Printf("⚠️ [Sync Balances] Заведение %d: снимок остатков на %s не получен: %v", restID, from, errBal)
	} else if len(balances) > 0 {
		_, _ = s.db.Exec(`DELETE FROM analytics_store_balances WHERE restaurant_id = $1 AND balance_date = $2`, restID, from)
		balanceTimestamp := from + "T00:00:00"
		importedBalances := 0
		for _, b := range balances {
			if b.Amount == 0 && b.Sum == 0 {
				continue
			}
			pName := catalog[b.Product]
			if pName == "" {
				if len(b.Product) >= 8 {
					pName = "Товар " + b.Product[:8]
				} else {
					pName = "Товар " + b.Product
				}
			}

			_, errBIns := s.db.Exec(`
				INSERT INTO analytics_store_balances (
					restaurant_id, balance_date, timestamp_str, store_uuid, product_uuid, product_name, amount, cost_rub
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				restID, from, balanceTimestamp, b.Store, b.Product, pName, b.Amount, b.Sum,
			)
			if errBIns == nil {
				importedBalances++
			}
		}
		log.Printf("📥 [Analytics Sync] Заведение %d: сохранено %d позиций остатков на дату %s", restID, importedBalances, from)
	}

	log.Printf("📥 [Analytics Sync] Заведение %d (company %d): синхронизировано %d накладных, %d позиций (включая Godmode)", restID, targetCompanyID, importedInvoices, importedItems)
	return importedInvoices, importedItems, nil
}

// autoSyncMissingInvoices проверяет активные рестораны без накладных и фоново запускает их загрузку
func (s *Server) autoSyncMissingInvoices() {
	rows, err := s.db.Query(`
		SELECT r.id, r.name 
		FROM analytics_restaurants r
		WHERE r.is_active = true 
		  AND (r.company_id != 10 OR r.company_id IS NULL)
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	type targetRest struct {
		id   int
		name string
	}
	var targets []targetRest
	for rows.Next() {
		var t targetRest
		if err := rows.Scan(&t.id, &t.name); err == nil {
			targets = append(targets, t)
		}
	}

	from := time.Now().AddDate(0, 0, -60).Format("2006-01-02")
	to := time.Now().Format("2006-01-02")

	for _, t := range targets {
		var invCount int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM analytics_invoices WHERE restaurant_id = $1`, t.id).Scan(&invCount)
		if invCount == 0 {
			if s.syncQueue != nil {
				s.syncQueue.Enqueue(queue.SyncTask{
					RestaurantID:   t.id,
					RestaurantName: t.name,
					From:           from,
					To:             to,
					Trigger:        "missing_invoices",
				})
			} else {
				go func(rid int, rname string) {
					_, _, _ = s.SyncRestaurant(rid, from, to)
				}(t.id, t.name)
			}
		}
	}
}

// HandleSyncAll ставит все активные заведения в очередь фоновой синхронизации
func (s *Server) HandleSyncAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Ожидается POST", http.StatusMethodNotAllowed)
		return
	}

	rows, err := s.db.Query(`
		SELECT id, name 
		FROM analytics_restaurants 
		WHERE is_active = true AND (company_id != 10 OR company_id IS NULL)
		ORDER BY id ASC`)
	if err != nil {
		http.Error(w, "Ошибка БД: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	days := 14
	if dStr := r.URL.Query().Get("days"); dStr != "" {
		if d, err := strconv.Atoi(dStr); err == nil && d > 0 {
			days = d
		}
	}

	from := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	to := time.Now().Format("2006-01-02")
	enqueued := 0

	for rows.Next() {
		var id int
		var name string
		if errScan := rows.Scan(&id, &name); errScan == nil {
			if s.syncQueue != nil {
				if s.syncQueue.Enqueue(queue.SyncTask{
					RestaurantID:   id,
					RestaurantName: name,
					From:           from,
					To:             to,
					Trigger:        "manual_sync_all",
				}) {
					enqueued++
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"enqueued": enqueued,
		"message":  fmt.Sprintf("Поставлено в очередь на обновление: %d заведений", enqueued),
	})
}

// ReclassifyAllItems выполняет глобальную реклассификацию всех товарных позиций в БД
// на основе обновленной таксономии (сливки, жирность, сыры, мясо, бакалея)
func (s *Server) ReclassifyAllItems() {
	rows, err := s.db.Query(`SELECT DISTINCT product_name FROM analytics_invoice_items`)
	if err != nil {
		return
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil && name != "" {
			names = append(names, name)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		UPDATE analytics_invoice_items 
		SET canonical_category = $1, is_commodity = $2, detected_brand = $3 
		WHERE product_name = $4
	`)
	if err != nil {
		return
	}
	defer stmt.Close()

	for _, name := range names {
		cls := engine.ClassifyProduct(name)
		_, _ = stmt.Exec(cls.CanonicalCategory, cls.IsCommodity, cls.DetectedBrand, name)
	}

	_ = tx.Commit()
	log.Printf("📊 [Classifier] Успешно реклассифицировано %d уникальных товаров номенклатуры", len(names))
}

func isNumericOrEmpty(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
