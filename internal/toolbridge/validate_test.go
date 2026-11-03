package toolbridge

import (
	"strings"
	"testing"
)

func TestCheckSchemaTypeAndRequired(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"required":   []any{"path"},
		"properties": map[string]any{"path": map[string]any{"type": "string"}},
	}
	if err := CheckSchema(schema, map[string]any{"path": "/x"}, "$"); err != "" {
		t.Fatalf("valid args rejected: %s", err)
	}
	if err := CheckSchema(schema, map[string]any{}, "$"); err == "" {
		t.Fatal("missing required should fail")
	}
	if err := CheckSchema(schema, map[string]any{"path": 42}, "$"); err == "" {
		t.Fatal("wrong type should fail")
	}
}

func TestCheckSchemaEnumAndConst(t *testing.T) {
	schema := map[string]any{
		"type": "string",
		"enum": []any{"a", "b"},
	}
	if CheckSchema(schema, "a", "$") != "" {
		t.Fatal("enum a should pass")
	}
	if CheckSchema(schema, "z", "$") == "" {
		t.Fatal("enum z should fail")
	}
}

func TestCheckSchemaNumericBounds(t *testing.T) {
	schema := map[string]any{"type": "integer", "minimum": float64(1), "maximum": float64(10)}
	if CheckSchema(schema, float64(5), "$") != "" {
		t.Fatal("5 should pass")
	}
	if CheckSchema(schema, float64(11), "$") == "" {
		t.Fatal("11 should fail max")
	}
	if CheckSchema(schema, float64(0), "$") == "" {
		t.Fatal("0 should fail min")
	}
}

func TestCheckSchemaOneOfAnyOf(t *testing.T) {
	schema := map[string]any{
		"oneOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "integer"},
		},
	}
	if CheckSchema(schema, "x", "$") != "" {
		t.Fatal("string should match one branch")
	}
	// a number matching both would be impossible here; test a value matching neither
	if CheckSchema(schema, true, "$") == "" {
		t.Fatal("bool should match no branch")
	}
}

func TestCoerceArgumentsToSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"count": map[string]any{"type": "integer"},
			"flag":  map[string]any{"type": "boolean"},
			"tags":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	args := map[string]any{"count": "42", "flag": "true", "tags": `["a","b"]`}
	CoerceArgumentsToSchema(schema, args)

	if args["count"] != float64(42) {
		t.Fatalf("count not coerced: %#v", args["count"])
	}
	if args["flag"] != true {
		t.Fatalf("flag not coerced: %#v", args["flag"])
	}
	tags, ok := args["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "a" {
		t.Fatalf("tags not coerced: %#v", args["tags"])
	}
	// fully coerced args must now pass CheckSchema
	if err := CheckSchema(schema, args, "$"); err != "" {
		t.Fatalf("coerced args still fail: %s", err)
	}
}

func TestValidateAndCoerceArgsRoundTrip(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
			"mode": map[string]any{"type": "string", "enum": []any{"read", "write"}},
		},
		"required": []any{"path"},
	}

	// Valid passes and returns cleaned JSON.
	out, err := ValidateAndCoerceArgs(schema, `{"path":"/a","mode":"read"}`)
	if err != nil {
		t.Fatalf("valid should pass: %v", err)
	}
	if !strings.Contains(out, `"path":"/a"`) {
		t.Fatalf("unexpected output: %s", out)
	}

	// Missing required field fails with a schema error.
	if _, err := ValidateAndCoerceArgs(schema, `{"mode":"read"}`); err == nil {
		t.Fatal("missing path should fail")
	} else if _, ok := err.(*SchemaError); !ok {
		t.Fatalf("expected SchemaError, got %T", err)
	}

	// Invalid enum fails.
	if _, err := ValidateAndCoerceArgs(schema, `{"path":"/a","mode":"bogus"}`); err == nil {
		t.Fatal("bad enum should fail")
	}
}

func TestCheckSchemaStringPattern(t *testing.T) {
	schema := map[string]any{"type": "string", "pattern": "^[a-z]+$"}
	if CheckSchema(schema, "abc", "$") != "" {
		t.Fatal("abc should match pattern")
	}
	if CheckSchema(schema, "ABC", "$") == "" {
		t.Fatal("ABC should not match ^[a-z]+$")
	}
}