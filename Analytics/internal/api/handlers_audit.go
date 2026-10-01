package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"analytics_service/internal/engine"
	"analytics_service/internal/llm"
)

// HandleAudit формирует экономический аудит переплат с плавающими когортами,
// рассчитывает переплаты к рыночной средней и к минимуму когорты, ценовой спред и AI-заключение
func (s *Server) HandleAudit(w http.ResponseWriter, r *http.Request) {
	restIDStr := r.URL.Query().Get("restaurant_id")
	if restIDStr == "" {
		http.Error(w, "Параметр restaurant_id обязателен", http.StatusBadRequest)
		return
	}
	restID, err := strconv.Atoi(restIDStr)
	if err != nil || restID <= 0 {
		http.Error(w, "Некорректный restaurant_id", http.StatusBadRequest)
		return
	}

	days := 60
	if dStr := r.URL.Query().Get("days"); dStr != "" {
		if d, err := strconv.Atoi(dStr); err == nil && d > 0 {
			days = d
		}
	}

	// Настройка плавающих когорт из параметров запроса
	cohortCfg := engine.DefaultCohortConfig()
	if sVal := r.URL.Query().Get("small_threshold"); sVal != "" {
		if st, err := strconv.ParseFloat(sVal, 64); err == nil && st > 0 {
			cohortCfg.SmallMaxThreshold = st
		}
	}
	if mVal := r.URL.Query().Get("medium_threshold"); mVal != "" {
		if mt, err := strconv.ParseFloat(mVal, 64); err == nil && mt > cohortCfg.SmallMaxThreshold {
			cohortCfg.MediumMaxThreshold = mt
		}
	}

	// 1. Получаем данные заведения
	var restName string
	var restHost string
	var monthlyRev float64
	err = s.db.QueryRow(`
		SELECT name, iiko_host, monthly_revenue 
		FROM analytics_restaurants 
		WHERE id = $1`, restID).Scan(&restName, &restHost, &monthlyRev)
	if err != nil {
		http.Error(w, "Заведение не найдено", http.StatusNotFound)
		return
	}

	startDate := time.Now().AddDate(0, 0, -days).Format("2006-01-02")

	// 2. Получаем количество накладных, общую сумму закупок и распределение трат по поставщикам для HHI
	var invoicesCount int
	var totalSpendPeriod float64
	_ = s.db.QueryRow(`
		SELECT COUNT(DISTINCT id), COALESCE(SUM(total_sum), 0) 
		FROM analytics_invoices 
		WHERE restaurant_id = $1 AND doc_date >= $2`, restID, startDate).Scan(&invoicesCount, &totalSpendPeriod)

	monthlyPurchases := (totalSpendPeriod / float64(days)) * 30.0

	// Количество независимых заведений на рынке (исключая текущее заведение, его дубли и тестовый сервер)
	var otherRestaurantsCount int
	_ = s.db.QueryRow(`
		SELECT COUNT(DISTINCT r.id) 
		FROM analytics_restaurants r
		JOIN analytics_invoices inv ON inv.restaurant_id = r.id
		WHERE r.iiko_host != $1 
		  AND r.is_active = true 
		  AND (r.company_id != 10 OR r.company_id IS NULL)`, restHost).Scan(&otherRestaurantsCount)

	supplierSpendMap := make(map[string]float64)
	rowsSuppliers, err := s.db.Query(`
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

	// 3. Собираем сырьевые позиции данного заведения (исключая хозтовары, инвентарь и чеки наличных)
	rowsItems, err := s.db.Query(`
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
		http.Error(w, "Ошибка чтения позиций: "+err.Error(), http.StatusInternalServerError)
		return
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

	type rawProductGroup struct {
		name        string
		brand       string
		category    string
		isCommodity bool
		unit        string
		totalQty    float64
		totalSum    float64
		lastPrice   float64
		lastDocDate string
		suppliers   map[string]*rawSupItem
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

	// 4. Сравнение СТРОГО С РЫНКОМ (другими независимыми ресторанами)
	// Заведение с самим собой НЕ сравнивается!
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
				rowsMarket, errM = s.db.Query(queryMarket, restID, restHost, it.Brand, startDate)
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
				rowsMarket, errM = s.db.Query(queryMarket, restID, restHost, it.Category, startDate)
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
				rowsMarket, errM = s.db.Query(queryMarket, restID, restHost, it.Name, startDate)
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
						key := fmt.Sprintf("%d_%s", pRestID, pSupplier)
						agg, exists := peerMap[key]
						if !exists {
							// Так как ORDER BY itm.doc_date DESC, первая встреченная строка — это САМАЯ ПОСЛЕДНЯЯ поставка товара от данного поставщика в данное заведение!
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

			// Рыночный срез рассчитывается СТРОГО по ценам ПОСЛЕДНИХ поставок других заведений (без старых цен 3-месячной давности)
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
					PricePerUnit:   agg.latestPrice, // Цена ПОСЛЕДНЕЙ поставки в данное заведение!
					MonthlyVolume:  monthlyVol,
					CohortTier:     peerTier,
					LastDocDate:    agg.latestDate,
				})
			}

			stats := engine.CalculateStats(latestMarketPrices)

			sort.Slice(peersList, func(i, j int) bool {
				return peersList[i].PricePerUnit < peersList[j].PricePerUnit
			})

			// Минимальная цена — это минимальная цена среди ПОСЛЕДНИХ поставок в заведения в когорте (или overall min)!
			bestPrice := stats.Min
			for _, p := range peersList {
				if p.CohortTier == cohortTier && p.PricePerUnit > 0 {
					if bestPrice == 0 || p.PricePerUnit < bestPrice {
						bestPrice = p.PricePerUnit
					}
				}
			}

			// Теперь обогащаем каждого поставщика в suppliers_breakdown
			suppliersBreakdown := make([]engine.ItemSupplierDetail, len(it.Suppliers))
			copy(suppliersBreakdown, it.Suppliers)

			for sIdx := range suppliersBreakdown {
				s := &suppliersBreakdown[sIdx]
				s.MarketMinPrice = bestPrice

				// Проверяем: отгружает ли этот же поставщик другим заведениям дешевле?
				var sameSupMin float64
				var sameSupPeer string
				for _, p := range peersList {
					if isSameSupplier(p.SupplierName, s.SupplierName) && p.PricePerUnit > 0 {
						if sameSupMin == 0 || p.PricePerUnit < sameSupMin {
							sameSupMin = p.PricePerUnit
							sameSupPeer = p.RestaurantName
						}
					}
				}
				s.SameSupplierMarketMin = sameSupMin
				s.SameSupplierPeerName = sameSupPeer

				// Внутрипоставочный ценовой спред (SupplierPriceSpread)
				if sameSupMin > 0 && s.AvgPrice > sameSupMin {
					s.SupplierPriceSpreadRub = math.Round((s.AvgPrice - sameSupMin) * s.MonthlyVolume)
					s.SupplierPriceSpreadPct = math.Round(((s.AvgPrice - sameSupMin) / sameSupMin) * 1000) / 10
				}

				// Сравнение цены поставщика с минимумом и медианой
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

			// 1. Сравнение с медианой рынка (базовый бенчмарк)
			priceDiffVsMedian := it.AvgPrice - stats.Median
			// Защита от копеек: разница должна быть >= 1.0 ₽ И >= 1%
			isOverpayVsMedian := (priceDiffVsMedian >= 1.0 && stats.Median > 0 && (priceDiffVsMedian/stats.Median) >= 0.01)

			// 2. Сравнение со средней рынка
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

			// 3. Сравнение с минимумом когорты (максимальный потенциал экономии)
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

			// 4. Анализ тренда по последней накладной
			priceTrend := "stable"
			priceTrendNote := ""
			isMarketAlignedLatest := false

			if it.LastPrice > 0 {
				// Проверяем: попадает ли последняя цена в медиану (с допуском 1.5%) или в рыночный диапазон P25-P75?
				if (stats.Median > 0 && math.Abs(it.LastPrice-stats.Median) <= 1.0) || (stats.Median > 0 && math.Abs(it.LastPrice-stats.Median)/stats.Median <= 0.015) {
					isMarketAlignedLatest = true
				} else if stats.P25 > 0 && stats.P75 > 0 && it.LastPrice >= (stats.P25-0.5) && it.LastPrice <= (stats.P75+0.5) {
					isMarketAlignedLatest = true
				}

				if it.LastPrice < it.AvgPrice-1.0 {
					priceTrend = "down"
					if isMarketAlignedLatest {
						priceTrendNote = fmt.Sprintf("В посл. поставке цена снижена до %.2f ₽ (выровнена с рынком)", it.LastPrice)
					} else {
						priceTrendNote = fmt.Sprintf("Цена снижается: посл. поставка %.2f ₽ (ср: %.2f ₽)", it.LastPrice, it.AvgPrice)
					}
				} else if it.LastPrice > it.AvgPrice+1.0 {
					priceTrend = "up"
					priceTrendNote = fmt.Sprintf("Внимание: посл. поставка дороже средней (%.2f ₽ vs ср. %.2f ₽)", it.LastPrice, it.AvgPrice)
				} else {
					priceTrend = "stable"
					if isMarketAlignedLatest {
						priceTrendNote = "Цена стабильна и соответствует рынку"
					}
				}
			}

			// Позиция признается переплатой/потенциалом экономии, если есть переплата к рынку ИЛИ ощутимая разница с минимумом (>=5%)
			isOverpay := isOverpayVsMedian || isOverpayVsAvg || (isOverpayVsMin && diffPctVsMin >= 5.0)

			// Базовая переплата: строго к средней рынка
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
				PriceTrend:             priceTrend,
				PriceTrendNote:          priceTrendNote,
				IsMarketAlignedLatest:  isMarketAlignedLatest,
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
	_ = s.db.QueryRow(`
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

	// Определение ключевого поставщика и топ-категорий переплат для AI аудитора
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


	var restCity string
	_ = s.db.QueryRow(`SELECT COALESCE(city, 'Пермь') FROM analytics_restaurants WHERE id = $1`, restID).Scan(&restCity)

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

	summary, errSummary := llm.GetOrGenerateSummary(r.Context(), s.db, s.llmClient, auditorPayload, restID, days)
	if errSummary == nil && summary != "" {
		audit.AuditorSummary = summary
	}

	// Применение Freemium маскирования (mode=demo по умолчанию, mode=full для полного отчета)
	mode := r.URL.Query().Get("mode")
	engine.ApplyFreemiumMask(&audit, mode)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(audit)
}
