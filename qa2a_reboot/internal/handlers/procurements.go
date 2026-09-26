package handlers

import (
	"encoding/json"
	"net/http"

	"qa2a/internal/models"
)

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

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Items []models.ProcurementItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат заявки")
		return
	}

	if len(req.Items) == 0 {
		respondError(w, http.StatusBadRequest, "Заявка должна содержать хотя бы одну позицию")
		return
	}

	for _, item := range req.Items {
		if item.Quantity <= 0 {
			respondError(w, http.StatusBadRequest, "Количество позиций в заявке должно быть строго больше нуля")
			return
		}
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
