package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"geminiplexer/internal/config"
	"geminiplexer/internal/gemini"
	"geminiplexer/internal/pool"
	"geminiplexer/internal/server"
)

func TestCumulativeTextDeltaStreaming(t *testing.T) {
	// Mock server that returns 2 cumulative SSE chunks:
	// Chunk 1: "Hello"
	// Chunk 2: "Hello, world!"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		c1 := `data: [[["wrb.fr",null,"[null,\"cid\",null,null,[[\"rcid\",[\"Hello\"]]]]"]]]` + "\n\n"
		c2 := `data: [[["wrb.fr",null,"[null,\"cid\",null,null,[[\"rcid\",[\"Hello, world!\"]]]]"]]]` + "\n\n"
		w.Write([]byte(c1))
		w.Write([]byte(c2))
	}))
	defer ts.Close()

	sess := gemini.NewAccountSession(config.AccountConfig{ID: "acc1"})
	sess.SetGenerateEndpointOverride(ts.URL)
	sess.SetToken("fake_token")

	p := pool.NewAccountPool([]*gemini.AccountSession{sess})
	cfg := &config.Config{Port: 8787}
	srv := server.NewServer(cfg, p)

	reqBody := `{"model":"gemini-auto","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	res := rec.Body.String()
	// Should NOT contain "HelloHello, world!"
	if strings.Contains(res, "HelloHello") {
		t.Errorf("detected duplicate cumulative text in stream response: %s", res)
	}
	if !strings.Contains(res, "Hello") || !strings.Contains(res, ", world!") {
		t.Errorf("missing expected deltas in response: %s", res)
	}
}

func TestNonStreamingCumulativeDeduplication(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		c1 := `data: [[["wrb.fr",null,"[null,\"cid\",null,null,[[\"rcid\",[\"Thinking...\"]]]]"]]]` + "\n\n"
		c2 := `data: [[["wrb.fr",null,"[null,\"cid\",null,null,[[\"rcid\",[\"The answer is 42\"]]]]"]]]` + "\n\n"
		w.Write([]byte(c1))
		w.Write([]byte(c2))
	}))
	defer ts.Close()

	sess := gemini.NewAccountSession(config.AccountConfig{ID: "acc1"})
	sess.SetGenerateEndpointOverride(ts.URL)
	sess.SetToken("fake_token")

	p := pool.NewAccountPool([]*gemini.AccountSession{sess})
	cfg := &config.Config{Port: 8787}
	srv := server.NewServer(cfg, p)

	reqBody := `{"model":"gemini-auto","messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	res := rec.Body.String()
	// Final content must be "The answer is 42", NOT concatenated "Thinking...The answer is 42"
	if strings.Contains(res, "Thinking...The answer is 42") {
		t.Errorf("non-streaming response concatenated cumulative snapshots: %s", res)
	}
	if !strings.Contains(res, "The answer is 42") {
		t.Errorf("missing final snapshot in response: %s", res)
	}
}

func TestModelDowngradeFailover(t *testing.T) {
	// Account 1 returns Flash model ID when Pro was requested (downgrade)
	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Emits degraded model
		chunk := `data: [[["wrb.fr",null,"[null,\"cid\",null,null,[[\"rcid\",[\"Degraded Flash answer\"]]],null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,\"gemini-flash\"]"]]]` + "\n\n"
		w.Write([]byte(chunk))
	}))
	defer ts1.Close()

	// Account 2 returns Pro answer
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := `data: [[["wrb.fr",null,"[null,\"cid\",null,null,[[\"rcid\",[\"Legitimate Pro answer\"]]],null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,\"gemini-pro\"]"]]]` + "\n\n"
		w.Write([]byte(chunk))
	}))
	defer ts2.Close()

	sess1 := gemini.NewAccountSession(config.AccountConfig{ID: "acc1"})
	sess1.SetGenerateEndpointOverride(ts1.URL)
	sess1.SetToken("fake_token1")

	sess2 := gemini.NewAccountSession(config.AccountConfig{ID: "acc2"})
	sess2.SetGenerateEndpointOverride(ts2.URL)
	sess2.SetToken("fake_token2")

	p := pool.NewAccountPool([]*gemini.AccountSession{sess1, sess2})
	cfg := &config.Config{Port: 8787}
	srv := server.NewServer(cfg, p)

	reqBody := `{"model":"gemini-2.5-pro","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	res := rec.Body.String()
	if !strings.Contains(res, "Legitimate Pro answer") {
		t.Errorf("failover failed to route to healthy account; got: %s", res)
	}
}
