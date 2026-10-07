package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"leads_monster/internal/api"
	"leads_monster/internal/config"
	"leads_monster/internal/db"
	"leads_monster/internal/middleware"
)

func main() {
	log.Println("🚀 [Leads Monster] Инициализация платформы B2B лидогенерации HoReCa...")

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("❌ Ошибка загрузки конфигурации: %v", err)
	}

	database, err := db.Connect(cfg)
	if err != nil {
		log.Fatalf("❌ Ошибка подключения к базе данных: %v", err)
	}
	defer database.Close()

	// Handlers
	authHandler := api.NewAuthHandler(database)
	leadsHandler := api.NewLeadsHandler(database)
	scraperHandler := api.NewScraperHandler(database, cfg)

	mux := http.NewServeMux()

	// API Routes
	mux.HandleFunc("/api/auth/login", authHandler.HandleLogin)
	mux.HandleFunc("/api/auth/me", authHandler.HandleMe)
	mux.HandleFunc("/api/auth/logout", authHandler.HandleLogout)

	mux.HandleFunc("/api/leads", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			leadsHandler.HandleGetLeads(w, r)
			return
		}
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
	})

	mux.HandleFunc("/api/leads/export", leadsHandler.HandleExportExcel)

	mux.HandleFunc("/api/leads/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			leadsHandler.HandleUpdateLead(w, r)
			return
		}
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
	})

	mux.HandleFunc("/api/scraper/start", scraperHandler.HandleStart)
	mux.HandleFunc("/api/scraper/stop", scraperHandler.HandleStop)
	mux.HandleFunc("/api/scraper/status", scraperHandler.HandleStatus)

	// Web Static Assets and Dashboard
	webDir := resolveWebDir()

	fileServer := http.FileServer(http.Dir(webDir))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Static assets
		if strings.HasPrefix(path, "/css/") || strings.HasPrefix(path, "/js/") || path == "/favicon.ico" {
			fileServer.ServeHTTP(w, r)
			return
		}

		if path == "/login" {
			http.ServeFile(w, r, filepath.Join(webDir, "login.html"))
			return
		}

		if path == "/" {
			http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
			return
		}

		// Fallback to static file server or 404
		fileServer.ServeHTTP(w, r)
	})

	// Wrap with super-admin gatekeeper
	handler := middleware.SuperAdminGatekeeper(database)(mux)

	addr := fmt.Sprintf("0.0.0.0:%s", cfg.Port)
	log.Printf("====================================================================")
	log.Printf("⚡ [Leads Monster] Сервер успешно запущен на http://localhost:%s", cfg.Port)
	log.Printf("🔒 Авторизация: требуется роль 'superadmin' (таблица accounting_users)")
	log.Printf("📊 Дашборд: http://localhost:%s", cfg.Port)
	log.Printf("====================================================================")

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("❌ Ошибка HTTP сервера: %v", err)
	}
}

func resolveWebDir() string {
	candidates := []string{
		"web",
		"../../web",
		"Z:/Parser/web",
	}
	for _, c := range candidates {
		if stat, err := os.Stat(c); err == nil && stat.IsDir() {
			return c
		}
	}
	return "web"
}
