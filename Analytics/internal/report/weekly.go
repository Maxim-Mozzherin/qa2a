package report

import (
	"database/sql"
	"fmt"
	"html"
	"strings"
	"time"
)

// PriceSpikeItem позиция со скачком цены
type PriceSpikeItem struct {
	ProductName string
	OldPrice    float64
	NewPrice    float64
	GrowthPct   float64
	Supplier    string
	DocDate     string
}

// OverpaidItem позиция с переплатой относительно рынка
type OverpaidItem struct {
	ProductName    string
	RestaurantPrice float64
	MarketAvgPrice float64
	Volume         float64
	Unit           string
	OverpayRub     float64
	Supplier       string
}

// WeeklyReportData структурированные данные еженедельного отчета
type WeeklyReportData struct {
	RestaurantID   int
	RestaurantName string
	StartDate      string
	EndDate        string
	WeekNumber     int
	Spikes         []PriceSpikeItem
	Overpaid       []OverpaidItem
	TotalOverpay   float64
	InvoicesCount  int
}

// BuildWeeklyReport производит расчет показателей за 7-дневный интервал
func BuildWeeklyReport(db *sql.DB, restID int, from, to string, weekNum int) (*WeeklyReportData, error) {
	var restName, restHost string
	err := db.QueryRow(`SELECT name, iiko_host FROM analytics_restaurants WHERE id = $1`, restID).Scan(&restName, &restHost)
	if err != nil {
		return nil, fmt.Errorf("заведение не найдено: %w", err)
	}

	var invoicesCount int
	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT id) 
		FROM analytics_invoices 
		WHERE restaurant_id = $1 AND doc_date >= $2 AND doc_date <= $3`,
		restID, from, to).Scan(&invoicesCount)

	rep := &WeeklyReportData{
		RestaurantID:   restID,
		RestaurantName: restName,
		StartDate:      from,
		EndDate:        to,
		WeekNumber:     weekNum,
		InvoicesCount:  invoicesCount,
	}

	// 1. ТОП-10 СКАЧКОВ ЦЕН (%)
	// Сравниваем цены текущей недели с базовой ценой (предшествующие 60 дней)
	spikeRows, err := db.Query(`
		WITH current_week_items AS (
			SELECT 
				product_name,
				supplier_product_name,
				MAX(doc_date) as last_date,
				(ARRAY_AGG(price_per_unit ORDER BY doc_date DESC))[1] as current_price,
				(ARRAY_AGG(COALESCE(NULLIF(supplier_product_name,''), product_name) ORDER BY doc_date DESC))[1] as display_name
			FROM analytics_invoice_items
			WHERE restaurant_id = $1 AND doc_date >= $2 AND doc_date <= $3 AND price_per_unit > 0
			GROUP BY product_name, supplier_product_name
		),
		previous_baseline AS (
			SELECT 
				product_name,
				AVG(price_per_unit) as baseline_price
			FROM analytics_invoice_items
			WHERE restaurant_id = $1 AND doc_date < $2 AND doc_date >= ($2::date - interval '60 days') AND price_per_unit > 0
			GROUP BY product_name
		)
		SELECT 
			c.display_name,
			p.baseline_price,
			c.current_price,
			((c.current_price - p.baseline_price) / p.baseline_price) * 100.0 as growth_pct,
			COALESCE((
				SELECT inv.supplier_name 
				FROM analytics_invoice_items itm
				JOIN analytics_invoices inv ON inv.id = itm.invoice_id
				WHERE itm.restaurant_id = $1 AND itm.doc_date = c.last_date AND itm.product_name = c.product_name
				LIMIT 1
			), '') as supplier_name,
			c.last_date
		FROM current_week_items c
		JOIN previous_baseline p ON p.product_name = c.product_name
		WHERE c.current_price > p.baseline_price * 1.05
		ORDER BY growth_pct DESC
		LIMIT 10
	`, restID, from, to)

	if err == nil {
		defer spikeRows.Close()
		for spikeRows.Next() {
			var sp PriceSpikeItem
			var lastDate time.Time
			if errScan := spikeRows.Scan(&sp.ProductName, &sp.OldPrice, &sp.NewPrice, &sp.GrowthPct, &sp.Supplier, &lastDate); errScan == nil {
				sp.DocDate = lastDate.Format("02.01")
				rep.Spikes = append(rep.Spikes, sp)
			}
		}
	}

	// 2. ТОП-10 ПЕРЕПЛАТ (К РЫНОЧНОЙ СРЕДНЕЙ ЗА НЕДЕЛЮ)
	overpayRows, err := db.Query(`
		WITH current_week_agg AS (
			SELECT 
				itm.product_name,
				COALESCE(NULLIF(itm.supplier_product_name, ''), itm.product_name) as display_name,
				MAX(itm.unit) as unit,
				SUM(itm.quantity) as week_qty,
				SUM(itm.total_sum) / NULLIF(SUM(itm.quantity), 0) as rest_avg_price,
				(ARRAY_AGG(inv.supplier_name ORDER BY itm.doc_date DESC))[1] as main_supplier
			FROM analytics_invoice_items itm
			JOIN analytics_invoices inv ON inv.id = itm.invoice_id
			WHERE itm.restaurant_id = $1 AND itm.doc_date >= $2 AND itm.doc_date <= $3
			GROUP BY itm.product_name, itm.supplier_product_name
			HAVING SUM(itm.quantity) > 0
		),
		market_benchmarks AS (
			SELECT 
				itm.product_name,
				AVG(itm.price_per_unit) as market_avg_price
			FROM analytics_invoice_items itm
			JOIN analytics_restaurants r ON r.id = itm.restaurant_id
			WHERE r.iiko_host != $4
			  AND r.is_active = true
			  AND (r.company_id != 10 OR r.company_id IS NULL)
			  AND itm.doc_date >= ($3::date - interval '60 days')
			  AND itm.price_per_unit > 0
			GROUP BY itm.product_name
		)
		SELECT 
			c.display_name,
			c.rest_avg_price,
			m.market_avg_price,
			c.week_qty,
			c.unit,
			(c.rest_avg_price - m.market_avg_price) * c.week_qty as week_overpay_rub,
			COALESCE(c.main_supplier, '') as supplier
		FROM current_week_agg c
		JOIN market_benchmarks m ON m.product_name = c.product_name
		WHERE c.rest_avg_price > m.market_avg_price
		ORDER BY week_overpay_rub DESC
		LIMIT 10
	`, restID, from, to, restHost)

	if err == nil {
		defer overpayRows.Close()
		for overpayRows.Next() {
			var op OverpaidItem
			if errScan := overpayRows.Scan(
				&op.ProductName, &op.RestaurantPrice, &op.MarketAvgPrice,
				&op.Volume, &op.Unit, &op.OverpayRub, &op.Supplier,
			); errScan == nil {
				rep.Overpaid = append(rep.Overpaid, op)
				rep.TotalOverpay += op.OverpayRub
			}
		}
	}

	return rep, nil
}

// FormatTelegramWeeklyHTML формирует аккуратное читаемое текстовое сообщение с абзацами без эмодзи
func FormatTelegramWeeklyHTML(rep *WeeklyReportData) string {
	var sb strings.Builder

	startDateStr := rep.StartDate
	endDateStr := rep.EndDate
	if pt, err := time.Parse("2006-01-02", rep.StartDate); err == nil {
		startDateStr = pt.Format("02.01.2006")
	}
	if pt, err := time.Parse("2006-01-02", rep.EndDate); err == nil {
		endDateStr = pt.Format("02.01.2006")
	}

	sb.WriteString(fmt.Sprintf("<b>Еженедельный аудит закупок: %s</b>\n\n", html.EscapeString(rep.RestaurantName)))
	sb.WriteString(fmt.Sprintf("Период: %s — %s (Неделя #%d)\n", startDateStr, endDateStr, rep.WeekNumber))
	sb.WriteString(fmt.Sprintf("Обработано накладных: %d\n\n", rep.InvoicesCount))

	// 1. Топ-10 скачков цен
	sb.WriteString("<b>Топ-10 продуктов с максимальным ростом цен:</b>\n")
	sb.WriteString("Сравнение цен последней поставки со средней ценой за предыдущий период.\n\n")

	if len(rep.Spikes) == 0 {
		sb.WriteString("Заметных скачков цен (более 5%) за прошедшую неделю не зафиксировано.\n\n")
	} else {
		for i, sp := range rep.Spikes {
			supplierInfo := ""
			if sp.Supplier != "" {
				supplierInfo = fmt.Sprintf("\nПоставщик: %s", html.EscapeString(sp.Supplier))
			}
			sb.WriteString(fmt.Sprintf(
				"%d. <b>%s</b>: рост на <b>+%.1f%%</b>\nБыло: %.2f ₽, стало: <b>%.2f ₽</b>%s\n\n",
				i+1,
				html.EscapeString(sp.ProductName),
				sp.GrowthPct,
				sp.OldPrice,
				sp.NewPrice,
				supplierInfo,
			))
		}
	}

	// 2. Топ-10 переплат к рынку
	sb.WriteString("<b>Топ-10 продуктов с переплатой к средней рынка:</b>\n")
	sb.WriteString("Сравнение цен заведения со средневзвешенными ценами других заведений города.\n\n")

	if len(rep.Overpaid) == 0 {
		sb.WriteString("Закупочные цены заведения находятся в пределах среднерыночных ориентиров.\n\n")
	} else {
		for i, op := range rep.Overpaid {
			supplierInfo := ""
			if op.Supplier != "" {
				supplierInfo = fmt.Sprintf("\nПоставщик: %s", html.EscapeString(op.Supplier))
			}
			sb.WriteString(fmt.Sprintf(
				"%d. <b>%s</b>: расчетная переплата <b>+%.0f ₽</b>\nВаша цена: %.2f ₽, рынок: %.2f ₽ (объем: %.1f %s)%s\n\n",
				i+1,
				html.EscapeString(op.ProductName),
				op.OverpayRub,
				op.RestaurantPrice,
				op.MarketAvgPrice,
				op.Volume,
				html.EscapeString(op.Unit),
				supplierInfo,
			))
		}
		sb.WriteString(fmt.Sprintf("<b>Итого переплата за неделю по Топ-10: %.0f ₽</b>\n\n", rep.TotalOverpay))
	}

	sb.WriteString("Отчет сформирован сервисом ресторанной аналитики QA2A.")
	return sb.String()
}
