package gemini_test

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"geminiplexer/internal/config"
	"geminiplexer/internal/gemini"
)

// TestSAPISIDHash validates the Authorization header formula:
// SAPISIDHASH <ts>_<hex(sha1("<ts> <SAPISID> <origin>"))>.
func TestSAPISIDHash(t *testing.T) {
	sess := gemini.NewAccountSession(config.AccountConfig{
		ID:      "acc",
		Cookies: "SAPISID=sap_test_value; __Secure-1PSID=x",
	})

	auth := sess.SAPISIDHash("https://gemini.google.com")
	if !strings.HasPrefix(auth, "SAPISIDHASH ") {
		t.Fatalf("missing prefix: %q", auth)
	}

	parts := strings.SplitN(strings.TrimPrefix(auth, "SAPISIDHASH "), "_", 2)
	if len(parts) != 2 {
		t.Fatalf("malformed value: %q", auth)
	}
	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || ts <= 0 {
		t.Fatalf("bad timestamp: %q", parts[0])
	}

	expected := sha1.Sum([]byte(fmt.Sprintf("%d sap_test_value https://gemini.google.com", ts)))
	if parts[1] != hex.EncodeToString(expected[:]) {
		t.Fatalf("hash mismatch: got %s, want %s", parts[1], hex.EncodeToString(expected[:]))
	}
}

// TestSAPISIDHashEmpty returns "" when no SAPISID cookie is present.
func TestSAPISIDHashEmpty(t *testing.T) {
	sess := gemini.NewAccountSession(config.AccountConfig{
		ID:      "acc",
		Cookies: "__Secure-1PSID=x", // no SAPISID
	})
	if auth := sess.SAPISIDHash("https://gemini.google.com"); auth != "" {
		t.Fatalf("expected empty hash without SAPISID, got %q", auth)
	}
}
