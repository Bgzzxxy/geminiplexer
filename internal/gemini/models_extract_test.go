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
		modelEntry("56fdd199312815e2", "Flash", "3.8 Flash", 20, 1),
		modelEntry("e6fa609c3fa255c0", "Pro", "3.1 Pro", 4, 3),
		modelEntry("8c46e95b1a07cecc", "Flash-Lite", "3.5 Flash-Lite", 1, 6),
		modelEntry("95a4c0abcba24364", "Argon", "4 Argon", 4, 8),
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

	// ModelNumber must come from md[17] (the field Gemini Web routes on),
	// not md[9]: argon=8, pro=3, flash=1, flash-lite=6.
	flash := byID["gemini-3.8-flash"]
	if flash.ModelID != "56fdd199312815e2" || flash.ModelNumber != 1 {
		t.Errorf("flash wrong: %+v", flash)
	}
	pro := byID["gemini-3.1-pro"]
	if pro.ModelID != "e6fa609c3fa255c0" || pro.ModelNumber != 3 {
		t.Errorf("pro wrong: %+v", pro)
	}
	argon := byID["gemini-4-argon"]
	if argon.ModelID != "95a4c0abcba24364" || argon.ModelNumber != 8 {
		t.Errorf("argon wrong: %+v", argon)
	}
	lite := byID["gemini-flash-lite"]
	if lite.ModelID != "8c46e95b1a07cecc" || lite.ModelNumber != 6 {
		t.Errorf("lite wrong: %+v", lite)
	}
}

// modelEntry builds one otAQ7b model record with both md[9] and md[17] set,
// mirroring the live registry shape.
func modelEntry(hex, category, display string, md9, md17 int) []any {
	md := make([]any, 18)
	md[0] = hex
	md[1] = category
	md[9] = float64(md9)
	md[11] = display
	md[17] = float64(md17)
	return md
}

// TestExtractModelsFromPayloadShort returns empty for a malformed payload.
func TestExtractModelsFromPayloadShort(t *testing.T) {
	if got := extractModelsFromPayload([]any{nil, nil}); len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}

func TestMapCategoryToFriendlyID(t *testing.T) {
	cases := map[string]string{
		"argon":      "gemini-4-argon",
		"pro":        "gemini-3.1-pro",
		"flash":      "gemini-3.8-flash",
		"Flash-Lite": "gemini-flash-lite",
		"bogus":      "",
		"":           "",
	}
	for in, want := range cases {
		if got := mapCategoryToFriendlyID(in); got != want {
			t.Errorf("mapCategoryToFriendlyID(%q) = %q, want %q", in, got, want)
		}
	}
}
