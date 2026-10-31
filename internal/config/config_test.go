package config_test

import (
	"os"
	"testing"
	"gemini-to-api/internal/config"
)

func TestLoadConfig(t *testing.T) {
	yamlData := `
port: 8787
admin_key: "secret123"
accounts:
  - id: "acc1"
    cookies: "__Secure-1PSID=sid1; __Secure-1PSIDTS=ts1"
    auth_user: "0"
`
	tmp, err := os.CreateTemp("", "cfg-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(yamlData); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmp.Close()

	cfg, err := config.LoadConfig(tmp.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 8787 || len(cfg.Accounts) != 1 || cfg.Accounts[0].ID != "acc1" {
		t.Errorf("unexpected config content: %+v", cfg)
	}
}
