package handlers

import (
	"encoding/json"
	"net/http"

	"qa2a/internal/models"
)

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
	userID := h.getUserID(r)
	hasAccess, err := h.checkAdminAccess(cID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Создавать склады могут только руководители заведения")
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
