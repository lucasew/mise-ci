package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	content := `
server:
  http_addr: ":8080"
jwt:
  secret: "test"
github:
  app_id: 1
nomad:
  job_name: "test"
`
	tmp, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Remove(tmp.Name())
	}()
	if _, err := tmp.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(tmp.Name())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Server.HTTPAddr != ":8080" {
		t.Errorf("expected :8080, got %s", cfg.Server.HTTPAddr)
	}
	if cfg.JWT.Secret != "test" {
		t.Errorf("expected test, got %s", cfg.JWT.Secret)
	}
	if cfg.GitHub.AppID != 1 {
		t.Errorf("expected 1, got %d", cfg.GitHub.AppID)
	}
}

func TestLoad_PartialAuthConfig(t *testing.T) {
	testCases := []struct {
		name    string
		content string
	}{
		{
			name: "username without password",
			content: `
jwt:
  secret: "test"
auth:
  admin_username: "admin"
`,
		},
		{
			name: "password without username",
			content: `
jwt:
  secret: "test"
auth:
  admin_password: "password"
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmp, err := os.CreateTemp("", "config-*.yaml")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = os.Remove(tmp.Name())
			}()
			if _, err := tmp.Write([]byte(tc.content)); err != nil {
				t.Fatal(err)
			}
			if err := tmp.Close(); err != nil {
				t.Fatal(err)
			}

			_, err = Load(tmp.Name())
			if err == nil {
				t.Error("expected an error for partial auth config, but got nil")
			}
		})
	}
}
