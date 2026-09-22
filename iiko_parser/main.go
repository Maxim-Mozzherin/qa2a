package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

var (
	db             *sql.DB
	encryptionKey  string
	externalApiKey string

	// Параметры нейросетевого парсера
	aiApiKey  string
	aiBaseUrl string
	aiModel   string

	// URL основного сервиса QA2A
	qa2aBaseURL string

	// Токен для суперадмина (Владельца платформы) для доступа к рыночной аналитике
	superadminToken string

	// Единый HTTP-клиент для вызовов iiko RMS API
	iikoHTTPClient = &http.Client{
		Timeout: 45 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Выделенный HTTP-клиент с увеличенным таймаутом для LLM API
	llmHTTPClient = &http.Client{
		Timeout: 300 * time.Second,
	}
)

type neuteredFileSystem struct {
	fs http.FileSystem
}

func (nfs neuteredFileSystem) Open(path string) (http.File, error) {
	f, err := nfs.fs.Open(path)
	if err != nil {
		return nil, err
	}
	s, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if s.IsDir() {
		f.Close()
		return nil, os.ErrPermission
	}
	return f, nil
}

func main() {
	_ = os.MkdirAll("temp", os.ModePerm)
	_ = os.MkdirAll("static", os.ModePerm)

	_ = godotenv.Load()
	_ = godotenv.Load("/opt/qa2a-reboot/.env")

	encryptionKey = getEnv("ENCRYPTION_KEY", "qa2a-reboot-default-aes-secret-key-32b")
	externalApiKey = getEnv("EXTERNAL_API_KEY", "moztech-secret-token-8099")

	aiApiKey = getEnv("AI_API_KEY", "sk-308827d72f902cf0-60fa88-4bff8692")
	aiBaseUrl = getEnv("AI_BASE_URL", "http://127.0.0.1:20128/v1/chat/completions")
	aiModel = getEnv("AI_MODEL", "gemini/gemini-3.5-flash,gemini/gemini-3.0-flash")

	qa2aBaseURL = getEnv("QA2A_URL", "http://127.0.0.1:8082")
	superadminToken = getEnv("SUPERADMIN_TOKEN", "boss-market-777")

	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5433")
	dbUser := getEnv("DB_USER", "admin")
	dbPass := getEnv("DB_PASS", "!123Maxim.!")
	dbName := getEnv("DB_NAME", "qa2a")
	serverPort := getEnv("PORT", "8099")

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable connect_timeout=10",
		dbHost, dbPort, dbUser, dbPass, dbName)

	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("❌ Ошибка открытия дескриптора PostgreSQL: %v", err)
	}

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(15 * time.Minute)

	ctxPing, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelPing()

	if err = db.PingContext(ctxPing); err != nil {
		log.Fatalf("❌ Сбой подключения к PostgreSQL на порту %s: %v", dbPort, err)
	}
	fmt.Printf("✅ Микросервис подключен к PostgreSQL (%s:%s/%s)\n", dbHost, dbPort, dbName)

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS purchase_history (
			id SERIAL PRIMARY KEY,
			company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
			invoice_date DATE NOT NULL,
			invoice_number VARCHAR(100) NOT NULL DEFAULT '',
			supplier_uuid VARCHAR(255) NOT NULL DEFAULT '',
			supplier_name VARCHAR(255) NOT NULL DEFAULT '',
			iiko_product_uuid VARCHAR(255) NOT NULL DEFAULT '',
			iiko_product_name VARCHAR(500) NOT NULL DEFAULT '',
			product_name_in_invoice VARCHAR(500) NOT NULL DEFAULT '',
			clean_category VARCHAR(255) NOT NULL DEFAULT '',
			brand VARCHAR(255) NOT NULL DEFAULT '',
			quantity NUMERIC(12, 3) NOT NULL DEFAULT 0,
			unit VARCHAR(50) NOT NULL DEFAULT 'кг/шт',
			multiplier NUMERIC(12, 4) NOT NULL DEFAULT 1.0,
			total_sum NUMERIC(12, 2) NOT NULL DEFAULT 0,
			price_per_base_unit NUMERIC(12, 2) NOT NULL DEFAULT 0,
			consignee TEXT NOT NULL DEFAULT '',
			shipper TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		);
		ALTER TABLE purchase_history 
		ADD COLUMN IF NOT EXISTS clean_category VARCHAR(255) DEFAULT '',
		ADD COLUMN IF NOT EXISTS brand VARCHAR(255) DEFAULT '',
		ADD COLUMN IF NOT EXISTS iiko_product_name VARCHAR(500) DEFAULT '',
		ADD COLUMN IF NOT EXISTS unit VARCHAR(50) DEFAULT 'кг/шт',
		ADD COLUMN IF NOT EXISTS consignee TEXT DEFAULT '',
		ADD COLUMN IF NOT EXISTS shipper TEXT DEFAULT '';
		CREATE INDEX IF NOT EXISTS idx_purchase_history_comp_date ON purchase_history (company_id, invoice_date DESC);
		CREATE INDEX IF NOT EXISTS idx_purchase_history_product ON purchase_history (company_id, iiko_product_uuid);
	`)
	if err != nil {
		log.Printf("⚠️ Предупреждение при авто-миграции purchase_history: %v", err)
	}

	initPromptPresets()
	initRootSuperadmin()

	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.Dir("./static")))
	parserUploadFS := neuteredFileSystem{fs: http.Dir("/opt/qa2a-reboot/uploads")}
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(parserUploadFS)))

	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/api/accountant-invite/generate", authMiddleware(handleGenerateAccountantInvite))
	mux.HandleFunc("/api/accountant-invite/register", handleRegisterAccountant)
	mux.HandleFunc("/api/invite/generate", authMiddleware(handleGenerateInvite))
	mux.HandleFunc("/api/companies", authMiddleware(handleCompanies))
	mux.HandleFunc("/api/catalog", authMiddleware(handleCatalog))
	mux.HandleFunc("/api/parse", authMiddleware(handleParse))
	mux.HandleFunc("/api/reconciliation/parse", authMiddleware(handleParseReconciliation))
	mux.HandleFunc("/api/reconciliation/registry", authMiddleware(handleGetReconciliationRegistry))
	mux.HandleFunc("/api/parser/presets", authMiddleware(handlePromptPresets))
	mux.HandleFunc("/api/parser/default-prompt", authMiddleware(handleDefaultPrompt))

	mux.HandleFunc("/api/import", authMiddleware(handleImport))
	mux.HandleFunc("/api/templates/save", authMiddleware(handleSaveTemplateProxy))
	mux.HandleFunc("/api/unlisted-operations", authMiddleware(handleGetUnlistedOperations))
	mux.HandleFunc("/api/unlisted-operations/resolve", authMiddleware(handleResolveUnlistedOperation))
	mux.HandleFunc("/api/unlisted-operations/reject", authMiddleware(handleRejectUnlistedOperation))
	mux.HandleFunc("/api/accounting/tickets", authMiddleware(handleGetAccountingTickets))
	mux.HandleFunc("/api/accounting/tickets/resolve", authMiddleware(handleUpdateAccountingTicket))

	mux.HandleFunc("/api/analytics", authMiddleware(handleAnalytics))
	mux.HandleFunc("/api/toxic-writeoffs", authMiddleware(handleToxicWriteoffs))
	mux.HandleFunc("/api/history/invoices", authMiddleware(handleGetHistoryInvoices))
	mux.HandleFunc("/api/history/invoice-items", authMiddleware(handleHistoryInvoiceItems))

	mux.HandleFunc("/api/market/search", handleMarketSearch)
	mux.HandleFunc("/api/market/dossier", handleMarketDossier)
	mux.HandleFunc("/api/market/arbitrage", handleMarketArbitrage)
	mux.HandleFunc("/api/market/supplier-dossier", handleMarketSupplierDossier)
	mux.HandleFunc("/api/market/cleanup", handleMarketCleanup)
	mux.HandleFunc("/api/market/inflation", handleMarketInflation)
	mux.HandleFunc("/api/market/volume", handleMarketVolume)
	mux.HandleFunc("/api/market/dumping", handleMarketDumping)
	mux.HandleFunc("/api/market/dependency", handleMarketDependency)
	mux.HandleFunc("/api/market/logistics", handleMarketLogistics)
	mux.HandleFunc("/api/market/share", handleMarketShare)
	mux.HandleFunc("/api/market/companies", handleMarketCompanies)
	mux.HandleFunc("/api/market/deals", handleMarketDeals)

	srv := &http.Server{
		Addr:              ":" + serverPort,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       300 * time.Second,
		WriteTimeout:      300 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		fmt.Printf("🚀 Сервер Bugh-Team запущен на http://127.0.0.1:%s\n", serverPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Ошибка работы сервера iiko_parser: %v\n", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	log.Println("⚠️ Остановка микросервиса iiko_parser...")
	ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	_ = srv.Shutdown(ctxShutdown)
	_ = db.Close()
	log.Println("✅ Микросервис iiko_parser безопасно остановлен.")
}

const defaultParserPrompt = `Ты — автоматический парсер накладных. Твоя задача: найти поставщика, получателя (грузополучателя), номер документа (УПД/ТОРГ-12) и все товары.
ОЧЕНЬ ВАЖНО: В названиях часто указана сложная фасовка (коробки, упаковки, граммы). Тебе нужно вычислить коэффициент перевода в базовые единицы (кг, литры или штуки) и вернуть его в поле ai_multiplier.

Правила расчета параметров:
1. ОСОБОЕ ПРАВИЛО ДЛЯ КОНСЕРВОВ (кукуруза, ананасы, горошек, оливки и т.д.):
   - В консервах часто пишут три значения: общий объем (мл), вес нетто (гр) и сухой вес без рассола (сух/сух./сухой).
   - Если единица измерения в накладной - "шт" (штуки, банки), а базовый учет в iiko всегда в КГ, то коэффициентом перевода (ai_multiplier) должен быть чистый СУХОЙ ВЕС (сух) одной банки в килограммах.
   - Например: "Кукуруза консервир. об425мл-н340гр-сух272гр кор1-12" -> пришел товар в "шт". Чистый вес кукурузы без жижи 272гр. Значит ai_multiplier = 0.272. Игнорируй "кор1-12", так как товар пришел в банках (шт), а не коробках.
   - Если сухого веса "сух" в названии нет, бери вес нетто в кг (н/нетто). Например: "Томаты нетто 400гр" -> ai_multiplier = 0.4.

2. Если указаны граммы для обычных весовых товаров (500 гр, 454гр, 800гр), переведи в кг -> 0.5, 0.454, 0.8.
3. Если указаны литры или килограммы в штучном товаре (Масло 5л, Соус 5.4 кг) -> 5.0, 5.4.
4. ПРАВИЛО РАЗЛИЧИЯ УПАКОВОК И КОРОБОК (КРИТИЧЕСКИ ВАЖНО):
   - Внимательно смотри на графу "Единица измерения" в самой таблице накладной.
   - Если единица измерения "уп", "упак", "шт", "пакет", "банка" -> ТОВАР ПРИЕХАЛ ПОШТУЧНО. Коэффициент (ai_multiplier) должен быть равен весу ОДНОЙ пачки. Даже если в названии написано "1.5кг кор1-6", бери только 1.5. НЕ УМНОЖАЙ на 6!
   - Если единица измерения "кор", "коробка", "ящ", "блок" -> ТОВАР ПРИЕХАЛ КОРОБКАМИ. Вот только тогда ты обязан умножить вес одной пачки на количество пачек в коробке (например, 1.5кг * 6 = 9.0).
5. Если товар УЖЕ пришел в весовых единицах (кг, л) и количество дробное (например 3.412 кг), то ai_multiplier = 1.0.
6. Название товара копируй ПОЛНОСТЬЮ, как в документе.

7. СТАВКА НДС (nds_percent):
   Найди для каждой позиции ставку НДС в процентах и верни числом (обычно это 20.0, 10.0 или 0.0). Если указано "без НДС", "0%", "без налога" или поле пустое — возвращай 0.0.

8. ЦЕНА (price) и СУММА (sum):
   Обязательно выгружай цену и итоговую сумму С УЧЕТОМ НДС (Всего с НДС / Сумма к оплате). Это критически важно!

9. Грузополучатель (consignee) и Грузоотправитель (shipper):
   - shipper: Ищи поле "Грузоотправитель и его адрес". Запиши в максимально полном виде.
   - consignee: Ищи поле "Грузополучатель и его адрес" или "Покупатель". Запиши в максимально полном виде.

10. ПРАВИЛО ДЛЯ ЛИСТОВЫХ ТОВАРОВ (Нори и т.д.):
    - Если в названии указано количество листов в пачке (нори 100л), а ед. измерения 'шт', то ai_multiplier = 100.0.

11. ПРАВИЛО ДЛЯ ИНТЕРВАЛЬНЫХ ОБЪЕМОВ И ВЕСОВ:
    - Всегда берите строго верхнюю (максимальную) границу интервала (для 470-505гр -> 0.505).

12. ПОДПИСЬ К ФАСОВКЕ ai_tip (ТЕКСТОВАЯ ПОДСКАЗКА ДЛЯ ЧЕЛОВЕКА):
    Разложи детально в текстовом виде фасовку (например: "1 шт = 5 л").

13. ДАТА ДОКУМЕНТА (doc_date):
    Найди дату составления документа и приведи её к формату YYYY-MM-DD.

14. Сумма без НДС (sum_without_nds):
    Считается из стоимости товаров без НДС (ставка 5).

15. СТРОГОЕ ПРАВИЛО: НЕ ВЫДУМЫВАТЬ И НЕ ДУБЛИРОВАТЬ НАЗВАНИЯ ТОВАРОВ!
    - Переписывай названия товаров СИМВОЛ В СИМВОЛ как в оригинале.
    - Никогда не дублируй предыдущую позицию, если в накладной написано другое название.
    - Если ИИ случайно распознает строку дважды или скопирует название соседней строки, это критическая ошибка!

16. ПРАВИЛО ДЛЯ ЧАЯ В ПАКЕТИКАХ:
    - ai_multiplier равен количеству пакетиков в упаковке (20.0, 100.0).

17. ПРАВИЛО ДЛЯ ЛИСТА БАМБУКА:
    - ai_multiplier = 100.0.

17. ПРАВИЛО ДЛЯ ГРИБОВ ШИМИДЖИ/ШИМЕДЖИ:
    - ai_multiplier = 0.15 (150 грамм).

18. ПРАВИЛО КОЛОНОК УПД И КОДОВ ОКЕИ (КРИТИЧЕСКИ ВАЖНО):
    - В таблице УПД перед количеством ВСЕГДА идет колонка 2 "Код единицы измерения" (коды 796, 778, 166, 112).
    - 796 — это код штуки (шт)! 778 — код упаковки (упак)! 166 — код кг!
    - КАТЕГОРИЧЕСКИ ЗАПРЕЩЕНО брать числа 796, 778, 166, 112 в качестве количества товара (quantity)!
    - Настоящее количество (quantity) ВСЕГДА находится в колонке 3 "Количество (объем)" (1.000, 2.000, 6.000, 12.000).
    - Сумму с налогом (sum) бери из графы 9.

19. Чистая категория (для аналитики рынка):
    - Выдели чистую категорию товара (clean_category). ВНИМАНИЕ: Категория ДОЛЖНА БЫТЬ СТРОГО одной из следующего списка: "Мясо и птица", "Рыба и морепродукты", "Овощи и фрукты", "Молочные продукты", "Бакалея", "Консервы", "Напитки", "Хозяйственные товары", "Прочее". Если товар не подходит ни под одну, пиши "Без категории".
    - Выведи бренд или производителя (brand), если он есть в названии (пример: "Мираторг", "Hochland", "Borealis"). Если бренда нет, оставь пустую строку "".

20. ВИЗУАЛЬНЫЙ ШУМ И ГАЛОЧКИ: На сканах часто присутствуют размашистые галочки ручкой, перечеркивающие соседние строки. Строго игнорируй их! Не позволяй галочкам сбивать привязку названия товара к его количеству и цене. Используй колонку 1 (номер по порядку) как жесткий горизонтальный якорь.
21. Если документ обрезан или является только частью накладной (например, нет итоговой суммы), просто извлеки те товары, которые видны на изображении.
22. ЖЕСТКОЕ ПРАВИЛО ДЛЯ УПАКОВОК И КОРОБОК:
Если в графе "Единица измерения" (Код 778 или текст упак/кор/ящ) указана упаковка, а в графе "Количество" стоит 1.000, НО в самом названии товара написано количество штук (например, "Пиво ... 24 шт" или "0.5л ... 20шт"), ТЫ ОБЯЗАН установить ai_multiplier равным этому числу из названия (24.0, 20.0). Ни в коем случае не оставляй ai_multiplier = 1.0 для таких случаев!

23. ЕДИНИЦА ИЗМЕРЕНИЯ (unit): Извлеки единицу измерения из документа (кг, л, шт, упак, порц) и запиши в поле unit.
24. БАЗОВАЯ ЕДИНИЦА (base_unit): Укажи базовую единицу измерения для складского учета (кг, л, шт, порц), в которой будет измеряться итоговое количество товара с учетом коэффициента ai_multiplier. Например, если в документе unit="упак" по 500г (ai_multiplier=0.5), то base_unit="кг". Если товар штучный (бутылки, банки), base_unit="шт".

25. ОРИЕНТАЦИЯ СТРАНИЦЫ И СКАНОВ:
    - Если переданное изображение (скан или фото) повернуто на 90°, 180° или 270° (боком или вверх ногами), мысленно поверни документ в правильное читаемое положение (текст горизонтально слева направо) перед чтением таблицы и извлечением данных. Никогда не пытайся читать повернутый текст столбцами сверху вниз.

26. ЖЕСТКОЕ ПРАВИЛО ЧТЕНИЯ ТАБЛИЦЫ: Сканируй строки строго по горизонтали слева направо. Название товара (колонка 2) должно АБСОЛЮТНО точно соответствовать количеству и сумме в этой же физической строке. Никогда не перемешивай названия товаров между строками из-за визуального шума.
27. ИТОГОВАЯ СУММА К ОПЛАТЕ: Найди в подвале документа графу "Всего к оплате" (или "Итого с НДС") и запиши это числовое значение в поле doc_printed_total_sum. Если не нашел, верни 0.0.

ВНИМАНИЕ: ТЕБЕ МОЖЕТ БЫТЬ ПЕРЕДАНО СРАЗУ НЕСКОЛЬКО ИЗОБРАЖЕНИЙ (ИЛИ СТРАНИЦ ТЕКСТА). ЭТО ВСЁ СТРАНИЦЫ ОДНОЙ И ТОЙ ЖЕ НАКЛАДНОЙ. ТЫ ОБЯЗАН ВНИМАТЕЛЬНО ИЗУЧИТЬ АБСОЛЮТНО ВСЕ ПЕРЕДАННЫЕ ИЗОБРАЖЕНИЯ И ИЗВЛЕЧЬ ТОВАРЫ СО ВСЕХ СТРАНИЦ, ОБЪЕДИНИВ ИХ В ОДИН ОБЩИЙ СПИСОК (МАССИВ items)!

КРИТИЧЕСКИ ВАЖНО: Если ты видишь фразы «Итого по странице», «Промежуточный итог» или промежуточные суммы — ИГНОРИРУЙ ИХ! Это не конец накладной! Продолжай парсить товары со следующих страниц.

Верни строго только JSON-объект без markdown и без пояснений:
{
  "vendor_name": "Название поставщика",
  "doc_number": "Номер документа",
  "doc_date": "YYYY-MM-DD",
  "consignee": "Грузополучатель и его адрес или Покупатель",
  "shipper": "Грузоотправитель и его адрес",
  "doc_printed_total_sum": 15400.00,
  "items": [
    {"num": 1, "name": "Название полностью", "clean_category": "Овощи и фрукты", "brand": "Фритто Аппетито", "quantity": 10.0, "unit": "упак", "base_unit": "кг", "price": 120.0, "sum": 1200.0, "sum_without_nds": 1000.0, "nds_percent": 20.0, "ai_multiplier": 0.55, "ai_tip": "1 шт = 550г"}
  ]
}`

const receiptParserPrompt = `Ты — специализированный парсер кассовых чеков из обычных розничных магазинов и супермаркетов (например: Пятерочка, Магнит, Лента, Метро, местный рынок).
Твоя задача: найти название магазина, дату чека и список купленных товаров.

ЖЕСТКИЕ ПРАВИЛА ДЛЯ РОЗНИЧНЫХ ЧЕКОВ:
1. Извлекай ТОЛЬКО 4 параметра для каждого товара: Название, Количество (вес или штуки), Цену за единицу и Итоговую сумму строки.
2. НДС (nds_percent) для розничных чеков игнорируем полностью — всегда ставь 0.0.
3. Коэффициент фасовки (ai_multiplier) для чеков не вычисляем — всегда ставь 1.0. Количество бери ровно то, что пробито в чеке (например, если пробито 0.450 кг, то quantity = 0.45, ai_multiplier = 1.0).
4. Сумма без НДС (sum_without_nds) всегда равна итоговой сумме (sum).
5. Грузополучатель (consignee) и Грузоотправитель (shipper) — оставляй пустыми строками "".
6. Имя поставщика (vendor_name) — это название магазина сверху чека (например "ООО Агроторг" или "Магазин Лента").
7. Номер документа (doc_number) — это номер чека (ФД, Чек №) или ФН. Если не нашел, пиши "Б/Н".
8. Выдели категорию товара clean_category ("Мясо и птица", "Рыба и морепродукты", "Овощи и фрукты", "Молочные продукты", "Бакалея", "Консервы", "Напитки", "Хозяйственные товары", "Прочее").

Если товар пробит как "Пакет майка" — можешь его игнорировать или записать в Хозяйственные товары.

Верни СТРОГО только JSON-объект (без markdown, без пояснений):
{
  "vendor_name": "Название магазина",
  "doc_number": "Номер чека",
  "doc_date": "YYYY-MM-DD",
  "consignee": "",
  "shipper": "",
  "items": [
    {
      "name": "Название товара из чека", 
      "clean_category": "Бакалея", 
      "brand": "", 
      "quantity": 1.5, 
      "unit": "шт",
      "base_unit": "шт",
      "price": 100.0, 
      "sum": 150.0, 
      "sum_without_nds": 150.0, 
      "nds_percent": 0.0, 
      "ai_multiplier": 1.0, 
      "ai_tip": "Кассовый чек"
    }
  ]
}`

const reconciliationPrompt = `Ты — парсер актов сверки взаиморасчетов от поставщиков. Твоя задача: найти только операции ОТГРУЗКИ товаров (Дебет / Продажа). 
ИГНОРИРУЙ операции ОПЛАТЫ (Кредит / Поступление денег на счет поставщика).
Для каждой отгрузки извлеки: номер документа (оставь как есть, не обрезай нули), дату и итоговую сумму отгрузки.
Верни СТРОГО JSON-объект без markdown:
{
  "items": [
    {"doc_number": "УПД-123", "doc_date": "YYYY-MM-DD", "amount": 15400.00}
  ]
}`

