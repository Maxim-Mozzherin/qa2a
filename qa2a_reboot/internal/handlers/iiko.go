package handlers

import (
	"encoding/json"
	"net/http"
)

// ============================================================================
// ИНТЕГРАЦИЯ С IIKO RMS
// ============================================================================

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

	if err := h.iikoService.ExportDailyOperations(cID, false); err != nil {
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
