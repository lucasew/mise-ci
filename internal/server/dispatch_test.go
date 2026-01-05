package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lucasew/mise-ci/internal/config"
	"github.com/lucasew/mise-ci/internal/core"
)

func TestDispatchAuth(t *testing.T) {
	// Setup minimal auth middleware
	authConfig := &config.AuthConfig{
		AdminUsername: "admin",
		AdminPassword: "password",
		BcryptCost:    4, // Use minimum cost for tests
	}
	// We pass nil core since we don't need token validation for Basic Auth
	mw := NewAuthMiddleware(&core.Core{}, authConfig)

	// Create a dummy handler
	handler := mw.RequireBasicAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Setup server
	mux := http.NewServeMux()
	mux.HandleFunc("/ui/test/dispatch", handler)
	server := httptest.NewServer(mux)
	defer server.Close()

	client := server.Client()

	// Test case 1: No credentials
	req, _ := http.NewRequest("POST", server.URL+"/ui/test/dispatch", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Test case 2: With credentials
	req, _ = http.NewRequest("POST", server.URL+"/ui/test/dispatch", nil)
	req.SetBasicAuth("admin", "password")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
