package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"geminiplexer/internal/config"
	"geminiplexer/internal/server"
)

func TestModelsEndpoint(t *testing.T) {
	cfg := &config.Config{Port: 8787}
	s := server.NewServer(cfg, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)

	s.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "gemini-auto") {
		t.Errorf("expected gemini-auto in response, got %s", rec.Body.String())
	}
}

func TestChatCompletionsValidation(t *testing.T) {
	cfg := &config.Config{Port: 8787}
	s := server.NewServer(cfg, nil)
	rec := httptest.NewRecorder()
	// Empty body should return 400 Bad Request
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))

	s.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty messages, got %d", rec.Code)
	}
}
