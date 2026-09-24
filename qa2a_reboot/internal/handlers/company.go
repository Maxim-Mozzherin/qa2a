package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"qa2a/pkg/ratelimit"
)

// ============================================================================
// УПРАВЛЕНИЕ КОМПАНИЕЙ И КОМАНДОЙ
// ============================================================================

// JoinCompanyHandler обрабатывает вступление сотрудника в заведение по инвайт-коду.
func (h *Handler) JoinCompanyHandler(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	if userID == 0 {
		respondError(w, http.StatusUnauthorized, "Пользователь не авторизован")
		return
	}

	clientIP := ratelimit.GetClientIP(r)
	rateKey := fmt.Sprintf("%d_%s", userID, clientIP)
	if allowed, remaining := h.joinLimiter.Allow(rateKey); !allowed {
		respondError(w, http.StatusTooManyRequests, fmt.Sprintf("Слишком много попыток входа по инвайт-коду. Пожалуйста, подождите %d сек.", int(remaining.Seconds())+1))
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

	h.joinLimiter.RecordSuccess(rateKey)

	// Notify admins about the new join request
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var cName string
		_ = h.inventoryService.GetRepo().GetDb().GetContext(ctx, &cName, "SELECT name FROM companies WHERE UPPER(invite_code) = UPPER($1) LIMIT 1", strings.TrimSpace(req.Code))
		
		var tgIDs []int64
		queryAdmins := `SELECT u.tg_id FROM users u JOIN memberships m ON u.id = m.user_id JOIN companies c ON m.company_id = c.id WHERE UPPER(c.invite_code) = UPPER($1) AND m.role IN ('owner', 'admin', 'manager')`
		_ = h.inventoryService.GetRepo().GetDb().SelectContext(ctx, &tgIDs, queryAdmins, strings.TrimSpace(req.Code))
		
		var reqUser string
		_ = h.inventoryService.GetRepo().GetDb().GetContext(ctx, &reqUser, "SELECT COALESCE(full_name, username) FROM users WHERE id = $1", userID)
		
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

// GetJoinRequestsHandler возвращает список активных заявок на вступление в заведение.
func (h *Handler) GetJoinRequestsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 { respondError(w, http.StatusForbidden, "Доступ запрещен"); return }
	hasAccess, _ := h.checkAdminAccess(cID, h.getUserID(r))
	if !hasAccess { respondError(w, http.StatusForbidden, "Только руководство может просматривать заявки"); return }
	
	reqs, err := h.inventoryService.GetRepo().GetJoinRequests(cID)
	if err != nil { respondError(w, http.StatusInternalServerError, err.Error()); return }
	respondJSON(w, http.StatusOK, reqs)
}

// ApproveJoinRequestHandler подтверждает заявку на вступление нового сотрудника.
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

// RejectJoinRequestHandler отклоняет заявку на вступление сотрудника.
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

	// Инвалидируем все активные сессии исключенного сотрудника
	_ = h.inventoryService.GetRepo().IncrementTokenVersion(targetUserID)

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
