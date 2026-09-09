package main

import (
	"encoding/json"
	"iiko_parser/crypto"
	"net/http"
	"os"
)

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func generateAuthToken(login, password string) string {
	return crypto.HashPasswordSHA1(login + ":" + password)
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		expectedToken := generateAuthToken(buhLogin, buhPassword)
		if token != expectedToken {
			http.Error(w, "Unauthorized (Недействительный токен авторизации)", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	if req.Login != buhLogin || req.Password != buhPassword {
		http.Error(w, "Неверный логин или пароль", http.StatusForbidden)
		return
	}

	token := generateAuthToken(buhLogin, buhPassword)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"token":  token,
	})
}
