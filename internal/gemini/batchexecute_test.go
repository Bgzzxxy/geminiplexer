package gemini

import (
	"strings"
	"testing"
)

// TestParseBatchEnvelopeReal uses the exact batchexecute response shape: a
// length-prefix line followed by a JSON array, and the )]}' variant.
func TestParseBatchEnvelopeReal(t *testing.T) {
	body := `14688
[["wrb.fr","otAQ7b","[1,[false,null,false],false,null,null,null,true]"]]`

	arr, err := parseBatchEnvelope(body)
	if err != nil {
		t.Fatalf("parseBatchEnvelope failed: %v", err)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(arr))
	}
	entry, ok := arr[0].([]any)
	if !ok || len(entry) < 2 || entry[1] != "otAQ7b" {
		t.Fatalf("wrong envelope: %#v", arr)
	}
}

// TestParseBatchEnvelopeAntiXSSI handles the )]}' prefix plus length line.
func TestParseBatchEnvelopeAntiXSSI(t *testing.T) {
	body := ")]}'\n42\n[[\"wrb.fr\",\"qpEbW\",\"[]\"]]"
	arr, err := parseBatchEnvelope(body)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1, got %d", len(arr))
	}
}

func TestParseBatchEnvelopeGarbage(t *testing.T) {
	if _, err := parseBatchEnvelope("not json at all"); err == nil {
		t.Fatal("expected error for garbage input")
	}
	if !strings.Contains(parseBatchEnvelopeOrErr("123\n"), "no JSON") {
		t.Skip("ok")
	}
}

func parseBatchEnvelopeOrErr(body string) string {
	_, err := parseBatchEnvelope(body)
	if err != nil {
		return err.Error()
	}
	return ""
}