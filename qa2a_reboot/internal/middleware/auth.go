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
	"time"

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

// VerifySignedTokenExported извлекает и верифицирует tgID из подписанного токена.
// VerifySignedTokenWithVersion извлекает и верифицирует tgID и version из подписанного токена.
// Поддерживает формат "tgID:version:exp:hex_signature", а также legacy "tgID:exp:hex_signature".
func VerifySignedTokenWithVersion(token, secret string) (int64, int) {
	parts := strings.Split(token, ":")
	if len(parts) == 4 {
		tgID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, 0
		}
		version, err := strconv.Atoi(parts[1])
		if err != nil || version <= 0 {
			return 0, 0
		}
		exp, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || time.Now().Unix() > exp {
			return 0, 0
		}

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(fmt.Sprintf("%d:%d:%d", tgID, version, exp)))
		expectedSig := hex.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(parts[3]), []byte(expectedSig)) {
			return 0, 0
		}
		return tgID, version
	} else if len(parts) == 3 {
		// Legacy формат для плавного перехода
		tgID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, 0
		}
		exp, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || time.Now().Unix() > exp {
			return 0, 0
		}

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(fmt.Sprintf("%d:%d", tgID, exp)))
		expectedSig := hex.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
			return 0, 0
		}
		return tgID, 1
	}
	return 0, 0
}

// VerifySignedTokenExported извлекает и верифицирует tgID из подписанного токена.
func VerifySignedTokenExported(token, secret string) int64 {
	tgID, _ := VerifySignedTokenWithVersion(token, secret)
	return tgID
}

// AuthMiddleware проверяет криптографически подписанный токен пользователя через HTTP-заголовки.
// Использование query-параметров (?tg_id=) исключено для предотвращения утечки токенов в access-логи.
func AuthMiddleware(repo *repository.Repository, botToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Пропускаем preflight CORS-запросы
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// 1. Ищем Signed Token в заголовках запроса (X-Telegram-ID или Authorization Bearer)
			tokenStr := strings.TrimSpace(r.Header.Get("X-Telegram-ID"))
			if tokenStr == "" {
				authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
				if strings.HasPrefix(authHeader, "Bearer ") {
					tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
				}
			}

			if tokenStr == "" {
				sendUnauthorizedResponse(w, "Отсутствует токен авторизации (X-Telegram-ID или Authorization Bearer)")
				return
			}

			// 2. Верифицируем криптографическую подпись токена и версию
			tgID, tokenVersion := VerifySignedTokenWithVersion(tokenStr, botToken)
			if tgID <= 0 {
				sendUnauthorizedResponse(w, "Недействительный или поддельный токен авторизации")
				return
			}

			// 3. Проверяем наличие пользователя в базе данных
			user, err := repo.GetUserByTgID(tgID)
			if err != nil || user == nil {
				sendUnauthorizedResponse(w, "Пользователь не зарегистрирован в системе")
				return
			}

			// 4. Проверяем валидность версии сессии (Session Invalidation)
			if user.TokenVersion != tokenVersion {
				sendUnauthorizedResponse(w, "Сессия была инвалидирована (сброс доступа). Пожалуйста, выполните повторный вход")
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

// SupplierAuthMiddleware checks if user is in marketplace_supplier_users and verifies token_version against db
func SupplierAuthMiddleware(repo *repository.Repository, botToken string, checkSupplier func(int64) int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			tokenStr := strings.TrimSpace(r.Header.Get("X-Telegram-ID"))
			if tokenStr == "" {
				authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
				if strings.HasPrefix(authHeader, "Bearer ") {
					tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
				}
			}
			if tokenStr == "" {
				sendUnauthorizedResponse(w, "Отсутствует токен авторизации (X-Telegram-ID или Authorization Bearer)")
				return
			}
			tgID, tokenVersion := VerifySignedTokenWithVersion(tokenStr, botToken)
			if tgID <= 0 {
				sendUnauthorizedResponse(w, "Недействительный или поддельный токен авторизации")
				return
			}
			if repo != nil {
				user, err := repo.GetUserByTgID(tgID)
				if err != nil || user == nil {
					sendUnauthorizedResponse(w, "Пользователь не зарегистрирован в системе")
					return
				}
				if user.TokenVersion != tokenVersion {
					sendUnauthorizedResponse(w, "Сессия была инвалидирована (сброс доступа). Пожалуйста, выполните повторный вход")
					return
				}
			}
			supplierID := checkSupplier(tgID)
			if supplierID <= 0 {
				sendUnauthorizedResponse(w, "Not a supplier")
				return
			}
			// Use contextKey to avoid collisions
			ctx := context.WithValue(r.Context(), "supplier_id", supplierID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

