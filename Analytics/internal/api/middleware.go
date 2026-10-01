package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"analytics_service/internal/auth"
)

// RequireSuperAdmin middleware restricts access strictly to authenticated super-admins
func (s *Server) RequireSuperAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := auth.ValidateSession(s.db, r)
		if err != nil || user == nil {
			// If it's a browser request for an HTML page, redirect or show login
			accept := r.Header.Get("Accept")
			if strings.Contains(accept, "text/html") && !strings.HasPrefix(r.URL.Path, "/api/") {
				http.Redirect(w, r, "/analytics/login", http.StatusSeeOther)
				return
			}

			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "Unauthorized: valid super-admin session required",
				"code":    401,
				"success": false,
			})
			return
		}

		if !user.IsSuperuser {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "Forbidden: super-admin privileges required",
				"code":    403,
				"success": false,
			})
			return
		}

		// Inject user into context
		ctx := context.WithValue(r.Context(), auth.UserContextKey, user)
		next(w, r.WithContext(ctx))
	}
}
