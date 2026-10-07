package handlers

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
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

// SaveIikoSettingsHandler сохраняет и шифрует (AES-256-GCM) реквизиты подключения к iiko RMS.
// Доступно только Владельцу, Администратору или Управляющему заведения.
func (h *Handler) SaveIikoSettingsHandler(w http.ResponseWriter, r *http.Request) {
	cID := h.getCompanyID(r)
	if cID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен")
		return
	}
	userID := h.getUserID(r)

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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

// SyncIikoHandler запускает синхронизацию номенклатуры и складов из iiko через безопасную очередь.
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

	if h.syncQueue != nil {
		// Ожидаем выполнения задачи в очереди с комфортным таймаутом (до 25 сек)
		err := h.syncQueue.EnqueueWait(cID, userID, 25*time.Second)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				respondJSON(w, http.StatusAccepted, map[string]string{
					"status":  "queued",
					"message": "Заведение поставлено в очередь синхронизации. Процесс выполняется в фоне.",
				})
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		if err := h.iikoService.SyncNomenclature(cID, userID); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
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

// ExternalForceExportHandler позволяет доверенным микросервисам (например, iiko_parser)
// инициировать экстренную выгрузку списаний заведения в iiko RMS с уведомлением в @qa2a_team.
func (h *Handler) ExternalForceExportHandler(w http.ResponseWriter, r *http.Request) {
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

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		CompanyID int `json:"company_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CompanyID <= 0 {
		respondError(w, http.StatusBadRequest, "Требуется корректный company_id")
		return
	}

	err := h.iikoService.ExportDailyOperations(req.CompanyID, false)
	if err != nil {
		h.sendTelegramNotification(fmt.Sprintf("⚠️ <b>Ошибка экстренной выгрузки в iiko</b>\nЗаведение ID: <code>#%d</code>\nИсточник: Кабинет бухгалтера (кнопка в парсере)\nОшибка: <code>%s</code>", req.CompanyID, err.Error()))
		respondError(w, http.StatusInternalServerError, "Сбой выгрузки в iiko: "+err.Error())
		return
	}

	h.sendTelegramNotification(fmt.Sprintf("✅ <b>Успешная экстренная выгрузка в iiko</b>\nЗаведение ID: <code>#%d</code>\nИсточник: Кабинет бухгалтера (кнопка в парсере)\nСтатус: Все подтвержденные списания и перемещения выгружены", req.CompanyID))
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "exported",
		"message": fmt.Sprintf("Выгрузка для заведения #%d успешно завершена", req.CompanyID),
	})
}
