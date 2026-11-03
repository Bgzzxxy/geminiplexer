package gemini

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geminiplexer/internal/config"
)

// TestPersistCookiesWritesToDisk verifies that after UpdateCookies (with a
// config path set), the on-disk accounts.yaml carries the new cookies.
func TestPersistCookiesWritesToDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.yaml")

	orig := "port: 4981\naccounts:\n  - id: acc1\n    cookies: __Secure-1PSID=OLD; SIDCC=old\n    auth_user: ''\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewAccountSession(config.AccountConfig{ID: "acc1", Cookies: "__Secure-1PSID=OLD; SIDCC=old"})
	s.SetConfigPath(path)

	newCookies := "__Secure-1PSID=NEW; __Secure-1PSIDTS=ts_new; SIDCC=new; SID=s; HSID=h; SSID=ss; APISID=a; SAPISID=sap; NID=n"
	s.UpdateCookies(newCookies)

	// Reload from disk and verify persistence.
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(cfg.Accounts))
	}
	got := cfg.Accounts[0].Cookies
	for _, want := range []string{"__Secure-1PSID=NEW", "__Secure-1PSIDTS=ts_new", "SIDCC=new", "SID=s", "SAPISID=sap", "NID=n"} {
		if !strings.Contains(got, want) {
			t.Errorf("persisted header missing %q; got %q", want, got)
		}
	}
	// Must NOT still be the old OLD 1PSID.
	if strings.Contains(got, "__Secure-1PSID=OLD") && !strings.Contains(got, "__Secure-1PSID=NEW") {
		t.Errorf("persisted header still has stale 1PSID: %q", got)
	}
}

// TestPersistCookiesNoConfigPathIsNoOp verifies no crash/effect without a path.
func TestPersistCookiesNoConfigPathIsNoOp(t *testing.T) {
	s := NewAccountSession(config.AccountConfig{ID: "acc1", Cookies: "__Secure-1PSID=x"})
	s.PersistCookies() // must not panic
}