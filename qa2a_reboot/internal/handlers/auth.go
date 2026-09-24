package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"qa2a/internal/auth"
	"qa2a/internal/middleware"
	"qa2a/internal/service"
)

// ============================================================================
// БЕЗОПАСНОСТЬ И КРИПТОГРАФИЯ (TELEGRAM WEBAPP)
// ============================================================================

// validateTelegramData проверяет валидность данных, пришедших от Telegram Mini App,
// с использованием единой функции auth.ValidateInitData с защитой от replay attacks.
func validateTelegramData(initData, botToken string) bool {
	return auth.ValidateInitData(initData, botToken)
}

// generateSignedToken создает криптографически подписанный токен для заголовков API.
// Формат: tgID:version:exp:signature
func generateSignedToken(tgID int64, version int, secret string) string {
	exp := time.Now().Add(30 * 24 * time.Hour).Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d:%d:%d", tgID, version, exp)))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%d:%d:%d:%s", tgID, version, exp, sig)
}

// verifySignedToken извлекает и верифицирует tgID из подписанного токена
func verifySignedToken(token, secret string) int64 {
	return middleware.VerifySignedTokenExported(token, secret)
}

// ============================================================================
// АВТОРИЗАЦИЯ
// ============================================================================

// AuthHandler выполняет вход или регистрацию на основе Telegram WebApp initData.
func (h *Handler) AuthHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InitData string `json:"initData"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат полезной нагрузки запроса")
		return
	}

	if strings.TrimSpace(req.InitData) == "" {
		respondError(w, http.StatusUnauthorized, "Необходима авторизация через Telegram initData")
		return
	}

	// КРИТИЧЕСКАЯ ЗАЩИТА: проверяем подпись Telegram перед доверием данным
	if !validateTelegramData(req.InitData, h.botToken) {
		respondError(w, http.StatusUnauthorized, "Недействительная подпись авторизации Telegram")
		return
	}

	params, _ := url.ParseQuery(req.InitData)
	userJSON := params.Get("user")
	if userJSON == "" {
		respondError(w, http.StatusBadRequest, "Отсутствуют данные пользователя в initData")
		return
	}

	var u struct {
		ID       int64  `json:"id"`
		FN       string `json:"first_name"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal([]byte(userJSON), &u); err != nil || u.ID <= 0 {
		respondError(w, http.StatusBadRequest, "Не удалось определить Telegram ID пользователя")
		return
	}

	name := u.FN
	if name == "" {
		name = "Пользователь"
	}
	username := u.Username
	if username == "" {
		username = "user"
	}

	res, err := h.authService.LoginOrRegister(u.ID, username, name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Генерируем подписанный токен с актуальной версией сессии
	token := generateSignedToken(u.ID, res.User.TokenVersion, h.botToken)

	responseWithToken := struct {
		*service.AuthResponse
		Token string `json:"token"`
	}{
		AuthResponse: res,
		Token:        token,
	}

	respondJSON(w, http.StatusOK, responseWithToken)
}
