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
			ph.price_per_base_unit
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
	Items         []UpdateHistoryItem `json:"items"`
}

type UpdateHistoryItem struct {
	ID               int     `json:"id"`
	Quantity         float64 `json:"quantity"`
	Multiplier       float64 `json:"multiplier"`
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

	stmt, err := tx.Prepare(`
		UPDATE purchase_history
		SET quantity = $1, multiplier = $2, total_sum = $3, price_per_base_unit = $4
		WHERE id = $5 AND company_id = $6
	`)
	if err != nil {
		http.Error(w, "Ошибка подготовки SQL: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	for _, it := range req.Items {
		finalQty := it.Quantity * it.Multiplier
		pricePerUnit := it.PricePerBaseUnit
		if pricePerUnit <= 0 && finalQty > 0 {
			pricePerUnit = it.TotalSum / finalQty
		}
		_, err := stmt.Exec(it.Quantity, it.Multiplier, it.TotalSum, pricePerUnit, it.ID, req.CompanyID)
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
