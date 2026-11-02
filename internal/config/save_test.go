package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateAccountCookiesPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.yaml")

	orig := "port: 4981\nhost: 0.0.0.0\nadmin_key: testkey123\naccounts:\n  - id: account_1\n    cookies: old_cookie_value\n    auth_user: ''\n  - id: account_2\n    cookies: old2\n    auth_user: '1'\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := UpdateAccountCookies(path, "account_1", "new_cookie_value_1PSID", ""); err != nil {
		t.Fatalf("UpdateAccountCookies existing: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(cfg.Accounts))
	}
	if cfg.Accounts[0].Cookies != "new_cookie_value_1PSID" {
		t.Fatalf("account_1 cookies not updated: %q", cfg.Accounts[0].Cookies)
	}
	if cfg.Accounts[0].AuthUser != "" {
		t.Fatalf("account_1 auth_user should stay empty, got %q", cfg.Accounts[0].AuthUser)
	}
	if cfg.Accounts[1].Cookies != "old2" {
		t.Fatalf("account_2 cookies should be untouched, got %q", cfg.Accounts[1].Cookies)
	}
	if cfg.AdminKey != "testkey123" {
		t.Fatalf("admin_key lost in round-trip: %q", cfg.AdminKey)
	}

	// Add a brand-new account
	if err := UpdateAccountCookies(path, "account_9", "fresh9", "8"); err != nil {
		t.Fatalf("UpdateAccountCookies new: %v", err)
	}
	cfg2, _ := LoadConfig(path)
	if len(cfg2.Accounts) != 3 {
		t.Fatalf("expected 3 accounts after add, got %d", len(cfg2.Accounts))
	}
	found := false
	for _, a := range cfg2.Accounts {
		if a.ID == "account_9" && a.Cookies == "fresh9" && a.AuthUser == "8" {
			found = true
		}
	}
	if !found {
		t.Fatalf("account_9 not persisted correctly: %+v", cfg2.Accounts)
	}
}