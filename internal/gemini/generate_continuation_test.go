package gemini

import (
	"encoding/json"
	"testing"
)

// TestBuildInnerPayloadFreshConversation validates the non-temporary payload
// contract: inner[45] is UNSET (nil) so Gemini returns continuation metadata,
// and inner[2]/inner[71] are nil when no continuation is supplied.
func TestBuildInnerPayloadFreshConversation(t *testing.T) {
	model := GeminiModel{ModelID: "56fdd199312815e2", ModelNumber: 20, Capacity: 2, CapacityField: 12}
	raw := buildInnerPayload("hi", model, 1, ContinuationMeta{}, "ABC")

	var inner []any
	if err := json.Unmarshal([]byte(raw), &inner); err != nil {
		t.Fatalf("buildInnerPayload produced unparseable JSON: %v", err)
	}
	if len(inner) != 81 {
		t.Fatalf("expected 81 elements, got %d", len(inner))
	}

	// inner[0] = [text, 0, nil, filesSlot, nil, nil, 0] (7 elements).
	p0, ok := inner[0].([]any)
	if !ok || len(p0) != 7 {
		t.Fatalf("inner[0] must be 7-element prompt array, got %#v", inner[0])
	}
	if p0[0] != "hi" {
		t.Fatalf("inner[0][0] wrong: %#v", p0[0])
	}

	// inner[2] must be nil when no continuation meta.
	if inner[2] != nil {
		t.Fatalf("inner[2] must be nil for fresh conversation, got %#v", inner[2])
	}

	// inner[45] must NOT be forced to 1 (the stateless-chat bug).
	if inner[45] != nil {
		t.Fatalf("inner[45] must be nil (non-temporary conversation), got %#v", inner[45])
	}

	// inner[71] must be nil when no continuation.
	if inner[71] != nil {
		t.Fatalf("inner[71] must be nil for fresh conversation, got %#v", inner[71])
	}

	// inner[79] = model number, inner[80] = thinking level.
	if float64N(inner[79]) != 20 {
		t.Fatalf("inner[79] expected 20, got %#v", inner[79])
	}
	if float64N(inner[80]) != 1 {
		t.Fatalf("inner[80] expected 1, got %#v", inner[80])
	}
}

// TestBuildInnerPayloadContinuation validates that continuation metadata lands
// in inner[2] (10-element) and inner[71] (8-element) exactly as gsb does.
func TestBuildInnerPayloadContinuation(t *testing.T) {
	meta := ContinuationMeta{CID: "c_123", RID: "r_456", RCID: "rc_789", ContextToken: "tok"}
	raw := buildInnerPayload("next turn", GeminiModel{ModelNumber: 4}, 2, meta, "XYZ")

	var inner []any
	if err := json.Unmarshal([]byte(raw), &inner); err != nil {
		t.Fatalf("parse: %v", err)
	}

	m2, ok := inner[2].([]any)
	if !ok || len(m2) != 10 {
		t.Fatalf("inner[2] must be 10-element continuation array, got %#v", inner[2])
	}
	if m2[0] != "c_123" || m2[1] != "r_456" || m2[2] != "rc_789" || m2[9] != "tok" {
		t.Fatalf("inner[2] contents wrong: %#v", m2)
	}

	m71, ok := inner[71].([]any)
	if !ok || len(m71) != 8 {
		t.Fatalf("inner[71] must be 8-element array, got %#v", inner[71])
	}
	if m71[0] != "c_123" || m71[7] != "tok" {
		t.Fatalf("inner[71] contents wrong: %#v", m71)
	}
}

// TestContinuationMetaFields validates Empty()/ToMeta() edge cases.
func TestContinuationMetaFields(t *testing.T) {
	if !(ContinuationMeta{}).Empty() {
		t.Fatal("zero meta should be Empty")
	}
	if (ContinuationMeta{CID: "c", ContextToken: ""}).Empty() == false {
		t.Fatal("CID-only meta should be Empty (contextToken still missing)")
	}
	if (ContinuationMeta{CID: "c", ContextToken: "t"}).Empty() {
		t.Fatal("CID+contextToken meta should not be Empty")
	}
}

// TestExtractChunksContinuationFields parses a realistic payload frame and
// verifies cid/rid/rcid/contextToken extraction from the correct field
// positions (pj[1], candidate[0], pj[25]).
func TestExtractChunksContinuationFields(t *testing.T) {
	// payload mirrors a real StreamGenerate envelope:
	//   pj[1] = ["c_abc", "r_def"] (cid, rid)
	//   pj[4] = [[ "rc_xyz", ["Hello"] ]] (candidate: rcid + text)
	//   pj[25] = "ctx_token_123"
	payload := []any{
		nil,
		[]any{"c_abc", "r_def"},
		nil,
		nil,
		[]any{[]any{"rc_xyz", []any{"Hello"}}},
	}
	// pad to length 26 then set index 25
	pad := make([]any, 26)
	for i := range pad {
		pad[i] = nil
	}
	copy(pad, payload)
	pad[25] = "ctx_token_123"

	inner, _ := json.Marshal(pad)
	envelope := []any{[]any{"wrb.fr", nil, string(inner)}}

	chunks := extractChunksFromEnvelope(envelope)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	c := chunks[0]
	if c.CID != "c_abc" {
		t.Errorf("CID = %q, want c_abc", c.CID)
	}
	if c.RID != "r_def" {
		t.Errorf("RID = %q, want r_def", c.RID)
	}
	if c.RCID != "rc_xyz" {
		t.Errorf("RCID = %q, want rc_xyz", c.RCID)
	}
	if c.ContextToken != "ctx_token_123" {
		t.Errorf("ContextToken = %q, want ctx_token_123", c.ContextToken)
	}
	if c.Text != "Hello" {
		t.Errorf("Text = %q, want Hello", c.Text)
	}
}

// TestExtractChunksSparseDictContextToken validates contextToken extraction
// from the trailing sparse-dict form ({"26": "tok"}) that appears in real frames.
func TestExtractChunksSparseDictContextToken(t *testing.T) {
	payload := []any{
		nil,
		[]any{"c_1", "r_2"},
		[]any{"rc_3", []any{"world"}},
	}
	// append a sparse dict as the last element with 1-based field "26" = pj[25]
	payload = append(payload, map[string]any{"26": "sparse_tok"})

	inner, _ := json.Marshal(payload)
	envelope := []any{[]any{"wrb.fr", nil, string(inner)}}
	chunks := extractChunksFromEnvelope(envelope)
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	if chunks[0].ContextToken != "sparse_tok" {
		t.Errorf("ContextToken = %q, want sparse_tok", chunks[0].ContextToken)
	}
}

// TestExtractChunksRealContextTokenFrame uses an exact frame captured live from
// Gemini Web: the context token arrives in a metadata-only frame where the
// payload is [null, [cid, rid], {"26":"Aw...", "44":true}] — no candidate array.
func TestExtractChunksRealContextTokenFrame(t *testing.T) {
	const line = `[["wrb.fr",null,"[null,[\"c_710ca6014028e060\",\"r_72b1f6d001517f81\"],{\"26\":\"AwAAAAAAAAAQANM7mBjXKZS5Ttu8GRo\",\"44\":true}]"]]`

	var envelope any
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Fatalf("unmarshal real frame: %v", err)
	}
	chunks := extractChunksFromEnvelope(envelope)
	if len(chunks) == 0 {
		t.Fatal("expected a chunk for the real context-token frame")
	}
	c := chunks[0]
	if c.CID != "c_710ca6014028e060" {
		t.Errorf("CID = %q", c.CID)
	}
	if c.RID != "r_72b1f6d001517f81" {
		t.Errorf("RID = %q", c.RID)
	}
	if c.ContextToken != "AwAAAAAAAAAQANM7mBjXKZS5Ttu8GRo" {
		t.Fatalf("ContextToken = %q, want the real token", c.ContextToken)
	}
}

func float64N(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return -1
}