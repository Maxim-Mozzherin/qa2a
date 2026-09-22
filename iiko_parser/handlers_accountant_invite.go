
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func handleGenerateAccountantInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	user := GetAuthUser(r)
	if user == nil || user.Role != "superadmin" {
		http.Error(w, "Только главный администратор платформы (superadmin) может генерировать инвайты", http.StatusForbidden)
		return
	}

	creatorID := &user.ID

	b := make([]byte, 16)
	rand.Read(b)
	inviteCode := hex.EncodeToString(b)
	expiresAt := time.Now().Add(24 * time.Hour)

	_, err := db.Exec("INSERT INTO accountant_invites (code, expires_at, created_by) VALUES ($1, $2, $3)", inviteCode, expiresAt, creatorID)
	if err != nil {
		http.Error(w, "Ошибка БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	inviteLink := "https://moztech.ru/bugh-team/?invite=" + inviteCode

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"invite_link": inviteLink,
	})
}

func handleRegisterAccountant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		InviteCode string `json:"invite_code"`
		Login      string `json:"login"`
		Email      string `json:"email"`
		Password   string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	if req.InviteCode == "" || req.Login == "" || req.Password == "" {
		http.Error(w, "Все поля обязательны", http.StatusBadRequest)
		return
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Ошибка шифрования пароля", http.StatusInternalServerError)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Ошибка БД", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// 1. Check invite code within transaction
	var id int
	var expiresAt time.Time
	err = tx.QueryRow("SELECT id, expires_at FROM accountant_invites WHERE code = $1 FOR UPDATE", req.InviteCode).Scan(&id, &expiresAt)
	if err != nil || time.Now().After(expiresAt) {
		http.Error(w, "Неверный или просроченный инвайт-код", http.StatusForbidden)
		return
	}

	// 2. Insert accounting firm
	var firmID int
	err = tx.QueryRow("INSERT INTO accounting_firms (name, max_companies) VALUES ($1, $2) RETURNING id", "Фирма "+req.Login, 10).Scan(&firmID)
	if err != nil {
		http.Error(w, "Ошибка создания фирмы", http.StatusInternalServerError)
		return
	}

	// 3. Insert user
	_, err = tx.Exec("INSERT INTO accounting_users (accounting_firm_id, login, password_hash, email, role) VALUES ($1, $2, $3, $4, $5)",
		firmID, req.Login, string(hash), req.Email, "admin")
	if err != nil {
		http.Error(w, "Ошибка создания пользователя (возможно логин уже занят)", http.StatusBadRequest)
		return
	}

	// 4. Delete invite code
	if _, err = tx.Exec("DELETE FROM accountant_invites WHERE id = $1", id); err != nil {
		http.Error(w, "Ошибка погашения инвайта", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Ошибка фиксации транзакции", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
	})
}
