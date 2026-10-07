package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"analytics_service/internal/crypto"
	"analytics_service/internal/netutil"
	"analytics_service/internal/queue"
	"analytics_service/internal/report"
)

// AddRestaurantRequest структура данных для подключения нового заведения через iiko RMS
type AddRestaurantRequest struct {
	Name               string  `json:"name"`
	City               string  `json:"city"`
	CuisineType        string  `json:"cuisine_type"`
	IikoHost           string  `json:"iiko_host"`
	IikoLogin          string  `json:"iiko_login"`
	IikoPassword       string  `json:"iiko_password"`
	MonthlyRev         float64 `json:"monthly_revenue"`
	IsSubscribed       bool    `json:"is_subscribed"`
	TelegramRecipients string  `json:"telegram_recipients"`
}

// UpdateSubscriptionRequest запрос на обновление подписки действующего заведения
type UpdateSubscriptionRequest struct {
	RestaurantID       int    `json:"restaurant_id"`
	IsSubscribed       bool   `json:"is_subscribed"`
	TelegramRecipients string `json:"telegram_recipients"`
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
		SELECT 
			id, name, cuisine_type, city, is_active,
			COALESCE(is_subscribed, false),
			subscription_started_at,
			COALESCE(telegram_recipients, ''),
			last_weekly_report_at,
			last_monthly_report_at,
			last_synced_at,
			COALESCE(last_sync_status, 'idle')
		FROM analytics_restaurants
		WHERE is_active = true AND (company_id != 10 OR company_id IS NULL)
		ORDER BY id ASC`)
	if err != nil {
		http.Error(w, "Ошибка БД: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type RestInfo struct {
		ID                    int     `json:"id"`
		Name                  string  `json:"name"`
		CuisineType           string  `json:"cuisine_type"`
		City                  string  `json:"city"`
		IsActive              bool    `json:"is_active"`
		IsSubscribed          bool    `json:"is_subscribed"`
		SubscriptionStartedAt *string `json:"subscription_started_at,omitempty"`
		TelegramRecipients    string  `json:"telegram_recipients"`
		LastWeeklyReportAt    *string `json:"last_weekly_report_at,omitempty"`
		LastMonthlyReportAt   *string `json:"last_monthly_report_at,omitempty"`
		LastSyncedAt          *string `json:"last_synced_at,omitempty"`
		LastSyncStatus        string  `json:"last_sync_status"`
	}

	var list []RestInfo
	for rows.Next() {
		var rest RestInfo
		var subStart, lastWeekly, lastMonthly, lastSynced sql.NullTime

		errScan := rows.Scan(
			&rest.ID, &rest.Name, &rest.CuisineType, &rest.City, &rest.IsActive,
			&rest.IsSubscribed, &subStart, &rest.TelegramRecipients,
			&lastWeekly, &lastMonthly, &lastSynced, &rest.LastSyncStatus,
		)
		if errScan == nil {
			if subStart.Valid {
				t := subStart.Time.Format(time.RFC3339)
				rest.SubscriptionStartedAt = &t
			}
			if lastWeekly.Valid {
				t := lastWeekly.Time.Format(time.RFC3339)
				rest.LastWeeklyReportAt = &t
			}
			if lastMonthly.Valid {
				t := lastMonthly.Time.Format(time.RFC3339)
				rest.LastMonthlyReportAt = &t
			}
			if lastSynced.Valid {
				t := lastSynced.Time.Format(time.RFC3339)
				rest.LastSyncedAt = &t
			}
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
	req.TelegramRecipients = strings.TrimSpace(req.TelegramRecipients)

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

	// 3. Сохраняем в analytics_restaurants с учетом флага подписки
	var newID int
	var subStartedVal interface{}
	if req.IsSubscribed {
		subStartedVal = time.Now()
	}

	err = s.db.QueryRow(`
		INSERT INTO analytics_restaurants (
			name, city, cuisine_type, iiko_host, iiko_login, iiko_password_enc, 
			monthly_revenue, is_subscribed, subscription_started_at, telegram_recipients
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		req.Name, req.City, req.CuisineType, req.IikoHost, req.IikoLogin, passEnc, req.MonthlyRev,
		req.IsSubscribed, subStartedVal, req.TelegramRecipients,
	).Scan(&newID)
	if err != nil {
		http.Error(w, "Ошибка сохранения в БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. Постановка задачи начальной выгрузки накладных в защищенную очередь SyncQueue
	from := time.Now().AddDate(0, 0, -60).Format("2006-01-02")
	to := time.Now().Format("2006-01-02")

	if s.syncQueue != nil {
		s.syncQueue.Enqueue(queue.SyncTask{
			RestaurantID:   newID,
			RestaurantName: req.Name,
			From:           from,
			To:             to,
			Trigger:        "onboarding",
		})
	} else {
		go func(restID int) {
			_, _, _ = s.SyncRestaurant(restID, from, to)
		}(newID)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "ok",
		"restaurant_id": newID,
		"name":          req.Name,
		"is_subscribed": req.IsSubscribed,
		"message":       "Заведение успешно добавлено и поставлено в очередь загрузки накладных!",
	})
}

// HandleUpdateSubscription обновляет статус подписки и логины Telegram
func (s *Server) HandleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Ожидается PUT или POST", http.StatusMethodNotAllowed)
		return
	}

	var req UpdateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Некорректный JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.RestaurantID <= 0 {
		http.Error(w, "Параметр restaurant_id обязателен", http.StatusBadRequest)
		return
	}

	req.TelegramRecipients = strings.TrimSpace(req.TelegramRecipients)

	var currentSub bool
	var currentStart sql.NullTime
	_ = s.db.QueryRow(`SELECT is_subscribed, subscription_started_at FROM analytics_restaurants WHERE id = $1`, req.RestaurantID).Scan(&currentSub, &currentStart)

	var subStartVal interface{}
	if req.IsSubscribed {
		if currentStart.Valid && currentSub {
			subStartVal = currentStart.Time
		} else {
			subStartVal = time.Now()
		}
	} else {
		subStartVal = nil
	}

	_, err := s.db.Exec(`
		UPDATE analytics_restaurants 
		SET is_subscribed = $1, 
		    subscription_started_at = $2, 
		    telegram_recipients = $3, 
		    updated_at = NOW() 
		WHERE id = $4`,
		req.IsSubscribed, subStartVal, req.TelegramRecipients, req.RestaurantID,
	)
	if err != nil {
		http.Error(w, "Ошибка обновления в БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "ok",
		"restaurant_id": req.RestaurantID,
		"is_subscribed": req.IsSubscribed,
		"message":       "Настройки подписки успешно сохранены",
	})
}

// HandleTestReport отправляет проверочный еженедельный или PDF отчет в Telegram
func (s *Server) HandleTestReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Ожидается POST", http.StatusMethodNotAllowed)
		return
	}

	restIDStr := r.URL.Query().Get("restaurant_id")
	reportType := r.URL.Query().Get("type") // "weekly" | "pdf"
	if restIDStr == "" {
		http.Error(w, "restaurant_id обязателен", http.StatusBadRequest)
		return
	}
	restID, _ := strconv.Atoi(restIDStr)

	if s.scheduler == nil {
		http.Error(w, "Планировщик отчетов не инициализирован", http.StatusInternalServerError)
		return
	}

	sentCount, err := s.scheduler.SendImmediateTestReport(restID, reportType)
	if err != nil {
		http.Error(w, "Ошибка отправки отчета: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"sent_count": sentCount,
		"message":    fmt.Sprintf("Тестовый отчет успешно отправлен %d получателям в Telegram!", sentCount),
	})
}

// HandleDownloadPDF генерирует и возвращает клиенту готовый бинарный PDF управленческого аудита
func (s *Server) HandleDownloadPDF(w http.ResponseWriter, r *http.Request) {
	restIDStr := r.URL.Query().Get("restaurant_id")
	if restIDStr == "" {
		http.Error(w, "restaurant_id обязателен", http.StatusBadRequest)
		return
	}
	restID, err := strconv.Atoi(restIDStr)
	if err != nil || restID <= 0 {
		http.Error(w, "Некорректный restaurant_id", http.StatusBadRequest)
		return
	}

	from := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	to := time.Now().Format("2006-01-02")

	pdfBytes, err := report.GenerateMonthlyPDF(s.db, s.llmClient, restID, from, to, s.cfg.FontPath)
	if err != nil {
		http.Error(w, "Ошибка формирования PDF аудита: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var name string
	_ = s.db.QueryRow(`SELECT name FROM analytics_restaurants WHERE id = $1`, restID).Scan(&name)
	filename := fmt.Sprintf("Аудит_закупок_%s_30_дней.pdf", cleanFilename(name))

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(pdfBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
}

func cleanFilename(s string) string {
	var res []rune
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || (r >= 'а' && r <= 'я') || (r >= 'А' && r <= 'Я') || r == '_' || r == '-' {
			res = append(res, r)
		} else {
			res = append(res, '_')
		}
	}
	return string(res)
}
