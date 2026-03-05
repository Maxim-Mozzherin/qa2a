package handlers

import (
	"encoding/json"
	"net/http"
	"qa2a/internal/service"
	"qa2a/internal/auth"
	"net/url"
)

type Handler struct {
	authService *service.AuthService
}

func New(svc *service.AuthService) *Handler {
	return &Handler{authService: svc}
}

func (h *Handler) AuthHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InitData string `json:"initData"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// 1. Валидация через наш новый auth пакет
	if !auth.ValidateInitData(req.InitData, h.botToken) {
		http.Error(w, "Unauthorized: invalid hash", http.StatusUnauthorized)
		return
	}

	// 2. Парсим данные (упрощенно для примера)
	params, _ := url.ParseQuery(req.InitData)
	userJSON := params.Get("user")
	var tgUser struct {
		ID        int64  `json:"id"`
		FirstName string `json:"first_name"`
		Username  string `json:"username"`
	}
	json.Unmarshal([]byte(userJSON), &tgUser)

	// 3. Логика сервиса
	user, err := h.authService.LoginOrRegister(tgUser.ID, tgUser.Username, tgUser.FirstName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}
