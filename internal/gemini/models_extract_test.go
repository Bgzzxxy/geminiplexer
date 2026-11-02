package gemini

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestExtractModelsFromPayloadReal uses the exact otAQ7b structure captured
// live from Gemini Web (index 15 is the model registry).
func TestExtractModelsFromPayloadReal(t *testing.T) {
	payload := make([]any, 16)
	payload[0] = float64(1)
	payload[1] = []any{false, nil, false}
	payload[2] = false
	payload[6] = true
	payload[12] = false
	payload[13] = false

	payload[15] = []any{
		[]any{"56fdd199312815e2", "Flash", "All-around help", []any{}, float64(2), nil, []any{}, false, "", float64(20), "Flash", "3.8 Flash"},
		[]any{"e6fa609c3fa255c0", "Pro", "Advanced reasoning", []any{}, float64(2), nil, []any{}, false, "", float64(4), "Pro", "3.1 Pro"},
		[]any{"8c46e95b1a07cecc", "Flash-Lite", "Fastest answers", []any{}, float64(2), nil, []any{}, false, "", float64(1), "Flash-Lite", "3.5 Flash-Lite"},
		[]any{"95a4c0abcba24364", "Argon", "Complex tasks", []any{}, float64(2), nil, []any{}, false, "", float64(4), "Argon", "4 Argon"},
	}

	inner, _ := json.Marshal(payload)
	if !strings.Contains(string(inner), "56fdd199312815e2") {
		t.Fatal("fixture broken")
	}

	models := extractModelsFromPayload(payload)
	if len(models) != 4 {
		t.Fatalf("expected 4 models, got %d: %+v", len(models), models)
	}

	byID := map[string]GeminiModel{}
	for _, m := range models {
		byID[m.ID] = m
	}

	flash := byID["gemini-3.8-flash"]
	if flash.ModelID != "56fdd199312815e2" || flash.ModelNumber != 20 {
		t.Errorf("flash wrong: %+v", flash)
	}
	pro := byID["gemini-3.1-pro"]
	if pro.ModelID != "e6fa609c3fa255c0" || pro.ModelNumber != 4 {
		t.Errorf("pro wrong: %+v", pro)
	}
	argon := byID["gemini-4-argon"]
	if argon.ModelID != "95a4c0abcba24364" || argon.ModelNumber != 4 {
		t.Errorf("argon wrong: %+v", argon)
	}
	lite := byID["gemini-flash-lite"]
	if lite.ModelID != "8c46e95b1a07cecc" || lite.ModelNumber != 1 {
		t.Errorf("lite wrong: %+v", lite)
	}
}

// TestExtractModelsFromPayloadShort returns empty for a malformed payload.
func TestExtractModelsFromPayloadShort(t *testing.T) {
	if got := extractModelsFromPayload([]any{nil, nil}); len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}

func TestMapCategoryToFriendlyID(t *testing.T) {
	cases := map[string]string{
		"argon":            "gemini-4-argon",
		"pro":              "gemini-3.1-pro",
		"flash":            "gemini-3.8-flash",
		"Flash-Lite":       "gemini-flash-lite",
		"bogus":            "",
		"":                 "",
	}
	for in, want := range cases {
		if got := mapCategoryToFriendlyID(in); got != want {
			t.Errorf("mapCategoryToFriendlyID(%q) = %q, want %q", in, got, want)
		}
	}
}