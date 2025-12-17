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
	tmp, err := os.CreateTemp("", "config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
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
