package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"qa2a/internal/middleware"
	"qa2a/internal/models"
	"qa2a/internal/service"

	"github.com/gorilla/mux"
)

// Handler агрегирует сервисы бизнес-логики и обрабатывает HTTP-запросы API.
type Handler struct {
	authService        *service.AuthService
	inventoryService   *service.InventoryService
	reportService      *service.ReportService
	iikoService        *service.IikoService
	marketplaceService *service.MarketplaceService
	botToken           string
}

// New создает новый экземпляр HTTP-обработчика.
func New(
	as *service.AuthService,
	is *service.InventoryService,
	rs *service.ReportService,
	iikoSvc *service.IikoService,
	ms *service.MarketplaceService,
	t string,
) *Handler {
	return &Handler{
		authService:        as,
		inventoryService:   is,
		reportService:      rs,
		iikoService:        iikoSvc,
		marketplaceService: ms,
		botToken:           t,
	}
}


// ============================================================================
// БЕЗОПАСНОСТЬ И КРИПТОГРАФИЯ (TELEGRAM WEBAPP)
// ============================================================================

// validateTelegramData проверяет валидность данных, пришедших от Telegram Mini App,
// с использованием HMAC-SHA256 и токена бота, исключая возможность подделки tg_id.
func validateTelegramData(initData, botToken string) bool {
	if botToken == "" {
		return false
	}

	parsed, err := url.ParseQuery(initData)
	if err != nil {
		return false
	}

	var hash string
	var dataCheckArr []string

	for k, v := range parsed {
		if k == "hash" {
			hash = v[0]
			continue
		}
		dataCheckArr = append(dataCheckArr, fmt.Sprintf("%s=%s", k, v[0]))
	}

	if hash == "" {
		return false
	}

	sort.Strings(dataCheckArr)
	dataCheckString := strings.Join(dataCheckArr, "\n")

	secretKey := hmac.New(sha256.New, []byte("WebAppData"))
	secretKey.Write([]byte(botToken))

	mac := hmac.New(sha256.New, secretKey.Sum(nil))
	mac.Write([]byte(dataCheckString))
	expectedHash := hex.EncodeToString(mac.Sum(nil))

	return hash == expectedHash
}

// generateSignedToken создает криптографически подписанный токен для заголовков API
func generateSignedToken(tgID int64, secret string) string {
	exp := time.Now().Add(30 * 24 * time.Hour).Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d:%d", tgID, exp)))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%d:%d:%s", tgID, exp, sig)
}

// verifySignedToken извлекает и верифицирует tgID из подписанного токена
func verifySignedToken(token, secret string) int64 {
	parts := strings.Split(token, ":")
	if len(parts) != 3 {
		return 0
	}
	tgID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0
	}

	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return 0 // Token expired
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d:%d", tgID, exp)))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if parts[2] != expectedSig {
		return 0
	}
	return tgID
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

// ============================================================================
// АВТОРИЗАЦИЯ И УПРАВЛЕНИЕ КОМПАНИЕЙ
// ============================================================================

// AuthHandler выполняет вход или регистрацию на основе Telegram WebApp initData.
func (h *Handler) AuthHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InitData string `json:"initData"`
		DemoID   int64  `json:"demo_id"`
		DemoName string `json:"demo_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат полезной нагрузки запроса")
		return
	}

	var tgID int64
	var username = "user"
	var name = "Пользователь"

	if req.InitData != "" {
		// КРИТИЧЕСКАЯ ЗАЩИТА: проверяем подпись Telegram перед доверием данным
		if !validateTelegramData(req.InitData, h.botToken) {
			respondError(w, http.StatusUnauthorized, "Недействительная подпись авторизации Telegram")
			return
		}

		params, _ := url.ParseQuery(req.InitData)
		userJSON := params.Get("user")
		if userJSON != "" {
			var u struct {
				ID       int64  `json:"id"`
				FN       string `json:"first_name"`
				Username string `json:"username"`
			}
			if err := json.Unmarshal([]byte(userJSON), &u); err == nil {
				tgID = u.ID
				if u.FN != "" {
					name = u.FN
				}
				if u.Username != "" {
					username = u.Username
				}
			} else {
				log.Printf("[auth] ⚠️ Ошибка декодирования объекта user из initData: %v", err)
			}
		}
	} else if req.DemoID > 0 && os.Getenv("APP_ENV") == "development" {
		tgID = req.DemoID
		name = req.DemoName
		username = "demo_user"
	} else {
		respondError(w, http.StatusUnauthorized, "Необходима авторизация через Telegram")
		return
	}

	if tgID == 0 {
		respondError(w, http.StatusBadRequest, "Не удалось определить Telegram ID пользователя")
		return
	}

	res, err := h.authService.LoginOrRegister(tgID, username, name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Генерируем подписанный токен для последующих защищенных запросов
	token := generateSignedToken(tgID, h.botToken)

	responseWithToken := struct {
		*service.AuthResponse
		Token string `json:"token"`
	}{
		AuthResponse: res,
		Token:        token,
	}

	respondJSON(w, http.StatusOK, responseWithToken)
}

// JoinCompanyHandler обрабатывает вступление сотрудника в заведение по инвайт-коду.
func (h *Handler) JoinCompanyHandler(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	if userID == 0 {
		respondError(w, http.StatusUnauthorized, "Пользователь не авторизован")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		respondError(w, http.StatusBadRequest, "Укажите код доступа")
		return
	}

	name, err := h.authService.JoinCompanyByCode(userID, req.Code)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Notify admins about the new join request
	go func() {
		var cName string
		_ = h.inventoryService.GetRepo().GetDb().Get(&cName, "SELECT name FROM companies WHERE UPPER(invite_code) = UPPER($1) LIMIT 1", strings.TrimSpace(req.Code))
		
		var tgIDs []int64
		queryAdmins := `SELECT u.tg_id FROM users u JOIN memberships m ON u.id = m.user_id JOIN companies c ON m.company_id = c.id WHERE UPPER(c.invite_code) = UPPER($1) AND m.role IN ('owner', 'admin', 'manager')`
		h.inventoryService.GetRepo().GetDb().Select(&tgIDs, queryAdmins, strings.TrimSpace(req.Code))
		
		var reqUser string
		h.inventoryService.GetRepo().GetDb().Get(&reqUser, "SELECT COALESCE(full_name, username) FROM users WHERE id = $1", userID)
		
		msg := fmt.Sprintf("🔔 <b>Новая заявка на вступление!</b>\nПользователь <b>%s</b> хочет присоединиться к заведению <b>%s</b>.\n\nЗайдите в настройки Mini App (Команда заведения), чтобы принять или отклонить заявку.",
			html.EscapeString(reqUser), html.EscapeString(cName))
		for _, tid := range tgIDs {
			h.sendTelegramMessage(tid, msg)
		}
	}()

	respondJSON(w, http.StatusOK, map[string]string{
		"company": name, 
		"message": "Заявка на вступление отправлена руководству заведения. Ожидайте подтверждения.",
	})
}

func (h *Handler) GetJoinRequestsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 { respondError(w, http.StatusForbidden, "Доступ запрещен"); return }
	hasAccess, _ := h.checkAdminAccess(cID, h.getUserID(r))
	if !hasAccess { respondError(w, http.StatusForbidden, "Только руководство может просматривать заявки"); return }
	
	reqs, err := h.inventoryService.GetRepo().GetJoinRequests(cID)
	if err != nil { respondError(w, http.StatusInternalServerError, err.Error()); return }
	respondJSON(w, http.StatusOK, reqs)
}

func (h *Handler) ApproveJoinRequestHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	reqID, _ := strconv.Atoi(mux.Vars(r)["id"])
	hasAccess, _ := h.checkAdminAccess(cID, h.getUserID(r))
	if !hasAccess { respondError(w, http.StatusForbidden, "Нет доступа"); return }
	
	var uData struct { TgID int64 `db:"tg_id"`; CompanyName string `db:"name"` }
	q := `SELECT u.tg_id, c.name FROM join_requests jr JOIN users u ON jr.user_id = u.id JOIN companies c ON jr.company_id = c.id WHERE jr.id = $1 AND jr.company_id = $2`
	_ = h.inventoryService.GetRepo().GetDb().Get(&uData, q, reqID, cID)

	if err := h.inventoryService.GetRepo().ApproveJoinRequest(reqID, cID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error()); return
	}
	
	if uData.TgID > 0 {
		msg := fmt.Sprintf("✅ <b>Заявка одобрена!</b>\nВы добавлены в команду заведения <b>%s</b>.\nПерезапустите приложение (закройте и откройте заново), чтобы начать работу.", html.EscapeString(uData.CompanyName))
		go h.sendTelegramMessage(uData.TgID, msg)
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (h *Handler) RejectJoinRequestHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	reqID, _ := strconv.Atoi(mux.Vars(r)["id"])
	hasAccess, _ := h.checkAdminAccess(cID, h.getUserID(r))
	if !hasAccess { respondError(w, http.StatusForbidden, "Нет доступа"); return }
	
	var uData struct { TgID int64 `db:"tg_id"`; CompanyName string `db:"name"` }
	q := `SELECT u.tg_id, c.name FROM join_requests jr JOIN users u ON jr.user_id = u.id JOIN companies c ON jr.company_id = c.id WHERE jr.id = $1 AND jr.company_id = $2`
	_ = h.inventoryService.GetRepo().GetDb().Get(&uData, q, reqID, cID)

	if err := h.inventoryService.GetRepo().RejectJoinRequest(reqID, cID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error()); return
	}
	
	if uData.TgID > 0 {
		msg := fmt.Sprintf("❌ <b>Заявка отклонена</b>\nРуководство заведения <b>%s</b> отклонило ваш запрос на присоединение.", html.EscapeString(uData.CompanyName))
		go h.sendTelegramMessage(uData.TgID, msg)
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// CreateCompanyHandler создает новое заведение от имени текущего пользователя.
func (h *Handler) CreateCompanyHandler(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	if userID == 0 {
		respondError(w, http.StatusUnauthorized, "Неавторизованный запрос")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	id, err := h.authService.CreateCompany(userID, req.Name)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]int{"id": id})
}

// GetInviteCodeHandler возвращает код приглашения заведения.
func (h *Handler) GetInviteCodeHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	userID := h.getUserID(r)

	code, err := h.authService.GetInviteCode(userID, cID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Код приглашения не найден")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"code": code})
}

// GetMembersHandler возвращает перечень членов команды текущего заведения.
func (h *Handler) GetMembersHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusBadRequest, "Отсутствует идентификатор заведения (X-Company-ID)")
		return
	}

	members, err := h.authService.GetCompanyMembers(cID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка чтения списка команды")
		return
	}

	respondJSON(w, http.StatusOK, members)
}

// UpdateMemberRoleHandler обновляет роль и должность сотрудника.
func (h *Handler) UpdateMemberRoleHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)

	var req struct {
		UserID      int    `json:"user_id"`
		Role        string `json:"role"`
		CustomTitle string `json:"custom_title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	err := h.authService.UpdateMemberRole(cID, userID, req.UserID, req.Role, req.CustomTitle)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

// RemoveMemberHandler исключает сотрудника из состава заведения.
func (h *Handler) RemoveMemberHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)
	targetUserID, _ := strconv.Atoi(mux.Vars(r)["id"])

	if targetUserID == 0 {
		respondError(w, http.StatusBadRequest, "Не указан ID пользователя для удаления")
		return
	}

	err := h.authService.RemoveMember(cID, userID, targetUserID)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ============================================================================
// ОПЕРАЦИИ (СПИСАНИЕ, ПЕРЕМЕЩЕНИЕ, РЕДАКТИРОВАНИЕ)
// ============================================================================

// CreateOperationHandler регистрирует списание или внутреннее перемещение.
func (h *Handler) CreateOperationHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)

	var req struct {
		Pos        string  `json:"position_name"`
		Qty        float64 `json:"quantity"`
		Unit       string  `json:"unit"`
		Type       string  `json:"type"`
		Loc        int     `json:"location_id"`
		ToLoc      int     `json:"to_location_id"`
		IsUnlisted bool    `json:"is_unlisted"`
		Comment    string  `json:"comment"`
		AccountID  string  `json:"account_id"`
		Date       string  `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат данных операции")
		return
	}

	if req.Qty <= 0 {
		respondError(w, http.StatusBadRequest, "Количество должно быть строго больше нуля")
		return
	}

	opDate := parseFlexibleDate(req.Date)

	var err error
	if req.Type == "transfer" {
		err = h.inventoryService.Transfer(userID, cID, req.Pos, req.Qty, req.Unit, req.Loc, req.ToLoc, req.Comment, opDate)
	} else {
		// InventoryService сам подставит дефолтный счет, если req.AccountID пустой
		err = h.inventoryService.WriteOff(userID, cID, req.Pos, req.Qty, req.Unit, req.Loc, req.IsUnlisted, req.Comment, req.AccountID, opDate)
	}

	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

// UpdateOperationHandler позволяет отредактировать параметры невыгруженного списания.
func (h *Handler) UpdateOperationHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)
	opID, _ := strconv.Atoi(mux.Vars(r)["id"])

	if opID == 0 {
		respondError(w, http.StatusBadRequest, "Неверный ID операции")
		return
	}

	var req struct {
		Qty       float64 `json:"quantity"`
		Loc       int     `json:"location_id"`
		Comment   string  `json:"comment"`
		AccountID string  `json:"account_id"`
		Date      string  `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	opDate := parseFlexibleDate(req.Date)

	err := h.inventoryService.EditWriteoff(userID, cID, opID, req.Qty, req.Loc, req.Comment, req.AccountID, opDate)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// GetOperationsHandler возвращает историю последних складских проводок.
func (h *Handler) GetOperationsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	res, err := h.inventoryService.GetHistory(cID, 50)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка загрузки истории операций")
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// GetBalancesHandler возвращает текущие остатки товаров по заведению.
func (h *Handler) GetBalancesHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	res, err := h.inventoryService.GetBalances(cID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка получения остатков")
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// GetUnlistedItemsHandler возвращает список позиций, списанных вручную вне номенклатуры.
func (h *Handler) GetUnlistedItemsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	items, err := h.inventoryService.GetGhostItems(cID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, items)
}

// ============================================================================
// СКЛАДЫ И ПОЗИЦИИ КАТАЛОГА
// ============================================================================

// GetLocationsHandler возвращает список складов заведения.
func (h *Handler) GetLocationsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	res, err := h.inventoryService.GetLocations(cID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка получения складов")
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// CreateLocationHandler создает новый склад в заведении.
func (h *Handler) CreateLocationHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	if err := h.inventoryService.CreateLocation(cID, req.Name); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

// GetPositionsHandler возвращает справочник номенклатуры заведения.
func (h *Handler) GetPositionsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	res, err := h.inventoryService.GetPositions(cID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка получения позиций")
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// CreatePositionHandler добавляет товар в номенклатуру заведения.
func (h *Handler) CreatePositionHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)

	var req struct {
		Name     string  `json:"name"`
		Unit     string  `json:"unit"`
		Supplier string  `json:"supplier"`
		InitQty  float64 `json:"initial_quantity"`
		Loc      int     `json:"location_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат данных")
		return
	}

	err := h.inventoryService.CreatePosition(&models.Position{
		CompanyID: cID,
		Name:      req.Name,
		Unit:      req.Unit,
		Supplier:  req.Supplier,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.InitQty > 0 && req.Loc > 0 {
		_ = h.inventoryService.SetInitialBalance(userID, cID, req.Name, req.InitQty, req.Unit, req.Loc)
	}

	respondJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

// ============================================================================
// ЗАЯВКИ НА ЗАКУПКУ (PROCUREMENT)
// ============================================================================

// CreateProcurementHandler регистрирует новую заявку на поставку.
func (h *Handler) CreateProcurementHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)

	var req struct {
		Items []models.ProcurementItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат заявки")
		return
	}

	if err := h.inventoryService.CreateProcurementRequest(cID, userID, req.Items); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

// GetProcurementsHandler возвращает заявки на закупку (по умолчанию: pending).
func (h *Handler) GetProcurementsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}

	requests, err := h.inventoryService.GetProcurementRequests(cID, status)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, requests)
}

// UpdateProcurementStatusHandler утверждает или отклоняет заявку руководством.
func (h *Handler) UpdateProcurementStatusHandler(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	if userID == 0 {
		respondError(w, http.StatusUnauthorized, "Неавторизованный запрос")
		return
	}

	var req struct {
		RequestID int    `json:"request_id"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	// 1. Извлекаем company_id заявки для проверки прав
	var companyID int
	err := h.inventoryService.GetRepo().GetDb().Get(&companyID, "SELECT company_id FROM procurement_requests WHERE id = $1", req.RequestID)
	if err != nil || companyID == 0 {
		respondError(w, http.StatusNotFound, "Заявка не найдена")
		return
	}

	// 2. Проверяем, является ли пользователь руководителем (Owner, Manager, Admin)
	hasAccess, err := h.checkAdminAccess(companyID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Согласовывать заявки могут только руководители заведения")
		return
	}

	if err := h.inventoryService.UpdateProcurementStatus(req.RequestID, req.Status, userID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// ============================================================================
// ИНВЕНТАРИЗАЦИЯ
// ============================================================================

// StartInventoryHandler начинает полную инвентаризацию склада с расчетом остатка в iiko.
func (h *Handler) StartInventoryHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)

	var req struct {
		LocationID int `json:"location_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	act, err := h.inventoryService.StartInventory(cID, userID, req.LocationID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, act)
}

// GetInventoriesHandler возвращает список инвентаризаций заведения.
func (h *Handler) GetInventoriesHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	acts, err := h.inventoryService.GetInventories(cID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, acts)
}

// GetInventoryHandler возвращает детальные данные акта инвентаризации со строками.
func (h *Handler) GetInventoryHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	actID, _ := strconv.Atoi(mux.Vars(r)["id"])

	act, err := h.inventoryService.GetInventory(cID, actID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Акт инвентаризации не найден")
		return
	}
	respondJSON(w, http.StatusOK, act)
}

// SaveInventoryHandler сохраняет промежуточные результаты подсчета (черновик).
func (h *Handler) SaveInventoryHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	actID, _ := strconv.Atoi(mux.Vars(r)["id"])

	var items []models.InventoryItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат позиций инвентаризации")
		return
	}

	err := h.inventoryService.SaveInventory(cID, actID, items)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// FinalizeInventoryHandler фиксирует факт завершения и выгружает акт в iiko RMS.
func (h *Handler) FinalizeInventoryHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	actID, _ := strconv.Atoi(mux.Vars(r)["id"])

	err := h.inventoryService.FinalizeInventory(cID, actID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "completed"})
}

// DeleteInventoryHandler удаляет незавершенный черновик инвентаризации.
func (h *Handler) DeleteInventoryHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	actID, _ := strconv.Atoi(mux.Vars(r)["id"])

	err := h.inventoryService.DeleteInventory(cID, actID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetIikoDraftsHandler возвращает черновики инвентаризаций, открытые на сервере iiko RMS.
func (h *Handler) GetIikoDraftsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	locID, _ := strconv.Atoi(r.URL.Query().Get("location_id"))

	locs, err := h.inventoryService.GetLocations(cID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка получения складов")
		return
	}

	var targetLoc *models.Location
	for _, l := range locs {
		if l.ID == locID {
			targetLoc = &l
			break
		}
	}
	if targetLoc == nil || targetLoc.ExternalID == "" {
		respondError(w, http.StatusBadRequest, "Склад не привязан к iiko RMS (требуется синхронизация)")
		return
	}

	drafts, err := h.iikoService.FetchDraftInventories(cID, targetLoc.ExternalID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, drafts)
}

// StartInventoryFromDraftHandler создает акт на базе бланка из iiko.
func (h *Handler) StartInventoryFromDraftHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)

	var req struct {
		LocationID int    `json:"location_id"`
		DraftID    string `json:"draft_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	act, err := h.inventoryService.StartInventoryFromDraft(cID, userID, req.LocationID, req.DraftID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, act)
}

// GetTemplatesHandler возвращает шаблоны пересчетов для склада.
func (h *Handler) GetTemplatesHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	locID, _ := strconv.Atoi(r.URL.Query().Get("location_id"))

	list, err := h.inventoryService.GetTemplates(cID, locID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, list)
}

// StartInventoryFromTemplateHandler начинает инвентаризацию по локальному бланку (срезу).
func (h *Handler) StartInventoryFromTemplateHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)

	var req struct {
		LocationID int `json:"location_id"`
		TemplateID int `json:"template_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	act, err := h.inventoryService.StartInventoryFromTemplate(cID, userID, req.LocationID, req.TemplateID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, act)
}

// CreateExternalTemplateHandler принимает созданный в iiko_parser шаблон от бухгалтера.
func (h *Handler) CreateExternalTemplateHandler(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")

	expectedToken := os.Getenv("EXTERNAL_API_KEY")
	if expectedToken == "" {
		expectedToken = "moztech-secret-token-8099"
	}

	if token != expectedToken {
		respondError(w, http.StatusUnauthorized, "Недействительный токен межсервисного взаимодействия")
		return
	}

	var req struct {
		StoreUUID string   `json:"store_uuid"`
		Name      string   `json:"name"`
		Items     []string `json:"items"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверная структура JSON")
		return
	}

	if req.StoreUUID == "" || req.Name == "" || len(req.Items) == 0 {
		respondError(w, http.StatusBadRequest, "Заполните обязательные поля (store_uuid, name, items)")
		return
	}

	var loc struct {
		ID        int `db:"id"`
		CompanyID int `db:"company_id"`
	}
	err := h.inventoryService.GetRepo().GetDb().Get(&loc, "SELECT id, company_id FROM locations WHERE UPPER(external_id) = UPPER($1) LIMIT 1", req.StoreUUID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Склад с указанным UUID не найден в системе")
		return
	}

	err = h.inventoryService.CreateTemplateFromExternal(loc.CompanyID, loc.ID, req.Name, req.Items)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{"status": "success"})
}

// ============================================================================
// СЧЕТА И ИНТЕГРАЦИЯ IIKO
// ============================================================================

// GetAccountsHandler возвращает доступные счета списания в заведении.
func (h *Handler) GetAccountsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	accs, err := h.inventoryService.GetAccounts(cID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка получения статей")
		return
	}
	respondJSON(w, http.StatusOK, accs)
}

// CreateAccountHandler создает новую статью списания.
func (h *Handler) CreateAccountHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}

	var req struct {
		Name       string `json:"name"`
		ExternalID string `json:"externalID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	if err := h.inventoryService.CreateAccount(cID, req.Name, req.ExternalID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "created"})
}

// DeleteAccountHandler удаляет статью списания.
func (h *Handler) DeleteAccountHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	if err := h.inventoryService.DeleteAccount(cID, id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetIikoSettingsHandler возвращает настройки подключения к iiko RMS (только для Владельца).
func (h *Handler) GetIikoSettingsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)

	settings, err := h.iikoService.GetSettings(cID, userID)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, settings)
}

// SaveIikoSettingsHandler сохраняет и шифрует реквизиты iiko RMS.
func (h *Handler) SaveIikoSettingsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)

	var req struct {
		Host     string `json:"host"`
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	err := h.iikoService.SaveSettings(cID, userID, req.Host, req.Login, req.Password)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// SyncIikoHandler запускает принудительную синхронизацию номенклатуры и складов из iiko.
func (h *Handler) SyncIikoHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)

	hasAccess, err := h.checkAdminAccess(cID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Синхронизацию iiko могут запускать только руководители заведения")
		return
	}

	if err := h.iikoService.SyncNomenclature(cID, userID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "synchronized"})
}

// ForceExportHandler запускает внеплановую выгрузку списаний и перемещений за день.
func (h *Handler) ForceExportHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)

	hasAccess, err := h.checkAdminAccess(cID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Внеплановую выгрузку в iiko могут запускать только руководители заведения")
		return
	}

	if err := h.iikoService.ExportDailyOperations(cID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "exported"})
}

// GetIikoAccountsHandler подгружает список расходных статей учета прямо из сервера iiko RMS.
func (h *Handler) GetIikoAccountsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}

	accs, err := h.iikoService.FetchIikoAccounts(cID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, accs)
}

// IikoWebhookHandler эндпоинт для приема внешних уведомлений iiko.
func (h *Handler) IikoWebhookHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// CreateAccountingTicketHandler создает новую заявку заведения в бухгалтерию.
func (h *Handler) CreateAccountingTicketHandler(w http.ResponseWriter, r *http.Request) {
	companyID := h.getCompanyID(r)
	userID := h.getUserID(r)

	if companyID == 0 || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Недостаточно данных авторизации")
		return
	}

	hasAccess, err := h.checkAdminAccess(companyID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Отправлять заявки могут только руководители заведения")
		return
	}

	// Лимит 50 МБ на запрос
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "Превышен лимит размера файлов (макс 50 МБ)")
		return
	}

	category := r.FormValue("category")
	description := strings.TrimSpace(r.FormValue("description"))

	if description == "" {
		respondError(w, http.StatusBadRequest, "Пожалуйста, опишите суть вопроса")
		return
	}

	var savedPaths []string
	files := r.MultipartForm.File["media"] // Массив файлов
	
	os.MkdirAll(filepath.Join("uploads", "tickets"), os.ModePerm)

	allowedExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
		".mp4": true, ".mov": true,
	}

	for i, fileHeader := range files {
		ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
		if !allowedExts[ext] {
			respondError(w, http.StatusBadRequest, "Недопустимый формат файла. Разрешены только фото и видео.")
			return
		}

		file, err := fileHeader.Open()
		if err != nil { continue }
		filename := fmt.Sprintf("%d_%d_%d%s", companyID, time.Now().UnixNano(), i, ext)
		outPath := filepath.Join("uploads", "tickets", filename)
		
		out, errCreate := os.Create(outPath)
		if errCreate == nil {
			io.Copy(out, file)
			out.Close()
			savedPaths = append(savedPaths, "/" + strings.ReplaceAll(outPath, "\\", "/"))
		}
		file.Close()
	}

	mediaJSON, _ := json.Marshal(savedPaths)
	if string(mediaJSON) == "null" { mediaJSON = []byte("[]") }

	err = h.inventoryService.GetRepo().CreateAccountingTicket(companyID, userID, category, "normal", description, string(mediaJSON))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка создания тикета: "+err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

// GetAccountingTicketsHandler возвращает историю обращений заведения в бухгалтерию.
func (h *Handler) GetAccountingTicketsHandler(w http.ResponseWriter, r *http.Request) {
	companyID := h.getCompanyID(r)
	userID := h.getUserID(r)

	if companyID == 0 || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Неавторизованный запрос")
		return
	}

	hasAccess, err := h.checkAdminAccess(companyID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Доступ разрешен только руководству заведения")
		return
	}

	tickets, err := h.inventoryService.GetRepo().GetCompanyTickets(companyID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка чтения списка заявок: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, tickets)
}

func (h *Handler) sendTelegramMessage(tgID int64, text string) {
	if h.botToken == "" || tgID <= 0 { return }
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", h.botToken)
	payload := map[string]interface{}{"chat_id": tgID, "text": text, "parse_mode": "HTML"}
	bodyBytes, _ := json.Marshal(payload)
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(bodyBytes))
	if err == nil { resp.Body.Close() }
}