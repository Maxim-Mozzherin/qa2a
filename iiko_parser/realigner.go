package main

import (
	"log"
	"math"
	"strings"
)

// realignAndValidateInvoice выполняет строгую математическую и структурную нормализацию
// распознанных позиций накладной (ТОРГ-12 / УПД / Чеки) до отправки в UI и базу данных.
//
// Ключевые решаемые проблемы:
// 1. Каскадный сдвиг колонок (off-by-one shift), когда количества (колонка 10) съезжают на строку ниже.
// 2. Захват служебного итога страницы (например 25.400 / 26.400) в качестве веса последней позиции.
// 3. Отличие цены с НДС и без НДС в УПД (колонка 4 vs колонка 9).
// 4. Ошибочные дикие мультипликаторы фасовки для весовых товаров (кг -> кг).
func realignAndValidateInvoice(aiData *AiResponse) *AiResponse {
	if aiData == nil || len(aiData.Items) == 0 {
		return aiData
	}

	// 1. Предварительная фильтрация служебных и балансирующих строк
	var initialItems []AiItem
	for _, it := range aiData.Items {
		if isSubtotalRow(it.Name) || isHallucinatedBalancingRow(it.Name) {
			log.Printf("🧹 [Realigner] Удалена служебная строка: %q (sum=%.2f)", it.Name, it.Sum)
			continue
		}
		if it.Quantity <= 0 && it.Sum <= 0 && it.Price <= 0 {
			continue
		}
		initialItems = append(initialItems, it)
	}

	if len(initialItems) == 0 {
		aiData.Items = initialItems
		return aiData
	}

	// 2. Детекция и исправление захвата итога страницы последней позицией
	lastIdx := len(initialItems) - 1
	lastItem := &initialItems[lastIdx]
	if lastItem.Price > 0 && lastItem.Sum > 0 && lastItem.Quantity > 0 {
		calcLastQ := math.Round((lastItem.Sum/lastItem.Price)*1000) / 1000
		// Если заявленное количество значительно больше расчетного (в 2+ раза) и при этом расчетное кол-во чистое
		if lastItem.Quantity > calcLastQ*1.8 && math.Abs(lastItem.Quantity*lastItem.Price-lastItem.Sum) > 1.0 {
			log.Printf("🎯 [Realigner] Последняя позиция %q: вес %.3f заменен на точный расчетный вес %.3f (предотвращен захват итога страницы)",
				lastItem.Name, lastItem.Quantity, calcLastQ)
			lastItem.Quantity = calcLastQ
		}
	}

	// 3. Анализ и выравнивание каждой строки
	for i := range initialItems {
		item := &initialItems[i]

		if item.Quantity <= 0 && item.Price > 0 && item.Sum > 0 {
			item.Quantity = math.Round((item.Sum/item.Price)*1000) / 1000
		}

		if item.Quantity <= 0 || item.Sum <= 0 {
			continue
		}

		// Проверяем: выполняется ли базовое равенство Q * P == S
		expectedSum := item.Quantity * item.Price
		diff := math.Abs(expectedSum - item.Sum)

		if diff <= 0.05 {
			// Строка идеально сходится
			cleanWeightMultipliers(item)
			continue
		}

		// Проверка 1: Цена в УПД была указана без НДС (колонка 4), а сумма — с НДС (колонка 9)
		if item.NdsPercent > 0 {
			sumWithVat := item.Quantity * item.Price * (1.0 + item.NdsPercent/100.0)
			if math.Abs(sumWithVat-item.Sum) <= 0.08 || (item.SumWithoutNds > 0 && math.Abs(expectedSum-item.SumWithoutNds) <= 0.08) {
				// Цена была без НДС. Пересчитываем цену С НДС:
				item.Price = math.Round((item.Sum/item.Quantity)*10000) / 10000
				cleanWeightMultipliers(item)
				continue
			}
		}

		// Проверка 2: Проверяем, не является ли цена правильной, а количество — сдвинутым
		// В ТОРГ-12 колонка 11 (Цена) и колонка 15 (Сумма) всегда напечатаны рядом справа.
		// Если разделить Sum / Price, мы получаем истинное количество позиции!
		if item.Price > 0 {
			calcQ := math.Round((item.Sum/item.Price)*1000) / 1000
			calcP := math.Round((item.Sum/item.Quantity)*10000) / 10000

			// Оцениваем, что "чище": восстановить количество calcQ или изменить цену calcP
			// Если calcQ дает красивое число (целое или с 1-3 знаками, например 3.0, 4.08, 1.1),
			// а calcP дает дробь с бесконечным хвостом (например 95.5882, 148.3636),
			// то очевидно сдвинуто было именно количество!
			isQCleaner := isCleanerNumber(calcQ, calcP)

			// Дополнительно: если текущее item.Quantity равно расчетному количеству следующей строки (признак сдвига на 1)
			isShiftCandidate := false
			if i+1 < len(initialItems) && initialItems[i+1].Price > 0 {
				nextCalcQ := math.Round((initialItems[i+1].Sum/initialItems[i+1].Price)*1000) / 1000
				if math.Abs(item.Quantity-nextCalcQ) < 0.01 {
					isShiftCandidate = true
				}
			}

			if isShiftCandidate || isQCleaner {
				log.Printf("🔧 [Realigner] Строка %d (%s): исправлен сдвиг веса с %.3f на %.3f (цена %.2f ₽ сохранена, сумма %.2f ₽)",
					i+1, item.Name, item.Quantity, calcQ, item.Price, item.Sum)
				item.Quantity = calcQ
			} else {
				// Фоллбэк: если количество подтверждено, нормализуем цену
				item.Price = calcP
			}
		}

		cleanWeightMultipliers(item)
	}

	// 4. Пересчет номеров строк 1..N
	for i := range initialItems {
		initialItems[i].Num = i + 1
	}

	aiData.Items = initialItems

	// 5. Логирование сходимости итоговых сумм
	calcTotal := calculateTotalSum(aiData.Items)
	if aiData.DocPrintedTotalSum > 0 {
		delta := math.Abs(calcTotal - aiData.DocPrintedTotalSum)
		log.Printf("📊 [Realigner] Результат валидации: расчетная сумма = %.2f ₽, печатная = %.2f ₽, дельта = %.2f ₽",
			calcTotal, aiData.DocPrintedTotalSum, delta)
	}

	return aiData
}

// cleanWeightMultipliers сбрасывает ошибочные коэффициенты фасовки для чисто весовых товаров
// и гарантирует точную фасовку для специальных позиций (например, голубика)
func cleanWeightMultipliers(item *AiItem) {
	unitNorm := strings.ToLower(strings.TrimSpace(item.Unit))
	baseNorm := strings.ToLower(strings.TrimSpace(item.BaseUnit))
	nameLower := strings.ToLower(item.Name)

	// Специальное правило для голубики (и ягод в лотках)
	if strings.Contains(nameLower, "голубик") {
		if unitNorm == "шт" || unitNorm == "уп" || unitNorm == "упак" || unitNorm == "лот" || unitNorm == "лоток" {
			item.AiMultiplier = 0.125
			item.BaseUnit = "кг"
			item.AiTip = "1 шт = 0.125 кг (125г)"
			return
		} else if unitNorm == "кг" {
			item.AiMultiplier = 1.0
			item.BaseUnit = "кг"
			item.AiTip = "1 шт = 1 кг"
			return
		}
	}

	// Если товар пришел в кг или л и базовая единица тоже кг/л
	if (unitNorm == "кг" || unitNorm == "л") && (baseNorm == "" || baseNorm == "кг" || baseNorm == "л") {
		item.AiMultiplier = 1.0
		if item.AiTip == "" || strings.Contains(item.AiTip, "0.") {
			item.AiTip = "1 шт = 1 кг"
		}
	}

	if item.AiMultiplier <= 0 {
		item.AiMultiplier = 1.0
	}
}

// isCleanerNumber сравнивает два числа и определяет, является ли qNum более естественным (весовым/штучным), чем pNum
func isCleanerNumber(qNum, pNum float64) bool {
	if qNum <= 0 {
		return false
	}
	// Проверяем, является ли qNum целым или с небольшим числом десятичных знаков
	qFrac := qNum - math.Floor(qNum)
	isQSimple := qFrac == 0 || math.Abs(qFrac-0.5) < 0.001 || math.Abs(qFrac-0.2) < 0.001 || math.Abs(qFrac-0.25) < 0.001

	// Проверяем pNum на "мусорные" десятичные хвосты (более 2 знаков после запятой)
	pScaled := pNum * 100
	pHasWeirdCents := math.Abs(pScaled-math.Round(pScaled)) > 0.01

	if pHasWeirdCents {
		return true
	}
	if isQSimple {
		return true
	}
	return false
}
