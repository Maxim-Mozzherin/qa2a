package api

import (
	"encoding/json"
	"net/http"

	"analytics_service/internal/auth"
)

// LoginRequest структура тела запроса на авторизацию
type LoginRequest struct {
	Email    string `json:"email"`
	Login    string `json:"login"`
	Password string `json:"password"`
}

// HandleLogin аутентифицирует пользователя в БД (таблицы accounting_users и users)
// и устанавливает безопасную сессионную cookie analytics_session
func (s *Server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается (требуется POST)", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Некорректный JSON запроса: "+err.Error(), http.StatusBadRequest)
		return
	}

	identifier := req.Email
	if identifier == "" {
		identifier = req.Login
	}
	if identifier == "" || req.Password == "" {
		http.Error(w, "Укажите логин/email и пароль", http.StatusBadRequest)
		return
	}

	user, err := auth.AuthenticateUser(s.db, identifier, req.Password)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Создаем сессионную cookie и токен
	token, err := auth.CreateSession(s.db, w, user)
	if err != nil {
		http.Error(w, "Ошибка создания сессии: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Успешная авторизация",
		"token":   token,
		"user": map[string]interface{}{
			"id":           user.ID,
			"login":        user.Login,
			"email":        user.Email,
			"role":         user.Role,
			"is_superuser": user.IsSuperuser,
		},
	})
}

// HandleLogout завершает текущую сессию и удаляет cookie
func (s *Server) HandleLogout(w http.ResponseWriter, r *http.Request) {
	auth.ClearSession(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Сессия завершена",
	})
}

// HandleMe возвращает информацию о текущем авторизованном пользователе
func (s *Server) HandleMe(w http.ResponseWriter, r *http.Request) {
	user, err := auth.ValidateSession(s.db, r)
	if err != nil || user == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": false,
			"error":         "Пользователь не авторизован",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"authenticated": true,
		"user": map[string]interface{}{
			"id":           user.ID,
			"login":        user.Login,
			"email":        user.Email,
			"role":         user.Role,
			"is_superuser": user.IsSuperuser,
		},
	})
}
