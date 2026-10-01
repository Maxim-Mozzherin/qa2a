package taxonomy

import (
	"strings"
)

// ProductClassification представляет результат нормализации и классификации товарной позиции iiko
type ProductClassification struct {
	IsCommodity          bool   // Является ли биржевым/коммодити товаром без привязки к бренду
	IsHouseholdOrNonFood bool   // Несырьевая статья (хозтовары, чеки, салфетки, посуда)
	DetectedBrand        string // Распознанная торговая марка
	CanonicalCategory    string // Эталонная категория HoReCa для межресторанного бенчмаркинга
}

// CleanUnit очищает единицу измерения от артикулов/кодов iiko и возвращает стандартную HoReCa единицу (кг, л, шт)
func CleanUnit(rawUnit, category string) string {
	u := strings.TrimSpace(strings.ToLower(rawUnit))
	catLower := strings.ToLower(category)

	isArticleCode := false
	if u == "" || len(u) >= 4 {
		isArticleCode = true
	} else {
		for _, r := range u {
			if r >= '0' && r <= '9' {
				isArticleCode = true
				break
			}
		}
	}

	if isArticleCode {
		if strings.Contains(catLower, "молоко") || strings.Contains(catLower, "сливки") ||
			strings.Contains(catLower, "масло") || strings.Contains(catLower, "сироп") ||
			strings.Contains(catLower, "соус") || strings.Contains(catLower, "уксус") ||
			strings.Contains(catLower, "напиток") || strings.Contains(catLower, "сок") {
			return "л"
		}
		if strings.Contains(catLower, "яйцо") || strings.Contains(catLower, "спрей") || strings.Contains(catLower, "баллон") {
			return "шт"
		}
		return "кг"
	}

	if strings.Contains(u, "кг") || strings.Contains(u, "кило") {
		return "кг"
	}
	if strings.Contains(u, "л") || strings.Contains(u, "литр") {
		return "л"
	}
	if strings.Contains(u, "шт") {
		return "шт"
	}
	return u
}

// ClassifyProduct выполняет глубокую нормализацию и сопоставление номенклатуры iiko с эталонными категориями HoReCa
func ClassifyProduct(rawName string) ProductClassification {
	lower := strings.ToLower(rawName)
	clean := strings.ReplaceAll(lower, "ё", "е")

	res := ProductClassification{
		IsCommodity:          false,
		IsHouseholdOrNonFood: false,
		DetectedBrand:        "",
		CanonicalCategory:    "Прочее сырье",
	}

	// 0. Отсекаем несырьевые статьи (хозтовары, чеки, инвентарь, упаковка)
	if strings.Contains(clean, "хоз.товар") || strings.Contains(clean, "хозтовар") ||
		strings.Contains(clean, "инвентар") || strings.Contains(clean, "посуда") ||
		strings.Contains(clean, "авансов") || strings.Contains(clean, "чек нал") ||
		strings.Contains(clean, "чек ") || strings.Contains(clean, "расходн") ||
		strings.Contains(clean, "упаковк") || strings.Contains(clean, "контейнер") ||
		strings.Contains(clean, "пакет") || strings.Contains(clean, "пленка") ||
		strings.Contains(clean, "пергамент") || strings.Contains(clean, "фольга") ||
		strings.Contains(clean, "моющее") || strings.Contains(clean, "мыло") ||
		strings.Contains(clean, "дезсред") || strings.Contains(clean, "салфетк") ||
		strings.Contains(clean, "полотенц") || strings.Contains(clean, "перчатк") {
		res.IsHouseholdOrNonFood = true
		res.CanonicalCategory = "Хоз. товары и расходники"
		return res
	}

	// 1. Поиск бренда по расширенному реестру HoReCa
	brandName, _ := DetectBrand(clean)
	res.DetectedBrand = brandName

	// 2. Молочная группа (сливки всех видов, жирность, молоко, сыры, масло)
	if cat, isComm, matched := ClassifyDairy(clean, res.DetectedBrand); matched {
		res.CanonicalCategory = cat
		res.IsCommodity = isComm
		return res
	}

	// 3. Мясо, птица, рыба и морепродукты
	if cat, isComm, matched := ClassifyMeatFish(clean, res.DetectedBrand); matched {
		res.CanonicalCategory = cat
		res.IsCommodity = isComm
		return res
	}

	// 4. Бакалея, картофель фри, овощи, соусы, масла
	if cat, isComm, matched := ClassifyGroceriesProduce(clean, res.DetectedBrand); matched {
		res.CanonicalCategory = cat
		res.IsCommodity = isComm
		return res
	}

	// 5. Базовые овощи/фрукты (лимон, лайм, чеснок, имбирь)
	if strings.Contains(clean, "лимон") && !strings.Contains(clean, "лимонад") {
		res.CanonicalCategory = "Лимоны свежие"
		res.IsCommodity = true
		return res
	}
	if strings.Contains(clean, "лайм") {
		res.CanonicalCategory = "Лайм свежий"
		res.IsCommodity = true
		return res
	}
	if strings.Contains(clean, "чеснок") {
		res.CanonicalCategory = "Чеснок свежий"
		res.IsCommodity = true
		return res
	}
	if strings.Contains(clean, "имбир") {
		if strings.Contains(clean, "маринован") || strings.Contains(clean, "розов") || strings.Contains(clean, "белый") {
			res.CanonicalCategory = "Имбирь маринованный"
			res.IsCommodity = true
			return res
		}
		res.CanonicalCategory = "Имбирь корень свежий"
		res.IsCommodity = true
		return res
	}

	return res
}
