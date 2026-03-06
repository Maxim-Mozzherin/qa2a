package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	"qa2a/internal/config"
	"qa2a/internal/database"
	"qa2a/internal/handlers"
	"qa2a/internal/middleware"
	"qa2a/internal/repository"
	"qa2a/internal/service"
)

func main() {
	// 1. Загрузка конфигурации
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("❌ Ошибка загрузки конфига: %v", err)
	}

	// 2. Подключение к базе данных
	db, err := database.New(cfg.DSN())
	if err != nil {
		log.Fatalf("❌ Ошибка подключения к БД: %v", err)
	}
	defer db.Close()

	// 3. Инициализация слоев (Dependency Injection)
	repo := repository.New(db)
	authSvc := service.NewAuthService(repo)
	invSvc := service.NewInventoryService(repo)
	h := handlers.New(authSvc, invSvc, cfg.BotToken)

	// 4. Создание главного роутера
	r := mux.NewRouter()

	// Глобальный логгер запросов (для отладки в терминале)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("📢 [%s] %s", r.Method, r.URL.Path)
			next.ServeHTTP(w, r)
		})
	})

	// --- СТАТИЧЕСКИЕ ФАЙЛЫ ---
	// Главная страница
	r.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, "web/templates/index.html")
	}).Methods("GET")

	// Статика (CSS, JS)
	staticDir := http.Dir("web/static")
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(staticDir)))

	// --- API РОУТЫ ---
	api := r.PathPrefix("/api").Subrouter()

	// 🔓 ПУБЛИЧНЫЕ: Вход через Telegram
	api.HandleFunc("/auth", h.AuthHandler).Methods("POST", "OPTIONS")

	// 🔒 ЗАЩИЩЕННЫЕ: Требуют X-Telegram-ID в заголовке
	protected := api.PathPrefix("/").Subrouter()
	protected.Use(middleware.AuthMiddleware(repo))

	// Операции и остатки
	protected.HandleFunc("/operations", h.CreateOperationHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/operations", h.GetOperationsHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/balances", h.GetBalancesHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/join", h.JoinCompanyHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/invite-code", h.GetInviteCodeHandler).Methods("GET")
	protected.HandleFunc("/companies", h.CreateCompanyHandler).Methods("POST")
	protected.HandleFunc("/locations", h.GetLocationsHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/locations", h.CreateLocationHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/positions", h.GetPositionsHandler).Methods("GET", "OPTIONS")
	protected.HandleFunc("/positions", h.CreatePositionHandler).Methods("POST", "OPTIONS")
	protected.HandleFunc("/members", h.GetMembersHandler).Methods("GET", "OPTIONS")
	// 5. Запуск сервера
	fmt.Println("-----------------------------------------------")
	fmt.Printf("🚀 QA2A Server started on :%s\n", cfg.Port)
	fmt.Println("-----------------------------------------------")

	log.Fatal(http.ListenAndServe(":"+cfg.Port, r))
}