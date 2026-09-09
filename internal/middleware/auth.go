package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"qa2a/internal/models"
	"qa2a/internal/repository"
)

type contextKey string

const (
	// UserIDKey — ключ контекста для числового ID пользователя в БД.
	UserIDKey contextKey = "userID"

	// UserContextKey — ключ контекста для полной структуры пользователя.
	UserContextKey contextKey = "userModel"
)

// GetUserID извлекает ID авторизованного пользователя из контекста запроса.
func GetUserID(ctx context.Context) int {
	if val := ctx.Value(UserIDKey); val != nil {
		if id, ok := val.(int); ok {
			return id
		}
	}
	return 0
}

// GetUser извлекает профиль пользователя models.User из контекста запроса.
func GetUser(ctx context.Context) *models.User {
	if val := ctx.Value(UserContextKey); val != nil {
		if u, ok := val.(*models.User); ok {
			return u
		}
	}
	return nil
}

// GenerateSignedToken генерирует токен формата "tgID:hex_signature" на основе secret.
func GenerateSignedToken(tgID int64, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d", tgID)))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%d:%s", tgID, sig)
}

// VerifySignedToken извлекает и верифицирует tgID из подписанного токена.
// Токен имеет формат "tgID:hex_signature".
func VerifySignedToken(token, secret string) int64 {
	parts := strings.Split(token, ":")
	if len(parts) != 2 {
		return 0
	}
	tgID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d", tgID)))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if parts[1] != expectedSig {
		return 0
	}
	return tgID
}

// AuthMiddleware проверяет Telegram ID пользователя через криптографически подписанный токен.
func AuthMiddleware(repo *repository.Repository, botToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Пропускаем preflight CORS-запросы
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// 1. Ищем Signed Token в заголовках запроса
			tokenStr := strings.TrimSpace(r.Header.Get("X-Telegram-ID"))

			// 2. Если заголовок отсутствует (например, при прямом скачивании PDF через браузер), ищем в query
			if tokenStr == "" {
				tokenStr = strings.TrimSpace(r.URL.Query().Get("tg_id"))
			}

			if tokenStr == "" {
				sendUnauthorizedResponse(w, "Отсутствует токен авторизации (X-Telegram-ID)")
				return
			}

			// 3. Верифицируем криптографическую подпись токена
			tgID := VerifySignedToken(tokenStr, botToken)
			if tgID <= 0 {
				sendUnauthorizedResponse(w, "Недействительный или поддельный токен авторизации")
				return
			}

			// Проверяем наличие пользователя в базе данных
			user, err := repo.GetUserByTgID(tgID)
			if err != nil || user == nil {
				sendUnauthorizedResponse(w, "Пользователь не зарегистрирован в системе или сессия недействительна")
				return
			}

			// Сохраняем данные пользователя в контекст выполнения запроса
			ctx := context.WithValue(r.Context(), UserIDKey, user.ID)
			ctx = context.WithValue(ctx, UserContextKey, user)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// sendUnauthorizedResponse формирует единообразный JSON-ответ со статусом 401 Unauthorized.
func sendUnauthorizedResponse(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error":   "Unauthorized",
		"message": message,
	})
}
