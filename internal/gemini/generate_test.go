package gemini_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"geminiplexer/internal/config"
	"geminiplexer/internal/gemini"
)

func TestStreamGenerateParsing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Emit mock SSE chunks containing json payload
		// Format: payload has [null, "cid", null, null, [["rcid", ["Hello, world!"]]]]
		chunk := `data: [[["wrb.fr",null,"[null,\"cid\",null,null,[[\"rcid\",[\"Hello, world!\"]]]]"]]]` + "\n\n"
		w.Write([]byte(chunk))
	}))
	defer ts.Close()

	sess := gemini.NewAccountSession(config.AccountConfig{ID: "acc1", Cookies: "__Secure-1PSID=sid; __Secure-1PSIDTS=ts"})
	sess.SetGenerateEndpointOverride(ts.URL)
	sess.SetToken("test_token")

	model := gemini.GeminiModel{ID: "gemini-auto", ModelID: "auto", ModelNumber: 1, Capacity: 1, CapacityField: 12}
	ch, err := sess.StreamGenerate(context.Background(), "Hello", model, 1)
	if err != nil {
		t.Fatalf("StreamGenerate failed: %v", err)
	}

	var collected []string
	for c := range ch {
		if c.Text != "" {
			collected = append(collected, c.Text)
		}
	}

	joined := strings.Join(collected, "")
	if !strings.Contains(joined, "Hello, world!") {
		t.Errorf("expected chunk to contain 'Hello, world!', got %q", joined)
	}
}

func TestStreamGenerateMultilineAssembly(t *testing.T) {
	// Chunk split across multiple lines
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Line 1: length prefix / partial array
		w.Write([]byte("data: [[[\"wrb.fr\",null,\n"))
		// Line 2: payload string closing
		w.Write([]byte("\"[null,\\\"cid\\\",null,null,[[\\\"rcid\\\",[\\\"Fragmented text\\\"]]]]\" ]]]\n\n"))
	}))
	defer ts.Close()

	sess := gemini.NewAccountSession(config.AccountConfig{ID: "acc1", Cookies: "__Secure-1PSID=sid; __Secure-1PSIDTS=ts"})
	sess.SetGenerateEndpointOverride(ts.URL)
	sess.SetToken("test_token")

	model := gemini.GeminiModel{ID: "gemini-auto", ModelID: "auto", ModelNumber: 1, Capacity: 1, CapacityField: 12}
	ch, err := sess.StreamGenerate(context.Background(), "Hello", model, 1)
	if err != nil {
		t.Fatalf("StreamGenerate failed: %v", err)
	}

	var collected []string
	for c := range ch {
		if c.Text != "" {
			collected = append(collected, c.Text)
		}
	}

	joined := strings.Join(collected, "")
	if !strings.Contains(joined, "Fragmented text") {
		t.Errorf("expected fragmented chunk to be assembled into 'Fragmented text', got %q", joined)
	}
}
