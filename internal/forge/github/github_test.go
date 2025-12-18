package github

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestParseWebhook_GitLink(t *testing.T) {
	secret := "test-secret"
	g := NewGitHubForge(1, []byte("key"), secret)

	t.Run("PushEvent", func(t *testing.T) {
		payload := map[string]interface{}{
			"ref":   "refs/heads/main",
			"after": "1234567890abcdef",
			"repository": map[string]interface{}{
				"full_name": "owner/repo",
				"clone_url": "https://github.com/owner/repo.git",
				"html_url":  "https://github.com/owner/repo",
			},
			"head_commit": map[string]interface{}{
				"url": "https://github.com/owner/repo/commit/1234567890abcdef",
			},
		}

		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-Hub-Signature-256", generateSignature(secret, body))

		event, err := g.ParseWebhook(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if event.Link != "https://github.com/owner/repo/commit/1234567890abcdef" {
			t.Errorf("expected link 'https://github.com/owner/repo/commit/1234567890abcdef', got '%s'", event.Link)
		}
	})

	t.Run("PullRequestEvent", func(t *testing.T) {
		payload := map[string]interface{}{
			"action": "opened",
			"number": 123,
			"repository": map[string]interface{}{
				"full_name": "owner/repo",
				"clone_url": "https://github.com/owner/repo.git",
			},
			"pull_request": map[string]interface{}{
				"html_url": "https://github.com/owner/repo/pull/123",
				"head": map[string]interface{}{
					"sha": "1234567890abcdef",
				},
			},
		}

		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "pull_request")
		req.Header.Set("X-Hub-Signature-256", generateSignature(secret, body))

		event, err := g.ParseWebhook(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if event.Link != "https://github.com/owner/repo/pull/123" {
			t.Errorf("expected link 'https://github.com/owner/repo/pull/123', got '%s'", event.Link)
		}
	})
}

func generateSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return fmt.Sprintf("sha256=%s", hex.EncodeToString(mac.Sum(nil)))
}
