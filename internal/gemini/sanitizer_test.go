package gemini_test

import (
	"geminiplexer/internal/gemini"
	"testing"
)

func TestSanitizeTextStripsCitations(t *testing.T) {
	raw := "Here is the answer [cite: 1, 2] with code [cite_start] snippet?code_reference and output?code_stdout"
	expected := "Here is the answer with code snippet and output"
	got := gemini.SanitizeText(raw)
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestSanitizeTextCleansRepeatedCites(t *testing.T) {
	raw := "Fact one [cite: 4]. Fact two [cite: 5, 6, 7]."
	expected := "Fact one. Fact two."
	got := gemini.SanitizeText(raw)
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestSanitizerPreservesIndentationAndSpaces(t *testing.T) {
	code := "def hello():\n    print('hi') [cite: 1]"
	expected := "def hello():\n    print('hi')"
	got := gemini.SanitizeText(code)
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}

	chunk := "hello "
	gotChunk := gemini.SanitizeText(chunk)
	if gotChunk != "hello " {
		t.Errorf("expected trailing space preserved: %q, got: %q", "hello ", gotChunk)
	}
}
