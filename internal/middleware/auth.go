package middleware

import (
	"context"
	"net/http"
	"strconv"

	"qa2a/internal/repository"
)

type contextKey string

const UserIDKey contextKey = "userID"

func AuthMiddleware(repo *repository.Repository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Ищем ID в заголовке
			tgIDStr := r.Header.Get("X-Telegram-ID")
			// Если нет в заголовке (например, при скачивании файла), ищем в URL: ?tg_id=...
			if tgIDStr == "" {
				tgIDStr = r.URL.Query().Get("tg_id")
			}

			tgID, err := strconv.ParseInt(tgIDStr, 10, 64)

			// Получаем юзера из БД
			user, err := repo.GetUserByTgID(tgID)
			if err != nil || user == nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Кладем ID юзера в контекст
			ctx := context.WithValue(r.Context(), UserIDKey, user.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}