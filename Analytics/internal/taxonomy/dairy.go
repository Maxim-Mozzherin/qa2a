package taxonomy

import (
	"strings"
)

// ClassifyDairy анализирует молочную группу с учетом жирности, физической формы и состава (животное vs ЗМЖ/растительное)
// Возвращает (категория, isCommodity, matched)
func ClassifyDairy(clean string, detectedBrand string) (string, bool, bool) {

	// =========================================================================
	// 1. СЛИВКИ: Устранение "эффекта взбитых сливок и просто сливок"
	// =========================================================================
	// В HoReCa существует 5 абсолютно разных типов сливок, которые нельзя смешивать:
	// А. Аэрозольные взбитые сливки в баллонах (готовый топпинг с газом N2O)
	// Б. Растительные кремы / сливки на заменителях молочного жира (Шантипак и т.д. ~190-230 ₽)
	// В. Сливки натуральные 33-35% кондитерские / для взбивания (~380-520 ₽)
	// Г. Сливки натуральные кулинарные 20-22% для соусов и паст (~260-320 ₽)
	// Д. Сливки питьевые / кофейные 10% (~150-190 ₽)
	// =========================================================================

	isCreamMentioned := strings.Contains(clean, "сливк") || strings.Contains(clean, "сливочн") ||
		strings.Contains(clean, "крем ") || strings.Contains(clean, "крем-") ||
		strings.Contains(clean, "chantypak") || strings.Contains(clean, "шантипак")

	// Проверяем, не является ли это творожным сыром ("сыр сливочный", "крем-чиз") или крем-супом
	isCheeseOrSoup := strings.Contains(clean, "сыр") || strings.Contains(clean, "чиз") ||
		strings.Contains(clean, "суп") || strings.Contains(clean, "соус") ||
		strings.Contains(clean, "креметт") || strings.Contains(clean, "cremette")

	if isCreamMentioned && !isCheeseOrSoup {
		// 1.1 Аэрозольные взбитые сливки в баллончиках (спрей)
		isSprayOrAerosol := strings.Contains(clean, "спрей") || strings.Contains(clean, "spray") ||
			strings.Contains(clean, "баллон") || strings.Contains(clean, "балон") ||
			strings.Contains(clean, "аэрозол") || strings.Contains(clean, "аэрозоль") ||
			strings.Contains(clean, "монтаре") || strings.Contains(clean, "montare") ||
			(strings.Contains(clean, "взбит") && (strings.Contains(clean, "дебик") || strings.Contains(clean, "debic") || strings.Contains(clean, "президент") || strings.Contains(clean, "president")) && strings.Contains(clean, "шт"))

		if isSprayOrAerosol {
			return "Сливки взбитые аэрозольные (спрей/баллон)", false, true
		}

		// 1.2 Растительные кремы для взбивания (ЗМЖ / аналог сливок)
		// Стоят почти в 2 раза дешевле натуральных животных сливок!
		isPlantOrVegetable := strings.Contains(clean, "шантипак") || strings.Contains(clean, "chantypak") ||
			strings.Contains(clean, "пуратос") || strings.Contains(clean, "puratos") ||
			strings.Contains(clean, "растительн") || strings.Contains(clean, "на растит") ||
			strings.Contains(clean, "змж") || strings.Contains(clean, "казелле") ||
			strings.Contains(clean, "caselle") || strings.Contains(clean, "мастер гурме") ||
			strings.Contains(clean, "master gourmet") || strings.Contains(clean, "беллария") ||
			strings.Contains(clean, "хулала") || strings.Contains(clean, "hulala") ||
			strings.Contains(clean, "дульсинея") || strings.Contains(clean, "dulcinea")

		if isPlantOrVegetable {
			return "Крем растительный для взбивания (ЗМЖ)", false, true
		}

		// 1.3 Сливки натуральные животные — разделение по жирности

		// 33% - 35% (Кондитерские, для взбивания)
		if strings.Contains(clean, "33") || strings.Contains(clean, "34") ||
			strings.Contains(clean, "35") || strings.Contains(clean, "36") ||
			strings.Contains(clean, "для взбивания") {
			return "Сливки 33-35% (для взбивания)", false, true
		}

		// 38% (Супер-жирные, Debic 38%)
		if strings.Contains(clean, "38") {
			return "Сливки 38% (высокожирные)", false, true
		}

		// 20% - 22% (Кулинарные, для паст и горячих соусов)
		if strings.Contains(clean, "20") || strings.Contains(clean, "22") ||
			strings.Contains(clean, "кулинарн") || strings.Contains(clean, "шеф") ||
			strings.Contains(clean, "chef") {
			return "Сливки кулинарные 20-22%", false, true
		}

		// 10% - 11% (Кофейные / питьевые)
		if strings.Contains(clean, "10") || strings.Contains(clean, "11") ||
			strings.Contains(clean, "питьев") || strings.Contains(clean, "для кофе") {
			return "Сливки питьевые / кофейные 10%", false, true
		}

		// Если жирность не указана в явном виде, но это профессиональные сливки
		if strings.Contains(clean, "петмол") || strings.Contains(clean, "чудское") || strings.Contains(clean, "parmalat") {
			// По умолчанию в HoReCa закупают 33% для десертов
			return "Сливки 33-35% (для взбивания)", false, true
		}

		return "Сливки кулинарные 20-22%", false, true
	}

	// =========================================================================
	// 2. МОЛОКО: Жирность, Barista, растительное и кокосовое
	// =========================================================================
	if strings.Contains(clean, "молоко") {
		// 2.1 Кокосовое молоко (для паназиатской кухни: Том Ям, карри, десерты)
		if strings.Contains(clean, "кокос") {
			if strings.Contains(clean, "сливки") || strings.Contains(clean, "20-22") || strings.Contains(clean, "24%") {
				return "Сливки кокосовые кулинарные", false, true
			}
			return "Молоко кокосовое кулинарное (Aroy-D/Chaokoh)", false, true
		}

		// 2.2 Растительное альтернативное молоко (миндаль, овес, соя, фундук)
		if strings.Contains(clean, "миндаль") || strings.Contains(clean, "соев") ||
			strings.Contains(clean, "овсян") || strings.Contains(clean, "фундук") ||
			strings.Contains(clean, "alpro") || strings.Contains(clean, "алпро") ||
			strings.Contains(clean, "planto") || strings.Contains(clean, "планто") ||
			strings.Contains(clean, "green milk") || strings.Contains(clean, "грин милк") ||
			strings.Contains(clean, "nemoloko") || strings.Contains(clean, "немолоко") {
			return "Молоко растительное (альтернативное)", false, true
		}

		// 2.3 Сгущенное молоко
		if strings.Contains(clean, "сгущ") || strings.Contains(clean, "варенка") {
			return "Молоко сгущенное цельное / вареное", false, true
		}

		// 2.4 Молоко Barista (специализированное ультрапастеризованное с белком 3.0-3.2g для микропены)
		if strings.Contains(clean, "бариста") || strings.Contains(clean, "barista") ||
			strings.Contains(clean, "для капучино") || strings.Contains(clean, "для кофе") {
			return "Молоко Barista ультрапастеризованное (для кофе)", false, true
		}

		// 2.5 Коровье молоко по процентам жирности
		if strings.Contains(clean, "2.5") || strings.Contains(clean, "2,5") {
			return "Молоко коровье 2.5%", (detectedBrand == ""), true
		}
		if strings.Contains(clean, "3.5") || strings.Contains(clean, "3,5") ||
			strings.Contains(clean, "3.8") || strings.Contains(clean, "3,8") ||
			strings.Contains(clean, "отборн") || strings.Contains(clean, "цельн") {
			return "Молоко коровье отборное 3.5-3.8%", (detectedBrand == ""), true
		}

		// По умолчанию для коровьего молока в HoReCa — стандарт 3.2%
		return "Молоко коровье 3.2%", (detectedBrand == ""), true
	}

	// =========================================================================
	// 3. МАСЛО СЛИВОЧНОЕ: 82.5% vs 72.5% vs Спреды/маргарины
	// =========================================================================
	if strings.Contains(clean, "масло") && (strings.Contains(clean, "сливоч") || strings.Contains(clean, "крестьян") || strings.Contains(clean, "традицион") || strings.Contains(clean, "82") || strings.Contains(clean, "72")) {
		if strings.Contains(clean, "спред") || strings.Contains(clean, "маргарин") || strings.Contains(clean, "растительно-жир") {
			return "Спред растительно-жировой / маргарин", true, true
		}
		if strings.Contains(clean, "82") || strings.Contains(clean, "82.5") || strings.Contains(clean, "82,5") || strings.Contains(clean, "традицион") {
			return "Масло сливочное 82.5% (Традиционное)", false, true
		}
		if strings.Contains(clean, "72") || strings.Contains(clean, "72.5") || strings.Contains(clean, "72,5") || strings.Contains(clean, "крестьян") {
			return "Масло сливочное 72.5% (Крестьянское)", false, true
		}
		// По умолчанию шефы берут 82.5%
		return "Масло сливочное 82.5% (Традиционное)", false, true
	}

	// =========================================================================
	// 4. СЫРЫ HORECA: Кремчиз, Моцарелла пицца vs рассольная, Чеддер, Пармезан
	// =========================================================================
	if strings.Contains(clean, "сыр") || strings.Contains(clean, "cremette") || strings.Contains(clean, "креметт") {
		// 4.1 Творожный сыр / Кремчиз (Креметте, Виолетте, Кукинг)
		if strings.Contains(clean, "творож") || strings.Contains(clean, "креметт") ||
			strings.Contains(clean, "cremette") || strings.Contains(clean, "чиз") ||
			strings.Contains(clean, "кукинг") || strings.Contains(clean, "cooking") ||
			strings.Contains(clean, "виолетт") || strings.Contains(clean, "violette") ||
			strings.Contains(clean, "сливочный творожный") || strings.Contains(clean, "cream cheese") {
			return "Сыр творожный / кремчиз (Cream Cheese)", false, true
		}

		// 4.2 Моцарелла: Строгое разделение Моцарелла для пиццы (блок/тертая) vs Рассольная (шарики беби)
		if strings.Contains(clean, "моцарелл") || strings.Contains(clean, "моццарелл") {
			isBrineOrSalad := strings.Contains(clean, "рассол") || strings.Contains(clean, "беби") ||
				strings.Contains(clean, "бэби") || strings.Contains(clean, "чильед") ||
				strings.Contains(clean, "боккончин") || strings.Contains(clean, "шарик")

			if isBrineOrSalad {
				return "Сыр Моцарелла рассольная (шарики/салатная)", false, true
			}
			// Для пиццы и запекания: блоки, бруски, тертый, фиор ди латте
			return "Сыр Моцарелла для пиццы (полутвердый/бруски)", false, true
		}

		// 4.3 Чеддер (слайсы для бургеров vs блок)
		if strings.Contains(clean, "чеддер") || strings.Contains(clean, "cheddar") {
			return "Сыр Чеддер (слайсы/блок)", false, true
		}

		// 4.4 Пармезан / Грана Падано (твердые сыры)
		if strings.Contains(clean, "пармезан") || strings.Contains(clean, "parmesan") ||
			strings.Contains(clean, "грана") || strings.Contains(clean, "grana") ||
			strings.Contains(clean, "джугас") || strings.Contains(clean, "djiugas") {
			return "Сыр Пармезан / Грана (твердый)", false, true
		}

		// 4.5 Рассольные сыры для салатов (Фета, Сиртаки, Брынза)
		if strings.Contains(clean, "фета") || strings.Contains(clean, "сиртаки") ||
			strings.Contains(clean, "брынз") || strings.Contains(clean, "фетакса") {
			return "Сыр Фета / Сиртаки / Брынза", false, true
		}

		// 4.6 Сыры с плесенью (Дор блю, Горгонзола, Бри, Камамбер)
		if strings.Contains(clean, "дор блю") || strings.Contains(clean, "дорблю") ||
			strings.Contains(clean, "горгонзол") || strings.Contains(clean, "голубой") ||
			strings.Contains(clean, "с плесенью") {
			return "Сыр с голубой плесенью (Дор Блю/Горгонзола)", false, true
		}
		if strings.Contains(clean, "бри") || strings.Contains(clean, "камамбер") {
			return "Сыр с белой плесенью (Бри/Камамбер)", false, true
		}

		// 4.7 Полутвердые столовые сыры (Гауда, Российский, Тильзитер)
		if strings.Contains(clean, "гауда") || strings.Contains(clean, "российск") ||
			strings.Contains(clean, "тильзитер") || strings.Contains(clean, "маасдам") {
			return "Сыр полутвердый столовый (Гауда/Тильзитер)", false, true
		}
	}

	return "", false, false
}
