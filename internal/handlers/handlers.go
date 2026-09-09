package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
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
	authService      *service.AuthService
	inventoryService *service.InventoryService
	reportService    *service.ReportService
	iikoService      *service.IikoService
	botToken         string
}

// New создает новый экземпляр HTTP-обработчика.
func New(as *service.AuthService, is *service.InventoryService, rs *service.ReportService, iikoSvc *service.IikoService, t string) *Handler {
	return &Handler{
		authService:      as,
		inventoryService: is,
		reportService:    rs,
		iikoService:      iikoSvc,
		botToken:         t,
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
	id, _ := strconv.Atoi(cIDStr)
	return id
}

// getUserID извлекает ID пользователя из контекста (через AuthMiddleware)
// либо через заголовок X-Telegram-ID для незащищенных роутов (onboarding).
func (h *Handler) getUserID(r *http.Request) int {
	if id := middleware.GetUserID(r.Context()); id > 0 {
		return id
	}

	// Fallback для незащищенных маршрутов (онбординг / создание первого бизнеса)
	tID, _ := strconv.ParseInt(r.Header.Get("X-Telegram-ID"), 10, 64)
	if tID != 0 {
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
	} else if req.DemoID > 0 {
		tgID = req.DemoID
		name = req.DemoName
		username = "demo_user"
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

	respondJSON(w, http.StatusOK, res)
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

	respondJSON(w, http.StatusOK, map[string]string{"company": name})
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

	opDate := parseFlexibleDate(req.Date)

	var err error
	if req.Type == "transfer" {
		err = h.inventoryService.Transfer(userID, cID, req.Pos, req.Qty, req.Unit, req.Loc, req.ToLoc, req.Comment, opDate)
	} else {
		if req.AccountID == "" {
			req.AccountID = "97036ddb-b2e1-cd47-1669-c145daa9f9c5"
		}
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
		_ = h.inventoryService.WriteOff(
			userID,
			cID,
			req.Name,
			-req.InitQty,
			req.Unit,
			req.Loc,
			false,
			"Начальный остаток при создании",
			"97036ddb-b2e1-cd47-1669-c145daa9f9c5",
			time.Now(),
		)
	}

	respondJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

// ============================================================================
// ЗАЯВКИ НА ЗАКУПКУ (PROCUREMENT)
// ============================================================================

// CreateProcurementHandler регистрирует новую заявку на поставку.
func (h *Handler) CreateProcurementHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
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

	var req struct {
		RequestID int    `json:"request_id"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	if err := h.inventoryService.UpdateProcurementStatus(req.RequestID, req.Status, userID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DownloadProcurementPDFHandler формирует и отдает печатный PDF-документ заявки.
func (h *Handler) DownloadProcurementPDFHandler(w http.ResponseWriter, r *http.Request) {
	if h.reportService == nil {
		respondError(w, http.StatusInternalServerError, "Сервис генерации PDF не инициализирован")
		return
	}

	reqID, _ := strconv.Atoi(mux.Vars(r)["id"])
	cID := h.getCompanyID(r)

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"procurement_request_%d.pdf\"", reqID))

	if err := h.reportService.GenerateProcurementPDF(reqID, cID, w); err != nil {
		log.Printf("[report] ❌ Сбой формирования PDF заявки #%d: %v", reqID, err)
	}
}

// ============================================================================
// ИНВЕНТАРИЗАЦИЯ
// ============================================================================

// StartInventoryHandler начинает полную инвентаризацию склада с расчетом остатка в iiko.
func (h *Handler) StartInventoryHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
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
	userID := h.getUserID(r)

	if err := h.iikoService.SyncNomenclature(cID, userID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "synchronized"})
}

// ForceExportHandler запускает внеплановую выгрузку списаний и перемещений за день.
func (h *Handler) ForceExportHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)

	if err := h.iikoService.ExportDailyOperations(cID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "exported"})
}

// GetIikoAccountsHandler подгружает список расходных статей учета прямо из сервера iiko RMS.
func (h *Handler) GetIikoAccountsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)

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

