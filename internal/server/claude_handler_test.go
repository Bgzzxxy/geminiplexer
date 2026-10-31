package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"geminiplexer/internal/config"
	"geminiplexer/internal/gemini"
	"geminiplexer/internal/pool"
	"geminiplexer/internal/server"
)

func TestClaudeMessagesNonStreaming(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := `data: [[["wrb.fr",null,"[null,\"cid\",null,null,[[\"rcid\",[\"Hello from Claude compatible endpoint!\"]]]]"]]]` + "\n\n"
		w.Write([]byte(chunk))
	}))
	defer ts.Close()

	sess := gemini.NewAccountSession(config.AccountConfig{ID: "acc1"})
	sess.SetGenerateEndpointOverride(ts.URL)
	sess.SetToken("fake_token")

	p := pool.NewAccountPool([]*gemini.AccountSession{sess})
	cfg := &config.Config{Port: 8787}
	srv := server.NewServer(cfg, p)

	reqBody := `{
		"model": "gemini-4-argon",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Hello!"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	if resp["type"] != "message" || resp["role"] != "assistant" {
		t.Errorf("unexpected response structure: %+v", resp)
	}

	content, ok := resp["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty content array, got: %+v", resp["content"])
	}

	firstBlock := content[0].(map[string]any)
	if firstBlock["type"] != "text" || !strings.Contains(firstBlock["text"].(string), "Hello from Claude") {
		t.Errorf("unexpected content block: %+v", firstBlock)
	}
}

func TestClaudeMessagesStreaming(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := `data: [[["wrb.fr",null,"[null,\"cid\",null,null,[[\"rcid\",[\"Streaming Claude text\"]]]]"]]]` + "\n\n"
		w.Write([]byte(chunk))
	}))
	defer ts.Close()

	sess := gemini.NewAccountSession(config.AccountConfig{ID: "acc1"})
	sess.SetGenerateEndpointOverride(ts.URL)
	sess.SetToken("fake_token")

	p := pool.NewAccountPool([]*gemini.AccountSession{sess})
	cfg := &config.Config{Port: 8787}
	srv := server.NewServer(cfg, p)

	reqBody := `{
		"model": "gemini-4-argon",
		"messages": [{"role": "user", "content": "Hi"}],
		"stream": true
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: message_start") {
		t.Errorf("missing message_start event in body: %s", body)
	}
	if !strings.Contains(body, "event: content_block_delta") {
		t.Errorf("missing content_block_delta event in body: %s", body)
	}
	if !strings.Contains(body, "event: message_stop") {
		t.Errorf("missing message_stop event in body: %s", body)
	}
}
