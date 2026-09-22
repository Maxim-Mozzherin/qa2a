
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

type AuthUser struct {
	ID               int
	AccountingFirmID *int
	Login            string
	Role             string
}

type contextKey string

const authUserKey contextKey = "authUser"

// ContextWithAuthUser attaches AuthUser to the request context
func ContextWithAuthUser(ctx context.Context, u *AuthUser) context.Context {
	return context.WithValue(ctx, authUserKey, u)
}

// GetAuthUser extracts the authenticated user from the request context
func GetAuthUser(r *http.Request) *AuthUser {
	if r == nil {
		return nil
	}
	if u, ok := r.Context().Value(authUserKey).(*AuthUser); ok {
		return u
	}
	return nil
}

func getAuthUserByToken(token string) (*AuthUser, error) {
	token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	if token == "" {
		return nil, fmt.Errorf("empty token")
	}
	var u AuthUser
	query := `SELECT id, accounting_firm_id, login, role FROM accounting_users WHERE access_token = $1 AND is_active = true`
	err := db.QueryRow(query, token).Scan(&u.ID, &u.AccountingFirmID, &u.Login, &u.Role)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		user, err := getAuthUserByToken(token)
		if err != nil || user == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := ContextWithAuthUser(r.Context(), user)
		next(w, r.WithContext(ctx))
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

	var userID int
	var hash string
	var dbRole string
	err := db.QueryRow("SELECT id, password_hash, role FROM accounting_users WHERE login = $1 AND is_active = true", req.Login).Scan(&userID, &hash, &dbRole)
	if err == nil {
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err == nil {
			tokenBytes := make([]byte, 32)
			if _, err := rand.Read(tokenBytes); err != nil {
				http.Error(w, "Ошибка генерации токена", http.StatusInternalServerError)
				return
			}
			token := hex.EncodeToString(tokenBytes)
			_, err = db.Exec("UPDATE accounting_users SET access_token = $1 WHERE id = $2", token, userID)
			if err != nil {
				http.Error(w, "Ошибка сохранения токена", http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "success",
				"token":  token,
				"role":   dbRole,
			})
			return
		}
	}

	http.Error(w, "Неверный логин или пароль", http.StatusForbidden)
}

func checkAccountantAccessUser(user *AuthUser, companyID int) bool {
	if user == nil || companyID <= 0 {
		return false
	}
	// Superadmin and global_accountant have unrestricted godmode access
	if user.Role == "superadmin" || user.Role == "global_accountant" {
		return true
	}
	if user.AccountingFirmID == nil {
		return false
	}
	var hasAccess bool
	query := `SELECT EXISTS(SELECT 1 FROM companies WHERE id = $1 AND accounting_firm_id = $2)`
	err := db.QueryRow(query, companyID, *user.AccountingFirmID).Scan(&hasAccess)
	return err == nil && hasAccess
}

func checkAccountantAccess(token string, companyID int) bool {
	if companyID <= 0 || strings.TrimSpace(token) == "" {
		return false
	}
	user, err := getAuthUserByToken(token)
	if err != nil {
		return false
	}
	return checkAccountantAccessUser(user, companyID)
}

