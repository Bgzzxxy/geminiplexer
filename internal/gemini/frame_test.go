package gemini_test

import (
	"geminiplexer/internal/gemini"
	"testing"
)

func TestParseStreamFrameDirect(t *testing.T) {
	data := `[[["wrb.fr",null,"[null,\"cid\",null,null,[[\"rcid\",[\"Hello, world!\"]]]]"]]]`
	chunks := gemini.ParseStreamFrameForTest(data)
	if len(chunks) == 0 {
		t.Fatalf("expected at least 1 chunk")
	}
	if chunks[0].Text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", chunks[0].Text)
	}
}
