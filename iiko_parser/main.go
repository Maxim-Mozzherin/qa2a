package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"iiko_parser/crypto"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

// ============================================================================
// ГЛОБАЛЬНЫЕ ПЕРЕМЕННЫЕ И НАСТРОЙКИ
// ============================================================================

var (
	db             *sql.DB
	buhLogin       string
	buhPassword    string
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

// ============================================================================
// СТРУКТУРЫ ДАННЫХ
// ============================================================================

type Company struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Mapping struct {
	InternalUUID string
	InternalName string
	Multiplier   float64
}

type AiResponse struct {
	VendorName string   `json:"vendor_name"`
	DocNumber  string   `json:"doc_number"`
	DocDate    string   `json:"doc_date"`
	Consignee  string   `json:"consignee"`
	Shipper    string   `json:"shipper"`
	Items      []AiItem `json:"items"`
}

type AiItem struct {
	Name          string  `json:"name"`
	CleanCategory string  `json:"clean_category"` // Базовая категория (для рынка)
	Brand         string  `json:"brand"`          // Производитель/Бренд (для рынка)
	Quantity      float64 `json:"quantity"`
	Price         float64 `json:"price"`
	Sum           float64 `json:"sum"`
	SumWithoutNds float64 `json:"sum_without_nds"`
	NdsPercent    float64 `json:"nds_percent"`
	AiMultiplier  float64 `json:"ai_multiplier"`
	AiTip         string  `json:"ai_tip"`
}

type XMLProducts struct {
	List []struct {
		ID          string `xml:"id"`
		Name        string `xml:"name"`
		ProductType string `xml:"productType"`
		Type        string `xml:"type"`
	} `xml:"productDto"`
}

type IikoProduct struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type IikoStore struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type IikoSupplier struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type UnlistedOperation struct {
	ID           int       `json:"id"`
	PositionName string    `json:"position_name"`
	Quantity     float64   `json:"quantity"`
	Unit         string    `json:"unit"`
	Comment      string    `json:"comment"`
	CreatedAt    time.Time `json:"created_at"`
	UserName     string    `json:"user_name"`
}

// Запись о закупке для локального дашборда бухгалтера
type PurchaseRecord struct {
	InvoiceDate     string  `json:"invoice_date"`
	InvoiceNum      string  `json:"invoice_number"`
	SupplierName    string  `json:"supplier_name"`
	ProductUUID     string  `json:"iiko_product_uuid"`
	IikoProductName string  `json:"iiko_product_name"`
	ProductName     string  `json:"product_name"`
	Quantity        float64 `json:"quantity"`
	Unit            string  `json:"unit"`
	Multiplier      float64 `json:"multiplier"`
	TotalSum        float64 `json:"total_sum"`
	PricePerUnit    float64 `json:"price_per_base_unit"`
}

// Запись для рыночной сводки (Суперадмин)
type MarketRecord struct {
	RestaurantName  string  `json:"restaurant_name"`
	InvoiceDate     string  `json:"invoice_date"`
	SupplierName    string  `json:"supplier_name"`
	IikoProductName string  `json:"iiko_product_name"`
	ProductName     string  `json:"product_name_in_invoice"`
	CleanCategory   string  `json:"clean_category"`
	Brand           string  `json:"brand"`
	PricePerUnit    float64 `json:"price_per_base_unit"`
}

// ============================================================================
// ТОЧКА ВХОДА И ИНИЦИАЛИЗАЦИЯ
// ============================================================================

func main() {
	_ = os.MkdirAll("temp", os.ModePerm)
	_ = os.MkdirAll("static", os.ModePerm)

	_ = godotenv.Load()
	_ = godotenv.Load("/opt/qa2a-reboot/.env")

	buhLogin = getEnv("ACCOUNTANT_LOGIN", "buh")
	buhPassword = getEnv("ACCOUNTANT_PASSWORD", "password")
	encryptionKey = getEnv("ENCRYPTION_KEY", "qa2a-reboot-default-aes-secret-key-32b")
	externalApiKey = getEnv("EXTERNAL_API_KEY", "moztech-secret-token-8099")

	aiApiKey = getEnv("AI_API_KEY", "sk-308827d72f902cf0-60fa88-4bff8692")
	aiBaseUrl = getEnv("AI_BASE_URL", "http://127.0.0.1:20128/v1/chat/completions")
	aiModel = getEnv("AI_MODEL", "gemini/gemini-3.1-flash-lite")

	qa2aBaseURL = getEnv("QA2A_URL", "http://127.0.0.1:8082")
	superadminToken = getEnv("SUPERADMIN_TOKEN", "boss-market-777") // Пароль от дашборда рынка

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

	// Авто-миграция БД для аналитики (Добавляем колонки бренда, категории и номенклатуры iiko без потери данных)
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
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		);
		ALTER TABLE purchase_history 
		ADD COLUMN IF NOT EXISTS clean_category VARCHAR(255) DEFAULT '',
		ADD COLUMN IF NOT EXISTS brand VARCHAR(255) DEFAULT '',
		ADD COLUMN IF NOT EXISTS iiko_product_name VARCHAR(500) DEFAULT '',
		ADD COLUMN IF NOT EXISTS unit VARCHAR(50) DEFAULT 'кг/шт';
		CREATE INDEX IF NOT EXISTS idx_purchase_history_comp_date ON purchase_history (company_id, invoice_date DESC);
		CREATE INDEX IF NOT EXISTS idx_purchase_history_product ON purchase_history (company_id, iiko_product_uuid);
	`)
	if err != nil {
		log.Printf("⚠️ Предупреждение при авто-миграции purchase_history: %v", err)
	}

	mux := http.NewServeMux()

	// Статический фронтенд личного кабинета бухгалтера
	mux.Handle("/", http.FileServer(http.Dir("./static")))

	// Маршруты API кабинета
	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/api/companies", authMiddleware(handleCompanies))
	mux.HandleFunc("/api/catalog", authMiddleware(handleCatalog))
	mux.HandleFunc("/api/parse", authMiddleware(handleParse))
	mux.HandleFunc("/api/import", authMiddleware(handleImport))
	mux.HandleFunc("/api/templates/save", authMiddleware(handleSaveTemplateProxy))
	mux.HandleFunc("/api/unlisted-operations", authMiddleware(handleGetUnlistedOperations))
	mux.HandleFunc("/api/unlisted-operations/resolve", authMiddleware(handleResolveUnlistedOperation))
	
	// Внутренняя аналитика для заведения (Бухгалтер)
	mux.HandleFunc("/api/analytics", authMiddleware(handleAnalytics))

	// Глобальная аналитика рынка (Суперадмин/Консалтинг)
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

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// ============================================================================
// АВТОРИЗАЦИЯ БУХГАЛТЕРА
// ============================================================================

func generateAuthToken(login, password string) string {
	return crypto.HashPasswordSHA1(login + ":" + password)
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		expectedToken := generateAuthToken(buhLogin, buhPassword)
		if token != expectedToken {
			http.Error(w, "Unauthorized (Недействительный токен авторизации)", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	if req.Login != buhLogin || req.Password != buhPassword {
		http.Error(w, "Неверный логин или пароль", http.StatusForbidden)
		return
	}

	token := generateAuthToken(buhLogin, buhPassword)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"token":  token,
	})
}

// ============================================================================
// СПРАВОЧНИКИ И СИНХРОНИЗАЦИЯ
// ============================================================================

func handleCompanies(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, name FROM companies WHERE iiko_host != '' ORDER BY name ASC")
	if err != nil {
		http.Error(w, "Ошибка чтения списка заведений: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list []Company
	for rows.Next() {
		var c Company
		if err := rows.Scan(&c.ID, &c.Name); err == nil {
			list = append(list, c)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func handleCatalog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CompanyID int `json:"company_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	var host, login, encryptedPass string
	err := db.QueryRow("SELECT iiko_host, iiko_api_login, iiko_api_password FROM companies WHERE id = $1", req.CompanyID).
		Scan(&host, &login, &encryptedPass)
	if err != nil {
		http.Error(w, "Заведение с указанным ID не найдено в базе", http.StatusNotFound)
		return
	}

	password, err := crypto.Decrypt(encryptedPass, encryptionKey)
	if err != nil {
		http.Error(w, "Ошибка дешифрования пароля iiko RMS: "+err.Error(), http.StatusInternalServerError)
		return
	}

	token, err := authIiko(host, login, password)
	if err != nil {
		http.Error(w, "Ошибка авторизации на сервере iiko: "+err.Error(), http.StatusUnauthorized)
		return
	}

	catalog, err := fetchIikoCatalog(host, token)
	if err != nil {
		http.Error(w, "Ошибка загрузки каталога iiko: "+err.Error(), http.StatusInternalServerError)
		return
	}

	stores, _ := fetchIikoStores(host, token)
	suppliers, _ := fetchIikoSuppliers(host, token)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":     token,
		"catalog":   catalog,
		"stores":    stores,
		"suppliers": suppliers,
	})
}

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ НОРМАЛИЗАЦИИ
// ============================================================================

// normalizeVendorName очищает наименование поставщика от организационно-правовых форм (ООО, ИП и др.),
// кавычек всех типов, знаков препинания и лишних пробелов для устойчивого сопоставления.
func normalizeVendorName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}

	s = strings.ReplaceAll(s, "ё", "е")

	quotes := []string{"\"", "«", "»", "'", "`", "“", "”", "„"}
	for _, q := range quotes {
		s = strings.ReplaceAll(s, q, " ")
	}

	// Отрезаем ИНН и КПП, если они содержатся в строке поставщика
	if idx := strings.Index(s, "инн"); idx != -1 {
		s = s[:idx]
	}

	legalForms := []string{
		"общество с ограниченной ответственностью",
		"индивидуальный предприниматель",
		"акционерное общество",
		"публичное акционерное общество",
		"закрытое акционерное общество",
		"товарищество с ограниченной ответственностью",
		"ооо",
		"ип",
		"ао",
		"пао",
		"зао",
		"тоо",
	}
	for _, lf := range legalForms {
		s = strings.ReplaceAll(s, " "+lf+" ", " ")
		if strings.HasPrefix(s, lf+" ") {
			s = strings.TrimPrefix(s, lf+" ")
		}
		if strings.HasSuffix(s, " "+lf) {
			s = strings.TrimSuffix(s, " "+lf)
		}
	}

	punct := []string{",", ".", ";", ":", "!", "?", "/", "\\", "(", ")", "[", "]", "-"}
	for _, p := range punct {
		s = strings.ReplaceAll(s, p, " ")
	}

	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// normalizeItemName нормализует наименование товара: переводит в нижний регистр,
// убирает кавычки, неразрывные пробелы, спецсимволы и схлопывает множественные пробелы.
func normalizeItemName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "ё", "е")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")

	quotes := []string{"\"", "«", "»", "'", "`", "“", "”"}
	for _, q := range quotes {
		s = strings.ReplaceAll(s, q, " ")
	}

	punct := []string{"-", "/", "\\", ",", ".", ";", ":", "!", "?", "(", ")", "[", "]", "{", "}"}
	for _, p := range punct {
		s = strings.ReplaceAll(s, p, " ")
	}

	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// ============================================================================
// НЕЙРОСЕТЕВОЙ ПАРСИНГ УПД В PDF
// ============================================================================

func handleParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(100 << 20); err != nil {
		http.Error(w, "Too large (max 100 MB per batch)", http.StatusBadRequest)
		return
	}

	companyIDStr := r.FormValue("company_id")
	if companyIDStr == "" {
		http.Error(w, "Не передан обязательный параметр company_id", http.StatusBadRequest)
		return
	}

	var companyID int
	if _, err := fmt.Sscanf(companyIDStr, "%d", &companyID); err != nil || companyID <= 0 {
		http.Error(w, "Некорректный ID заведения", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["pdf"]
	if len(files) == 0 {
		http.Error(w, "Files not found", http.StatusBadRequest)
		return
	}

	var textBytes []byte
	var imagesBase64 []string

	ctxCmd, cancelCmd := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancelCmd()

	for i, header := range files {
		file, err := header.Open()
		if err != nil {
			log.Printf("Error opening file %s: %v", header.Filename, err)
			continue
		}

		cleanFileName := filepath.Base(header.Filename)
		tempBase := fmt.Sprintf("upd_%d_%d_%d_%s", companyID, time.Now().UnixNano(), i, cleanFileName)
		filePath := filepath.Join("temp", tempBase)

		out, err := os.Create(filePath)
		if err != nil {
			file.Close()
			http.Error(w, "Temp file creation error", http.StatusInternalServerError)
			return
		}
		if _, err = io.Copy(out, file); err != nil {
			out.Close()
			file.Close()
			http.Error(w, "Save error", http.StatusInternalServerError)
			return
		}
		out.Close()
		file.Close()

		ext := strings.ToLower(filepath.Ext(cleanFileName))

		if ext == ".pdf" {
			txtPath := filePath + ".txt"
			cmdTxt := exec.CommandContext(ctxCmd, "pdftotext", "-layout", filePath, txtPath)
			_ = cmdTxt.Run()
			tb, _ := os.ReadFile(txtPath)
			textBytes = append(textBytes, tb...)
			textBytes = append(textBytes, []byte("\n\n")...)

			imgPrefix := filePath + "_img"
			cmdImg := exec.CommandContext(ctxCmd, "pdftoppm", "-jpeg", "-f", "1", "-l", "10", filePath, imgPrefix)
			if err := cmdImg.Run(); err != nil {
				log.Printf("pdftoppm error: %v", err)
			}

			matches, _ := filepath.Glob(imgPrefix + "-*.jpg")
			for _, m := range matches {
				imgBytes, err := os.ReadFile(m)
				if err == nil {
					imagesBase64 = append(imagesBase64, base64.StdEncoding.EncodeToString(imgBytes))
				}
				os.Remove(m)
			}
			os.Remove(txtPath)
		} else if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" {
			imgBytes, err := os.ReadFile(filePath)
			if err == nil {
				imagesBase64 = append(imagesBase64, base64.StdEncoding.EncodeToString(imgBytes))
			}
		} else {
			tb, _ := os.ReadFile(filePath)
			textBytes = append(textBytes, tb...)
			textBytes = append(textBytes, []byte("\n\n")...)
		}

		os.Remove(filePath)
	}

	aiData, err := parseWithClaude(string(textBytes), imagesBase64)
	if err != nil {
		http.Error(w, "Сбой распознавания AI: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 1. Поиск сопоставленного поставщика в iiko (с нормализацией наименования)
	mappedSupplierUUID := ""
	normVendor := normalizeVendorName(aiData.VendorName)
	suppRows, err := db.Query("SELECT vendor_name, iiko_supplier_uuid FROM supplier_mappings WHERE company_id = $1", companyID)
	if err == nil {
		defer suppRows.Close()
		for suppRows.Next() {
			var vName, suppUUID string
			if err := suppRows.Scan(&vName, &suppUUID); err == nil {
				if vName == aiData.VendorName {
					mappedSupplierUUID = suppUUID
					break
				}
				if normVendor != "" && normalizeVendorName(vName) == normVendor && mappedSupplierUUID == "" {
					mappedSupplierUUID = suppUUID
				}
			}
		}
	}

	// 2. Поиск сопоставленного склада в iiko (по грузополучателю)
	mappedStoreUUID := ""
	if aiData.Consignee != "" {
		_ = db.QueryRow("SELECT iiko_store_uuid FROM store_mappings WHERE company_id = $1 AND consignee = $2", companyID, aiData.Consignee).
			Scan(&mappedStoreUUID)
		if mappedStoreUUID == "" {
			_ = db.QueryRow("SELECT iiko_store_uuid FROM store_mappings WHERE company_id = $1 AND ($2 ILIKE '%' || consignee || '%' OR consignee ILIKE '%' || $2 || '%') LIMIT 1", companyID, strings.TrimSpace(aiData.Consignee)).
				Scan(&mappedStoreUUID)
		}
	}

	// 3. Загрузка маппингов товаров для активного заведения
	type candidateMapping struct {
		VendorName   string
		VendorItem   string
		InternalUUID string
		InternalName string
		Multiplier   float64
	}

	var allCompanyMappings []candidateMapping
	mapRows, err := db.Query("SELECT vendor_name, vendor_item_name, iiko_product_uuid, iiko_product_name, multiplier FROM product_mappings WHERE company_id = $1", companyID)
	if err == nil {
		defer mapRows.Close()
		for mapRows.Next() {
			var cm candidateMapping
			if err := mapRows.Scan(&cm.VendorName, &cm.VendorItem, &cm.InternalUUID, &cm.InternalName, &cm.Multiplier); err == nil {
				allCompanyMappings = append(allCompanyMappings, cm)
			}
		}
	}

	// Фильтруем маппинги текущего поставщика (прямое совпадение или через нормализацию ООО/кавычек)
	var vendorCandidates []candidateMapping
	for _, cm := range allCompanyMappings {
		if cm.VendorName == aiData.VendorName || (normVendor != "" && normalizeVendorName(cm.VendorName) == normVendor) {
			vendorCandidates = append(vendorCandidates, cm)
		}
	}

	type EnrichedItem struct {
		AiItem
		MappedUUID        string  `json:"mapped_uuid"`
		MappedName        string  `json:"mapped_name"`
		Multiplier        float64 `json:"multiplier"`
		IsAiGuessed       bool    `json:"is_ai_guessed"`
		IsWeightChanged   bool    `json:"is_weight_changed"`
		HistoryMultiplier float64 `json:"history_multiplier"`
	}

	var resultItems []EnrichedItem
	for _, item := range aiData.Items {
		enriched := EnrichedItem{
			AiItem:     item,
			Multiplier: 1.0,
		}

		itemNorm := normalizeItemName(item.Name)
		var matched *candidateMapping

		// Уровень 1: Точное совпадение наименования у данного поставщика
		for _, cm := range vendorCandidates {
			if cm.VendorItem == item.Name {
				matched = &cm
				break
			}
		}

		// Уровень 2: Нормализованное совпадение (без кавычек, спецсимволов и лишних пробелов)
		if matched == nil && itemNorm != "" {
			for _, cm := range vendorCandidates {
				if normalizeItemName(cm.VendorItem) == itemNorm {
					matched = &cm
					break
				}
			}
		}

		// Уровень 3: Префиксное / подстрочное совпадение у данного поставщика
		if matched == nil && len(itemNorm) >= 4 {
			for _, cm := range vendorCandidates {
				cNorm := normalizeItemName(cm.VendorItem)
				if len(cNorm) >= 4 && (strings.HasPrefix(itemNorm, cNorm) || strings.HasPrefix(cNorm, itemNorm)) {
					matched = &cm
					break
				}
			}
		}

		// Уровень 4 (Резервный): Точное или нормализованное совпадение по другим поставщикам этого же заведения
		if matched == nil {
			for _, cm := range allCompanyMappings {
				if cm.VendorItem == item.Name || (itemNorm != "" && normalizeItemName(cm.VendorItem) == itemNorm) {
					matched = &cm
					break
				}
			}
		}

		if matched != nil {
			enriched.MappedUUID = matched.InternalUUID
			enriched.MappedName = matched.InternalName
			enriched.Multiplier = matched.Multiplier
			enriched.IsAiGuessed = false

			if item.AiMultiplier > 0 && item.AiMultiplier != 1.0 && item.AiMultiplier != matched.Multiplier {
				enriched.IsWeightChanged = true
				enriched.HistoryMultiplier = matched.Multiplier
				enriched.Multiplier = item.AiMultiplier
			}
		} else {
			if item.AiMultiplier > 0 {
				enriched.Multiplier = item.AiMultiplier
				if item.AiMultiplier != 1.0 {
					enriched.IsAiGuessed = true
				}
			}
		}
		resultItems = append(resultItems, enriched)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"vendor_name":          aiData.VendorName,
		"doc_number":           aiData.DocNumber,
		"doc_date":             aiData.DocDate,
		"consignee":            aiData.Consignee,
		"shipper":              aiData.Shipper,
		"mapped_supplier_uuid": mappedSupplierUUID,
		"mapped_store_uuid":    mappedStoreUUID,
		"items":                resultItems,
	})
}

// ============================================================================
// ИМПОРТ ПРИХОДНОЙ НАКЛАДНОЙ В IIKO RMS И СОХРАНЕНИЕ АНАЛИТИКИ
// ============================================================================

func handleImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CompanyID     int    `json:"company_id"`
		Token         string `json:"token"`
		StoreUUID     string `json:"store_uuid"`
		SupplierUUID  string `json:"supplier_uuid"`
		VendorName    string `json:"vendor_name"`
		Consignee     string `json:"consignee"`
		Shipper       string `json:"shipper"`
		InvoiceNumber string `json:"invoice_number"`
		InvoiceDate   string `json:"invoice_date"`
		Items         []struct {
			Name          string  `json:"name"`
			CleanCategory string  `json:"clean_category"` // Новые поля из AI
			Brand         string  `json:"brand"`          // Новые поля из AI
			Quantity      float64 `json:"quantity"`
			Price         float64 `json:"price"`
			Sum           float64 `json:"sum"`
			SumWithoutNds float64 `json:"sum_without_nds"`
			NdsPercent    float64 `json:"nds_percent"`
			MappedUUID    string  `json:"mapped_uuid"`
			MappedName    string  `json:"mapped_name"`
			Multiplier    float64 `json:"multiplier"`
		} `json:"items"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	var host string
	err := db.QueryRow("SELECT iiko_host FROM companies WHERE id = $1", req.CompanyID).Scan(&host)
	if err != nil {
		http.Error(w, "Заведение не найдено в базе данных", http.StatusNotFound)
		return
	}

	parsedDocDate := time.Now()
	if req.InvoiceDate != "" {
		if pt, err := time.Parse("2006-01-02", req.InvoiceDate); err == nil {
			parsedDocDate = pt
		}
	}
	dbInvoiceDate := parsedDocDate.Format("2006-01-02")

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Ошибка транзакции базы данных", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	for _, item := range req.Items {
		if item.MappedUUID != "" {
			// 1. Сохраняем маппинг
			_, err = tx.Exec(`
				INSERT INTO product_mappings (company_id, vendor_name, vendor_item_name, iiko_product_uuid, iiko_product_name, multiplier, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, now())
				ON CONFLICT (company_id, vendor_name, vendor_item_name)
				DO UPDATE SET iiko_product_uuid = EXCLUDED.iiko_product_uuid,
				              iiko_product_name = EXCLUDED.iiko_product_name,
				              multiplier = EXCLUDED.multiplier,
				              updated_at = now()`,
				req.CompanyID, req.VendorName, item.Name, item.MappedUUID, item.MappedName, item.Multiplier)
			if err != nil {
				http.Error(w, "Ошибка сохранения маппинга товара: "+err.Error(), http.StatusInternalServerError)
				return
			}

			// 2. АНАЛИТИКА: Сохраняем Бренд и Категорию вместе с ценой
			finalQty := item.Quantity * item.Multiplier
			pricePerUnit := 0.0
			if finalQty > 0 {
				pricePerUnit = item.Sum / finalQty
			}

			_, err = tx.Exec(`
				INSERT INTO purchase_history (
					company_id, invoice_date, invoice_number, supplier_uuid, supplier_name,
					iiko_product_uuid, iiko_product_name, product_name_in_invoice, clean_category, brand, quantity, multiplier, total_sum, price_per_base_unit,
					consignee, shipper
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
				req.CompanyID, dbInvoiceDate, req.InvoiceNumber, req.SupplierUUID, req.VendorName,
				item.MappedUUID, item.MappedName, item.Name, item.CleanCategory, item.Brand, item.Quantity, item.Multiplier, item.Sum, pricePerUnit,
				req.Consignee, req.Shipper)

			if err != nil {
				http.Error(w, "Ошибка записи истории закупок (Аналитика): "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	if req.SupplierUUID != "" && req.VendorName != "" {
		_, err = tx.Exec(`
			INSERT INTO supplier_mappings (company_id, vendor_name, iiko_supplier_uuid)
			VALUES ($1, $2, $3)
			ON CONFLICT (company_id, vendor_name)
			DO UPDATE SET iiko_supplier_uuid = EXCLUDED.iiko_supplier_uuid`,
			req.CompanyID, req.VendorName, req.SupplierUUID)
		if err != nil {
			http.Error(w, "Ошибка сохранения связи поставщика: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if req.Consignee != "" && req.StoreUUID != "" {
		_, err = tx.Exec(`
			INSERT INTO store_mappings (company_id, consignee, iiko_store_uuid)
			VALUES ($1, $2, $3)
			ON CONFLICT (company_id, consignee)
			DO UPDATE SET iiko_store_uuid = EXCLUDED.iiko_store_uuid`,
			req.CompanyID, req.Consignee, req.StoreUUID)
		if err != nil {
			http.Error(w, "Ошибка сохранения связи склада: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err = tx.Commit(); err != nil {
		http.Error(w, "Ошибка фиксации транзакции в БД", http.StatusInternalServerError)
		return
	}

	var itemsXML strings.Builder
	for i, item := range req.Items {
		if item.MappedUUID == "" {
			continue
		}

		finalQuantity := item.Quantity * item.Multiplier
		finalSum := item.Sum
		finalPrice := finalSum / finalQuantity

		vatSum := 0.0
		if item.NdsPercent > 0 {
			vatSum = (finalSum * item.NdsPercent) / (100.0 + item.NdsPercent)
		}

		itemXML := fmt.Sprintf(`
        <item>
            <amount>%.3f</amount>
            <product>%s</product>
            <num>%d</num>
            <sum>%.2f</sum>
            <vatPercent>%.2f</vatPercent>
            <vatSum>%.2f</vatSum>
            <price>%.4f</price>
            <store>%s</store>
            <actualAmount>%.3f</actualAmount>
        </item>`,
			finalQuantity, item.MappedUUID, i+1, finalSum, item.NdsPercent, vatSum, finalPrice, req.StoreUUID, finalQuantity)
		itemsXML.WriteString(itemXML)
	}

	docDate := req.InvoiceDate
	if docDate == "" {
		docDate = time.Now().Format("2006-01-02")
	}
	dateIncomingStr := ""
	parsedTime, err := time.Parse("2006-01-02", docDate)
	if err != nil {
		dateIncomingStr = time.Now().Format("02.01.2006")
		docDate = time.Now().Format("2006-01-02")
	} else {
		dateIncomingStr = parsedTime.Format("02.01.2006")
	}

	xmlPayload := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<document>
    <incomingDate>%s</incomingDate>
    <supplier>%s</supplier>
    <defaultStore>%s</defaultStore>
    <dateIncoming>%s</dateIncoming>
    <useDefaultDocumentTime>true</useDefaultDocumentTime>
    <incomingDocumentNumber>%s</incomingDocumentNumber>
    <status>NEW</status>
    <items>%s
    </items>
</document>`,
		docDate, req.SupplierUUID, req.StoreUUID, dateIncomingStr, req.InvoiceNumber, itemsXML.String())

	url := fmt.Sprintf("%s/resto/api/documents/import/incomingInvoice?key=%s", host, req.Token)
	iikoReq, err := http.NewRequest("POST", url, bytes.NewBufferString(xmlPayload))
	if err != nil {
		http.Error(w, "Ошибка создания HTTP-запроса", http.StatusInternalServerError)
		return
	}
	iikoReq.Header.Set("Content-Type", "application/xml")

	res, err := iikoHTTPClient.Do(iikoReq)
	if err != nil {
		http.Error(w, "Ошибка отправки накладной в iiko: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()

	respBody, _ := io.ReadAll(res.Body)

	if res.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("iiko RMS отклонил документ (HTTP %d): %s", res.StatusCode, string(respBody)), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "success",
		"message":       "Накладная успешно проведена в iiko RMS",
		"iiko_response": string(respBody),
	})
}

// ============================================================================
// ПРОКСИ ШАБЛОНОВ И ДИСПЕТЧЕР НЕУЧТЕНКИ
// ============================================================================

func handleSaveTemplateProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		StoreUUID string   `json:"store_uuid"`
		Name      string   `json:"name"`
		Items     []string `json:"items"`
		CompanyID int      `json:"company_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	if req.StoreUUID == "" || req.Name == "" || len(req.Items) == 0 || req.CompanyID == 0 {
		http.Error(w, "Не все обязательные поля заполнены (store_uuid, name, items, company_id)", http.StatusBadRequest)
		return
	}

	payloadBytes, err := json.Marshal(req)
	if err != nil {
		http.Error(w, "Ошибка сериализации", http.StatusInternalServerError)
		return
	}

	qa2aURL := fmt.Sprintf("%s/api/external/inventory-templates", strings.TrimSuffix(qa2aBaseURL, "/"))
	qa2aReq, err := http.NewRequest("POST", qa2aURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		http.Error(w, "Ошибка формирования запроса", http.StatusInternalServerError)
		return
	}
	qa2aReq.Header.Set("Content-Type", "application/json")
	qa2aReq.Header.Set("Authorization", "Bearer "+externalApiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(qa2aReq)
	if err != nil {
		http.Error(w, "Сбой связи с сервером QA2A: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()

	respBody, _ := io.ReadAll(res.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(respBody)
}

func handleGetUnlistedOperations(w http.ResponseWriter, r *http.Request) {
	companyIDStr := r.URL.Query().Get("company_id")
	if companyIDStr == "" {
		http.Error(w, "Отсутствует обязательный параметр company_id", http.StatusBadRequest)
		return
	}

	var companyID int
	if _, err := fmt.Sscanf(companyIDStr, "%d", &companyID); err != nil || companyID <= 0 {
		http.Error(w, "Некорректный company_id", http.StatusBadRequest)
		return
	}

	query := `
		SELECT 
			o.id, 
			o.position_name, 
			o.quantity, 
			o.unit, 
			COALESCE(o.comment, '') as comment, 
			o.created_at, 
			COALESCE(u.full_name, u.username, 'Система') as user_name
		FROM operations o
		LEFT JOIN users u ON o.user_id = u.id
		WHERE o.company_id = $1 AND o.is_unlisted = true AND o.type = 'writeoff'
		ORDER BY o.created_at DESC`

	rows, err := db.Query(query, companyID)
	if err != nil {
		http.Error(w, "Ошибка чтения неучтенных списаний: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list []UnlistedOperation
	for rows.Next() {
		var op UnlistedOperation
		if err := rows.Scan(&op.ID, &op.PositionName, &op.Quantity, &op.Unit, &op.Comment, &op.CreatedAt, &op.UserName); err == nil {
			list = append(list, op)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func handleResolveUnlistedOperation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CompanyID       int    `json:"company_id"`
		OperationID     int    `json:"operation_id"`
		IikoProductName string `json:"iiko_product_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	req.IikoProductName = strings.TrimSpace(req.IikoProductName)
	if req.CompanyID <= 0 || req.OperationID <= 0 || req.IikoProductName == "" {
		http.Error(w, "Заполните все обязательные поля (company_id, operation_id, iiko_product_name)", http.StatusBadRequest)
		return
	}

	query := `
		UPDATE operations 
		SET position_name = $1, is_unlisted = false 
		WHERE id = $2 AND company_id = $3 AND is_unlisted = true`

	res, err := db.Exec(query, req.IikoProductName, req.OperationID, req.CompanyID)
	if err != nil {
		http.Error(w, "Ошибка обновления операции в БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		http.Error(w, "Операция не найдена, либо уже была сопоставлена ранее", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Операция успешно привязана к товару iiko RMS!",
	})
}

// ============================================================================
// АНАЛИТИЧЕСКИЙ API (ИСТОРИЯ ЦЕН И ЗАКУПОК)
// ============================================================================

func handleAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Только GET метод", http.StatusMethodNotAllowed)
		return
	}

	companyIDStr := r.URL.Query().Get("company_id")
	if companyIDStr == "" {
		http.Error(w, "Отсутствует обязательный параметр company_id", http.StatusBadRequest)
		return
	}

	var companyID int
	if _, err := fmt.Sscanf(companyIDStr, "%d", &companyID); err != nil || companyID <= 0 {
		http.Error(w, "Некорректный company_id", http.StatusBadRequest)
		return
	}

	daysStr := r.URL.Query().Get("days")
	days := 30
	if daysStr != "" {
		if d, err := fmt.Sscanf(daysStr, "%d", &days); err != nil || d <= 0 {
			days = 30
		}
	}

	query := `
		SELECT 
			TO_CHAR(invoice_date, 'YYYY-MM-DD') as invoice_date,
			invoice_number,
			supplier_name,
			iiko_product_uuid,
			COALESCE(iiko_product_name, '') as iiko_product_name,
			product_name_in_invoice,
			quantity,
			unit,
			multiplier,
			total_sum,
			price_per_base_unit
		FROM purchase_history
		WHERE company_id = $1 AND invoice_date >= NOW() - INTERVAL '1 day' * $2
		ORDER BY invoice_date DESC, id DESC`

	rows, err := db.Query(query, companyID, days)
	if err != nil {
		http.Error(w, "Ошибка чтения истории закупок: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var records []PurchaseRecord
	for rows.Next() {
		var rec PurchaseRecord
		err := rows.Scan(
			&rec.InvoiceDate,
			&rec.InvoiceNum,
			&rec.SupplierName,
			&rec.ProductUUID,
			&rec.IikoProductName,
			&rec.ProductName,
			&rec.Quantity,
			&rec.Unit,
			&rec.Multiplier,
			&rec.TotalSum,
			&rec.PricePerUnit,
		)
		if err == nil {
			records = append(records, rec)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(records)
}

// ============================================================================
// MARKET GODMODE API (СУПЕРАДМИН АНАЛИТИКА ПО ВСЕМУ РЫНКУ)
// ============================================================================

func handleMarketSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Только GET метод", http.StatusMethodNotAllowed)
		return
	}

	// 1. Проверка прав суперадмина
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token != superadminToken {
		http.Error(w, "Unauthorized (Недействительный токен рынка)", http.StatusUnauthorized)
		return
	}

	// 2. Параметры поиска
	queryParam := r.URL.Query().Get("q")
	if strings.TrimSpace(queryParam) == "" {
		http.Error(w, "Введите поисковый запрос (q=бренд или название)", http.StatusBadRequest)
		return
	}

	// 3. Выборка по всем ресторанам (с очищенными названиями и брендами от ИИ)
	searchMask := "%" + queryParam + "%"
	query := `
		SELECT 
			c.name as restaurant_name,
			TO_CHAR(ph.invoice_date, 'YYYY-MM-DD') as invoice_date,
			ph.supplier_name,
			ph.product_name_in_invoice,
			COALESCE(ph.clean_category, '') as clean_category,
			COALESCE(ph.brand, '') as brand,
			ph.price_per_base_unit
		FROM purchase_history ph
		JOIN companies c ON ph.company_id = c.id
		WHERE ph.brand ILIKE $1 OR ph.clean_category ILIKE $1 OR ph.product_name_in_invoice ILIKE $1
		ORDER BY ph.price_per_base_unit ASC, ph.invoice_date DESC
		LIMIT 150`

	rows, err := db.Query(query, searchMask)
	if err != nil {
		http.Error(w, "Ошибка SQL выборки рынка: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var records []MarketRecord
	for rows.Next() {
		var rec MarketRecord
		if err := rows.Scan(
			&rec.RestaurantName,
			&rec.InvoiceDate,
			&rec.SupplierName,
			&rec.ProductName,
			&rec.CleanCategory,
			&rec.Brand,
			&rec.PricePerUnit,
		); err == nil {
			records = append(records, rec)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(records)
}

// ============================================================================
// ВЗАИМОДЕЙСТВИЕ С IIKO REST API
// ============================================================================

func authIiko(host, login, pass string) (string, error) {
	passHash := crypto.HashPasswordSHA1(pass)
	authURL := fmt.Sprintf("%s/resto/api/auth?login=%s&pass=%s", strings.TrimSuffix(host, "/"), login, passHash)

	resp, err := iikoHTTPClient.Get(authURL)
	if err != nil {
		return "", fmt.Errorf("ошибка соединения с сервером iiko: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ошибка авторизации (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return strings.TrimSpace(string(body)), nil
}

func fetchIikoCatalog(host, token string) ([]IikoProduct, error) {
	url := fmt.Sprintf("%s/resto/api/products?key=%s", strings.TrimSuffix(host, "/"), token)
	resp, err := iikoHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса товаров: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iiko вернул статус %d при загрузке каталога", resp.StatusCode)
	}

	var data XMLProducts
	if err := xml.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("ошибка разбора XML каталога: %w", err)
	}

	var goods, prepared, dishes, modifiers, others []IikoProduct

	for _, p := range data.List {
		pType := strings.ToUpper(strings.TrimSpace(p.ProductType))
		if pType == "" {
			pType = strings.ToUpper(strings.TrimSpace(p.Type))
		}

		prod := IikoProduct{
			UUID: p.ID,
			Name: p.Name,
			Type: pType,
		}

		switch pType {
		case "GOODS":
			goods = append(goods, prod)
		case "PREPARED":
			prepared = append(prepared, prod)
		case "DISH":
			dishes = append(dishes, prod)
		case "MODIFIER":
			modifiers = append(modifiers, prod)
		default:
			others = append(others, prod)
		}
	}

	var sortedProducts []IikoProduct
	sortedProducts = append(sortedProducts, goods...)
	sortedProducts = append(sortedProducts, prepared...)
	sortedProducts = append(sortedProducts, dishes...)
	sortedProducts = append(sortedProducts, modifiers...)
	sortedProducts = append(sortedProducts, others...)

	return sortedProducts, nil
}

func fetchIikoStores(host, token string) ([]IikoStore, error) {
	url := fmt.Sprintf("%s/resto/api/corporation/stores?key=%s", strings.TrimSuffix(host, "/"), token)
	resp, err := iikoHTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("статус %d при загрузке складов", resp.StatusCode)
	}

	var stores []IikoStore
	decoder := xml.NewDecoder(resp.Body)
	for {
		t, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		switch se := t.(type) {
		case xml.StartElement:
			if se.Name.Local == "corporateItemDto" {
				var item struct {
					ID   string `xml:"id"`
					Name string `xml:"name"`
				}
				if err := decoder.DecodeElement(&item, &se); err == nil && item.ID != "" {
					stores = append(stores, IikoStore{UUID: item.ID, Name: item.Name})
				}
			}
		}
	}
	return stores, nil
}

func fetchIikoSuppliers(host, token string) ([]IikoSupplier, error) {
	url := fmt.Sprintf("%s/resto/api/suppliers?key=%s", strings.TrimSuffix(host, "/"), token)
	resp, err := iikoHTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("статус %d при запросе поставщиков", resp.StatusCode)
	}

	var data struct {
		XMLName xml.Name `xml:"employees"`
		List    []struct {
			ID       string `xml:"id"`
			Name     string `xml:"name"`
			Supplier bool   `xml:"supplier"`
			Deleted  bool   `xml:"deleted"`
		} `xml:"employee"`
	}

	if err := xml.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("ошибка парсинга поставщиков: %w", err)
	}

	var suppliers []IikoSupplier
	for _, emp := range data.List {
		if emp.Supplier && !emp.Deleted {
			suppliers = append(suppliers, IikoSupplier{
				UUID: emp.ID,
				Name: emp.Name,
			})
		}
	}
	return suppliers, nil
}

// ============================================================================
// НЕЙРОСЕТЕВОЙ ПАРСИНГ AI (ПРОМПТ И ИНТЕГРАЦИЯ)
// ============================================================================

func parseWithClaude(text string, imagesBase64 []string) (*AiResponse, error) {
	prompt := `Ты — автоматический парсер накладных. Твоя задача: найти поставщика, получателя (грузополучателя), номер документа (УПД/ТОРГ-12) и все товары.
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
   - Четко различай единицы измерения в накладной: коробка (кор, короб, ящ) и упаковка/пачка/штука (уп, упак, шт, пакет).
   - Если единица измерения в накладной указана как "уп", "упак", "шт" или "пакет", а в названии товара есть фасовка вида "1,5кг кор1-6" — это означает, что товар пришел в индивидуальных упаковках (пачках) по 1,5 кг, а не целыми коробками. В этом случае коэффициент ai_multiplier должен быть равен строго весу одной пачки в кг (т.е. 1.5). НЕ умножай на количество в коробке (6).
   - Умножать количество в коробке на вес пачки нужно ТОЛЬКО тогда, когда единица измерения в самой накладной явно указана как "кор", "коробка" или "ящ".
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

14. СУММА БЕЗ НАЛОГА (sum_without_nds):
    Найди стоимость товаров без налога (колонка 5).

15. ПРАВИЛО ДЛЯ ЧАЯ В ПАКЕТИКАХ:
    - ai_multiplier равен количеству пакетиков в упаковке (20.0, 100.0).

16. ПРАВИЛО ДЛЯ ЛИСТА БАМБУКА:
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

20. Игнорируй пометки ручкой, закорючки и прочий визуальный шум на сканах или фото.
21. Если документ обрезан или является только частью накладной (например, нет итоговой суммы), просто извлеки те товары, которые видны на изображении.

Верни строго только JSON-объект без markdown и без пояснений:
{
  "vendor_name": "Название поставщика",
  "doc_number": "Номер документа",
  "doc_date": "YYYY-MM-DD",
  "consignee": "Грузополучатель и его адрес или Покупатель",
  "shipper": "Грузоотправитель и его адрес",
  "items": [
    {"name": "Название полностью", "clean_category": "Картофель фри", "brand": "Фритто Аппетито", "quantity": 10.0, "price": 120.0, "sum": 1200.0, "sum_without_nds": 1000.0, "nds_percent": 20.0, "ai_multiplier": 0.55, "ai_tip": "1 шт = 550г"}
  ]
}`

	var contentParts []map[string]interface{}
	contentParts = append(contentParts, map[string]interface{}{
		"type": "text",
		"text": prompt + "\n\nТекст накладной (может быть пустым, если это скан):\n" + text,
	})
	for _, b64 := range imagesBase64 {
		contentParts = append(contentParts, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]string{
				"url": "data:image/jpeg;base64," + b64,
			},
		})
	}

	payload := map[string]interface{}{
		"model":            aiModel,
		"stream":           false,
		"max_tokens":       65536,
		"temperature":      0.1,
		"reasoning_effort": "none",
		"thinking_config":  map[string]int{"thinking_budget": 0},
		"messages": []map[string]interface{}{
			{"role": "user", "content": contentParts},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации JSON для AI: %w", err)
	}

	var respBody []byte
	maxRetries := 3
	var lastErr error

	// Retry-цикл: пробуем сделать запрос к AI несколько раз при ошибках нагрузки
	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest("POST", aiBaseUrl, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("ошибка формирования HTTP запроса к AI: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+aiApiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := llmHTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("сетевой сбой при обращении к AI (%s): %w", aiBaseUrl, err)
			time.Sleep(time.Duration(attempt*2) * time.Second) // Задержка 2s, 4s, 6s...
			continue
		}

		respBody, err = io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			lastErr = fmt.Errorf("ошибка чтения ответа AI: %w", err)
			time.Sleep(time.Duration(attempt*2) * time.Second)
			continue
		}

		// 429 или 5xx — временные ошибки (лимиты/недоступность API), пробуем снова
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
			lastErr = fmt.Errorf("AI API вернул ошибку (HTTP %d): %s", resp.StatusCode, string(respBody))
			time.Sleep(time.Duration(attempt*2) * time.Second)
			continue
		}

		// Другие ошибки (400, 401, 403) не исправятся повтором, прерываем сразу
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("AI API вернул ошибку (HTTP %d): %s", resp.StatusCode, string(respBody))
		}

		// Успех
		lastErr = nil
		break
	}

	if lastErr != nil {
		return nil, fmt.Errorf("не удалось получить ответ от нейросети после %d попыток. Последняя ошибка: %v", maxRetries, lastErr)
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка разбора JSON ответа AI: %w\nСырые данные: %s", err, string(respBody))
	}

	if apiResp.Error.Message != "" {
		return nil, fmt.Errorf("ошибка модели AI: %s", apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("AI вернул пустой список вариантов ответа")
	}

	content := strings.TrimSpace(apiResp.Choices[0].Message.Content)

	// Очищаем от возможных Markdown тэгов ```json ... ```
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
	} else if strings.HasPrefix(content, "```JSON") {
		content = strings.TrimPrefix(content, "```JSON")
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
	}
	if strings.HasSuffix(content, "```") {
		content = strings.TrimSuffix(content, "```")
	}
	content = strings.TrimSpace(content)

	startIdx := strings.Index(content, "{")
	endIdx := strings.LastIndex(content, "}")

	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return nil, fmt.Errorf("в ответе AI не найден валидный JSON-объект:\n%s", content)
	}

	cleanJsonStr := content[startIdx : endIdx+1]

	var aiResp AiResponse
	if err := json.Unmarshal([]byte(cleanJsonStr), &aiResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга итогового JSON накладной: %w\nИзвлеченный фрагмент: %s", err, cleanJsonStr)
	}

	return &aiResp, nil
}