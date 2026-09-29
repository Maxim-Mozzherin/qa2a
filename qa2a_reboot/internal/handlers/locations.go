package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"qa2a/internal/models"
)

// ============================================================================
// СКЛАДЫ И ПОЗИЦИИ КАТАЛОГА
// ============================================================================

// GetLocationsHandler возвращает список складов текущего заведения.
// Доступно всем авторизованным сотрудникам.
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

// CreateLocationHandler создает новый физический или виртуальный склад в заведении.
// Доступно только руководству заведения (Owner, Admin, Manager).
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

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	cleanName := strings.TrimSpace(req.Name)
	if cleanName == "" {
		respondError(w, http.StatusBadRequest, "Наименование склада не может быть пустым")
		return
	}

	if err := h.inventoryService.CreateLocation(cID, cleanName); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

// GetPositionsHandler возвращает справочник номенклатуры (товаров/полуфабрикатов) заведения.
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

// CreatePositionHandler добавляет новую товарную позицию в номенклатуру заведения.
// При указании начального остатка и склада атомарно фиксирует initial_balance.
func (h *Handler) CreatePositionHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)
	hasAccess, err := h.checkAdminAccess(cID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Создавать позиции номенклатуры могут только руководители заведения")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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

	cleanName := strings.TrimSpace(req.Name)
	if cleanName == "" {
		respondError(w, http.StatusBadRequest, "Наименование товара не может быть пустым")
		return
	}

	cleanUnit := strings.TrimSpace(req.Unit)
	if cleanUnit == "" {
		cleanUnit = "шт"
	}

	err = h.inventoryService.CreatePosition(&models.Position{
		CompanyID: cID,
		Name:      cleanName,
		Unit:      cleanUnit,
		Supplier:  strings.TrimSpace(req.Supplier),
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.InitQty > 0 && req.Loc > 0 {
		if errBal := h.inventoryService.SetInitialBalance(userID, cID, cleanName, req.InitQty, cleanUnit, req.Loc); errBal != nil {
			log.Printf("[locations] ⚠️ Ошибка фиксации начального остатка для '%s': %v", cleanName, errBal)
			respondError(w, http.StatusBadRequest, "Позиция создана, но произошла ошибка фиксации остатка: "+errBal.Error())
			return
		}
	}

	respondJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}
