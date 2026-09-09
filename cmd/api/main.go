package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"

	"qa2a/internal/config"
	"qa2a/internal/database"
	"qa2a/internal/handlers"
	"qa2a/internal/middleware"
	"qa2a/internal/repository"
	"qa2a/internal/service"
)

func main() {
	// 1. Устанавливаем пути поиска шрифтов для PDF-генератора
	_ = os.Setenv("GOFPDF_FONTPATH", "./fonts")

	log.Println("⚡ Инициализация конфигурации QA2A...")
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("❌ Критическая ошибка загрузки конфигурации: %v", err)
	}

	// 2. Инициализация пула соединений PostgreSQL
	log.Printf("🔌 Подключение к базе данных PostgreSQL (%s:%s/%s)...", cfg.DBHost, cfg.DBPort, cfg.DBName)
	db, err := database.NewWithConfig(cfg)
	if err != nil {
		log.Fatalf("❌ Сбой подключения к базе данных: %v", err)
	}
	defer func() {
		log.Println("🔒 Закрытие соединений с базой данных...")
		_ = db.Close()
	}()
	log.Println("✅ База данных успешно подключена и проверена (Ping OK)")

	// 3. Внедрение зависимостей (Dependency Injection)
	repo := repository.New(db)

	authSvc := service.NewAuthService(repo)
	repSvc := service.NewReportService(repo)
	iikoSvc := service.NewIikoService(repo, cfg.EncryptionKey)
	invSvc := service.NewInventoryService(repo, iikoSvc)

	h := handlers.New(authSvc, invSvc, repSvc, iikoSvc, cfg.BotToken)

	// 4. Запуск фонового регламентного планировщика (выгрузка в iiko в 06:30 МСК)
	scheduler := service.NewScheduler(repo, iikoSvc)
	scheduler.Start()

	// 5. Маршрутизация HTTP
	r := mux.NewRouter()

	// Глобальное логирование входящих запросов и базовые CORS заголовки
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Telegram-ID, X-Company-ID")

			if req.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			start := time.Now()
			next.ServeHTTP(w, req)
			log.Printf("📢 [%s] %s (IP: %s) -> %v", req.Method, req.URL.Path, req.RemoteAddr, time.Since(start))
		})
	})

	// Статический фронтенд Mini App
	r.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, "web/templates/index.html")
	}).Methods("GET")

	staticDir := http.Dir("web/static")
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(staticDir)))

	api := r.PathPrefix("/api").Subrouter()

	// ==========================================
	// ОТКРЫТЫЕ И ВНЕШНИЕ МАРШРУТЫ API
	// ==========================================
	api.HandleFunc("/auth", h.AuthHandler).Methods("POST", "OPTIONS")
	api.HandleFunc("/join", h.JoinCompanyHandler).Methods("POST", "OPTIONS")
	api.HandleFunc("/companies", h.CreateCompanyHandler).Methods("POST", "OPTIONS")
	api.HandleFunc("/iiko/webhook", h.IikoWebhookHandler).Methods("POST")
	api.HandleFunc("/external/inventory-templates", h.CreateExternalTemplateHandler).Methods("POST", "OPTIONS")

	// ==========================================
	// ЗАЩИЩЕННЫЕ МАРШРУТЫ API (AuthMiddleware)
	// ==========================================
	protected := api.PathPrefix("/").Subrouter()
	protected.Use(middleware.AuthMiddleware(repo))

	// Управление компанией и командой
	protected.HandleFunc("/invite-code", h.GetInviteCodeHandler).Methods("GET")
	protected.HandleFunc("/members", h.GetMembersHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/members", h.UpdateMemberRoleHandler).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/members/{id:[0-9]+}", h.RemoveMemberHandler).Methods("DELETE", "OPTIONS")

	// Склады и номенклатура
	protected.HandleFunc("/locations", h.GetLocationsHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/locations", h.CreateLocationHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/positions", h.GetPositionsHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/positions", h.CreatePositionHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/positions/{uuid}/suppliers", h.GetPositionSuppliersHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/unlisted", h.GetUnlistedItemsHandler).Methods("GET", "OPTIONS")

	// Складские движения и балансы
	protected.HandleFunc("/balances", h.GetBalancesHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/operations", h.GetOperationsHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/operations", h.CreateOperationHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/operations/{id:[0-9]+}", h.UpdateOperationHandler).Methods("PUT", "OPTIONS")

	// Заявки на закупку (Procurements)
	protected.HandleFunc("/procurements", h.GetProcurementsHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/procurements", h.CreateProcurementHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/procurements/status", h.UpdateProcurementStatusHandler).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/procurements/{id:[0-9]+}/suppliers", h.GetProcurementSuppliersHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/procurements/download/{id:[0-9]+}", h.DownloadProcurementPDFHandler).Methods("GET", "OPTIONS")

	// Инвентаризация и бланки
	protected.HandleFunc("/inventories", h.GetInventoriesHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/inventories/start", h.StartInventoryHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/inventories/start-from-draft", h.StartInventoryFromDraftHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/inventories/start-from-template", h.StartInventoryFromTemplateHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/inventories/templates", h.GetTemplatesHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/inventories/iiko-drafts", h.GetIikoDraftsHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/inventories/{id:[0-9]+}", h.GetInventoryHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/inventories/{id:[0-9]+}", h.SaveInventoryHandler).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/inventories/{id:[0-9]+}", h.DeleteInventoryHandler).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/inventories/{id:[0-9]+}/finalize", h.FinalizeInventoryHandler).Methods("POST", "OPTIONS")

	// Поставщики
	protected.HandleFunc("/suppliers", h.GetSuppliersHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/suppliers/contacts", h.SaveSupplierContactHandler).Methods("POST", "OPTIONS")

	// Настройки и выгрузка iiko RMS
	protected.HandleFunc("/iiko/settings", h.GetIikoSettingsHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/iiko/settings", h.SaveIikoSettingsHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/iiko/sync", h.SyncIikoHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/iiko/force-export", h.ForceExportHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/iiko/accounts", h.GetIikoAccountsHandler).Methods("GET", "OPTIONS")

	// Счета и статьи расходов
	protected.HandleFunc("/accounts", h.GetAccountsHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/accounts", h.CreateAccountHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/accounts/{id:[0-9]+}", h.DeleteAccountHandler).Methods("DELETE", "OPTIONS")

	// 6. Конфигурация HTTP-сервера с таймаутами
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 7. Асинхронный запуск сервера
	go func() {
		fmt.Println("==========================================================")
		fmt.Printf("🚀 Сервер QA2A успешно запущен на порту :%s\n", cfg.Port)
		fmt.Println("==========================================================")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Ошибка работы веб-сервера: %v", err)
		}
	}()

	// 8. Ожидание сигналов операционной системы для безопасной остановки (Graceful Shutdown)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	log.Println("⚠️ Получен сигнал прерывания. Начало процедуры безопасного завершения...")

	// Останавливаем фоновый планировщик задач
	scheduler.Stop()

	// Ожидаем завершения активных HTTP-запросов (таймаут 15 секунд)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("❌ Принудительное завершение сервера по таймауту: %v", err)
	}

	log.Println("✅ Все соединения закрыты. Сервер QA2A безопасно остановлен.")
}

