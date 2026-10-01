package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const UserContextKey contextKey = "analyticsAuthUser"

// User represents an authenticated identity with RBAC flags
type User struct {
	ID          int    `json:"id"`
	Email       string `json:"email"`
	Login       string `json:"login"`
	Role        string `json:"role"`
	IsSuperuser bool   `json:"is_superuser"`
	IsActive    bool   `json:"is_active"`
}

// AuthenticateUser validates credentials against users / accounting_users using bcrypt
func AuthenticateUser(db *sql.DB, emailOrLogin, password string) (*User, error) {
	emailOrLogin = strings.TrimSpace(emailOrLogin)
	if emailOrLogin == "" || password == "" {
		return nil, errors.New("empty credentials provided")
	}

	// 1. Try querying `users` table if schema matches specification
	var u User
	var hash string
	var err error

	queryUsers := `SELECT id, email, password_hash, is_superuser, is_active FROM users WHERE email = $1 AND is_active = true`
	err = db.QueryRow(queryUsers, emailOrLogin).Scan(&u.ID, &u.Email, &hash, &u.IsSuperuser, &u.IsActive)
	if err == nil {
		if bcryptErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); bcryptErr == nil {
			return &u, nil
		}
		return nil, errors.New("invalid password")
	}

	// 2. Query `accounting_users` table from core parser service
	var role string
	queryAccounting := `
		SELECT id, COALESCE(email, ''), COALESCE(login, ''), password_hash, (role = 'superadmin'), is_active, role
		FROM accounting_users 
		WHERE (LOWER(email) = LOWER($1) OR LOWER(login) = LOWER($1)) AND is_active = true`
	err = db.QueryRow(queryAccounting, emailOrLogin).Scan(&u.ID, &u.Email, &u.Login, &hash, &u.IsSuperuser, &u.IsActive, &role)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("user not found or inactive")
		}
		return nil, err
	}

	if bcryptErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); bcryptErr != nil {
		return nil, errors.New("invalid password")
	}

	u.Role = role
	// Ensure superadmin flag is set for superadmin role
	if role == "superadmin" {
		u.IsSuperuser = true
	}

	return &u, nil
}

// CreateSession generates a secure token, saves it, and sets an HTTP-only session cookie
func CreateSession(db *sql.DB, w http.ResponseWriter, user *User) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tokenBytes)

	// Persist token in accounting_users
	_, err := db.Exec(`UPDATE accounting_users SET access_token = $1 WHERE id = $2`, token, user.ID)
	if err != nil {
		// Also try updating users table if possible
		_, _ = db.Exec(`UPDATE users SET access_token = $1 WHERE id = $2`, token, user.ID)
	}

	// Set secure HTTP-only cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "analytics_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 30, // 30 days
	})

	// Also set access_token cookie for cross-service compatibility
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 30,
	})

	return token, nil
}

// ValidateSession verifies the incoming request session token and checks super-admin privileges
func ValidateSession(db *sql.DB, r *http.Request) (*User, error) {
	token := extractToken(r)
	if token == "" {
		return nil, errors.New("unauthorized: missing session token")
	}

	var u User
	var role string

	// 1. Check accounting_users by access_token
	query := `
		SELECT id, COALESCE(email, ''), COALESCE(login, ''), (role = 'superadmin'), is_active, role
		FROM accounting_users
		WHERE access_token = $1 AND is_active = true`
	err := db.QueryRow(query, token).Scan(&u.ID, &u.Email, &u.Login, &u.IsSuperuser, &u.IsActive, &role)
	if err == nil {
		u.Role = role
		if role == "superadmin" {
			u.IsSuperuser = true
		}
		return &u, nil
	}

	// 2. Check users table by access_token (if column exists)
	queryUsers := `SELECT id, email, is_superuser, is_active FROM users WHERE access_token = $1 AND is_active = true`
	err = db.QueryRow(queryUsers, token).Scan(&u.ID, &u.Email, &u.IsSuperuser, &u.IsActive)
	if err == nil {
		return &u, nil
	}

	return nil, errors.New("unauthorized: invalid or expired session")
}

// ClearSession clears the session cookies
func ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "analytics_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// GetUserFromContext retrieves authenticated User from context
func GetUserFromContext(ctx context.Context) *User {
	if ctx == nil {
		return nil
	}
	if u, ok := ctx.Value(UserContextKey).(*User); ok {
		return u
	}
	return nil
}

func extractToken(r *http.Request) string {
	// 1. Authorization: Bearer <token>
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}

	// 2. Cookie analytics_session
	if cookie, err := r.Cookie("analytics_session"); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return strings.TrimSpace(cookie.Value)
	}

	// 3. Cookie access_token (from parser web app)
	if cookie, err := r.Cookie("access_token"); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return strings.TrimSpace(cookie.Value)
	}

	return ""
}
