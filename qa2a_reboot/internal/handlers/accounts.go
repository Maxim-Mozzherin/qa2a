package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// ============================================================================
// СЧЕТА СПИСАНИЯ
// ============================================================================

// GetAccountsHandler возвращает доступные статьи и счета списания в заведении.
// Доступно всем авторизованным сотрудникам заведения.
func (h *Handler) GetAccountsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	accs, err := h.inventoryService.GetAccounts(cID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка получения статей списания")
		return
	}
	respondJSON(w, http.StatusOK, accs)
}

// CreateAccountHandler создает новую статью списания в заведении.
// Требуются права руководства заведения (Owner, Admin, Manager).
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

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Name       string `json:"name"`
		ExternalID string `json:"externalID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	cleanName := strings.TrimSpace(req.Name)
	if cleanName == "" {
		respondError(w, http.StatusBadRequest, "Наименование статьи списания не может быть пустым")
		return
	}

	if err := h.inventoryService.CreateAccount(cID, cleanName, strings.TrimSpace(req.ExternalID)); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "created"})
}

// DeleteAccountHandler удаляет статью списания по ID.
// Требуются права руководства заведения (Owner, Admin, Manager).
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
	if id <= 0 {
		respondError(w, http.StatusBadRequest, "Некорректный ID счета списания")
		return
	}

	if err := h.inventoryService.DeleteAccount(cID, id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
