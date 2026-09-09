package middleware

import (
	"context"
	"encoding/json"
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

// AuthMiddleware проверяет Telegram ID пользователя через заголовок или GET-параметр.
func AuthMiddleware(repo *repository.Repository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Пропускаем preflight CORS-запросы
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// 1. Ищем Telegram ID в заголовках запроса
			tgIDStr := strings.TrimSpace(r.Header.Get("X-Telegram-ID"))

			// 2. Если заголовок отсутствует (например, при прямом скачивании PDF через браузер), ищем в query
			if tgIDStr == "" {
				tgIDStr = strings.TrimSpace(r.URL.Query().Get("tg_id"))
			}

			if tgIDStr == "" {
				sendUnauthorizedResponse(w, "Отсутствует идентификатор авторизации Telegram (X-Telegram-ID)")
				return
			}

			tgID, err := strconv.ParseInt(tgIDStr, 10, 64)
			if err != nil || tgID <= 0 {
				sendUnauthorizedResponse(w, "Некорректный формат идентификатора Telegram ID")
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

