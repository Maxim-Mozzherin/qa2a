package engine

import (
	"fmt"
	"math"
	"sort"
)

type CohortConfig struct {
	SmallMaxThreshold  float64 `json:"small_max_threshold"`  // по умолчанию 50 кг
	MediumMaxThreshold float64 `json:"medium_max_threshold"` // по умолчанию 200 кг
}

func DefaultCohortConfig() CohortConfig {
	return CohortConfig{
		SmallMaxThreshold:  50.0,
		MediumMaxThreshold: 200.0,
	}
}

func (c CohortConfig) GetTier(monthlyVolume float64) string {
	if monthlyVolume <= c.SmallMaxThreshold {
		return "Small (< " + formatFloat(c.SmallMaxThreshold) + " кг)"
	}
	if monthlyVolume <= c.MediumMaxThreshold {
		return "Medium (" + formatFloat(c.SmallMaxThreshold) + " - " + formatFloat(c.MediumMaxThreshold) + " кг)"
	}
	return "Large (> " + formatFloat(c.MediumMaxThreshold) + " кг)"
}

func formatFloat(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

type MarketPriceStats struct {
	P25    float64
	Median float64
	P75    float64
	Min    float64
	Max    float64
	Avg    float64
	Count  int
}

func CalculateStats(prices []float64) MarketPriceStats {
	if len(prices) == 0 {
		return MarketPriceStats{}
	}
	sort.Float64s(prices)
	n := len(prices)

	minVal := prices[0]
	maxVal := prices[n-1]
	medVal := percentile(prices, 0.50)
	p25Val := percentile(prices, 0.25)
	p75Val := percentile(prices, 0.75)

	var sum float64
	for _, p := range prices {
		sum += p
	}
	avgVal := sum / float64(n)

	return MarketPriceStats{
		Min:    minVal,
		Max:    maxVal,
		Median: medVal,
		P25:    p25Val,
		P75:    p75Val,
		Avg:    math.Round(avgVal*100) / 100,
		Count:  n,
	}
}

func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	pos := p * float64(n-1)
	low := int(math.Floor(pos))
	high := int(math.Ceil(pos))
	if low == high {
		return sorted[low]
	}
	weight := pos - float64(low)
	return sorted[low]*(1-weight) + sorted[high]*weight
}

type MarketPeerDetail struct {
	RestaurantID   int     `json:"restaurant_id"`
	RestaurantName string  `json:"restaurant_name"`
	City           string  `json:"city"`
	SupplierName   string  `json:"supplier_name"`
	PricePerUnit   float64 `json:"price_per_unit"`
	MonthlyVolume  float64 `json:"monthly_volume"`
	CohortTier     string  `json:"cohort_tier"`
	LastDocDate    string  `json:"last_doc_date"`
}

type ItemSupplierDetail struct {
	SupplierName          string  `json:"supplier_name"`
	Volume                float64 `json:"volume"`
	MonthlyVolume         float64 `json:"monthly_volume"`
	TotalSum              float64 `json:"total_sum"`
	AvgPrice              float64 `json:"avg_price"`
	LastPrice             float64 `json:"last_price"`
	LastDocDate           string  `json:"last_doc_date"`
	InvoicesCount         int     `json:"invoices_count"`
	SharePct              float64 `json:"share_pct"`
	MarketMinPrice        float64 `json:"market_min_price"`
	SameSupplierMarketMin float64 `json:"same_supplier_market_min,omitempty"`
	SameSupplierPeerName  string  `json:"same_supplier_peer_name,omitempty"`
	SupplierPriceSpreadRub float64 `json:"supplier_price_spread_rub,omitempty"`
	SupplierPriceSpreadPct float64 `json:"supplier_price_spread_pct,omitempty"`
	PriceDiffPercent      float64 `json:"price_diff_percent"`
	MonthlyOverpaymentRub float64 `json:"monthly_overpayment_rub"`
	IsOverpay             bool    `json:"is_overpay"`
}

type AuditedItem struct {
	Rank                  int                  `json:"rank"`
	ProductName           string               `json:"product_name"`
	DetectedBrand         string               `json:"detected_brand,omitempty"`
	CanonicalCategory     string               `json:"canonical_category"`
	IsCommodity           bool                 `json:"is_commodity"`
	Unit                  string               `json:"unit"`
	MonthlyVolume         float64              `json:"monthly_volume"`
	CohortTier            string               `json:"cohort_tier"`
	RestaurantPrice       float64              `json:"restaurant_price"`
	LastPrice             float64              `json:"last_price"`
	LastDocDate           string               `json:"last_doc_date"`
	MarketMedianPrice     float64              `json:"market_median_price"`
	MarketAvgPrice        float64              `json:"market_avg_price"`
	MarketMinPrice        float64              `json:"market_min_price"`
	MarketP25Price        float64              `json:"market_p25_price"`
	MarketP75Price        float64              `json:"market_p75_price"`
	PriceDiffPercent      float64              `json:"price_diff_percent"`
	MonthlyOverpaymentRub float64              `json:"monthly_overpayment_rub"`
	YearlyOverpaymentRub  float64              `json:"yearly_overpayment_rub"`

	// Двойная оценка переплаты: к средней рынка и к минимуму когорты
	MonthlyOverpayVsAvgRub float64 `json:"monthly_overpay_vs_avg_rub"`
	YearlyOverpayVsAvgRub  float64 `json:"yearly_overpay_vs_avg_rub"`
	PriceDiffPercentVsAvg  float64 `json:"price_diff_percent_vs_avg"`

	MonthlyOverpayVsMinRub float64 `json:"monthly_overpay_vs_min_rub"`
	YearlyOverpayVsMinRub  float64 `json:"yearly_overpay_vs_min_rub"`
	PriceDiffPercentVsMin  float64 `json:"price_diff_percent_vs_min"`

	// Аналитика тренда цены (по последней накладной)
	PriceTrend            string `json:"price_trend"`               // "down" | "up" | "stable"
	PriceTrendNote         string `json:"price_trend_note"`          // Текстовое пояснение динамики
	IsMarketAlignedLatest bool   `json:"is_market_aligned_latest"`  // Выровнена ли последняя цена с рынком

	FoodCostImpactPoints  float64              `json:"food_cost_impact_points"`
	SupplierName          string               `json:"supplier_name,omitempty"`
	MainSupplier          string               `json:"main_supplier"`
	MainSupplierShare     float64              `json:"main_supplier_share"`
	SuppliersCount        int                  `json:"suppliers_count"`
	SuppliersBreakdown    []ItemSupplierDetail `json:"suppliers_breakdown,omitempty"`
	IsOverpay             bool                 `json:"is_overpay"`
	MarketPeers           []MarketPeerDetail   `json:"market_peers,omitempty"`
}

type ExecutiveFinancialAudit struct {
	RestaurantID           int           `json:"restaurant_id"`
	RestaurantName         string        `json:"restaurant_name"`
	MonthlyRevenue         float64       `json:"monthly_revenue"`
	MonthlyPurchasesRub    float64       `json:"monthly_purchases_rub"`      // Реальный месячный объем закупок
	TotalMonthlyOverpayRub float64       `json:"total_monthly_overpay_rub"`  // Суммарная переплата в месяц
	TotalYearlyOverpayRub  float64       `json:"total_yearly_overpay_rub"`   // Суммарная переплата в год (x12)
	OverpayBudgetPercent   float64       `json:"overpay_budget_percent"`     // Доля переплаты в закупках (%)

	// Суммарные агрегаты по двум планкам:
	TotalMonthlyOverpayVsAvgRub float64 `json:"total_monthly_overpay_vs_avg_rub"` // Переплата относительно средней рынка
	TotalYearlyOverpayVsAvgRub  float64 `json:"total_yearly_overpay_vs_avg_rub"`

	TotalMonthlyOverpayVsMinRub float64 `json:"total_monthly_overpay_vs_min_rub"` // Максимальный потенциал экономии к минимуму когорты
	TotalYearlyOverpayVsMinRub  float64 `json:"total_yearly_overpay_vs_min_rub"`
	OverpayVsMinBudgetPercent   float64 `json:"overpay_vs_min_budget_percent"`

	FoodCostImpactTotalPP  float64       `json:"food_cost_impact_total_pp"`  // Влияние на фудкост (если выручка известна)
	SupplierHHI            float64       `json:"supplier_hhi"`               // Индекс концентрации HHI (0-10000)
	SupplierHHIStatus      string        `json:"supplier_hhi_status"`        // Статус концентрации
	OtherRestaurantsCount  int           `json:"other_restaurants_count"`    // Количество независимых ресторанов в базе

	// Внутрипоставочный ценовой спред (SupplierPriceSpread)
	TotalSupplierPriceSpreadRub float64 `json:"total_supplier_price_spread_rub"`

	// Издержки оперативных (розничных) закупок (OffContractSpend)
	OffContractSpendMonthlyRub float64 `json:"off_contract_spend_monthly_rub"`
	OffContractSpendSharePct   float64 `json:"off_contract_spend_share_pct"`
	OffContractAvgMarkupPct    float64 `json:"off_contract_avg_markup_pct"`

	// Freemium Masking & Paywall метрики (к средней и к минимуму когорты)
	RevealedItemsOverpayRub      float64 `json:"revealed_items_overpay_rub"`
	HiddenItemsOverpayRub        float64 `json:"hidden_items_overpay_rub"`
	RevealedItemsOverpayVsMinRub float64 `json:"revealed_items_overpay_vs_min_rub"`
	HiddenItemsOverpayVsMinRub   float64 `json:"hidden_items_overpay_vs_min_rub"`
	HiddenPositionsCount         int     `json:"hidden_positions_count"`
	UnblurredPositionsCount      int     `json:"unblurred_positions_count"`
	AuditMode                    string  `json:"audit_mode"` // "demo" | "full"

	// Внутренняя динамика закупочных цен (инфляция за период)
	TopPriceHikes         []PriceInflationItem `json:"top_price_hikes,omitempty"`
	TotalInflationLossRub float64              `json:"total_inflation_loss_rub"`

	// Внутренние списания (потери кухни и бара)
	TopWriteoffsByCost     []WriteoffLossItem `json:"top_writeoffs_by_cost,omitempty"`
	TopWriteoffsByAmount   []WriteoffLossItem `json:"top_writeoffs_by_amount,omitempty"`
	TotalWriteoffsCostRub  float64            `json:"total_writeoffs_cost_rub"`
	WriteoffsSpendSharePct float64            `json:"writeoffs_spend_share_pct"`

	// AI Заключение управленческого аудитора
	AuditorSummary string `json:"auditor_summary,omitempty"`

	ItemsAudit             []AuditedItem `json:"items_audit"`
	CohortConfig           CohortConfig  `json:"cohort_config"`
	TotalAnalyzedPositions int           `json:"total_analyzed_positions"`
	TotalInvoicesCount     int           `json:"total_invoices_count"`
}

type PriceInflationItem struct {
	Rank                      int     `json:"rank"`
	ProductName               string  `json:"product_name"`
	SupplierName              string  `json:"supplier_name"`
	Unit                      string  `json:"unit"`
	FirstPrice                float64 `json:"first_price"`
	FirstDocDate              string  `json:"first_doc_date"`
	LastPrice                 float64 `json:"last_price"`
	LastDocDate               string  `json:"last_doc_date"`
	PriceDiffRub              float64 `json:"price_diff_rub"`
	PriceDiffPercent          float64 `json:"price_diff_percent"`
	PeriodVolume              float64 `json:"period_volume"`
	EstimatedInflationLossRub float64 `json:"estimated_inflation_loss_rub"`
}

type WriteoffLossItem struct {
	Rank         int     `json:"rank"`
	ProductName  string  `json:"product_name"`
	Unit         string  `json:"unit"`
	TotalAmount  float64 `json:"total_amount"`
	TotalCostRub float64 `json:"total_cost_rub"`
	SharePct     float64 `json:"share_pct"`
	Reason       string  `json:"reason,omitempty"`
}

// CalculateExecutiveAudit производит полный финансово-управленческий аудит переплат ресторана
func CalculateExecutiveAudit(
	restID int,
	restName string,
	monthlyRevenue float64,
	monthlyPurchases float64,
	candidateItems []AuditedItem,
	supplierSpendMap map[string]float64,
	cohortCfg CohortConfig,
	invoicesCount int,
	otherRestaurantsCount int,
) ExecutiveFinancialAudit {
	// Сортируем: сначала переплаты (по убыванию суммы), затем остальные
	sort.Slice(candidateItems, func(i, j int) bool {
		if candidateItems[i].IsOverpay != candidateItems[j].IsOverpay {
			return candidateItems[i].IsOverpay
		}
		if candidateItems[i].MonthlyOverpaymentRub != candidateItems[j].MonthlyOverpaymentRub {
			return candidateItems[i].MonthlyOverpaymentRub > candidateItems[j].MonthlyOverpaymentRub
		}
		return candidateItems[i].MonthlyVolume > candidateItems[j].MonthlyVolume
	})

	var totalOverpay float64
	var totalOverpayVsAvg float64
	var totalOverpayVsMin float64
	var finalAuditItems []AuditedItem
	rank := 1

	for i := range candidateItems {
		item := candidateItems[i]
		if item.IsOverpay {
			item.Rank = rank
			rank++
			item.YearlyOverpaymentRub = math.Round(item.MonthlyOverpaymentRub * 12)
			item.YearlyOverpayVsAvgRub = math.Round(item.MonthlyOverpayVsAvgRub * 12)
			item.YearlyOverpayVsMinRub = math.Round(item.MonthlyOverpayVsMinRub * 12)
			if monthlyRevenue > 0 {
				item.FoodCostImpactPoints = math.Round((item.MonthlyOverpaymentRub/monthlyRevenue)*10000) / 100
			}
			totalOverpay += item.MonthlyOverpaymentRub
			totalOverpayVsAvg += item.MonthlyOverpayVsAvgRub
			totalOverpayVsMin += item.MonthlyOverpayVsMinRub
		} else {
			item.Rank = rank
			rank++
			item.MonthlyOverpaymentRub = 0
			item.YearlyOverpaymentRub = 0
			item.MonthlyOverpayVsAvgRub = 0
			item.YearlyOverpayVsAvgRub = 0
			item.MonthlyOverpayVsMinRub = 0
			item.YearlyOverpayVsMinRub = 0
			item.FoodCostImpactPoints = 0
		}

		finalAuditItems = append(finalAuditItems, item)
	}

	// Доля переплаты в реальном бюджете закупок
	var overpayBudgetPct float64
	if monthlyPurchases > 0 {
		overpayBudgetPct = math.Round((totalOverpay/monthlyPurchases)*10000) / 100
	}

	var overpayVsMinBudgetPct float64
	if monthlyPurchases > 0 {
		overpayVsMinBudgetPct = math.Round((totalOverpayVsMin/monthlyPurchases)*10000) / 100
	}

	// Влияние на фудкост (только если известна выручка)
	var foodCostImpactTotal float64
	if monthlyRevenue > 0 {
		foodCostImpactTotal = math.Round((totalOverpay/monthlyRevenue)*10000) / 100
	}

	// Индекс концентрации Херфиндаля-Хиршмана (HHI)
	var totalSpend float64
	for _, spend := range supplierSpendMap {
		totalSpend += spend
	}

	var hhi float64
	if totalSpend > 0 {
		for _, spend := range supplierSpendMap {
			share := (spend / totalSpend) * 100
			hhi += share * share
		}
	}
	hhi = math.Round(hhi)

	hhiStatus := "Здоровая диверсификация"
	if hhi > 2500 {
		hhiStatus = "Критическая зависимость от монопольного поставщика"
	} else if hhi > 1500 {
		hhiStatus = "Умеренная концентрация поставок"
	}

	var totalSpread float64
	for _, item := range finalAuditItems {
		for _, s := range item.SuppliersBreakdown {
			totalSpread += s.SupplierPriceSpreadRub
		}
	}

	return ExecutiveFinancialAudit{
		RestaurantID:                restID,
		RestaurantName:              restName,
		MonthlyRevenue:              monthlyRevenue,
		MonthlyPurchasesRub:         math.Round(monthlyPurchases),
		TotalMonthlyOverpayRub:      math.Round(totalOverpay),
		TotalYearlyOverpayRub:       math.Round(totalOverpay * 12),
		OverpayBudgetPercent:        overpayBudgetPct,
		TotalMonthlyOverpayVsAvgRub: math.Round(totalOverpayVsAvg),
		TotalYearlyOverpayVsAvgRub:  math.Round(totalOverpayVsAvg * 12),
		TotalMonthlyOverpayVsMinRub: math.Round(totalOverpayVsMin),
		TotalYearlyOverpayVsMinRub:  math.Round(totalOverpayVsMin * 12),
		OverpayVsMinBudgetPercent:   overpayVsMinBudgetPct,
		FoodCostImpactTotalPP:       foodCostImpactTotal,
		SupplierHHI:                 hhi,
		SupplierHHIStatus:           hhiStatus,
		OtherRestaurantsCount:       otherRestaurantsCount,
		TotalSupplierPriceSpreadRub: math.Round(totalSpread),
		ItemsAudit:                  finalAuditItems,
		CohortConfig:                cohortCfg,
		TotalAnalyzedPositions:      len(candidateItems),
		TotalInvoicesCount:          invoicesCount,
	}
}

// ApplyFreemiumMask applies paywall obfuscation to items beyond unblurCount if mode is "demo" (or default).
// In "full" mode, reveals all items.
func ApplyFreemiumMask(audit *ExecutiveFinancialAudit, mode string, unblurCount int) {
	if mode == "full" {
		audit.AuditMode = "full"
		audit.UnblurredPositionsCount = len(audit.ItemsAudit)
		var totalOverpay float64
		var totalOverpayVsMin float64
		for _, item := range audit.ItemsAudit {
			totalOverpay += item.MonthlyOverpaymentRub
			totalOverpayVsMin += item.MonthlyOverpayVsMinRub
		}
		audit.RevealedItemsOverpayRub = math.Round(totalOverpay)
		audit.RevealedItemsOverpayVsMinRub = math.Round(totalOverpayVsMin)
		audit.HiddenItemsOverpayRub = 0
		audit.HiddenItemsOverpayVsMinRub = 0
		audit.HiddenPositionsCount = 0
		return
	}

	if unblurCount < 0 {
		unblurCount = 3
	}

	audit.AuditMode = "demo"
	audit.UnblurredPositionsCount = unblurCount
	var revealedOverpay float64
	var hiddenOverpay float64
	var revealedOverpayVsMin float64
	var hiddenOverpayVsMin float64
	var hiddenCount int

	for i := range audit.ItemsAudit {
		item := &audit.ItemsAudit[i]
		if item.Rank <= unblurCount {
			revealedOverpay += item.MonthlyOverpaymentRub
			revealedOverpayVsMin += item.MonthlyOverpayVsMinRub
		} else {
			hiddenOverpay += item.MonthlyOverpaymentRub
			hiddenOverpayVsMin += item.MonthlyOverpayVsMinRub
			hiddenCount++
			item.ProductName = "[Скрыто в демо-версии]"
			item.DetectedBrand = ""
			item.SupplierName = "Скрытый поставщик"
			item.MainSupplier = "Скрытый поставщик"
			item.SuppliersBreakdown = nil
			item.MarketPeers = nil
			item.PriceTrendNote = "Доступно в полном отчете"
		}
	}

	for i := range audit.TopPriceHikes {
		if audit.TopPriceHikes[i].Rank > unblurCount {
			audit.TopPriceHikes[i].ProductName = "[Скрыто в демо-версии]"
			audit.TopPriceHikes[i].SupplierName = "Скрытый поставщик"
		}
	}

	for i := range audit.TopWriteoffsByCost {
		if audit.TopWriteoffsByCost[i].Rank > unblurCount {
			audit.TopWriteoffsByCost[i].ProductName = "[Скрыто в демо-версии]"
			audit.TopWriteoffsByCost[i].Reason = "Скрыто в демо"
		}
	}

	for i := range audit.TopWriteoffsByAmount {
		if audit.TopWriteoffsByAmount[i].Rank > unblurCount {
			audit.TopWriteoffsByAmount[i].ProductName = "[Скрыто в демо-версии]"
			audit.TopWriteoffsByAmount[i].Reason = "Скрыто в демо"
		}
	}

	audit.RevealedItemsOverpayRub = math.Round(revealedOverpay)
	audit.HiddenItemsOverpayRub = math.Round(hiddenOverpay)
	audit.RevealedItemsOverpayVsMinRub = math.Round(revealedOverpayVsMin)
	audit.HiddenItemsOverpayVsMinRub = math.Round(hiddenOverpayVsMin)
	audit.HiddenPositionsCount = hiddenCount
}

