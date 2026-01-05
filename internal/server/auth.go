package server

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/lucasew/mise-ci/internal/config"
	"github.com/lucasew/mise-ci/internal/core"
	"golang.org/x/crypto/bcrypt"
)

type AuthMiddleware struct {
	core              *core.Core
	authConfig        *config.AuthConfig
	adminPasswordHash []byte
}

func NewAuthMiddleware(c *core.Core, config *config.AuthConfig) *AuthMiddleware {
	var passHash []byte
	if config.AdminPassword != "" {
		cost := config.BcryptCost
		if cost == 0 {
			cost = bcrypt.DefaultCost
		}
		// Panics on invalid cost, which is a programming error.
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(config.AdminPassword), cost)
		if err == nil {
			passHash = hashedPassword
		}
	}
	return &AuthMiddleware{
		core:              c,
		authConfig:        config,
		adminPasswordHash: passHash,
	}
}

// checkBasicAuth handles the logic for validating a user's basic authentication credentials.
// It returns true if the user is authenticated, false otherwise.
// If authentication fails, it writes the appropriate error response and headers.
func (m *AuthMiddleware) checkBasicAuth(w http.ResponseWriter, r *http.Request) bool {
	if m.authConfig.AdminUsername == "" || m.authConfig.AdminPassword == "" || len(m.adminPasswordHash) == 0 {
		w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
		http.Error(w, "Authentication required but not configured", http.StatusUnauthorized)
		return false
	}

	user, pass, ok := r.BasicAuth()

	// Constant-time comparison for username
	userMatch := subtle.ConstantTimeCompare([]byte(user), []byte(m.authConfig.AdminUsername))

	// bcrypt.CompareHashAndPassword is inherently resistant to timing attacks
	err := bcrypt.CompareHashAndPassword(m.adminPasswordHash, []byte(pass))
	passMatch := (err == nil)

	if !ok || userMatch != 1 || !passMatch {
		w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}

	return true
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
		if !m.checkBasicAuth(w, r) {
			return // checkBasicAuth handles the error response
		}

		next(w, r)
	}
}

// RequireBasicAuth protects admin endpoints (e.g. /ui/)
func (m *AuthMiddleware) RequireBasicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.checkBasicAuth(w, r) {
			return // checkBasicAuth handles the error response
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
		if !m.checkBasicAuth(w, r) {
			return // checkBasicAuth handles the error response
		}
		next(w, r)
	}
}
