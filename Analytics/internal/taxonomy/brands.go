package taxonomy

import (
	"strings"
)

// BrandType определяет сегмент бренда (профессиональный HoReCa, ритейл, B2B-завод)
type BrandType string

const (
	BrandHoReCaPro BrandType = "horeca_pro" // Специализированный бренд для профессиональной кухни
	BrandRetail    BrandType = "retail"     // Общеизвестный федеральный ритейл-бренд
	BrandB2B       BrandType = "b2b_monolit" // Оптовый производитель / завод (монолиты, мешки)
)

// BrandInfo хранит метаданные о выявленном бренде
type BrandInfo struct {
	CanonicalName string    // Каноническое отображаемое название (например "Чудское Озеро")
	Type          BrandType // Сегмент
	Country       string    // Страна происхождения бренда
	Aliases       []string  // Варианты написания в накладных iiko (рус/англ, транслит)
}

// knownBrandsRegistry — реестр торговых марок, встречающихся в российских ресторанных накладных HoReCa
var knownBrandsRegistry = []BrandInfo{
	// -------------------------------------------------------------
	// 1. МОЛОЧНАЯ ПРОДУКЦИЯ И СЛИВКИ ДЛЯ HORECA
	// -------------------------------------------------------------
	{
		CanonicalName: "Чудское Озеро",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"чудское озеро", "чудское", "chudskoe ozero", "chudskoe", "ч.о.", " ч о "},
	},
	{
		CanonicalName: "Петмол",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"петмол", "petmol"},
	},
	{
		CanonicalName: "Parmalat",
		Type:          BrandHoReCaPro,
		Country:       "IT/RU",
		Aliases:       []string{"parmalat", "пармалат", "parmalat chef", "chef parmalat"},
	},
	{
		CanonicalName: "Debic",
		Type:          BrandHoReCaPro,
		Country:       "NL/BE",
		Aliases:       []string{"debic", "дебик"},
	},
	{
		CanonicalName: "ЭкоНива",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"эконива", "ekoniva", "эко нива", "эконива professional", "ekoniva professional"},
	},
	{
		CanonicalName: "Белый Город",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"белый город", "белыйгород"},
	},
	{
		CanonicalName: "Молочная Речка",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"молочная речка", "мол.речка", "милком"},
	},
	{
		CanonicalName: "Campina",
		Type:          BrandHoReCaPro,
		Country:       "NL/RU",
		Aliases:       []string{"campina", "кампина", "фрислэнд"},
	},
	{
		CanonicalName: "Viola / Valio",
		Type:          BrandHoReCaPro,
		Country:       "FI/RU",
		Aliases:       []string{"viola", "виола", "valio", "валио"},
	},
	{
		CanonicalName: "Село Зеленое",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"село зеленое", "село зелёное"},
	},
	{
		CanonicalName: "Пестравка",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"пестравка"},
	},
	{
		CanonicalName: "Перммолоко",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"пермское", "перммолоко", "пермская"},
	},
	{
		CanonicalName: "Danone",
		Type:          BrandRetail,
		Country:       "FR/RU",
		Aliases:       []string{"danone", "данон", "health & nutrition", "эйч энд эн"},
	},
	{
		CanonicalName: "Простоквашино",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"простоквашино"},
	},
	{
		CanonicalName: "Домик в деревне",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"домик в деревне", "вимм-билль-данн", "вбд"},
	},

	// -------------------------------------------------------------
	// 2. РАСТИТЕЛЬНЫЕ СЛИВКИ И КРЕМЫ (ЗМЖ / ЗАМЕНИТЕЛИ МОЛОЧНОГО ЖИРА)
	// -------------------------------------------------------------
	{
		CanonicalName: "Шантипак (Puratos)",
		Type:          BrandHoReCaPro,
		Country:       "BE/RU",
		Aliases:       []string{"шантипак", "chantypak", "пуратос", "puratos"},
	},
	{
		CanonicalName: "Caselle",
		Type:          BrandHoReCaPro,
		Country:       "IT",
		Aliases:       []string{"caselle", "казелле"},
	},
	{
		CanonicalName: "Master Gourmet",
		Type:          BrandHoReCaPro,
		Country:       "IT",
		Aliases:       []string{"master gourmet", "мастер гурме", "мастергурме"},
	},
	{
		CanonicalName: "Hulala",
		Type:          BrandHoReCaPro,
		Country:       "IT",
		Aliases:       []string{"hulala", "хулала"},
	},
	{
		CanonicalName: "Беллария",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"беллария", "bellaria"},
	},
	{
		CanonicalName: "Dulcinea",
		Type:          BrandHoReCaPro,
		Country:       "ES",
		Aliases:       []string{"dulcinea", "дульсинея"},
	},
	{
		CanonicalName: "Cremio",
		Type:          BrandHoReCaPro,
		Country:       "BG",
		Aliases:       []string{"cremio", "кремио"},
	},
	{
		CanonicalName: "Meggle",
		Type:          BrandHoReCaPro,
		Country:       "DE",
		Aliases:       []string{"meggle", "меггле"},
	},

	// -------------------------------------------------------------
	// 3. АЭРОЗОЛЬНЫЕ ВЗБИТЫЕ СЛИВКИ (СПРЕЙ В БАЛЛОНАХ)
	// -------------------------------------------------------------
	{
		CanonicalName: "Anchor Spray",
		Type:          BrandHoReCaPro,
		Country:       "NZ",
		Aliases:       []string{"anchor spray", "анкор спрей", "anchor баллон"},
	},
	{
		CanonicalName: "Montare d'Oro",
		Type:          BrandHoReCaPro,
		Country:       "BE",
		Aliases:       []string{"montare d'oro", "монтаре д оро", "montare d oro"},
	},
	{
		CanonicalName: "President Spray",
		Type:          BrandRetail,
		Country:       "FR/RU",
		Aliases:       []string{"president спрей", "президент спрей", "president баллон"},
	},

	// -------------------------------------------------------------
	// 4. СЫРЫ HORECA (ТВОРОЖНЫЕ, МОЦАРЕЛЛА, ЧЕДДЕР, ПАРМЕЗАН)
	// -------------------------------------------------------------
	{
		CanonicalName: "Hochland Cremette",
		Type:          BrandHoReCaPro,
		Country:       "DE/RU",
		Aliases:       []string{"cremette", "креметте", "креметта", "креметто", "hochland cremette", "хохланд креметте"},
	},
	{
		CanonicalName: "Hochland",
		Type:          BrandHoReCaPro,
		Country:       "DE/RU",
		Aliases:       []string{"hochland", "хохланд"},
	},
	{
		CanonicalName: "Violette (Карат)",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"violette", "виолетте", "виолетта", "карат виолетте"},
	},
	{
		CanonicalName: "Cooking",
		Type:          BrandHoReCaPro,
		Country:       "BY",
		Aliases:       []string{"cooking", "кукинг", "туровский cooking"},
	},
	{
		CanonicalName: "Bonfesto",
		Type:          BrandHoReCaPro,
		Country:       "BY",
		Aliases:       []string{"bonfesto", "бонфесто", "туровский молочный комбинат", "тмк"},
	},
	{
		CanonicalName: "Galbani",
		Type:          BrandHoReCaPro,
		Country:       "IT/RU",
		Aliases:       []string{"galbani", "гальбани"},
	},
	{
		CanonicalName: "Unagrande",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"unagrande", "унагранде", "умалат", "umalat"},
	},
	{
		CanonicalName: "Pretto",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"pretto", "претто"},
	},
	{
		CanonicalName: "Philadelphia",
		Type:          BrandHoReCaPro,
		Country:       "US/EU",
		Aliases:       []string{"philadelphia", "филадельфия"},
	},
	{
		CanonicalName: "Senior Pizzaiolo",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"senior pizzaiolo", "сеньор пиццайоло", "ммз пиццайоло", "пиццайоло"},
	},
	{
		CanonicalName: "Milkana",
		Type:          BrandHoReCaPro,
		Country:       "FR/RU",
		Aliases:       []string{"milkana", "милкана"},
	},
	{
		CanonicalName: "Ичалки",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"ичалки", "ичалковский"},
	},
	{
		CanonicalName: "Брест-Литовск",
		Type:          BrandRetail,
		Country:       "BY",
		Aliases:       []string{"брест-литовск", "брест литовск", "савушкин"},
	},
	{
		CanonicalName: "Белебеевский",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"белебеевский"},
	},
	{
		CanonicalName: "Тысяча Озер",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"тысяча озер", "тысяча озёр"},
	},
	{
		CanonicalName: "Вкуснотеево",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"вкуснотеево", "молвест"},
	},

	// -------------------------------------------------------------
	// 5. РАСТИТЕЛЬНОЕ И КОКОСОВОЕ МОЛОКО
	// -------------------------------------------------------------
	{
		CanonicalName: "Alpro",
		Type:          BrandHoReCaPro,
		Country:       "BE/RU",
		Aliases:       []string{"alpro", "алпро"},
	},
	{
		CanonicalName: "Planto",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"planto", "планто"},
	},
	{
		CanonicalName: "Green Milk",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"green milk", "грин милк", "гринмилк"},
	},
	{
		CanonicalName: "Nemoloko",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"nemoloko", "немолоко"},
	},
	{
		CanonicalName: "Aroy-D",
		Type:          BrandHoReCaPro,
		Country:       "TH",
		Aliases:       []string{"aroy-d", "арой-д", "арой д", "аройд"},
	},
	{
		CanonicalName: "Chaokoh",
		Type:          BrandHoReCaPro,
		Country:       "TH",
		Aliases:       []string{"chaokoh", "чаокох"},
	},
	{
		CanonicalName: "Real THAI",
		Type:          BrandHoReCaPro,
		Country:       "TH",
		Aliases:       []string{"real thai", "реал тай"},
	},

	// -------------------------------------------------------------
	// 6. СОУСЫ, ПРИПРАВЫ И АЗИЯ
	// -------------------------------------------------------------
	{
		CanonicalName: "Kikkoman",
		Type:          BrandHoReCaPro,
		Country:       "JP/NL",
		Aliases:       []string{"kikkoman", "киккоман", "кикоман"},
	},
	{
		CanonicalName: "Heinz",
		Type:          BrandHoReCaPro,
		Country:       "US/RU",
		Aliases:       []string{"heinz", "хайнц"},
	},
	{
		CanonicalName: "Tamaki",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"tamaki", "тамаки"},
	},
	{
		CanonicalName: "ResFood",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"resfood", "ресфуд"},
	},
	{
		CanonicalName: "Sen Soy",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"sen soy", "сэн сой", "сен сой"},
	},
	{
		CanonicalName: "Astoria",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"astoria", "астория", "нмжк"},
	},
	{
		CanonicalName: "Слобода",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"слобода", "эфко"},
	},
	{
		CanonicalName: "Mr.Ricco",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"mr.ricco", "мистер рикко", "нэфис"},
	},

	// -------------------------------------------------------------
	// 7. КАРТОФЕЛЬ ФРИ И ПОЛУФАБРИКАТЫ
	// -------------------------------------------------------------
	{
		CanonicalName: "Lamb Weston",
		Type:          BrandHoReCaPro,
		Country:       "US/NL",
		Aliases:       []string{"lamb weston", "ламб вестон", "вестон", "we meijer"},
	},
	{
		CanonicalName: "Aviko",
		Type:          BrandHoReCaPro,
		Country:       "NL",
		Aliases:       []string{"aviko", "авико"},
	},
	{
		CanonicalName: "Lutosa",
		Type:          BrandHoReCaPro,
		Country:       "BE",
		Aliases:       []string{"lutosa", "лютоса", "лутоса"},
	},
	{
		CanonicalName: "McCain",
		Type:          BrandHoReCaPro,
		Country:       "CA/RU",
		Aliases:       []string{"mccain", "маккейн"},
	},
	{
		CanonicalName: "Farm Frites",
		Type:          BrandHoReCaPro,
		Country:       "NL",
		Aliases:       []string{"farm frites", "фарм фритес", "фарм фрайтс"},
	},

	// -------------------------------------------------------------
	// 8. МЯСО, ПТИЦА, МЯСОКОМБИНАТЫ
	// -------------------------------------------------------------
	{
		CanonicalName: "Мираторг",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"мираторг", "miratorg"},
	},
	{
		CanonicalName: "Черкизово",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"черкизово", "cherkizovo"},
	},
	{
		CanonicalName: "Петелинка",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"петелинка"},
	},
	{
		CanonicalName: "Приосколье",
		Type:          BrandRetail,
		Country:       "RU",
		Aliases:       []string{"приосколье"},
	},
	{
		CanonicalName: "Праймбиф",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"праймбиф", "primebeef"},
	},
	{
		CanonicalName: "Заречное",
		Type:          BrandHoReCaPro,
		Country:       "RU",
		Aliases:       []string{"заречное"},
	},
}

// DetectBrand анализирует строку наименования товара и выявляет торговую марку с учетом алиасов
func DetectBrand(rawName string) (string, *BrandInfo) {
	lower := strings.ToLower(rawName)
	clean := strings.ReplaceAll(lower, "ё", "е")

	for i := range knownBrandsRegistry {
		b := &knownBrandsRegistry[i]
		for _, alias := range b.Aliases {
			// Проверяем наличие алиаса в строке
			if strings.Contains(clean, alias) {
				return b.CanonicalName, b
			}
		}
	}
	return "", nil
}
