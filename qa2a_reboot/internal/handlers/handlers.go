package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"qa2a/internal/middleware"
	"qa2a/internal/service"
	"qa2a/pkg/ratelimit"
)

// Handler агрегирует сервисы бизнес-логики и обрабатывает HTTP-запросы API.
type Handler struct {
	authService        *service.AuthService
	inventoryService   *service.InventoryService
	reportService      *service.ReportService
	iikoService        *service.IikoService
	marketplaceService *service.MarketplaceService
	botToken           string
	adminTgID          int64
	externalApiKey     string
	joinLimiter        *ratelimit.Limiter
}

// New создает новый экземпляр HTTP-обработчика.
func New(
	as *service.AuthService,
	is *service.InventoryService,
	rs *service.ReportService,
	iikoSvc *service.IikoService,
	ms *service.MarketplaceService,
	t string,
	adminTgID int64,
	externalApiKey string,
) *Handler {
	return &Handler{
		authService:        as,
		inventoryService:   is,
		reportService:      rs,
		iikoService:        iikoSvc,
		marketplaceService: ms,
		botToken:           t,
		adminTgID:          adminTgID,
		externalApiKey:     externalApiKey,
		joinLimiter:        ratelimit.NewLimiter(5, 1*time.Minute, 5*time.Minute, 10000),
	}
}

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ МЕТОДЫ
// ============================================================================

// respondJSON отправляет структурированный ответ в формате JSON.
func respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// respondError формирует стандартизированный JSON-ответ с описанием ошибки.
func respondError(w http.ResponseWriter, statusCode int, message string) {
	respondJSON(w, statusCode, map[string]string{
		"error": message,
	})
}

// getCompanyID извлекает ID компании из заголовка X-Company-ID или URL-параметра ?c_id=.
func (h *Handler) getCompanyID(r *http.Request) int {
	cIDStr := r.Header.Get("X-Company-ID")
	if cIDStr == "" {
		cIDStr = r.URL.Query().Get("c_id")
	}
	cID, _ := strconv.Atoi(cIDStr)
	userID := h.getUserID(r)

	if cID <= 0 || userID <= 0 {
		return 0
	}

	// Строгая изоляция: проверяем, что юзер реально состоит в этой компании
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM memberships WHERE company_id = $1 AND user_id = $2)`
	err := h.inventoryService.GetRepo().GetDb().QueryRow(query, cID, userID).Scan(&exists)
	if err != nil || !exists {
		return 0 // Скрываем данные, если доступа нет
	}

	return cID
}

// getUserID извлекает ID пользователя из контекста (через AuthMiddleware)
// либо через заголовок X-Telegram-ID для незащищенных роутов (onboarding).
func (h *Handler) getUserID(r *http.Request) int {
	if id := middleware.GetUserID(r.Context()); id > 0 {
		return id
	}

	// Fallback для незащищенных маршрутов (онбординг / создание первого бизнеса)
	tIDStr := r.Header.Get("X-Telegram-ID")
	if tID := verifySignedToken(tIDStr, h.botToken); tID != 0 {
		if user, err := h.authService.GetUserByTgID(tID); err == nil && user != nil {
			return user.ID
		}
	}
	return 0
}

// parseFlexibleDate разбирает строку даты в различных поддерживаемых форматах.
func parseFlexibleDate(dateStr string) time.Time {
	if dateStr == "" {
		return time.Now()
	}

	formats := []string{
		"2006-01-02T15:04",
		time.RFC3339,
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, layout := range formats {
		if t, err := time.Parse(layout, dateStr); err == nil {
			if layout == "2006-01-02" {
				return time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, time.Local)
			}
			return t
		}
	}

	return time.Now()
}

// sendTelegramMessage отправляет сервисное сообщение в Telegram через HTTP API бота.
func (h *Handler) sendTelegramMessage(tgID int64, text string) {
	if h.botToken == "" || tgID <= 0 {
		return
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", h.botToken)
	payload := map[string]interface{}{"chat_id": tgID, "text": text, "parse_mode": "HTML"}
	bodyBytes, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(bodyBytes))
	if err == nil {
		resp.Body.Close()
	}
}