package server

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/lucasew/mise-ci/internal/core"
)

type AuthConfig struct {
	AdminUsername string
	AdminPassword string
}

type AuthMiddleware struct {
	core       *core.Core
	authConfig *AuthConfig
}

func NewAuthMiddleware(c *core.Core, config *AuthConfig) *AuthMiddleware {
	return &AuthMiddleware{
		core:       c,
		authConfig: config,
	}
}

// Helper: extract token from query parameter
func extractTokenFromQuery(r *http.Request) string {
	return r.URL.Query().Get("token")
}

// Helper: extract run ID from path
// Expected format: /ui/run/<runID> or /ui/logs/<runID>
func extractRunIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	// /ui/run/123 -> ["", "ui", "run", "123"]
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

// RequireRunToken protects endpoints that need a specific run token (e.g. /ui/run/<id>)
func (m *AuthMiddleware) RequireRunToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractTokenFromQuery(r)
		if token == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}

		runID, tokenType, err := m.core.ValidateToken(token)
		if err != nil {
			http.Error(w, "invalid token", http.StatusForbidden)
			return
		}

		if tokenType != core.TokenTypeUI {
			http.Error(w, "invalid token type", http.StatusForbidden)
			return
		}

		pathRunID := extractRunIDFromPath(r.URL.Path)
		if pathRunID == "" {
			// If we can't extract runID from path, we can't validate the token against the path
			// But maybe the handler doesn't need it or it's a different kind of route?
			// The requirement is "Verify token's run_id matches path's run_id"
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		if runID != pathRunID {
			http.Error(w, "token mismatch", http.StatusForbidden)
			return
		}

		next(w, r)
	}
}

// RequireBasicAuth protects admin endpoints (e.g. /ui/)
func (m *AuthMiddleware) RequireBasicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// If no credentials configured, allow access (graceful degradation)
		if m.authConfig.AdminUsername == "" || m.authConfig.AdminPassword == "" {
			next(w, r)
			return
		}

		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(m.authConfig.AdminUsername)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(m.authConfig.AdminPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// RequireStatusStreamAuth protects endpoints that can be accessed via token OR basic auth
// e.g. /ui/status-stream
func (m *AuthMiddleware) RequireStatusStreamAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. Try Token Auth
		token := extractTokenFromQuery(r)
		if token != "" {
			_, tokenType, err := m.core.ValidateToken(token)
			if err == nil && tokenType == core.TokenTypeUI {
				next(w, r)
				return
			}
			// If token is present but invalid, we could either fail or fall through to Basic Auth.
			// Let's fall through to Basic Auth, maybe they provided a bad token but have credentials.
		}

		// 2. Fallback to Basic Auth
		// If no credentials configured, allow access
		if m.authConfig.AdminUsername == "" || m.authConfig.AdminPassword == "" {
			next(w, r)
			return
		}

		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(m.authConfig.AdminUsername)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(m.authConfig.AdminPassword)) != 1 {
			// If they tried a token and it failed, we probably shouldn't prompt for Basic Auth if it was an API call?
			// But since this is for SSE stream consumed by browser, browser handles 401 with WWW-Authenticate.
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}
