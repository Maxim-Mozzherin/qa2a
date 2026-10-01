package taxonomy

import (
	"strings"
	"unicode"
)

// containsWord проверяет, входит ли слово как отдельный токен (исключая ложные склейки вроде "соль" в "фасоль")
func containsWord(s, word string) bool {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, f := range fields {
		if f == word {
			return true
		}
	}
	return false
}

// ClassifyGroceriesProduce классифицирует бакалею, овощи, фрукты, соусы и картофель фри
// Возвращает (категория, isCommodity, matched)
func ClassifyGroceriesProduce(clean string, detectedBrand string) (string, bool, bool) {

	// =========================================================================
	// 1. КАРТОФЕЛЬ: ФРИ (полуфабрикат) vs СВЕЖИЙ КАРТОФЕЛЬ
	// =========================================================================
	if strings.Contains(clean, "картофел") || strings.Contains(clean, "картошка") {
		switch {
		case strings.Contains(clean, "фри") || strings.Contains(clean, "9*9") || strings.Contains(clean, "6*6") ||
			strings.Contains(clean, "дип") || strings.Contains(clean, "дольк") || strings.Contains(clean, "деревенск") ||
			strings.Contains(clean, "хэшбраун") || strings.Contains(clean, "хашбраун") || strings.Contains(clean, "крисперс"):
			return "Картофель фри (п/ф с/м)", true, true
		case strings.Contains(clean, "крахмал"):
			return "Крахмал картофельный", true, true
		case strings.Contains(clean, "хлопь") || strings.Contains(clean, "сухое пюре"):
			return "Картофельные хлопья / пюре", true, true
		default:
			if !strings.Contains(clean, "чипс") && !strings.Contains(clean, "пай") {
				return "Картофель свежий", true, true
			}
		}
	}

	// =========================================================================
	// 2. РИС: Для суши (японика, фушигон) vs длиннозерный/басмати
	// =========================================================================
	if strings.Contains(clean, "рис") && !strings.Contains(clean, "рисов") {
		switch {
		case strings.Contains(clean, "суши") || strings.Contains(clean, "круглозерн") || strings.Contains(clean, "среднезерн") || strings.Contains(clean, "фушигон") || strings.Contains(clean, "японика"):
			return "Рис для суши / круглозерный", (detectedBrand == ""), true
		case strings.Contains(clean, "басмати") || strings.Contains(clean, "жасмин") || strings.Contains(clean, "длиннозерн"):
			return "Рис длиннозерный/басмати", (detectedBrand == ""), true
		}
	}

	// =========================================================================
	// 3. МАСЛО: Фритюрное / растительное vs Оливковое Extra Virgin
	// =========================================================================
	if strings.Contains(clean, "масло") && !strings.Contains(clean, "сливоч") {
		switch {
		case strings.Contains(clean, "оливков") || strings.Contains(clean, "extra virgin") || strings.Contains(clean, "экстра вирджин"):
			return "Масло оливковое Extra Virgin", false, true
		case strings.Contains(clean, "кунжутн"):
			return "Масло кунжутное", false, true
		case strings.Contains(clean, "трюфельн"):
			return "Масло трюфельное", false, true
		case strings.Contains(clean, "подсолнечн") || strings.Contains(clean, "фритюр") || strings.Contains(clean, "растительн"):
			return "Масло фритюрное/растительное", (detectedBrand == ""), true
		}
	}

	// =========================================================================
	// 4. МУКА И КРАХМАЛ
	// =========================================================================
	if strings.Contains(clean, "мука") {
		switch {
		case strings.Contains(clean, "рисов"):
			return "Мука рисовая", true, true
		case strings.Contains(clean, "миндальн"):
			return "Мука миндальная", false, true
		case strings.Contains(clean, "темпур") || strings.Contains(clean, "кляр"):
			return "Мука темпурная", true, true
		case strings.Contains(clean, "кукурузн"):
			return "Мука кукурузная", true, true
		case strings.Contains(clean, "гречнев"):
			return "Мука гречневая", true, true
		case strings.Contains(clean, "ржан"):
			return "Мука ржаная", true, true
		default:
			return "Мука пшеничная в/с", true, true
		}
	}

	if strings.Contains(clean, "крахмал") {
		if strings.Contains(clean, "кукуруз") {
			return "Крахмал кукурузный", true, true
		}
		if strings.Contains(clean, "тапиок") {
			return "Крахмал тапиоковый", true, true
		}
		return "Крахмал картофельный", true, true
	}

	// =========================================================================
	// 5. СОУСЫ (Соевый, Майонез, Терияки, Томатная паста)
	// =========================================================================
	if strings.Contains(clean, "соус") || strings.Contains(clean, "майонез") || strings.Contains(clean, "кетчуп") {
		switch {
		case strings.Contains(clean, "соев"):
			return "Соус соевый классический", false, true
		case strings.Contains(clean, "терияк"):
			return "Соус Терияки", false, true
		case strings.Contains(clean, "унаги"):
			return "Соус Унаги", false, true
		case strings.Contains(clean, "майонез"):
			return "Майонез профессиональный 67%", (detectedBrand == ""), true
		case strings.Contains(clean, "кетчуп") || strings.Contains(clean, "томатная паста"):
			return "Кетчуп / томатная паста", (detectedBrand == ""), true
		case strings.Contains(clean, "кимчи") || strings.Contains(clean, "кимчи"):
			return "Соус Кимчи", false, true
		case strings.Contains(clean, "шрирач") || strings.Contains(clean, "срирач"):
			return "Соус Шрирача", false, true
		case strings.Contains(clean, "песто"):
			return "Соус Песто", false, true
		}
	}

	// =========================================================================
	// 6. ОВОЩИ И ЗЕЛЕНЬ
	// =========================================================================
	if strings.Contains(clean, "томат") || strings.Contains(clean, "помидор") {
		if strings.Contains(clean, "черри") {
			return "Томаты черри", true, true
		}
		if strings.Contains(clean, "бакинск") || strings.Contains(clean, "розов") {
			return "Томаты розовые/бакинские", true, true
		}
		if strings.Contains(clean, "вялен") {
			return "Томаты вяленые", false, true
		}
		return "Томаты свежие", true, true
	}

	if strings.Contains(clean, "огур") {
		if strings.Contains(clean, "корнишон") || strings.Contains(clean, "маринован") || strings.Contains(clean, "солен") {
			return "Огурцы маринованные / корнишоны", true, true
		}
		if strings.Contains(clean, "длинноплод") || strings.Contains(clean, "гладк") {
			return "Огурцы гладкие длинноплодные", true, true
		}
		return "Огурцы свежие короткоплодные", true, true
	}

	if strings.Contains(clean, "авокадо") {
		if strings.Contains(clean, "хасс") || strings.Contains(clean, "hass") {
			return "Авокадо Хасс", true, true
		}
		return "Авокадо свежее", true, true
	}

	if strings.Contains(clean, "лук") {
		switch {
		case strings.Contains(clean, "зелен") || strings.Contains(clean, "перо"):
			return "Лук зеленый (перо)", true, true
		case strings.Contains(clean, "порей"):
			return "Лук порей", true, true
		case strings.Contains(clean, "красн") || strings.Contains(clean, "ялтинск"):
			return "Лук красный салатный", true, true
		case strings.Contains(clean, "фри") || strings.Contains(clean, "сушен") || strings.Contains(clean, "жарен"):
			return "Лук фри / сушеный / жареный", true, true
		default:
			return "Лук репчатый", true, true
		}
	}

	if strings.Contains(clean, "лист") && (strings.Contains(clean, "лайм") || strings.Contains(clean, "кафир") || strings.Contains(clean, "кафр")) {
		return "Листья лайма кафир (сухие/с/м)", false, true
	}

	if strings.Contains(clean, "салат") || strings.Contains(clean, "айсберг") || strings.Contains(clean, "романо") || strings.Contains(clean, "руккол") {
		switch {
		case strings.Contains(clean, "айсберг"):
			return "Салат Айсберг", true, true
		case strings.Contains(clean, "роман") || strings.Contains(clean, "ромэн"):
			return "Салат Романо", true, true
		case strings.Contains(clean, "руккол") || strings.Contains(clean, "рукол"):
			return "Руккола свежая", true, true
		case strings.Contains(clean, "шпинат"):
			return "Шпинат свежий", true, true
		case strings.Contains(clean, "микс"):
			return "Салатный микс", true, true
		}
	}

	if strings.Contains(clean, "яйцо") || strings.Contains(clean, "яйца") {
		if strings.Contains(clean, "перепел") {
			return "Яйцо перепелиное", true, true
		}
		if strings.Contains(clean, "с0") || strings.Contains(clean, "со") || strings.Contains(clean, "отборн") {
			return "Яйцо куриное С0", true, true
		}
		return "Яйцо куриное С1", true, true
	}

	// Соль — только отдельное слово
	if containsWord(clean, "соль") && !strings.Contains(clean, "фасол") && !strings.Contains(clean, "консол") {
		if strings.Contains(clean, "морск") {
			return "Соль морская", false, true
		}
		return "Соль экстра/поваренная", true, true
	}

	// Сахар и кондитерские подсластители
	if strings.Contains(clean, "тримолин") {
		return "Тримолин / инвертный сахар", false, true
	}
	if strings.Contains(clean, "изомальт") {
		return "Изомальт", false, true
	}
	if strings.Contains(clean, "сахар") && !strings.Contains(clean, "бумаг") {
		if strings.Contains(clean, "пудра") {
			return "Сахарная пудра", true, true
		}
		if strings.Contains(clean, "тростников") {
			return "Сахар тростниковый", false, true
		}
		return "Сахар-песок белый", true, true
	}

	return "", false, false
}
