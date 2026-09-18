package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/lib/pq"
)

func checkMarketAuth(r *http.Request) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	return token == superadminToken
}

func sendMarketError(w http.ResponseWriter, msg string, code int) {
	http.Error(w, msg, code)
}

// 1. Досье заведения (Establishment Dossier)
func handleMarketDossier(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendMarketError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !checkMarketAuth(r) {
		sendMarketError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	companyIDStr := r.URL.Query().Get("company_id")
	if companyIDStr == "" {
		sendMarketError(w, "Missing company_id", http.StatusBadRequest)
		return
	}

	days := 30
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)

	// Общая сумма
	var totalSpent float64
	err := db.QueryRow(`
		SELECT COALESCE(SUM(total_sum), 0) 
		FROM purchase_history 
		WHERE company_id = $1 AND invoice_date >= NOW() - INTERVAL '1 day' * $2
	`, companyIDStr, days).Scan(&totalSpent)
	if err != nil {
		sendMarketError(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// GROUP SUPPLIERS BY UUID to merge "ООО РЕМО" and "Общество с ограниченной..."
	// We use MAX(supplier_name) for display name
	type SupplierStat struct {
		Name  string  `json:"name"`
		Total float64 `json:"total"`
	}
	suppliers := make([]SupplierStat, 0)
	rows, err := db.Query(`
		SELECT MAX(supplier_name) as display_name, SUM(total_sum) as total
		FROM purchase_history
		WHERE company_id = $1 AND invoice_date >= NOW() - INTERVAL '1 day' * $2
		GROUP BY supplier_uuid
		ORDER BY total DESC
	`, companyIDStr, days)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var s SupplierStat
			if rows.Scan(&s.Name, &s.Total) == nil {
				suppliers = append(suppliers, s)
			}
		}
	}

	// Категории
	type CategoryStat struct {
		Name  string  `json:"name"`
		Total float64 `json:"total"`
	}
	categories := make([]CategoryStat, 0)
	rowsCat, err := db.Query(`
		SELECT CASE WHEN clean_category = '' THEN 'Без категории' ELSE clean_category END as cat, SUM(total_sum) as total
		FROM purchase_history
		WHERE company_id = $1 AND invoice_date >= NOW() - INTERVAL '1 day' * $2
		GROUP BY cat
		ORDER BY total DESC
	`, companyIDStr, days)
	if err == nil {
		defer rowsCat.Close()
		for rowsCat.Next() {
			var c CategoryStat
			if rowsCat.Scan(&c.Name, &c.Total) == nil {
				categories = append(categories, c)
			}
		}
	}

	// Топ-20 товаров по объему (затратам)
	type TopItem struct {
		Name          string  `json:"name"`
		Category      string  `json:"category"`
		Supplier      string  `json:"supplier"`
		Quantity      float64 `json:"quantity"`
		Unit          string  `json:"unit"`
		AvgPrice      float64 `json:"avg_price"`
		TotalSum      float64 `json:"total_sum"`
	}
	topItems := make([]TopItem, 0)
	rowsTop, err := db.Query(`
		SELECT 
			product_name_in_invoice, 
			MAX(clean_category),
			MAX(supplier_name),
			SUM(quantity * multiplier) as total_qty, 
			MAX(unit),
			SUM(total_sum) / NULLIF(SUM(quantity * multiplier), 0) as avg_price,
			SUM(total_sum) as total
		FROM purchase_history
		WHERE company_id = $1 AND invoice_date >= NOW() - INTERVAL '1 day' * $2
		GROUP BY product_name_in_invoice
		ORDER BY total DESC
		LIMIT 20
	`, companyIDStr, days)
	if err == nil {
		defer rowsTop.Close()
		for rowsTop.Next() {
			var t TopItem
			if rowsTop.Scan(&t.Name, &t.Category, &t.Supplier, &t.Quantity, &t.Unit, &t.AvgPrice, &t.TotalSum) == nil {
				topItems = append(topItems, t)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_spent": totalSpent,
		"suppliers":   suppliers,
		"categories":  categories,
		"top_items":   topItems,
	})
}

// Утилита для очистки (вызывается один раз)
func handleMarketCleanup(w http.ResponseWriter, r *http.Request) {
	if !checkMarketAuth(r) { sendMarketError(w, "Unauthorized", http.StatusUnauthorized); return }
	
	res, err := db.Exec("DELETE FROM purchase_history WHERE clean_category IN ('Глутамат натрия', 'Тест') OR (product_name_in_invoice ILIKE '%тест%' AND total_sum = 0)")
	if err != nil {
		sendMarketError(w, err.Error(), 500)
		return
	}
	affected, _ := res.RowsAffected()
	w.Write([]byte(fmt.Sprintf("Deleted %d test rows", affected)))
}

// 2. Радар маржинального арбитража (Arbitrage)
func handleMarketArbitrage(w http.ResponseWriter, r *http.Request) {
	if !checkMarketAuth(r) { sendMarketError(w, "Unauthorized", http.StatusUnauthorized); return }

	days := 30
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)

	query := `
		WITH market_stats AS (
			SELECT 
				product_name_in_invoice,
				PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY price_per_base_unit) as median_price,
				COUNT(*) as purchases_count
			FROM purchase_history
			WHERE invoice_date >= NOW() - INTERVAL '1 day' * $1
			GROUP BY product_name_in_invoice
			HAVING COUNT(*) > 3
		)
		SELECT 
			c.name as restaurant_name,
			ph.supplier_name,
			ph.product_name_in_invoice,
			ph.price_per_base_unit,
			ms.median_price,
			((ph.price_per_base_unit - ms.median_price) / ms.median_price) * 100 as overprice_percent,
			TO_CHAR(ph.invoice_date, 'YYYY-MM-DD') as invoice_date
		FROM purchase_history ph
		JOIN companies c ON ph.company_id = c.id
		JOIN market_stats ms ON ph.product_name_in_invoice = ms.product_name_in_invoice
		WHERE ph.invoice_date >= NOW() - INTERVAL '1 day' * $1
		  AND ph.price_per_base_unit > ms.median_price * 1.15
		ORDER BY overprice_percent DESC
		LIMIT 100
	`

	type ArbitrageRecord struct {
		RestaurantName  string  `json:"restaurant_name"`
		SupplierName    string  `json:"supplier_name"`
		ProductName     string  `json:"product_name"`
		ActualPrice     float64 `json:"actual_price"`
		MedianPrice     float64 `json:"median_price"`
		OverpricePct    float64 `json:"overprice_percent"`
		InvoiceDate     string  `json:"invoice_date"`
	}

	results := make([]ArbitrageRecord, 0)
	rows, err := db.Query(query, days)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var rec ArbitrageRecord
			if rows.Scan(&rec.RestaurantName, &rec.SupplierName, &rec.ProductName, &rec.ActualPrice, &rec.MedianPrice, &rec.OverpricePct, &rec.InvoiceDate) == nil {
				results = append(results, rec)
			}
		}
	} else {
		sendMarketError(w, "DB error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// 3. Досье поставщика (Supplier Dossier)
func handleMarketSupplierDossier(w http.ResponseWriter, r *http.Request) {
	if !checkMarketAuth(r) { sendMarketError(w, "Unauthorized", http.StatusUnauthorized); return }

	supplierName := r.URL.Query().Get("supplier_name")
	if supplierName == "" {
		sendMarketError(w, "Missing supplier_name", http.StatusBadRequest)
		return
	}
	days := 30
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)

	type ClientStat struct {
		RestaurantName string  `json:"restaurant_name"`
		TotalSum       float64 `json:"total_sum"`
	}
	clients := make([]ClientStat, 0)
	var totalTurnover float64

	rows, err := db.Query(`
		SELECT c.name, COALESCE(SUM(ph.total_sum), 0) as total
		FROM purchase_history ph
		JOIN companies c ON ph.company_id = c.id
		WHERE (
			ph.supplier_uuid IN (SELECT supplier_uuid FROM purchase_history WHERE supplier_name ILIKE $1 AND supplier_uuid != '')
			OR ph.supplier_name ILIKE $1
		) AND ph.invoice_date >= NOW() - INTERVAL '1 day' * $2
		GROUP BY c.name
		ORDER BY total DESC
	`, "%"+supplierName+"%", days)
	
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cs ClientStat
			if rows.Scan(&cs.RestaurantName, &cs.TotalSum) == nil {
				clients = append(clients, cs)
				totalTurnover += cs.TotalSum
			}
		}
	}

	type TopItem struct {
		Name     string  `json:"name"`
		TotalSum float64 `json:"total_sum"`
	}
	topItems := make([]TopItem, 0)
	rowsTop, _ := db.Query(`
		SELECT product_name_in_invoice, COALESCE(SUM(total_sum), 0) as total
		FROM purchase_history
		WHERE (
			supplier_uuid IN (SELECT supplier_uuid FROM purchase_history WHERE supplier_name ILIKE $1 AND supplier_uuid != '')
			OR supplier_name ILIKE $1
		) AND invoice_date >= NOW() - INTERVAL '1 day' * $2
		GROUP BY product_name_in_invoice
		ORDER BY total DESC
		LIMIT 10
	`, "%"+supplierName+"%", days)
	if rowsTop != nil {
		defer rowsTop.Close()
		for rowsTop.Next() {
			var ti TopItem
			if rowsTop.Scan(&ti.Name, &ti.TotalSum) == nil {
				topItems = append(topItems, ti)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"supplier_name":  supplierName,
		"total_turnover": totalTurnover,
		"clients":        clients,
		"top_items":      topItems,
	})
}

// 4. Детектор ползучей инфляции (Inflation)
func handleMarketInflation(w http.ResponseWriter, r *http.Request) {
	if !checkMarketAuth(r) { sendMarketError(w, "Unauthorized", http.StatusUnauthorized); return }

	days := 90
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)

	query := `
		WITH first_period AS (
			SELECT product_name_in_invoice, AVG(price_per_base_unit) as avg_start
			FROM purchase_history
			WHERE invoice_date >= NOW() - INTERVAL '1 day' * $1 
			  AND invoice_date < NOW() - INTERVAL '1 day' * ($1 - 30)
			GROUP BY product_name_in_invoice
			HAVING COUNT(*) > 1
		),
		last_period AS (
			SELECT product_name_in_invoice, AVG(price_per_base_unit) as avg_end
			FROM purchase_history
			WHERE invoice_date >= NOW() - INTERVAL '30 days'
			GROUP BY product_name_in_invoice
			HAVING COUNT(*) > 1
		)
		SELECT 
			fp.product_name_in_invoice,
			fp.avg_start,
			lp.avg_end,
			((lp.avg_end - fp.avg_start) / NULLIF(fp.avg_start, 0)) * 100 as inflation_percent
		FROM first_period fp
		JOIN last_period lp ON fp.product_name_in_invoice = lp.product_name_in_invoice
		WHERE lp.avg_end > fp.avg_start * 1.05 
		ORDER BY inflation_percent DESC
		LIMIT 50
	`

	type InflationRecord struct {
		ProductName string  `json:"product_name"`
		PriceStart  float64 `json:"price_start"`
		PriceEnd    float64 `json:"price_end"`
		Inflation   float64 `json:"inflation_percent"`
	}

	results := make([]InflationRecord, 0)
	rows, err := db.Query(query, days)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var rec InflationRecord
			if rows.Scan(&rec.ProductName, &rec.PriceStart, &rec.PriceEnd, &rec.Inflation) == nil {
				results = append(results, rec)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// 5. Консолидированный объем (Volume)
func handleMarketVolume(w http.ResponseWriter, r *http.Request) {
	if !checkMarketAuth(r) { sendMarketError(w, "Unauthorized", http.StatusUnauthorized); return }

	days := 30
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)

	query := `
		SELECT 
			CASE WHEN clean_category = '' THEN 'Без категории' ELSE clean_category END as category,
			product_name_in_invoice,
			SUM(quantity * multiplier) as total_quantity,
			MAX(unit) as base_unit,
			SUM(total_sum) / NULLIF(SUM(quantity * multiplier), 0) as avg_market_price
		FROM purchase_history
		WHERE invoice_date >= NOW() - INTERVAL '1 day' * $1
		GROUP BY category, product_name_in_invoice
		ORDER BY total_quantity DESC
		LIMIT 100
	`

	type VolumeRecord struct {
		Category     string  `json:"category"`
		ProductName  string  `json:"product_name"`
		TotalQty     float64 `json:"total_quantity"`
		Unit         string  `json:"unit"`
		AvgPrice     float64 `json:"avg_market_price"`
	}

	results := make([]VolumeRecord, 0)
	rows, err := db.Query(query, days)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var rec VolumeRecord
			if rows.Scan(&rec.Category, &rec.ProductName, &rec.TotalQty, &rec.Unit, &rec.AvgPrice) == nil {
				results = append(results, rec)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// 6. Калькулятор демпинга (Dumping)
func handleMarketDumping(w http.ResponseWriter, r *http.Request) {
	if !checkMarketAuth(r) { sendMarketError(w, "Unauthorized", http.StatusUnauthorized); return }

	itemName := r.URL.Query().Get("item")
	if itemName == "" {
		sendMarketError(w, "Missing item", http.StatusBadRequest)
		return
	}
	
	targetPriceStr := r.URL.Query().Get("target_price")
	var targetPrice float64
	fmt.Sscanf(targetPriceStr, "%f", &targetPrice)

	days := 30
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)

	query := `
		SELECT 
			c.name as restaurant_name,
			ph.supplier_name,
			ph.product_name_in_invoice,
			ph.price_per_base_unit,
			ph.price_per_base_unit - $3 as potential_profit,
			TO_CHAR(ph.invoice_date, 'YYYY-MM-DD') as invoice_date
		FROM purchase_history ph
		JOIN companies c ON ph.company_id = c.id
		WHERE ph.product_name_in_invoice ILIKE $1 
		  AND ph.invoice_date >= NOW() - INTERVAL '1 day' * $2
		  AND ph.price_per_base_unit > $3
		ORDER BY potential_profit DESC
		LIMIT 100
	`

	type DumpingRecord struct {
		RestaurantName  string  `json:"restaurant_name"`
		SupplierName    string  `json:"supplier_name"`
		ProductName     string  `json:"product_name"`
		ActualPrice     float64 `json:"actual_price"`
		PotentialProfit float64 `json:"potential_profit"`
		InvoiceDate     string  `json:"invoice_date"`
	}

	results := make([]DumpingRecord, 0)
	rows, err := db.Query(query, "%"+itemName+"%", days, targetPrice)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var rec DumpingRecord
			if rows.Scan(&rec.RestaurantName, &rec.SupplierName, &rec.ProductName, &rec.ActualPrice, &rec.PotentialProfit, &rec.InvoiceDate) == nil {
				results = append(results, rec)
			}
		}
	} else {
		sendMarketError(w, "DB error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// 7. Индекс зависимости (Dependency Index)
func handleMarketDependency(w http.ResponseWriter, r *http.Request) {
	if !checkMarketAuth(r) { sendMarketError(w, "Unauthorized", http.StatusUnauthorized); return }

	days := 30
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)

	query := `
		WITH company_totals AS (
			SELECT company_id, SUM(total_sum) as grand_total
			FROM purchase_history
			WHERE invoice_date >= NOW() - INTERVAL '1 day' * $1
			GROUP BY company_id
		),
		supplier_totals AS (
			SELECT company_id, supplier_uuid, MAX(supplier_name) as supplier_name, SUM(total_sum) as supp_total
			FROM purchase_history
			WHERE invoice_date >= NOW() - INTERVAL '1 day' * $1
			GROUP BY company_id, supplier_uuid
		)
		SELECT 
			c.name as restaurant_name,
			st.supplier_name,
			st.supp_total,
			ct.grand_total,
			(st.supp_total / NULLIF(ct.grand_total, 0)) * 100 as dependency_percent
		FROM supplier_totals st
		JOIN company_totals ct ON st.company_id = ct.company_id
		JOIN companies c ON st.company_id = c.id
		WHERE (st.supp_total / NULLIF(ct.grand_total, 0)) > 0.6 
		ORDER BY dependency_percent DESC
	`

	type DependencyRecord struct {
		RestaurantName string  `json:"restaurant_name"`
		SupplierName   string  `json:"supplier_name"`
		SupplierTotal  float64 `json:"supplier_total"`
		GrandTotal     float64 `json:"grand_total"`
		DependencyPct  float64 `json:"dependency_percent"`
	}

	results := make([]DependencyRecord, 0)
	rows, err := db.Query(query, days)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var rec DependencyRecord
			if rows.Scan(&rec.RestaurantName, &rec.SupplierName, &rec.SupplierTotal, &rec.GrandTotal, &rec.DependencyPct) == nil {
				results = append(results, rec)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// 8. Логистический профиль (Logistics)
func handleMarketLogistics(w http.ResponseWriter, r *http.Request) {
	if !checkMarketAuth(r) { sendMarketError(w, "Unauthorized", http.StatusUnauthorized); return }

	days := 30
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)

	query := `
		SELECT 
			MAX(supplier_name) as display_name,
			COUNT(DISTINCT invoice_number) as total_deliveries,
			SUM(total_sum) / NULLIF(COUNT(DISTINCT invoice_number), 0) as avg_invoice_sum,
			SUM(total_sum) as total_turnover
		FROM purchase_history
		WHERE invoice_date >= NOW() - INTERVAL '1 day' * $1
		GROUP BY supplier_uuid
		ORDER BY total_deliveries DESC
		LIMIT 50
	`

	type LogisticsRecord struct {
		SupplierName   string  `json:"supplier_name"`
		Deliveries     int     `json:"total_deliveries"`
		AvgInvoiceSum  float64 `json:"avg_invoice_sum"`
		TotalTurnover  float64 `json:"total_turnover"`
	}

	results := make([]LogisticsRecord, 0)
	rows, err := db.Query(query, days)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var rec LogisticsRecord
			if rows.Scan(&rec.SupplierName, &rec.Deliveries, &rec.AvgInvoiceSum, &rec.TotalTurnover) == nil {
				results = append(results, rec)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// 9. Карта долей поставщиков (Share)
func handleMarketShare(w http.ResponseWriter, r *http.Request) {
	if !checkMarketAuth(r) { sendMarketError(w, "Unauthorized", http.StatusUnauthorized); return }

	days := 30
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)

	query := `
		SELECT 
			MAX(supplier_name) as display_name,
			SUM(total_sum) as market_share
		FROM purchase_history
		WHERE invoice_date >= NOW() - INTERVAL '1 day' * $1
		GROUP BY supplier_uuid
		ORDER BY market_share DESC
		LIMIT 20
	`

	type ShareRecord struct {
		SupplierName string  `json:"supplier_name"`
		MarketShare  float64 `json:"market_share"`
	}

	results := make([]ShareRecord, 0)
	rows, err := db.Query(query, days)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var rec ShareRecord
			if rows.Scan(&rec.SupplierName, &rec.MarketShare) == nil {
				results = append(results, rec)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// 10. Список всех заведений (Для фильтров в Godmode)
func handleMarketCompanies(w http.ResponseWriter, r *http.Request) {
	if !checkMarketAuth(r) { sendMarketError(w, "Unauthorized", http.StatusUnauthorized); return }

	rows, err := db.Query("SELECT id, name FROM companies ORDER BY name ASC")
	if err != nil {
		sendMarketError(w, "DB Error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	list := make([]Company, 0)
	for rows.Next() {
		var c Company
		if rows.Scan(&c.ID, &c.Name) == nil {
			list = append(list, c)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// handleMarketDeals gathers Total Volume and Latest Price for the Deal Builder
func handleMarketDeals(w http.ResponseWriter, r *http.Request) {
	if !checkMarketAuth(r) { sendMarketError(w, "Unauthorized", http.StatusUnauthorized); return }

	searchType := r.URL.Query().Get("type") // "product", "company", "supplier"
	queryStr := r.URL.Query().Get("q")
	days := 30
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)

	if strings.TrimSpace(queryStr) == "" {
		sendMarketError(w, "Empty search query", http.StatusBadRequest)
		return
	}

	rawTokens := strings.Split(queryStr, ",")
	var patterns []string
	for _, t := range rawTokens {
		trimmed := strings.TrimSpace(t)
		if trimmed != "" {
			patterns = append(patterns, "%"+trimmed+"%")
		}
	}

	if len(patterns) == 0 {
		sendMarketError(w, "Empty search query", http.StatusBadRequest)
		return
	}

	// Build the WHERE clause dynamically
	var whereClause string
	if searchType == "company" {
		whereClause = "c.name ILIKE ANY($1)"
	} else if searchType == "supplier" {
		whereClause = "ph.supplier_name ILIKE ANY($1)"
	} else { // default to product
		whereClause = "ph.product_name_in_invoice ILIKE ANY($1)"
	}

	// Use CTEs to get Total Volume and DISTINCT ON to get the strictly latest price
	sqlQuery := fmt.Sprintf(`
		WITH totals AS (
			SELECT 
				ph.company_id, 
				c.name as restaurant_name,
				ph.product_name_in_invoice, 
				MAX(ph.supplier_name) as supplier_name, 
				SUM(ph.quantity * ph.multiplier) as total_volume,
				MAX(ph.unit) as unit
			FROM purchase_history ph
			JOIN companies c ON ph.company_id = c.id
			WHERE ph.invoice_date >= NOW() - INTERVAL '1 day' * $2
			  AND %s
			GROUP BY ph.company_id, c.name, ph.product_name_in_invoice
		),
		latest_prices AS (
			SELECT DISTINCT ON (company_id, product_name_in_invoice) 
				company_id, 
				product_name_in_invoice, 
				price_per_base_unit as latest_price, 
				TO_CHAR(invoice_date, 'YYYY-MM-DD') as latest_date
			FROM purchase_history
			WHERE invoice_date >= NOW() - INTERVAL '1 day' * $2
			ORDER BY company_id, product_name_in_invoice, invoice_date DESC
		)
		SELECT 
			t.restaurant_name,
			t.supplier_name,
			t.product_name_in_invoice,
			t.total_volume,
			t.unit,
			lp.latest_price,
			lp.latest_date
		FROM totals t
		JOIN latest_prices lp ON t.company_id = lp.company_id AND t.product_name_in_invoice = lp.product_name_in_invoice
		ORDER BY t.total_volume DESC
		LIMIT 200
	`, whereClause)

	type DealRecord struct {
		RestaurantName string  `json:"restaurant_name"`
		SupplierName   string  `json:"supplier_name"`
		ProductName    string  `json:"product_name"`
		TotalVolume    float64 `json:"total_volume"`
		Unit           string  `json:"unit"`
		LatestPrice    float64 `json:"latest_price"`
		LatestDate     string  `json:"latest_date"`
	}

	results := make([]DealRecord, 0)
	rows, err := db.Query(sqlQuery, pq.Array(patterns), days)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var rec DealRecord
			if rows.Scan(&rec.RestaurantName, &rec.SupplierName, &rec.ProductName, &rec.TotalVolume, &rec.Unit, &rec.LatestPrice, &rec.LatestDate) == nil {
				results = append(results, rec)
			}
		}
	} else {
		sendMarketError(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

