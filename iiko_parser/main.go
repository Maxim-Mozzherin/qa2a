package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"iiko_parser/pkg/netutil"

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

	// Единый HTTP-клиент для вызовов iiko RMS API с защитой от SSRF
	iikoHTTPClient = &http.Client{
		Timeout:   45 * time.Second,
		Transport: netutil.NewSafeHTTPTransport(15 * time.Second),
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
	_ = os.MkdirAll("temp", 0750)
	_ = os.MkdirAll("static", 0755)

	_ = godotenv.Load()
	_ = godotenv.Load("/opt/qa2a-reboot/.env")

	encryptionKey = os.Getenv("ENCRYPTION_KEY")
	externalApiKey = os.Getenv("EXTERNAL_API_KEY")
	superadminToken = os.Getenv("SUPERADMIN_TOKEN")
	dbPass := os.Getenv("DB_PASS")

	if len(encryptionKey) < 32 {
		log.Fatalf("❌ Критическая ошибка: ENCRYPTION_KEY должен быть задан в .env и иметь длину не менее 32 символов (текущая длина: %d)", len(encryptionKey))
	}
	if externalApiKey == "" {
		log.Fatalf("❌ Критическая ошибка: EXTERNAL_API_KEY отсутствует в .env")
	}
	// SUPERADMIN_TOKEN is optional; authentication is handled via database (accounting_users table)
	if dbPass == "" {
		log.Fatalf("❌ Критическая ошибка: DB_PASS отсутствует в .env")
	}

	aiApiKey = os.Getenv("AI_API_KEY")
	if aiApiKey == "" {
		log.Fatalf("❌ Критическая ошибка: AI_API_KEY отсутствует в .env")
	}
	aiBaseUrl = getEnv("AI_BASE_URL", "http://127.0.0.1:20128/v1/chat/completions")
	aiModel = getEnv("AI_MODEL", "gemini/gemini-3.5-flash,gemini/gemini-3.0-flash")

	qa2aBaseURL = getEnv("QA2A_URL", "http://127.0.0.1:8082")

	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5433")
	dbUser := getEnv("DB_USER", "admin")
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
	initCompanyInvites()

	mux := http.NewServeMux()

	mux.HandleFunc("/health", handleHealthCheck)
	mux.HandleFunc("/api/health", handleHealthCheck)

	mux.Handle("/", http.FileServer(http.Dir("./static")))
	mux.HandleFunc("/api/uploads/tickets/", authMiddleware(handleServeTicketMedia))

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

	allowedOrigins := []string{
		"https://web.telegram.org",
		"https://webk.telegram.org",
		"https://webz.telegram.org",
	}
	if envOrigins := os.Getenv("ALLOWED_ORIGINS"); envOrigins != "" {
		for _, o := range strings.Split(envOrigins, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
	}

	corsHandler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		origin := req.Header.Get("Origin")
		if origin != "" {
			isAllowed := false
			for _, o := range allowedOrigins {
				if o == "*" || strings.EqualFold(o, origin) {
					isAllowed = true
					break
				}
				if strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:") {
					isAllowed = true
					break
				}
			}
			if isAllowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Telegram-ID, X-Company-ID, X-Firm-Token")

		if req.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		mux.ServeHTTP(w, req)
	})

	srv := &http.Server{
		Addr:              ":" + serverPort,
		Handler:           corsHandler,
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

func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if db == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"error","service":"iiko_parser","error":"database not initialized"}`))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status":"error","service":"iiko_parser","error":%q}`, err.Error())))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok","service":"iiko_parser"}`))
}
