package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"qa2a/internal/models"

	"github.com/gorilla/mux"
)

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
	userID := h.getUserID(r)
	hasAccess, err := h.checkAdminAccess(cID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Удалять инвентаризации могут только руководители заведения")
		return
	}

	actID, _ := strconv.Atoi(mux.Vars(r)["id"])

	err = h.inventoryService.DeleteInventory(cID, actID)
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
	token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))

	expectedToken := strings.TrimSpace(h.externalApiKey)
	if expectedToken == "" {
		expectedToken = strings.TrimSpace(os.Getenv("EXTERNAL_API_KEY"))
	}
	if expectedToken == "" {
		respondError(w, http.StatusInternalServerError, "Критическая ошибка конфигурации: EXTERNAL_API_KEY не задан")
		return
	}

	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
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
