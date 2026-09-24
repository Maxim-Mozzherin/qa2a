package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"qa2a/internal/models"

	"github.com/gorilla/mux"
)

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

// GetPendingWriteoffsHandler возвращает список ожидающих подтверждения списаний (для шефа/руководства).
func (h *Handler) GetPendingWriteoffsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	userID := h.getUserID(r)
	if cID == 0 || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Необходима авторизация")
		return
	}

	isManager, err := h.checkAdminAccess(cID, userID)
	if err != nil || !isManager {
		respondError(w, http.StatusForbidden, "Доступ разрешен только шефам и руководству")
		return
	}

	items, err := h.inventoryService.GetRepo().GetPendingWriteoffs(cID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка загрузки активных списаний: "+err.Error())
		return
	}
	if items == nil {
		items = []models.PendingWriteoffDTO{}
	}

	notes, err := h.inventoryService.GetRepo().GetShiftNotesByCompany(cID)
	if err != nil {
		notes = []models.WriteoffShiftNote{}
	}

	notesMap := make(map[string]string)
	for _, n := range notes {
		key := n.AccountID + "_" + n.BusinessDate
		notesMap[key] = n.Note
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"items": items,
		"notes": notesMap,
	})
}

// GetPendingWriteoffsCountHandler возвращает счетчик списаний в ожидании подтверждения для бейджа.
func (h *Handler) GetPendingWriteoffsCountHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	userID := h.getUserID(r)
	if cID == 0 || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Необходима авторизация")
		return
	}

	isManager, err := h.checkAdminAccess(cID, userID)
	if err != nil || !isManager {
		respondJSON(w, http.StatusOK, map[string]int{"count": 0})
		return
	}

	count, err := h.inventoryService.GetRepo().GetPendingWriteoffsCount(cID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка подсчета ожидающих списаний")
		return
	}

	respondJSON(w, http.StatusOK, map[string]int{"count": count})
}

// ApproveWriteoffHandler утверждает списание шефом.
func (h *Handler) ApproveWriteoffHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	userID := h.getUserID(r)
	if cID == 0 || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Необходима авторизация")
		return
	}

	isManager, err := h.checkAdminAccess(cID, userID)
	if err != nil || !isManager {
		respondError(w, http.StatusForbidden, "Доступ разрешен только шефам и руководству")
		return
	}

	opID, _ := strconv.Atoi(mux.Vars(r)["id"])
	if opID == 0 {
		respondError(w, http.StatusBadRequest, "Неверный ID операции")
		return
	}

	if err := h.inventoryService.ApproveWriteoff(cID, opID, userID); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

// RejectWriteoffHandler отклоняет списание с возвратом остатка на склад.
func (h *Handler) RejectWriteoffHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	userID := h.getUserID(r)
	if cID == 0 || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Необходима авторизация")
		return
	}

	isManager, err := h.checkAdminAccess(cID, userID)
	if err != nil || !isManager {
		respondError(w, http.StatusForbidden, "Доступ разрешен только шефам и руководству")
		return
	}

	opID, _ := strconv.Atoi(mux.Vars(r)["id"])
	if opID == 0 {
		respondError(w, http.StatusBadRequest, "Неверный ID операции")
		return
	}

	if err := h.inventoryService.RejectWriteoff(cID, opID, userID); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// SaveShiftNoteHandler сохраняет заметку шефа к смене и статье расходов для выгрузки в iiko.
func (h *Handler) SaveShiftNoteHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	userID := h.getUserID(r)
	if cID == 0 || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Необходима авторизация")
		return
	}

	isManager, err := h.checkAdminAccess(cID, userID)
	if err != nil || !isManager {
		respondError(w, http.StatusForbidden, "Доступ разрешен только шефам и руководству")
		return
	}

	var req struct {
		AccountID    string `json:"account_id"`
		BusinessDate string `json:"business_date"`
		Note         string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}

	cleanAccountID := strings.TrimSpace(req.AccountID)
	cleanDate := strings.TrimSpace(req.BusinessDate)
	if cleanAccountID == "" || cleanDate == "" {
		respondError(w, http.StatusBadRequest, "account_id и business_date обязательны")
		return
	}

	note := &models.WriteoffShiftNote{
		CompanyID:    cID,
		AccountID:    cleanAccountID,
		BusinessDate: cleanDate,
		Note:         strings.TrimSpace(req.Note),
		ChefUserID:   &userID,
	}

	if err := h.inventoryService.GetRepo().UpsertShiftNote(note); err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка сохранения заметки к смене: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "saved"})
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
