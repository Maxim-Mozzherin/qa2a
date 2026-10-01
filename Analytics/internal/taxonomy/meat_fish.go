package taxonomy

import (
	"strings"
)

// ClassifyMeatFish классифицирует мясо, птицу, рыбу и морепродукты
// Возвращает (категория, isCommodity, matched)
func ClassifyMeatFish(clean string, detectedBrand string) (string, bool, bool) {

	// =========================================================================
	// 1. МЯСО ПТИЦЫ: Филе грудки vs Филе бедра vs Крылья vs Субпродукты
	// =========================================================================
	isPoultry := strings.Contains(clean, "кур") || strings.Contains(clean, "цыпл") ||
		strings.Contains(clean, "индейк") || strings.Contains(clean, "утиный") ||
		strings.Contains(clean, "утк") || strings.Contains(clean, "индееч")

	if isPoultry {
		if strings.Contains(clean, "индейк") || strings.Contains(clean, "индееч") {
			if strings.Contains(clean, "бедр") {
				return "Филе бедра индейки", true, true
			}
			return "Филе грудки индейки", true, true
		}

		if strings.Contains(clean, "утк") || strings.Contains(clean, "утин") {
			if strings.Contains(clean, "грудк") || strings.Contains(clean, "магре") {
				return "Утиная грудка (магре)", false, true
			}
			if strings.Contains(clean, "ножк") || strings.Contains(clean, "окороч") {
				return "Утиная ножка / конфи", false, true
			}
			return "Утка / утиное мясо", false, true
		}

		// Курица
		switch {
		case strings.Contains(clean, "печен"):
			return "Куриная печень", true, true
		case strings.Contains(clean, "сердц"):
			return "Куриные сердечки", true, true
		case strings.Contains(clean, "шейк") || strings.Contains(clean, "суповой"):
			return "Куриные шейки / суповой набор", true, true
		case strings.Contains(clean, "бедр") || strings.Contains(clean, "окороч"):
			return "Куриное филе бедра", true, true
		case strings.Contains(clean, "голен"):
			return "Куриная голень", true, true
		case strings.Contains(clean, "крыл"):
			return "Куриное крыло", true, true
		case strings.Contains(clean, "филе") || strings.Contains(clean, "грудк"):
			return "Куриное филе грудки", true, true
		case strings.Contains(clean, "ц/б") || strings.Contains(clean, "тушка"):
			return "Цыпленок-бройлер тушка", true, true
		}
	}

	// Отдельная проверка "филе бедра" без прямого слова "куриное"
	if strings.Contains(clean, "филе") && strings.Contains(clean, "бедр") {
		return "Куриное филе бедра", true, true
	}

	// =========================================================================
	// 2. ГОВЯДИНА И СВИНИНА
	// =========================================================================
	if strings.Contains(clean, "говядин") || strings.Contains(clean, "говяж") || strings.Contains(clean, "телятин") {
		switch {
		case strings.Contains(clean, "вырезк") || strings.Contains(clean, "тендерлойн"):
			return "Говяжья вырезка (тендерлойн)", false, true
		case strings.Contains(clean, "рибай") || strings.Contains(clean, "стриплойн") || strings.Contains(clean, "толстый край") || strings.Contains(clean, "тонкий край"):
			return "Говядина стейковая (рибай/стриплойн)", false, true
		case strings.Contains(clean, "брискет") || strings.Contains(clean, "грудинк"):
			return "Говяжья грудинка / брискет", true, true
		case strings.Contains(clean, "щек"):
			return "Говяжьи щечки", false, true
		case strings.Contains(clean, "лопатк") || strings.Contains(clean, "окорок") || strings.Contains(clean, "тазобедр"):
			return "Говядина мякоть (лопатка/окорок)", true, true
		case strings.Contains(clean, "фарш"):
			return "Фарш говяжий", true, true
		default:
			return "Говядина охлажденная/замороженная", true, true
		}
	}

	if strings.Contains(clean, "свинин") || strings.Contains(clean, "свин") {
		switch {
		case strings.Contains(clean, "шея") || strings.Contains(clean, "шейка"):
			return "Свиная шея", true, true
		case strings.Contains(clean, "вырезк"):
			return "Свиная вырезка", true, true
		case strings.Contains(clean, "ребр") || strings.Contains(clean, "ребрышк"):
			return "Свиные ребра", true, true
		case strings.Contains(clean, "корейк") || strings.Contains(clean, "карбонад"):
			return "Свиная корейка / карбонад", true, true
		case strings.Contains(clean, "окорок") || strings.Contains(clean, "лопатк"):
			return "Свинина мякоть (окорок/лопатка)", true, true
		case strings.Contains(clean, "грудинк") || strings.Contains(clean, "бекон"):
			return "Свиная грудинка / бекон", true, true
		}
	}

	// =========================================================================
	// 3. РЫБА И МОРЕПРОДУКТЫ (Красная рыба, Угорь, Тунец, Креветки)
	// =========================================================================
	if strings.Contains(clean, "лосос") || strings.Contains(clean, "семг") || strings.Contains(clean, "форел") {
		switch {
		case strings.Contains(clean, "филе") || strings.Contains(clean, "трим") || strings.Contains(clean, "trim"):
			return "Лосось/форель филе (Trim D/E)", false, true
		case strings.Contains(clean, "пбг") || strings.Contains(clean, "псг") || strings.Contains(clean, "тушка"):
			return "Лосось/форель тушка охл/с/м", false, true
		case strings.Contains(clean, "слабосол") || strings.Contains(clean, "с/с"):
			return "Лосось/форель с/с филе", false, true
		case strings.Contains(clean, "хребет") || strings.Contains(clean, "суповой") || strings.Contains(clean, "обрезь"):
			return "Лосось суповой набор / обрезь", true, true
		default:
			return "Лосось / семга / форель", false, true
		}
	}

	if strings.Contains(clean, "тунец") {
		return "Тунец филе (Саку/стейк)", false, true
	}

	if strings.Contains(clean, "угорь") {
		return "Угорь жареный унаги (Unagi)", false, true
	}

	if strings.Contains(clean, "креветк") {
		switch {
		case strings.Contains(clean, "тигров") || strings.Contains(clean, "16/20") || strings.Contains(clean, "21/25") || strings.Contains(clean, "16-20"):
			return "Креветки тигровые 16/20 - 21/25", true, true
		case strings.Contains(clean, "ваннамей") || strings.Contains(clean, "белоног") || strings.Contains(clean, "31/40") || strings.Contains(clean, "41/50"):
			return "Креветки ваннамей (белоногие)", true, true
		case strings.Contains(clean, "северн") || strings.Contains(clean, "70/90") || strings.Contains(clean, "90/120"):
			return "Креветки северные варено-мороженые", true, true
		default:
			return "Креветки", true, true
		}
	}

	if strings.Contains(clean, "кальмар") {
		if strings.Contains(clean, "щупальц") {
			return "Щупальца кальмара", true, true
		}
		return "Кальмар командорский (филе/тушка)", true, true
	}

	if strings.Contains(clean, "мидии") {
		return "Мидии (в створках/мясо)", true, true
	}

	return "", false, false
}
