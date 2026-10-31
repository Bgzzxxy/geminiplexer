package gemini_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"gemini-to-api/internal/config"
	"gemini-to-api/internal/gemini"
)

func TestSessionInitExtractsSNlM0e(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><script>"SNlM0e":"test_csrf_token_123","bl":"boq_assistant_20261031"</script></html>`))
	}))
	defer ts.Close()

	sess := gemini.NewAccountSession(config.AccountConfig{
		ID:      "acc1",
		Cookies: "__Secure-1PSID=test_psid; __Secure-1PSIDTS=test_ts",
	})
	sess.SetEndpointOverride(ts.URL)

	if err := sess.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if sess.GetToken() != "test_csrf_token_123" {
		t.Errorf("expected token test_csrf_token_123, got %s", sess.GetToken())
	}
}

func TestRotateCookiesUpdatesTS(t *testing.T) {
	rotateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:  "__Secure-1PSIDTS",
			Value: "new_rotated_ts_token",
		})
		w.WriteHeader(http.StatusOK)
	}))
	defer rotateServer.Close()

	sess := gemini.NewAccountSession(config.AccountConfig{
		ID:      "acc1",
		Cookies: "__Secure-1PSID=initial_psid; __Secure-1PSIDTS=initial_ts",
	})
	sess.SetRotateEndpointOverride(rotateServer.URL)

	if err := sess.RotateCookies(context.Background()); err != nil {
		t.Fatalf("RotateCookies failed: %v", err)
	}
	if sess.GetPSIDTS() != "new_rotated_ts_token" {
		t.Errorf("expected new_rotated_ts_token, got %s", sess.GetPSIDTS())
	}
}
