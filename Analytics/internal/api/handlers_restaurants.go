package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"analytics_service/internal/crypto"
	"analytics_service/internal/netutil"
)

// AddRestaurantRequest структура данных для подключения нового заведения через iiko RMS
type AddRestaurantRequest struct {
	Name         string  `json:"name"`
	City         string  `json:"city"`
	CuisineType  string  `json:"cuisine_type"`
	IikoHost     string  `json:"iiko_host"`
	IikoLogin    string  `json:"iiko_login"`
	IikoPassword string  `json:"iiko_password"`
	MonthlyRev   float64 `json:"monthly_revenue"`
}

// HandleRestaurants маршрутизирует GET (список ресторанов) и POST (подключение заведения)
func (s *Server) HandleRestaurants(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.handleGetRestaurants(w, r)
		return
	}
	if r.Method == http.MethodPost {
		s.handleAddRestaurant(w, r)
		return
	}
	http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
}

// handleGetRestaurants возвращает список активных ресторанов, синхронизируя базу с основной таблицей companies
func (s *Server) handleGetRestaurants(w http.ResponseWriter, r *http.Request) {
	// Автоматическая синхронизация всех заведений из companies (кроме id 10 - тестовый сервер)
	_, _ = s.db.Exec(`
		INSERT INTO analytics_restaurants (company_id, name, cuisine_type, city, iiko_host, iiko_login, iiko_password_enc, is_active)
		SELECT DISTINCT ON (iiko_host) id, name, 'Ресторан', 'Пермь', iiko_host, iiko_api_login, iiko_api_password, true
		FROM companies
		WHERE id != 10 AND length(iiko_host) > 0 AND length(iiko_api_login) > 0 AND length(iiko_api_password) > 0
		ORDER BY iiko_host, id ASC
		ON CONFLICT (company_id) DO UPDATE 
		SET name = EXCLUDED.name, 
		    iiko_host = EXCLUDED.iiko_host, 
		    iiko_login = EXCLUDED.iiko_login, 
		    iiko_password_enc = EXCLUDED.iiko_password_enc,
		    is_active = true`)

	// Исключаем тестовый сервер
	_, _ = s.db.Exec(`UPDATE analytics_restaurants SET is_active = false WHERE company_id = 10`)

	// Деактивируем дубликаты по одинаковому iiko_host, оставляя каноническую запись с наименьшим id
	_, _ = s.db.Exec(`
		UPDATE analytics_restaurants 
		SET is_active = false 
		WHERE id NOT IN (
			SELECT MIN(id) 
			FROM analytics_restaurants 
			WHERE (company_id != 10 OR company_id IS NULL)
			GROUP BY iiko_host
		) AND (company_id != 10 OR company_id IS NULL)`)

	// Фоновая автосинхронизация накладных для активных заведений, у которых еще нет данных
	go s.autoSyncMissingInvoices()
	go s.ReclassifyAllItems()

	rows, err := s.db.Query(`
		SELECT id, name, cuisine_type, city, is_active
		FROM analytics_restaurants
		WHERE is_active = true AND (company_id != 10 OR company_id IS NULL)
		ORDER BY id ASC`)
	if err != nil {
		http.Error(w, "Ошибка БД: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type RestInfo struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		CuisineType string `json:"cuisine_type"`
		City        string `json:"city"`
		IsActive    bool   `json:"is_active"`
	}

	var list []RestInfo
	for rows.Next() {
		var rest RestInfo
		if err := rows.Scan(&rest.ID, &rest.Name, &rest.CuisineType, &rest.City, &rest.IsActive); err == nil {
			list = append(list, rest)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(list)
}

// handleAddRestaurant валидирует реквизиты iiko RMS, шифрует пароль и сохраняет новое заведение в БД
func (s *Server) handleAddRestaurant(w http.ResponseWriter, r *http.Request) {
	var req AddRestaurantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Некорректный JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.IikoHost = strings.TrimSpace(req.IikoHost)
	req.IikoLogin = strings.TrimSpace(req.IikoLogin)
	req.IikoPassword = strings.TrimSpace(req.IikoPassword)

	if req.Name == "" || req.IikoHost == "" || req.IikoLogin == "" || req.IikoPassword == "" {
		http.Error(w, "Заполните обязательные поля: название, адрес iiko, логин и пароль", http.StatusBadRequest)
		return
	}

	if req.City == "" {
		req.City = "Пермь"
	}
	if req.CuisineType == "" {
		req.CuisineType = "Ресторан"
	}

	// Нормализация хоста
	req.IikoHost = strings.TrimRight(req.IikoHost, "/")
	if !strings.HasPrefix(req.IikoHost, "http://") && !strings.HasPrefix(req.IikoHost, "https://") {
		req.IikoHost = "https://" + req.IikoHost
	}

	if err := netutil.ValidateHost(req.IikoHost); err != nil {
		http.Error(w, "Недопустимый адрес сервера iiko RMS (заблокировано политикой безопасности SSRF): "+err.Error(), http.StatusBadRequest)
		return
	}

	// 1. Проверяем авторизацию в iiko RMS
	passHash := crypto.HashPasswordSHA1(req.IikoPassword)
	token, err := s.iikoClient.Auth(req.IikoHost, req.IikoLogin, passHash)
	if err != nil {
		http.Error(w, "Не удалось подключиться к iiko RMS: "+err.Error(), http.StatusBadRequest)
		return
	}
	_ = s.iikoClient.Logout(req.IikoHost, token)

	// 2. Шифруем пароль для безопасного хранения
	passEnc, err := crypto.Encrypt(req.IikoPassword, s.cfg.EncryptionKey)
	if err != nil {
		http.Error(w, "Ошибка шифрования пароля: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. Сохраняем в analytics_restaurants
	var newID int
	err = s.db.QueryRow(`
		INSERT INTO analytics_restaurants (name, city, cuisine_type, iiko_host, iiko_login, iiko_password_enc, monthly_revenue)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		req.Name, req.City, req.CuisineType, req.IikoHost, req.IikoLogin, passEnc, req.MonthlyRev,
	).Scan(&newID)
	if err != nil {
		http.Error(w, "Ошибка сохранения в БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. Запускаем фоновую синхронизацию накладных за 60 дней
	go func(restID int) {
		log.Printf("🚀 [Auto-Sync] Фоновая синхронизация накладных для нового заведения ID %d (%s)...", restID, req.Name)
		from := time.Now().AddDate(0, 0, -60).Format("2006-01-02")
		to := time.Now().Format("2006-01-02")
		_, _, _ = s.SyncRestaurant(restID, from, to)
	}(newID)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "ok",
		"restaurant_id": newID,
		"name":          req.Name,
		"message":       "Заведение успешно добавлено и начата загрузка накладных!",
	})
}
