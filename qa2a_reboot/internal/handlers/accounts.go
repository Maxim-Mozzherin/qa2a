package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

// ============================================================================
// СЧЕТА СПИСАНИЯ
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
	userID := h.getUserID(r)
	hasAccess, err := h.checkAdminAccess(cID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Управлять счетами списания могут только руководители заведения")
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
	userID := h.getUserID(r)
	hasAccess, err := h.checkAdminAccess(cID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Удалять счета списания могут только руководители заведения")
		return
	}
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	if err := h.inventoryService.DeleteAccount(cID, id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
