package toolbridge

import (
	"testing"
)

func TestCoerceCallsAgainstTools(t *testing.T) {
	tools := []OpenAITool{
		{
			Type: "function",
			Function: FunctionDefinition{
				Name: "write_file",
				Parameters: map[string]any{
					"type":     "object",
					"required": []any{"path"},
					"properties": map[string]any{
						"path":   map[string]any{"type": "string"},
						"append": map[string]any{"type": "boolean"},
					},
				},
			},
		},
	}

	// Append is a string "true" — should be coerced to boolean; valid.
	calls := []ToolCall{
		{ID: "c1", Type: "function", Function: FunctionCall{Name: "write_file", Arguments: `{"path":"/tmp/x","append":"true"}`}},
	}
	out := CoerceCallsAgainstTools(calls, tools)
	if len(out) != 1 {
		t.Fatalf("expected 1 call to survive, got %d", len(out))
	}
	if out[0].Function.Arguments != `{"append":true,"path":"/tmp/x"}` {
		t.Fatalf("append not coerced to bool: %s", out[0].Function.Arguments)
	}
}

func TestCoerceCallsDropsInvalid(t *testing.T) {
	tools := []OpenAITool{
		{
			Type: "function",
			Function: FunctionDefinition{
				Name:       "write_file",
				Parameters: map[string]any{"type": "object", "required": []any{"path"}, "properties": map[string]any{"path": map[string]any{"type": "string"}}},
			},
		},
	}
	// Missing required "path" -> invalid -> dropped.
	calls := []ToolCall{
		{ID: "c1", Type: "function", Function: FunctionCall{Name: "write_file", Arguments: `{}`}},
	}
	out := CoerceCallsAgainstTools(calls, tools)
	if len(out) != 0 {
		t.Fatalf("invalid call should be dropped, got %d", len(out))
	}
}

func TestCoerceCallsNoToolsPassthrough(t *testing.T) {
	calls := []ToolCall{
		{ID: "c1", Type: "function", Function: FunctionCall{Name: "nope", Arguments: `{}`}},
	}
	// No tools declared -> nothing to validate against -> pass through unchanged.
	out := CoerceCallsAgainstTools(calls, nil)
	if len(out) != 1 {
		t.Fatalf("no-schema passthrough should keep the call, got %d", len(out))
	}
}

func TestCoerceCallsUnknownToolDropped(t *testing.T) {
	tools := []OpenAITool{
		{Type: "function", Function: FunctionDefinition{Name: "known", Parameters: map[string]any{"type": "object"}}},
	}
	// Tool name not in the declared set -> dropped (defensive).
	calls := []ToolCall{
		{ID: "c1", Type: "function", Function: FunctionCall{Name: "unknown", Arguments: `{}`}},
	}
	out := CoerceCallsAgainstTools(calls, tools)
	if len(out) != 0 {
		t.Fatalf("unknown tool name should be dropped, got %d", len(out))
	}
}