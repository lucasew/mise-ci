package server

import (
	"crypto/sha256"
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
	core              *core.Core
	authConfig        *AuthConfig
	adminPasswordHash []byte
}

func NewAuthMiddleware(c *core.Core, config *AuthConfig) *AuthMiddleware {
	var passHash []byte
	if config.AdminPassword != "" {
		hash := sha256.Sum256([]byte(config.AdminPassword))
		passHash = hash[:]
	}
	return &AuthMiddleware{
		core:              c,
		authConfig:        config,
		adminPasswordHash: passHash,
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
		id := parts[3]
		// Handle .log extension for raw logs
		id = strings.TrimSuffix(id, ".log")
		return id
	}
	return ""
}

// RequireRunToken protects endpoints that need a specific run token (e.g. /ui/run/<id>)
func (m *AuthMiddleware) RequireRunToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractTokenFromQuery(r)
		if token != "" {
			runID, tokenType, err := m.core.ValidateToken(token)
			if err == nil && tokenType == core.TokenTypeUI {
				pathRunID := extractRunIDFromPath(r.URL.Path)
				if pathRunID != "" && runID == pathRunID {
					next(w, r)
					return
				}
			}
			// If invalid token or mismatch, fall through to Basic Auth
		}

		// Fallback to Basic Auth for admins
		if m.authConfig.AdminUsername == "" || m.authConfig.AdminPassword == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Authentication required but not configured", http.StatusUnauthorized)
			return
		}

		user, pass, ok := r.BasicAuth()
		passHash := sha256.Sum256([]byte(pass))
		userMatch := subtle.ConstantTimeCompare([]byte(user), []byte(m.authConfig.AdminUsername))
		passMatch := subtle.ConstantTimeCompare(passHash[:], m.adminPasswordHash)
		// Use a bitwise AND to prevent timing attacks from short-circuiting
		if !ok || (userMatch&passMatch) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// RequireBasicAuth protects admin endpoints (e.g. /ui/)
func (m *AuthMiddleware) RequireBasicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// If no credentials configured, deny access (always secure by default)
		if m.authConfig.AdminUsername == "" || m.authConfig.AdminPassword == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Authentication required but not configured", http.StatusUnauthorized)
			return
		}

		user, pass, ok := r.BasicAuth()
		passHash := sha256.Sum256([]byte(pass))
		userMatch := subtle.ConstantTimeCompare([]byte(user), []byte(m.authConfig.AdminUsername))
		passMatch := subtle.ConstantTimeCompare(passHash[:], m.adminPasswordHash)
		// Use a bitwise AND to prevent timing attacks from short-circuiting
		if !ok || (userMatch&passMatch) != 1 {
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
		// If no credentials configured, allow access (graceful degradation)
		// but status stream requires auth, so we deny access if no credentials
		if m.authConfig.AdminUsername == "" || m.authConfig.AdminPassword == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Authentication required but not configured", http.StatusUnauthorized)
			return
		}

		user, pass, ok := r.BasicAuth()
		passHash := sha256.Sum256([]byte(pass))
		userMatch := subtle.ConstantTimeCompare([]byte(user), []byte(m.authConfig.AdminUsername))
		passMatch := subtle.ConstantTimeCompare(passHash[:], m.adminPasswordHash)
		// Use a bitwise AND to prevent timing attacks from short-circuiting
		if !ok || (userMatch&passMatch) != 1 {
			// If they tried a token and it failed, we probably shouldn't prompt for Basic Auth if it was an API call?
			// But since this is for SSE stream consumed by browser, browser handles 401 with WWW-Authenticate.
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}
