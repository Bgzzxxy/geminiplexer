package gemini

import (
	"encoding/json"
	"testing"
)

// Gemini keeps the response body open after the terminal frame, so IsDone must
// be derived from candidate[8][0]==2 rather than from EOF. Without this the
// reader loop hangs until the client disconnects.
func TestExtractChunksTerminalFrameSetsIsDone(t *testing.T) {
	candidate := make([]any, 9)
	candidate[0] = "rcid"
	candidate[1] = []any{"Hello"}
	candidate[8] = []any{float64(2)}

	payload := make([]any, 5)
	payload[1] = []any{"cid", "rid"}
	payload[4] = []any{candidate}

	raw, _ := json.Marshal(payload)
	envelope := []any{[]any{"wrb.fr", "rpc", string(raw)}}

	chunks := extractChunksFromEnvelope(envelope)
	if len(chunks) == 0 {
		t.Fatal("terminal frame was dropped; caller would block until EOF")
	}
	if !chunks[0].IsDone {
		t.Fatalf("expected IsDone=true for candidate[8][0]==2, got %+v", chunks[0])
	}
}

// A non-terminal frame must not be marked done.
func TestExtractChunksNonTerminalFrameNotDone(t *testing.T) {
	candidate := make([]any, 9)
	candidate[0] = "rcid"
	candidate[1] = []any{"Hel"}
	candidate[8] = []any{float64(1)}

	payload := make([]any, 5)
	payload[1] = []any{"cid", "rid"}
	payload[4] = []any{candidate}

	raw, _ := json.Marshal(payload)
	envelope := []any{[]any{"wrb.fr", "rpc", string(raw)}}

	chunks := extractChunksFromEnvelope(envelope)
	if len(chunks) == 0 {
		t.Fatal("expected a chunk")
	}
	if chunks[0].IsDone {
		t.Fatalf("non-terminal frame wrongly marked done: %+v", chunks[0])
	}
}
