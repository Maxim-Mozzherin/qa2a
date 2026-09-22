package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/lib/pq"
)

func handleGetUnlistedOperations(w http.ResponseWriter, r *http.Request) {
	companyIDStr := r.URL.Query().Get("company_id")
	if companyIDStr == "" {
		http.Error(w, "Отсутствует обязательный параметр company_id", http.StatusBadRequest)
		return
	}

	var companyID int
	if _, err := fmt.Sscanf(companyIDStr, "%d", &companyID); err != nil || companyID <= 0 {
		http.Error(w, "Некорректный company_id", http.StatusBadRequest)
		return
	}

	if !checkAccountantAccessUser(GetAuthUser(r), companyID) {
		http.Error(w, "Доступ к аналитике данного заведения запрещен", http.StatusForbidden)
		return
	}

	query := `
		SELECT 
			o.id, 
			o.position_name, 
			o.quantity, 
			o.unit, 
			COALESCE(o.comment, '') as comment, 
			o.created_at, 
			COALESCE(u.full_name, u.username, 'Система') as user_name
		FROM operations o
		LEFT JOIN users u ON o.user_id = u.id
		WHERE o.company_id = $1 AND o.is_unlisted = true AND o.type = 'writeoff'
		ORDER BY o.created_at DESC`

	rows, err := db.Query(query, companyID)
	if err != nil {
		http.Error(w, "Ошибка чтения неучтенных списаний: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list []UnlistedOperation
	for rows.Next() {
		var op UnlistedOperation
		if err := rows.Scan(&op.ID, &op.PositionName, &op.Quantity, &op.Unit, &op.Comment, &op.CreatedAt, &op.UserName); err == nil {
			list = append(list, op)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func handleResolveUnlistedOperation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CompanyID       int    `json:"company_id"`
		OperationID     int    `json:"operation_id"`
		IikoProductName string `json:"iiko_product_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	if !checkAccountantAccessUser(GetAuthUser(r), req.CompanyID) {
		http.Error(w, "Доступ к данному заведению запрещен", http.StatusForbidden)
		return
	}

	req.IikoProductName = strings.TrimSpace(req.IikoProductName)
	if req.CompanyID <= 0 || req.OperationID <= 0 || req.IikoProductName == "" {
		http.Error(w, "Заполните все обязательные поля (company_id, operation_id, iiko_product_name)", http.StatusBadRequest)
		return
	}

	query := `
		UPDATE operations 
		SET position_name = $1, is_unlisted = false 
		WHERE id = $2 AND company_id = $3 AND is_unlisted = true`

	res, err := db.Exec(query, req.IikoProductName, req.OperationID, req.CompanyID)
	if err != nil {
		http.Error(w, "Ошибка обновления операции в БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		http.Error(w, "Операция не найдена, либо уже была сопоставлена ранее", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Операция успешно привязана к товару iiko RMS!",
	})
}

func handleRejectUnlistedOperation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Только DELETE метод", http.StatusMethodNotAllowed)
		return
	}
	companyIDStr := r.URL.Query().Get("company_id")
	opIDStr := r.URL.Query().Get("operation_id")

	companyID, err1 := strconv.Atoi(companyIDStr)
	opID, err2 := strconv.Atoi(opIDStr)
	if err1 != nil || err2 != nil || companyID <= 0 || opID <= 0 {
		http.Error(w, "Некорректные параметры company_id или operation_id", http.StatusBadRequest)
		return
	}

	if !checkAccountantAccessUser(GetAuthUser(r), companyID) {
		http.Error(w, "Доступ запрещен", http.StatusForbidden)
		return
	}

	// Soft delete: keep in history but remove from unlisted queue
	query := `UPDATE operations SET is_unlisted = false, status = 'rejected', comment = comment || ' [Отклонено бухгалтером]' WHERE id = $1 AND company_id = $2 AND is_unlisted = true`
	_, err := db.Exec(query, opID, companyID)
	if err != nil {
		http.Error(w, "Ошибка БД", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func handleAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Только GET метод", http.StatusMethodNotAllowed)
		return
	}

	companyIDStr := r.URL.Query().Get("company_id")
	if companyIDStr == "" {
		http.Error(w, "Отсутствует обязательный параметр company_id", http.StatusBadRequest)
		return
	}

	var companyID int
	if _, err := fmt.Sscanf(companyIDStr, "%d", &companyID); err != nil || companyID <= 0 {
		http.Error(w, "Некорректный company_id", http.StatusBadRequest)
		return
	}

	if !checkAccountantAccessUser(GetAuthUser(r), companyID) {
		http.Error(w, "Доступ к аналитике данного заведения запрещен", http.StatusForbidden)
		return
	}

	daysStr := r.URL.Query().Get("days")
	days := 30
	if daysStr != "" {
		if d, err := fmt.Sscanf(daysStr, "%d", &days); err != nil || d <= 0 {
			days = 30
		}
	}

	query := `
		SELECT 
			TO_CHAR(invoice_date, 'YYYY-MM-DD') as invoice_date,
			invoice_number,
			supplier_name,
			iiko_product_uuid,
			COALESCE(iiko_product_name, '') as iiko_product_name,
			product_name_in_invoice,
			quantity,
			unit,
			multiplier,
			total_sum,
			price_per_base_unit
		FROM purchase_history
		WHERE company_id = $1 AND invoice_date >= NOW() - INTERVAL '1 day' * $2
		ORDER BY invoice_date DESC, id DESC`

	rows, err := db.Query(query, companyID, days)
	if err != nil {
		http.Error(w, "Ошибка чтения истории закупок: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var records []PurchaseRecord
	for rows.Next() {
		var rec PurchaseRecord
		err := rows.Scan(
			&rec.InvoiceDate,
			&rec.InvoiceNum,
			&rec.SupplierName,
			&rec.ProductUUID,
			&rec.IikoProductName,
			&rec.ProductName,
			&rec.Quantity,
			&rec.Unit,
			&rec.Multiplier,
			&rec.TotalSum,
			&rec.PricePerUnit,
		)
		if err == nil {
			records = append(records, rec)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(records)
}

type WriteoffAccountBreakdown struct {
	AccountName string  `json:"account_name"`
	Quantity    float64 `json:"quantity"`
	SharePct    float64 `json:"share_pct"`
}

type ToxicWriteoffRecord struct {
	PositionName     string                     `json:"position_name"`
	TotalWrittenOff  float64                    `json:"total_written_off"`
	Unit             string                     `json:"unit"`
	AvgPurchasePrice float64                    `json:"avg_purchase_price"`
	TotalLossRub     float64                    `json:"total_loss_rub"`
	WasteRatioPct    float64                    `json:"waste_ratio_pct"`
	Breakdown        []WriteoffAccountBreakdown `json:"breakdown"`
}

func handleToxicWriteoffs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Только GET метод", http.StatusMethodNotAllowed)
		return
	}

	companyIDStr := r.URL.Query().Get("company_id")
	if companyIDStr == "" {
		http.Error(w, "Отсутствует обязательный параметр company_id", http.StatusBadRequest)
		return
	}

	var companyID int
	if _, err := fmt.Sscanf(companyIDStr, "%d", &companyID); err != nil || companyID <= 0 {
		http.Error(w, "Некорректный company_id", http.StatusBadRequest)
		return
	}

	if !checkAccountantAccessUser(GetAuthUser(r), companyID) {
		http.Error(w, "Доступ к аналитике данного заведения запрещен", http.StatusForbidden)
		return
	}

	daysStr := r.URL.Query().Get("days")
	days := 30
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	query := `
WITH writeoffs AS (
    SELECT 
        COALESCE(pos.external_id, '') AS product_uuid,
        o.position_name,
        SUM(o.quantity) AS total_written_off,
        MAX(pos.unit) AS pos_unit
    FROM operations o
    LEFT JOIN positions pos ON pos.name = o.position_name AND pos.company_id = o.company_id
    WHERE o.company_id = $1 
      AND o.type = 'writeoff'
      AND o.created_at >= NOW() - ($2 * INTERVAL '1 day')
    GROUP BY pos.external_id, o.position_name
),
purchases_by_uuid AS (
    SELECT 
        iiko_product_uuid AS product_uuid,
        SUM(quantity * multiplier) AS total_purchased,
        SUM(total_sum) AS total_spent,
        MAX(unit) AS ph_unit
    FROM purchase_history
    WHERE company_id = $1 
      AND iiko_product_uuid != ''
      AND invoice_date >= CURRENT_DATE - ($2 * INTERVAL '1 day')
    GROUP BY iiko_product_uuid
),
purchases_by_name AS (
    SELECT 
        LOWER(TRIM(COALESCE(NULLIF(iiko_product_name, ''), product_name_in_invoice))) AS norm_name,
        SUM(quantity * multiplier) AS total_purchased,
        SUM(total_sum) AS total_spent,
        MAX(unit) AS ph_unit
    FROM purchase_history
    WHERE company_id = $1 
      AND invoice_date >= CURRENT_DATE - ($2 * INTERVAL '1 day')
    GROUP BY LOWER(TRIM(COALESCE(NULLIF(iiko_product_name, ''), product_name_in_invoice)))
)
SELECT 
    w.position_name,
    w.total_written_off,
    COALESCE(NULLIF(pu.ph_unit, ''), NULLIF(pn.ph_unit, ''), NULLIF(w.pos_unit, ''), 'ед.') AS unit,
    CASE 
        WHEN COALESCE(pu.total_purchased, pn.total_purchased, 0) > 0 
        THEN COALESCE(pu.total_spent, pn.total_spent, 0) / COALESCE(pu.total_purchased, pn.total_purchased, 1)
        ELSE 0 
    END AS avg_purchase_price,
    w.total_written_off * (
        CASE 
            WHEN COALESCE(pu.total_purchased, pn.total_purchased, 0) > 0 
            THEN COALESCE(pu.total_spent, pn.total_spent, 0) / COALESCE(pu.total_purchased, pn.total_purchased, 1)
            ELSE 0 
        END
    ) AS total_loss_rub,
    CASE 
        WHEN COALESCE(pu.total_purchased, pn.total_purchased, 0) > 0 
        THEN (w.total_written_off / COALESCE(pu.total_purchased, pn.total_purchased, 1)) * 100.0
        ELSE 0 
    END AS waste_ratio_pct
FROM writeoffs w
LEFT JOIN purchases_by_uuid pu ON (w.product_uuid != '' AND w.product_uuid = pu.product_uuid)
LEFT JOIN purchases_by_name pn ON (w.product_uuid = '' AND LOWER(TRIM(w.position_name)) = pn.norm_name)
WHERE w.total_written_off > 0
ORDER BY total_loss_rub DESC, w.total_written_off DESC
LIMIT 10;`

	rows, err := db.Query(query, companyID, days) // Pass days directly as integer!
	if err != nil {
		http.Error(w, "Ошибка выполнения аналитики списаний: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	records := make([]ToxicWriteoffRecord, 0)
	for rows.Next() {
		var rec ToxicWriteoffRecord
		if err := rows.Scan(
			&rec.PositionName,
			&rec.TotalWrittenOff,
			&rec.Unit,
			&rec.AvgPurchasePrice,
			&rec.TotalLossRub,
			&rec.WasteRatioPct,
		); err != nil {
			continue
		}
		rec.Breakdown = make([]WriteoffAccountBreakdown, 0)
		records = append(records, rec)
	}

	if len(records) > 0 {
		posNames := make([]string, len(records))
		for i, r := range records {
			posNames[i] = r.PositionName
		}

		breakdownQuery := `
SELECT 
    o.position_name,
    COALESCE(NULLIF(wa.name, ''), 'Расход продуктов (Стандарт)') AS account_name,
    SUM(o.quantity) AS qty
FROM operations o
LEFT JOIN writeoff_accounts wa ON (wa.external_id = o.account_id AND wa.company_id = o.company_id)
WHERE o.company_id = $1 
  AND o.type = 'writeoff'
  AND o.created_at >= NOW() - ($2 * INTERVAL '1 day')
  AND o.position_name = ANY($3)
GROUP BY o.position_name, COALESCE(NULLIF(wa.name, ''), 'Расход продуктов (Стандарт)')
ORDER BY qty DESC;`

		bRows, bErr := db.Query(breakdownQuery, companyID, days, pq.Array(posNames))
		if bErr == nil {
			defer bRows.Close()
			breakdowns := make(map[string][]WriteoffAccountBreakdown)
			for bRows.Next() {
				var posName, accountName string
				var qty float64
				if err := bRows.Scan(&posName, &accountName, &qty); err == nil {
					breakdowns[posName] = append(breakdowns[posName], WriteoffAccountBreakdown{
						AccountName: accountName,
						Quantity:    qty,
					})
				}
			}

			for i := range records {
				bdList := breakdowns[records[i].PositionName]
				if bdList == nil {
					bdList = make([]WriteoffAccountBreakdown, 0)
				}
				for j := range bdList {
					if records[i].TotalWrittenOff > 0 {
						bdList[j].SharePct = (bdList[j].Quantity / records[i].TotalWrittenOff) * 100.0
					}
				}
				records[i].Breakdown = bdList
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(records)
}

func handleMarketSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Только GET метод", http.StatusMethodNotAllowed)
		return
	}

	if !checkMarketAuth(r) {
		http.Error(w, "Unauthorized (Недействительный токен рынка)", http.StatusUnauthorized)
		return
	}

	queryParam := r.URL.Query().Get("q")
	if strings.TrimSpace(queryParam) == "" {
		http.Error(w, "Введите поисковый запрос (q=бренд или название)", http.StatusBadRequest)
		return
	}

	rawTokens := strings.Split(queryParam, ",")
	var patterns []string
	for _, t := range rawTokens {
		trimmed := strings.TrimSpace(t)
		if trimmed != "" {
			patterns = append(patterns, "%"+trimmed+"%")
		}
	}

	if len(patterns) == 0 {
		http.Error(w, "Введите поисковый запрос", http.StatusBadRequest)
		return
	}

	query := `
		SELECT 
			c.name as restaurant_name,
			TO_CHAR(ph.invoice_date, 'YYYY-MM-DD') as invoice_date,
			ph.supplier_name,
			ph.product_name_in_invoice,
			COALESCE(ph.clean_category, '') as clean_category,
			COALESCE(ph.brand, '') as brand,
			ph.price_per_base_unit,
			COALESCE(ph.unit, 'ед.') as unit
		FROM purchase_history ph
		JOIN companies c ON ph.company_id = c.id
		WHERE ph.brand ILIKE ANY($1) 
		   OR ph.clean_category ILIKE ANY($1) 
		   OR ph.product_name_in_invoice ILIKE ANY($1)
		ORDER BY ph.price_per_base_unit ASC, ph.invoice_date DESC
		LIMIT 150`

	rows, err := db.Query(query, pq.Array(patterns))
	if err != nil {
		http.Error(w, "Ошибка SQL выборки рынка: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var records []MarketRecord
	for rows.Next() {
		var rec MarketRecord
		if err := rows.Scan(
			&rec.RestaurantName,
			&rec.InvoiceDate,
			&rec.SupplierName,
			&rec.ProductName,
			&rec.CleanCategory,
			&rec.Brand,
			&rec.PricePerUnit,
			&rec.Unit,
		); err == nil {
			records = append(records, rec)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(records)
}

// ============================================================================
// АРХИВ НАКЛАДНЫХ И РЕДАКТОР ИСТОРИИ ЗАКУПОК (purchase_history)
// ============================================================================

type HistoryInvoice struct {
	InvoiceNumber string  `json:"invoice_number"`
	InvoiceDate   string  `json:"invoice_date"`
	SupplierName  string  `json:"supplier_name"`
	ItemsCount    int     `json:"items_count"`
	TotalSum      float64 `json:"total_sum"`
}

type HistoryInvoiceItem struct {
	ID                   int     `json:"id"`
	ProductNameInInvoice string  `json:"product_name_in_invoice"`
	Quantity             float64 `json:"quantity"`
	Unit                 string  `json:"unit"`
	Multiplier           float64 `json:"multiplier"`
	TotalSum             float64 `json:"total_sum"`
	PricePerBaseUnit     float64 `json:"price_per_base_unit"`
	CleanCategory        string  `json:"clean_category"`
	Brand                string  `json:"brand"`
}

type UpdateHistoryItemsRequest struct {
	CompanyID     int                 `json:"company_id"`
	InvoiceNumber string              `json:"invoice_number"`
	SupplierName  string              `json:"supplier_name"`
	Items         []UpdateHistoryItem `json:"items"`
}

type UpdateHistoryItem struct {
	ID               int     `json:"id"`
	FinalQty         float64 `json:"final_qty"`
	Unit             string  `json:"unit"`
	TotalSum         float64 `json:"total_sum"`
	PricePerBaseUnit float64 `json:"price_per_base_unit"`
}

func handleGetHistoryInvoices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	companyIDStr := r.URL.Query().Get("company_id")
	if companyIDStr == "" {
		http.Error(w, "Отсутствует параметр company_id", http.StatusBadRequest)
		return
	}

	companyID, err := strconv.Atoi(companyIDStr)
	if err != nil || companyID <= 0 {
		http.Error(w, "Некорректный company_id", http.StatusBadRequest)
		return
	}

	if !checkAccountantAccessUser(GetAuthUser(r), companyID) {
		http.Error(w, "Доступ запрещен", http.StatusForbidden)
		return
	}

	query := `
		SELECT 
			invoice_number, 
			TO_CHAR(invoice_date, 'YYYY-MM-DD') as invoice_date, 
			COALESCE(supplier_name, '') as supplier_name, 
			COUNT(*) as items_count, 
			COALESCE(SUM(total_sum), 0) as total_sum
		FROM purchase_history
		WHERE company_id = $1
		GROUP BY invoice_number, invoice_date, supplier_name
		ORDER BY invoice_date DESC, invoice_number DESC
		LIMIT 200`

	rows, err := db.Query(query, companyID)
	if err != nil {
		http.Error(w, "Ошибка SQL выборки истории накладных: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var invoices []HistoryInvoice
	for rows.Next() {
		var inv HistoryInvoice
		if err := rows.Scan(&inv.InvoiceNumber, &inv.InvoiceDate, &inv.SupplierName, &inv.ItemsCount, &inv.TotalSum); err == nil {
			invoices = append(invoices, inv)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(invoices)
}

func handleHistoryInvoiceItems(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		handleGetInvoiceItems(w, r)
	} else if r.Method == http.MethodPut {
		handleUpdateInvoiceItems(w, r)
	} else {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func handleGetInvoiceItems(w http.ResponseWriter, r *http.Request) {
	companyIDStr := r.URL.Query().Get("company_id")
	invoiceNumber := r.URL.Query().Get("invoice_number")
	if companyIDStr == "" || invoiceNumber == "" {
		http.Error(w, "Параметры company_id и invoice_number обязательны", http.StatusBadRequest)
		return
	}

	companyID, err := strconv.Atoi(companyIDStr)
	if err != nil || companyID <= 0 {
		http.Error(w, "Некорректный company_id", http.StatusBadRequest)
		return
	}

	if !checkAccountantAccessUser(GetAuthUser(r), companyID) {
		http.Error(w, "Доступ запрещен", http.StatusForbidden)
		return
	}

	query := `
		SELECT 
			id, 
			product_name_in_invoice, 
			quantity, 
			COALESCE(unit, 'кг/шт') as unit, 
			multiplier, 
			total_sum, 
			price_per_base_unit, 
			COALESCE(clean_category, '') as clean_category, 
			COALESCE(brand, '') as brand
		FROM purchase_history
		WHERE company_id = $1 AND invoice_number = $2
		ORDER BY id ASC`

	rows, err := db.Query(query, companyID, invoiceNumber)
	if err != nil {
		http.Error(w, "Ошибка SQL выборки позиций накладной: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var items []HistoryInvoiceItem
	for rows.Next() {
		var it HistoryInvoiceItem
		if err := rows.Scan(
			&it.ID,
			&it.ProductNameInInvoice,
			&it.Quantity,
			&it.Unit,
			&it.Multiplier,
			&it.TotalSum,
			&it.PricePerBaseUnit,
			&it.CleanCategory,
			&it.Brand,
		); err == nil {
			items = append(items, it)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func handleUpdateInvoiceItems(w http.ResponseWriter, r *http.Request) {
	var req UpdateHistoryItemsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Некорректный JSON запрос: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.CompanyID <= 0 {
		http.Error(w, "Некорректный company_id", http.StatusBadRequest)
		return
	}

	if !checkAccountantAccessUser(GetAuthUser(r), req.CompanyID) {
		http.Error(w, "Доступ запрещен", http.StatusForbidden)
		return
	}

	if len(req.Items) == 0 {
		http.Error(w, "Список позиций пуст", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Ошибка старта транзакции: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if req.SupplierName != "" {
		_, err = tx.Exec("UPDATE purchase_history SET supplier_name = $1 WHERE company_id = $2 AND invoice_number = $3", req.SupplierName, req.CompanyID, req.InvoiceNumber)
		if err != nil {
			http.Error(w, "Ошибка обновления поставщика: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	stmt, err := tx.Prepare(`
		UPDATE purchase_history
		SET quantity = $1, multiplier = 1.0, unit = $2, total_sum = $3, price_per_base_unit = $4
		WHERE id = $5 AND company_id = $6
	`)
	if err != nil {
		http.Error(w, "Ошибка подготовки SQL: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	for _, it := range req.Items {
		pricePerUnit := it.PricePerBaseUnit
		if pricePerUnit <= 0 && it.FinalQty > 0 {
			pricePerUnit = it.TotalSum / it.FinalQty
		}
		unit := it.Unit
		if unit == "" {
			unit = "кг/шт"
		}
		_, err := stmt.Exec(it.FinalQty, unit, it.TotalSum, pricePerUnit, it.ID, req.CompanyID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Ошибка обновления строки id=%d: %v", it.ID, err), http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Ошибка фиксации транзакции: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"updated": len(req.Items),
	})
}
