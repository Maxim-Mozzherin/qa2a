package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"analytics_service/internal/crypto"
	"analytics_service/internal/engine"
	"analytics_service/internal/iiko"
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
	err := s.db.QueryRow(`
		SELECT iiko_host, iiko_login, iiko_password_enc 
		FROM analytics_restaurants 
		WHERE id = $1`, restID).Scan(&host, &login, &encPass)
	if err != nil {
		return 0, 0, fmt.Errorf("заведение не найдено: %w", err)
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

	// Параллельная загрузка накладных, контрагентов и каталога номенклатуры
	var (
		wg          sync.WaitGroup
		invoicesXML *iiko.ExportedInvoicesXML
		suppliers   map[string]string
		catalog     map[string]string
		errInv      error
		errSup      error
		errCat      error
	)

	wg.Add(3)
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
		docDate, _ := time.Parse("2006-01-02T15:04:05", doc.DateIncoming)
		if docDate.IsZero() {
			docDate, _ = time.Parse("2006-01-02", doc.DateIncoming)
		}

		// Вычисляем суммарную стоимость накладной
		var totalSum float64
		for _, it := range doc.Items {
			totalSum += it.Sum
		}

		var invoiceDBID int
		err := s.db.QueryRow(`
			INSERT INTO analytics_invoices (restaurant_id, iiko_doc_id, doc_number, doc_date, supplier_id, supplier_name, total_sum, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (restaurant_id, iiko_doc_id) DO UPDATE 
			SET total_sum = EXCLUDED.total_sum,
			    supplier_name = EXCLUDED.supplier_name,
			    doc_date = EXCLUDED.doc_date
			RETURNING id`,
			restID, doc.ID, doc.DocumentNumber, docDate, doc.SupplierUUID, supplierName, totalSum, doc.Status,
		).Scan(&invoiceDBID)

		if err != nil {
			continue
		}
		importedInvoices++

		// Очищаем старые позиции перед повторной вставкой для исключения дублирования
		_, _ = s.db.Exec(`DELETE FROM analytics_invoice_items WHERE invoice_id = $1`, invoiceDBID)

		// Сохраняем позиции с классификацией
		for _, it := range doc.Items {
			prodName := catalog[it.ProductUUID]
			if prodName == "" {
				prodName = it.ProductArticle
			}

			// Классификация новой детальной таксономией
			cls := engine.ClassifyProduct(prodName)

			qty := it.ActualAmount
			if qty == 0 {
				qty = it.Amount
			}

			price := it.Price
			if price == 0 && qty > 0 {
				price = it.Sum / qty
			}

			_, _ = s.db.Exec(`
				INSERT INTO analytics_invoice_items (
					invoice_id, restaurant_id, doc_date, product_uuid, product_name, product_article,
					is_commodity, detected_brand, canonical_category,
					quantity, unit, price_per_unit, total_sum, vat_sum
				) VALUES (
					$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
				)`,
				invoiceDBID, restID, docDate, it.ProductUUID, prodName, it.ProductArticle,
				cls.IsCommodity, cls.DetectedBrand, cls.CanonicalCategory,
				qty, it.Code, price, it.Sum, it.VatSum,
			)
			importedItems++
		}
	}

	log.Printf("📥 [Analytics Sync] Заведение %d: синхронизировано %d накладных, %d позиций", restID, importedInvoices, importedItems)
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
			log.Printf("🔄 [Auto-Sync] Фоновая синхронизация накладных для заведения %d (%s)...", t.id, t.name)
			go func(rid int, rname string) {
				invs, items, errSync := s.SyncRestaurant(rid, from, to)
				if errSync != nil {
					log.Printf("⚠️ [Auto-Sync] Ошибка синхронизации %s (id %d): %v", rname, rid, errSync)
				} else {
					log.Printf("✅ [Auto-Sync] Успешно синхронизировано %s: %d накладных, %d позиций", rname, invs, items)
				}
			}(t.id, t.name)
		}
	}
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
