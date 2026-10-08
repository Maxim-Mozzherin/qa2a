package audit

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"analytics_service/internal/engine"
	"analytics_service/internal/llm"
)

func cleanSupplierName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, `"'«»`)
	return name
}

// ExecuteAudit выполняет полный расчет экономического управленческого аудита ресторана
func ExecuteAudit(ctx context.Context, db *sql.DB, llmClient *llm.Client, restID int, days int, cohortCfg engine.CohortConfig) (*engine.ExecutiveFinancialAudit, error) {
	if days <= 0 {
		days = 30
	}

	// 1. Получаем данные заведения
	var restName string
	var restHost string
	var monthlyRev float64
	var restCity string
	err := db.QueryRowContext(ctx, `
		SELECT name, iiko_host, monthly_revenue, COALESCE(city, 'Пермь')
		FROM analytics_restaurants 
		WHERE id = $1`, restID).Scan(&restName, &restHost, &monthlyRev, &restCity)
	if err != nil {
		return nil, fmt.Errorf("заведение #%d не найдено: %w", restID, err)
	}

	startDate := time.Now().AddDate(0, 0, -days).Format("2006-01-02")

	// 2. Количество накладных, сумма закупок за период и распределение трат по поставщикам для HHI
	var invoicesCount int
	var totalSpendPeriod float64
	_ = db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT id), COALESCE(SUM(total_sum), 0) 
		FROM analytics_invoices 
		WHERE restaurant_id = $1 AND doc_date >= $2`, restID, startDate).Scan(&invoicesCount, &totalSpendPeriod)

	monthlyPurchases := (totalSpendPeriod / float64(days)) * 30.0

	// Количество независимых заведений на рынке
	var otherRestaurantsCount int
	_ = db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT r.id) 
		FROM analytics_restaurants r
		JOIN analytics_invoices inv ON inv.restaurant_id = r.id
		WHERE r.iiko_host != $1 
		  AND r.is_active = true 
		  AND (r.company_id != 10 OR r.company_id IS NULL)`, restHost).Scan(&otherRestaurantsCount)

	supplierSpendMap := make(map[string]float64)
	rowsSuppliers, err := db.QueryContext(ctx, `
		SELECT supplier_name, SUM(total_sum)
		FROM analytics_invoices
		WHERE restaurant_id = $1 AND doc_date >= $2 AND supplier_name != ''
		GROUP BY supplier_name`, restID, startDate)
	if err == nil {
		for rowsSuppliers.Next() {
			var sName string
			var sum float64
			if err := rowsSuppliers.Scan(&sName, &sum); err == nil {
				supplierSpendMap[sName] = sum
			}
		}
		rowsSuppliers.Close()
	}

	// 3. Собираем сырьевые позиции данного заведения (исключая хозтовары, инвентарь и чеки розницы)
	rowsItems, err := db.QueryContext(ctx, `
		SELECT 
			it.product_name,
			it.detected_brand,
			it.canonical_category,
			it.is_commodity,
			COALESCE(NULLIF(it.unit, ''), 'кг') as unit,
			COALESCE(NULLIF(TRIM(inv.supplier_name), ''), 'Не указан') as supplier_name,
			it.price_per_unit,
			it.quantity,
			it.total_sum,
			to_char(it.doc_date, 'YYYY-MM-DD') as doc_date
		FROM analytics_invoice_items it
		LEFT JOIN analytics_invoices inv ON inv.id = it.invoice_id
		WHERE it.restaurant_id = $1 
		  AND it.doc_date >= $2
		  AND it.product_name NOT ILIKE '%хоз%'
		  AND it.product_name NOT ILIKE '%инвентар%'
		  AND it.product_name NOT ILIKE '%чек %'
		  AND it.product_name NOT ILIKE '%посуда%'
		  AND it.product_name NOT ILIKE '%авансов%'
		  AND it.product_name NOT ILIKE '%расходн%'
		  AND it.quantity > 0
		  AND it.price_per_unit > 0
		ORDER BY it.product_name, it.doc_date DESC`, restID, startDate)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения позиций накладных: %w", err)
	}
	defer rowsItems.Close()

	type rawSupItem struct {
		name          string
		totalQty      float64
		totalSum      float64
		lastPrice     float64
		lastDocDate   string
		invoicesCount int
	}

	type rawInvoiceRecord struct {
		price    float64
		qty      float64
		docDate  string
		supplier string
	}

	type rawProductGroup struct {
		name         string
		brand        string
		category     string
		isCommodity  bool
		unit         string
		totalQty     float64
		totalSum     float64
		lastPrice    float64
		lastDocDate  string
		firstPrice   float64
		firstDocDate string
		suppliers    map[string]*rawSupItem
		history      []rawInvoiceRecord
	}

	productMap := make(map[string]*rawProductGroup)
	var orderedProductNames []string

	for rowsItems.Next() {
		var pName, brand, cat, unit, sName, docDate string
		var isComm bool
		var price, qty, sum float64
		if err := rowsItems.Scan(&pName, &brand, &cat, &isComm, &unit, &sName, &price, &qty, &sum, &docDate); err == nil {
			cleanSup := cleanSupplierName(sName)
			pg, exists := productMap[pName]
			if !exists {
				pg = &rawProductGroup{
					name:        pName,
					brand:       brand,
					category:    cat,
					isCommodity: isComm,
					unit:        unit,
					lastPrice:   price,
					lastDocDate: docDate,
					suppliers:   make(map[string]*rawSupItem),
				}
				productMap[pName] = pg
				orderedProductNames = append(orderedProductNames, pName)
			}
			pg.totalQty += qty
			pg.totalSum += sum
			if pg.unit == "" && unit != "" {
				pg.unit = unit
			}

			pg.history = append(pg.history, rawInvoiceRecord{
				price:    price,
				qty:      qty,
				docDate:  docDate,
				supplier: cleanSup,
			})

			sup, supExists := pg.suppliers[cleanSup]
			if !supExists {
				sup = &rawSupItem{
					name:        cleanSup,
					lastPrice:   price,
					lastDocDate: docDate,
				}
				pg.suppliers[cleanSup] = sup
			}
			sup.totalQty += qty
			sup.totalSum += sum
			sup.invoicesCount++
		}
	}
	rowsItems.Close()

	type ItemAggregate struct {
		Name              string
		Brand             string
		Category          string
		IsCommodity       bool
		Unit              string
		TotalQty          float64
		TotalSum          float64
		AvgPrice          float64
		LastPrice         float64
		LastDocDate       string
		MonthlyQty        float64
		MainSupplier      string
		MainSupplierShare float64
		SuppliersCount    int
		Suppliers         []engine.ItemSupplierDetail
	}

	var myItems []ItemAggregate
	for _, pName := range orderedProductNames {
		pg := productMap[pName]
		if pg.totalQty <= 0 {
			continue
		}
		avgPrice := pg.totalSum / pg.totalQty
		unit := engine.CleanUnit(pg.unit, pg.category)
		monthlyQty := (pg.totalQty / float64(days)) * 30.0

		var sups []engine.ItemSupplierDetail
		for _, s := range pg.suppliers {
			if s.totalQty <= 0 {
				continue
			}
			sAvgPrice := s.totalSum / s.totalQty
			sMonthlyQty := (s.totalQty / float64(days)) * 30.0
			sharePct := (s.totalQty / pg.totalQty) * 100.0

			sups = append(sups, engine.ItemSupplierDetail{
				SupplierName:  s.name,
				Volume:        math.Round(s.totalQty*100) / 100,
				MonthlyVolume: math.Round(sMonthlyQty*100) / 100,
				TotalSum:      math.Round(s.totalSum*100) / 100,
				AvgPrice:      math.Round(sAvgPrice*100) / 100,
				LastPrice:     s.lastPrice,
				LastDocDate:   s.lastDocDate,
				InvoicesCount: s.invoicesCount,
				SharePct:      math.Round(sharePct*10) / 10,
			})
		}

		sort.Slice(sups, func(i, j int) bool {
			return sups[i].Volume > sups[j].Volume
		})

		mainSup := "—"
		var mainShare float64
		if len(sups) > 0 {
			mainSup = sups[0].SupplierName
			mainShare = sups[0].SharePct
		}

		myItems = append(myItems, ItemAggregate{
			Name:              pg.name,
			Brand:             pg.brand,
			Category:          pg.category,
			IsCommodity:       pg.isCommodity,
			Unit:              unit,
			TotalQty:          pg.totalQty,
			TotalSum:          pg.totalSum,
			AvgPrice:          avgPrice,
			LastPrice:         pg.lastPrice,
			LastDocDate:       pg.lastDocDate,
			MonthlyQty:        math.Round(monthlyQty*100) / 100,
			MainSupplier:      mainSup,
			MainSupplierShare: mainShare,
			SuppliersCount:    len(sups),
			Suppliers:         sups,
		})
	}

	// 4. Сравнение с рынком (другими независимыми ресторанами)
	var candidates []engine.AuditedItem

	if otherRestaurantsCount > 0 {
		for _, it := range myItems {
			cohortTier := cohortCfg.GetTier(it.MonthlyQty)

			var queryMarket string
			var rowsMarket *sql.Rows
			var errM error

			if it.Brand != "" {
				queryMarket = `
					SELECT 
						r.id, 
						r.name, 
						COALESCE(r.city, 'Пермь'), 
						COALESCE(inv.supplier_name, 'Не указан'), 
						itm.price_per_unit, 
						itm.quantity, 
						to_char(itm.doc_date, 'YYYY-MM-DD') 
					FROM analytics_invoice_items itm
					JOIN analytics_restaurants r ON r.id = itm.restaurant_id
					LEFT JOIN analytics_invoices inv ON inv.id = itm.invoice_id
					WHERE r.id != $1 
					  AND (r.iiko_host != $2 OR $2 = '') 
					  AND r.is_active = true 
					  AND (r.company_id != 10 OR r.company_id IS NULL)
					  AND itm.detected_brand = $3 
					  AND itm.doc_date >= $4 
					  AND itm.price_per_unit > 0
					ORDER BY itm.doc_date DESC`
				rowsMarket, errM = db.QueryContext(ctx, queryMarket, restID, restHost, it.Brand, startDate)
			} else if it.IsCommodity && it.Category != "Прочее сырье" {
				queryMarket = `
					SELECT 
						r.id, 
						r.name, 
						COALESCE(r.city, 'Пермь'), 
						COALESCE(inv.supplier_name, 'Не указан'), 
						itm.price_per_unit, 
						itm.quantity, 
						to_char(itm.doc_date, 'YYYY-MM-DD') 
					FROM analytics_invoice_items itm
					JOIN analytics_restaurants r ON r.id = itm.restaurant_id
					LEFT JOIN analytics_invoices inv ON inv.id = itm.invoice_id
					WHERE r.id != $1 
					  AND (r.iiko_host != $2 OR $2 = '') 
					  AND r.is_active = true 
					  AND (r.company_id != 10 OR r.company_id IS NULL)
					  AND itm.canonical_category = $3 
					  AND itm.doc_date >= $4 
					  AND itm.price_per_unit > 0
					ORDER BY itm.doc_date DESC`
				rowsMarket, errM = db.QueryContext(ctx, queryMarket, restID, restHost, it.Category, startDate)
			} else {
				queryMarket = `
					SELECT 
						r.id, 
						r.name, 
						COALESCE(r.city, 'Пермь'), 
						COALESCE(inv.supplier_name, 'Не указан'), 
						itm.price_per_unit, 
						itm.quantity, 
						to_char(itm.doc_date, 'YYYY-MM-DD') 
					FROM analytics_invoice_items itm
					JOIN analytics_restaurants r ON r.id = itm.restaurant_id
					LEFT JOIN analytics_invoices inv ON inv.id = itm.invoice_id
					WHERE r.id != $1 
					  AND (r.iiko_host != $2 OR $2 = '') 
					  AND r.is_active = true 
					  AND (r.company_id != 10 OR r.company_id IS NULL)
					  AND LOWER(TRIM(itm.product_name)) = LOWER(TRIM($3))
					  AND itm.doc_date >= $4 
					  AND itm.price_per_unit > 0
					ORDER BY itm.doc_date DESC`
				rowsMarket, errM = db.QueryContext(ctx, queryMarket, restID, restHost, it.Name, startDate)
			}

			type peerAgg struct {
				restID      int
				restName    string
				city        string
				supplier    string
				latestPrice float64
				latestDate  string
				totalQty    float64
			}
			peerMap := make(map[string]*peerAgg)
			peerRestVolumes := make(map[int]float64)

			if errM == nil {
				for rowsMarket.Next() {
					var pRestID int
					var pRestName, pCity, pSupplier, pDocDate string
					var pPrice, pQty float64
					if err := rowsMarket.Scan(&pRestID, &pRestName, &pCity, &pSupplier, &pPrice, &pQty, &pDocDate); err == nil && pPrice > 0 {
						pSupplier = cleanSupplierName(pSupplier)
						pRestName = strings.TrimSpace(pRestName)
						if pRestName == "" {
							pRestName = fmt.Sprintf("Ресторан #%d", pRestID)
						}

						key := fmt.Sprintf("%d_%s", pRestID, pSupplier)
						agg, exists := peerMap[key]
						if !exists {
							agg = &peerAgg{
								restID:      pRestID,
								restName:    pRestName,
								city:        pCity,
								supplier:    pSupplier,
								latestPrice: pPrice,
								latestDate:  pDocDate,
							}
							peerMap[key] = agg
						}
						agg.totalQty += pQty
						peerRestVolumes[pRestID] += pQty
					}
				}
				rowsMarket.Close()
			}

			if len(peerMap) == 0 {
				continue
			}

			var latestMarketPrices []float64
			var peersList []engine.MarketPeerDetail

			for _, agg := range peerMap {
				latestMarketPrices = append(latestMarketPrices, agg.latestPrice)
				monthlyVol := math.Round(((agg.totalQty / float64(days)) * 30.0)*100) / 100
				restMonthlyVol := (peerRestVolumes[agg.restID] / float64(days)) * 30.0
				peerTier := cohortCfg.GetTier(restMonthlyVol)

				peersList = append(peersList, engine.MarketPeerDetail{
					RestaurantID:   agg.restID,
					RestaurantName: agg.restName,
					City:           agg.city,
					SupplierName:   agg.supplier,
					PricePerUnit:   agg.latestPrice,
					MonthlyVolume:  monthlyVol,
					CohortTier:     peerTier,
					LastDocDate:    agg.latestDate,
				})
			}

			stats := engine.CalculateStats(latestMarketPrices)

			sort.Slice(peersList, func(i, j int) bool {
				return peersList[i].PricePerUnit < peersList[j].PricePerUnit
			})

			bestPrice := stats.Min
			for _, p := range peersList {
				if p.CohortTier == cohortTier && p.PricePerUnit > 0 {
					if bestPrice == 0 || p.PricePerUnit < bestPrice {
						bestPrice = p.PricePerUnit
					}
				}
			}

			suppliersBreakdown := make([]engine.ItemSupplierDetail, len(it.Suppliers))
			copy(suppliersBreakdown, it.Suppliers)

			for sIdx := range suppliersBreakdown {
				s := &suppliersBreakdown[sIdx]
				s.MarketMinPrice = bestPrice
				if bestPrice > 0 {
					s.PriceDiffPercent = math.Round(((s.AvgPrice-bestPrice)/bestPrice)*1000) / 10
				}
				if s.AvgPrice > stats.Median+1.0 && stats.Median > 0 && ((s.AvgPrice-stats.Median)/stats.Median) >= 0.01 {
					s.IsOverpay = true
					s.MonthlyOverpaymentRub = math.Round((s.AvgPrice - stats.Median) * s.MonthlyVolume)
				} else {
					s.IsOverpay = false
					s.MonthlyOverpaymentRub = 0
				}
			}

			priceDiffVsAvg := it.AvgPrice - stats.Avg
			isOverpayVsAvg := (priceDiffVsAvg >= 1.0 && stats.Avg > 0 && (priceDiffVsAvg/stats.Avg) >= 0.01)
			var diffPctVsAvg float64
			var monthlyOverpayVsAvg float64
			if stats.Avg > 0 {
				diffPctVsAvg = math.Round(((it.AvgPrice-stats.Avg)/stats.Avg)*1000) / 10
				if isOverpayVsAvg {
					monthlyOverpayVsAvg = math.Round(priceDiffVsAvg * it.MonthlyQty)
				}
			}

			priceDiffVsMin := it.AvgPrice - bestPrice
			isOverpayVsMin := (priceDiffVsMin >= 1.0 && bestPrice > 0 && (priceDiffVsMin/bestPrice) >= 0.01)
			var diffPctVsMin float64
			var monthlyOverpayVsMin float64
			if bestPrice > 0 {
				diffPctVsMin = math.Round(((it.AvgPrice-bestPrice)/bestPrice)*1000) / 10
				if isOverpayVsMin {
					monthlyOverpayVsMin = math.Round(priceDiffVsMin * it.MonthlyQty)
				}
			}

			isOverpay := isOverpayVsAvg && monthlyOverpayVsAvg > 0
			baseMonthlyOverpay := monthlyOverpayVsAvg
			baseDiffPct := diffPctVsAvg

			candidates = append(candidates, engine.AuditedItem{
				ProductName:            it.Name,
				DetectedBrand:          it.Brand,
				CanonicalCategory:      it.Category,
				IsCommodity:            it.IsCommodity,
				Unit:                   it.Unit,
				MonthlyVolume:          it.MonthlyQty,
				CohortTier:             cohortTier,
				RestaurantPrice:        it.AvgPrice,
				LastPrice:              it.LastPrice,
				LastDocDate:            it.LastDocDate,
				MarketMedianPrice:      stats.Median,
				MarketAvgPrice:         stats.Avg,
				MarketMinPrice:         bestPrice,
				MarketP25Price:         stats.P25,
				MarketP75Price:         stats.P75,
				PriceDiffPercent:       baseDiffPct,
				MonthlyOverpaymentRub:  baseMonthlyOverpay,
				MonthlyOverpayVsAvgRub: monthlyOverpayVsAvg,
				PriceDiffPercentVsAvg:  diffPctVsAvg,
				MonthlyOverpayVsMinRub: monthlyOverpayVsMin,
				PriceDiffPercentVsMin:  diffPctVsMin,
				PriceTrend:             "stable",
				SupplierName:           it.MainSupplier,
				MainSupplier:           it.MainSupplier,
				MainSupplierShare:      it.MainSupplierShare,
				SuppliersCount:         it.SuppliersCount,
				SuppliersBreakdown:     suppliersBreakdown,
				IsOverpay:              isOverpay,
				MarketPeers:            peersList,
			})
		}
	}

	audit := engine.CalculateExecutiveAudit(
		restID,
		restName,
		monthlyRev,
		monthlyPurchases,
		candidates,
		supplierSpendMap,
		cohortCfg,
		invoicesCount,
		otherRestaurantsCount,
	)

	// Издержки оперативных (розничных) закупок (OffContractSpend)
	var totalOffContract float64
	_ = db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(it.total_sum), 0)
		FROM analytics_invoice_items it
		LEFT JOIN analytics_invoices inv ON inv.id = it.invoice_id
		WHERE it.restaurant_id = $1 
		  AND it.doc_date >= $2
		  AND (
		      inv.supplier_name ILIKE '%чек%' 
		      OR inv.supplier_name ILIKE '%розниц%' 
		      OR inv.supplier_name ILIKE '%налич%'
		      OR inv.supplier_name ILIKE '%аванс%'
		      OR inv.supplier_name ILIKE '%лента%'
		      OR inv.supplier_name ILIKE '%metro%'
		      OR inv.supplier_name ILIKE '%метро%'
		      OR inv.supplier_name ILIKE '%магнит%'
		      OR inv.supplier_name ILIKE '%пятероч%'
		      OR it.product_name ILIKE '%чек %'
		      OR it.product_name ILIKE '%авансов%'
		  )
		  AND it.total_sum > 0`, restID, startDate).Scan(&totalOffContract)

	monthlyOffContract := (totalOffContract / float64(days)) * 30.0
	audit.OffContractSpendMonthlyRub = math.Round(monthlyOffContract)
	if monthlyPurchases > 0 {
		audit.OffContractSpendSharePct = math.Round((monthlyOffContract/monthlyPurchases)*10000) / 100
	}
	audit.OffContractAvgMarkupPct = 24.5

	// 5. Расчет динамики цен (инфляция цен поставщиков за период)
	var inflationCandidates []engine.PriceInflationItem
	var totalInflationLoss float64

	for _, pName := range orderedProductNames {
		pg := productMap[pName]
		if len(pg.history) < 2 {
			continue
		}

		sort.Slice(pg.history, func(i, j int) bool {
			if pg.history[i].docDate != pg.history[j].docDate {
				return pg.history[i].docDate < pg.history[j].docDate
			}
			return pg.history[i].price < pg.history[j].price
		})

		firstRec := pg.history[0]
		lastRec := pg.history[len(pg.history)-1]

		if firstRec.price <= 0 || lastRec.price <= firstRec.price {
			continue
		}

		priceDiffRub := lastRec.price - firstRec.price
		priceDiffPct := (priceDiffRub / firstRec.price) * 100.0

		// Сумма реального ущерба от роста цены
		var lossRub float64
		for _, rec := range pg.history {
			if rec.price > firstRec.price {
				lossRub += (rec.price - firstRec.price) * rec.qty
			}
		}
		if lossRub <= 0 && priceDiffRub > 0 {
			lossRub = priceDiffRub * (pg.totalQty / 2.0)
		}

		totalInflationLoss += lossRub

		supName := lastRec.supplier
		if supName == "" {
			supName = "Не указан"
		}

		inflationCandidates = append(inflationCandidates, engine.PriceInflationItem{
			ProductName:               pg.name,
			SupplierName:              supName,
			Unit:                      pg.unit,
			FirstPrice:                math.Round(firstRec.price*100) / 100,
			FirstDocDate:              firstRec.docDate,
			LastPrice:                 math.Round(lastRec.price*100) / 100,
			LastDocDate:               lastRec.docDate,
			PriceDiffRub:              math.Round(priceDiffRub*100) / 100,
			PriceDiffPercent:          math.Round(priceDiffPct*10) / 10,
			PeriodVolume:              math.Round(pg.totalQty*100) / 100,
			EstimatedInflationLossRub: math.Round(lossRub*100) / 100,
		})
	}

	sort.Slice(inflationCandidates, func(i, j int) bool {
		if inflationCandidates[i].EstimatedInflationLossRub != inflationCandidates[j].EstimatedInflationLossRub {
			return inflationCandidates[i].EstimatedInflationLossRub > inflationCandidates[j].EstimatedInflationLossRub
		}
		return inflationCandidates[i].PriceDiffPercent > inflationCandidates[j].PriceDiffPercent
	})

	var topPriceHikes []engine.PriceInflationItem
	for i := 0; i < len(inflationCandidates) && i < 5; i++ {
		cand := inflationCandidates[i]
		cand.Rank = i + 1
		topPriceHikes = append(topPriceHikes, cand)
	}
	audit.TopPriceHikes = topPriceHikes
	audit.TotalInflationLossRub = math.Round(totalInflationLoss*100) / 100

	// 6. Сбор и расчет ручных списаний (Write-offs)
	rowsWriteoffs, errW := db.QueryContext(ctx, `
		SELECT 
			product_name,
			unit,
			COALESCE(SUM(amount), 0) as total_amount,
			COALESCE(SUM(cost), 0) as total_cost,
			COALESCE(NULLIF(comment, ''), 'Ручное списание') as reason
		FROM analytics_writeoff_items
		WHERE restaurant_id = $1 AND doc_date >= $2
		GROUP BY product_name, unit, reason
	`, restID, startDate)
	if errW == nil {
		type woAgg struct {
			productName string
			unit        string
			amount      float64
			cost        float64
			reason      string
		}
		var rawWOs []woAgg
		var totalWriteoffsCost float64
		for rowsWriteoffs.Next() {
			var w woAgg
			if errScan := rowsWriteoffs.Scan(&w.productName, &w.unit, &w.amount, &w.cost, &w.reason); errScan == nil {
				totalWriteoffsCost += w.cost
				rawWOs = append(rawWOs, w)
			}
		}
		rowsWriteoffs.Close()

		// Топ по стоимости
		sort.Slice(rawWOs, func(i, j int) bool {
			return rawWOs[i].cost > rawWOs[j].cost
		})
		var topWOCost []engine.WriteoffLossItem
		for i := 0; i < len(rawWOs) && i < 5; i++ {
			share := 0.0
			if totalWriteoffsCost > 0 {
				share = math.Round((rawWOs[i].cost/totalWriteoffsCost)*1000) / 10
			}
			topWOCost = append(topWOCost, engine.WriteoffLossItem{
				Rank:         i + 1,
				ProductName:  rawWOs[i].productName,
				Unit:         rawWOs[i].unit,
				TotalAmount:  math.Round(rawWOs[i].amount*100) / 100,
				TotalCostRub: math.Round(rawWOs[i].cost*100) / 100,
				SharePct:     share,
				Reason:       rawWOs[i].reason,
			})
		}
		audit.TopWriteoffsByCost = topWOCost

		// Топ по объему
		sort.Slice(rawWOs, func(i, j int) bool {
			return rawWOs[i].amount > rawWOs[j].amount
		})
		var topWOAmount []engine.WriteoffLossItem
		for i := 0; i < len(rawWOs) && i < 5; i++ {
			topWOAmount = append(topWOAmount, engine.WriteoffLossItem{
				Rank:         i + 1,
				ProductName:  rawWOs[i].productName,
				Unit:         rawWOs[i].unit,
				TotalAmount:  math.Round(rawWOs[i].amount*100) / 100,
				TotalCostRub: math.Round(rawWOs[i].cost*100) / 100,
				Reason:       rawWOs[i].reason,
			})
		}
		audit.TopWriteoffsByAmount = topWOAmount
		audit.TotalWriteoffsCostRub = math.Round(totalWriteoffsCost*100) / 100
		if monthlyPurchases > 0 {
			monthlyWOCost := (totalWriteoffsCost / float64(days)) * 30.0
			audit.WriteoffsSpendSharePct = math.Round((monthlyWOCost/monthlyPurchases)*1000) / 10
		}
	}

	// Топ-категории переплат для AI аудитора
	var topOverpaidCats []string
	catSeen := make(map[string]bool)
	for _, it := range audit.ItemsAudit {
		if it.IsOverpay && it.CanonicalCategory != "" && !catSeen[it.CanonicalCategory] {
			topOverpaidCats = append(topOverpaidCats, it.CanonicalCategory)
			catSeen[it.CanonicalCategory] = true
			if len(topOverpaidCats) >= 3 {
				break
			}
		}
	}

	auditorPayload := llm.AuditorPayload{
		RestaurantName:              restName,
		City:                        restCity,
		AnalysisPeriodDays:          days,
		MonthlyPurchasesRub:         audit.MonthlyPurchasesRub,
		TotalMonthlyOverpayRub:      audit.TotalMonthlyOverpayVsAvgRub,
		TotalMonthlyOverpayVsMinRub: audit.TotalMonthlyOverpayVsMinRub,
		OverpayBudgetPercent:        audit.OverpayBudgetPercent,
		SupplierHHI:                 audit.SupplierHHI,
		SupplierHHIStatus:           audit.SupplierHHIStatus,
		TopOverpaidCategories:       topOverpaidCats,
		OffContractSpendMonthlyRub:  audit.OffContractSpendMonthlyRub,
		OffContractSpendSharePct:    audit.OffContractSpendSharePct,
	}

	summary, errSummary := llm.GetOrGenerateSummary(ctx, db, llmClient, auditorPayload, restID, days)
	if errSummary == nil && summary != "" {
		audit.AuditorSummary = summary
	}

	return &audit, nil
}
