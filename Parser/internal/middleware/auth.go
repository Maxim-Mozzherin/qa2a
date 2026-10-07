package middleware

import (
	"context"
	"net/http"
	"strings"

	"leads_monster/internal/db"
)

type contextKey string

const UserContextKey contextKey = "leads_user"

type SessionUser struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Role  string `json:"role"`
}

func ContextWithUser(ctx context.Context, u *SessionUser) context.Context {
	return context.WithValue(ctx, UserContextKey, u)
}

func GetUserFromContext(ctx context.Context) *SessionUser {
	if ctx == nil {
		return nil
	}
	if u, ok := ctx.Value(UserContextKey).(*SessionUser); ok {
		return u
	}
	return nil
}

// SuperAdminGatekeeper verifies session and checks role == 'superadmin'
func SuperAdminGatekeeper(database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			prefix := strings.TrimSuffix(r.Header.Get("X-Forwarded-Prefix"), "/")
			loginURL := "/login"
			homeURL := "/"
			if prefix != "" {
				loginURL = prefix + "/login"
				homeURL = prefix + "/"
			}

			// Allow unauthenticated access to login API and static assets needed for login
			if path == "/api/auth/login" || path == "/login" || strings.HasPrefix(path, "/css/") || strings.HasPrefix(path, "/js/") || path == "/favicon.ico" {
				// If user is already authenticated and visits /login, redirect to /
				if path == "/login" {
					if user := validateRequest(r, database); user != nil {
						http.Redirect(w, r, homeURL, http.StatusSeeOther)
						return
					}
				}
				next.ServeHTTP(w, r)
				return
			}

			user := validateRequest(r, database)
			if user == nil || user.Role != "superadmin" {
				if strings.HasPrefix(path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":"Unauthorized: Superadmin session required"}`))
					return
				}
				// For browser HTML requests, redirect to /login
				http.Redirect(w, r, loginURL, http.StatusSeeOther)
				return
			}

			ctx := ContextWithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func validateRequest(r *http.Request, database *db.DB) *SessionUser {
	var token string

	// 1. Check HTTP-only cookie qa2a_leads_session
	if cookie, err := r.Cookie("qa2a_leads_session"); err == nil && cookie.Value != "" {
		token = cookie.Value
	}

	// 2. Check analytics_session cookie (from QA2A Analytics)
	if token == "" {
		if cookie, err := r.Cookie("analytics_session"); err == nil && strings.TrimSpace(cookie.Value) != "" {
			token = strings.TrimSpace(cookie.Value)
		}
	}

	// 3. Check access_token cookie
	if token == "" {
		if cookie, err := r.Cookie("access_token"); err == nil && strings.TrimSpace(cookie.Value) != "" {
			token = strings.TrimSpace(cookie.Value)
		}
	}

	// 4. Check Authorization Bearer header as fallback
	if token == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		}
	}

	if token == "" {
		return nil
	}

	// First check leads_sessions
	var user SessionUser
	query := `
		SELECT user_id, login, role 
		FROM leads_sessions 
		WHERE token = $1 AND expires_at > NOW() AND role = 'superadmin'
	`
	err := database.QueryRow(query, token).Scan(&user.ID, &user.Login, &user.Role)
	if err == nil {
		return &user
	}

	// Fallback check: check accounting_users by access_token
	queryAccounting := `
		SELECT id, login, role
		FROM accounting_users
		WHERE access_token = $1 AND role = 'superadmin' AND is_active = true
	`
	err = database.QueryRow(queryAccounting, token).Scan(&user.ID, &user.Login, &user.Role)
	if err == nil {
		return &user
	}

	return nil
}
