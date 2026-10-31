package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"gemini-relay/internal/config"
	"gemini-relay/internal/server"
)

func TestAdminCookiesUnauthorized(t *testing.T) {
	cfg := &config.Config{AdminKey: "secret_admin"}
	s := server.NewServer(cfg, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/cookies", strings.NewReader(`{"id":"acc1","cookies":"foo"}`))

	s.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
	}
}

func TestAdminCookiesAuthorizedMissingBearer(t *testing.T) {
	cfg := &config.Config{AdminKey: "secret_admin"}
	s := server.NewServer(cfg, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/cookies", strings.NewReader(`{"id":"acc1","cookies":"foo"}`))
	req.Header.Set("Authorization", "Bearer wrong_key")

	s.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong bearer key, got %d", rec.Code)
	}
}
