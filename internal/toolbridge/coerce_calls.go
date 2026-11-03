package toolbridge

// CoerceCallsAgainstTools validates each parsed tool call's arguments against
// the matching tool's JSON Schema, coercing values where possible. It returns
// only the calls whose arguments are valid (or were coerced to valid); calls
// that fail validation are dropped so the caller can trigger a repair turn.
func CoerceCallsAgainstTools(calls []ToolCall, tools []OpenAITool) []ToolCall {
	if len(calls) == 0 || len(tools) == 0 {
		return calls
	}
	schemaByName := make(map[string]any, len(tools))
	for _, t := range tools {
		if t.Function.Name != "" {
			schemaByName[t.Function.Name] = t.Function.Parameters
		}
	}

	out := make([]ToolCall, 0, len(calls))
	for _, c := range calls {
		schema, ok := schemaByName[c.Function.Name]
		if !ok {
			// Unknown tool: keep as-is (defensive; shouldn't happen).
			continue
		}
		cleaned, err := ValidateAndCoerceArgs(schema, c.Function.Arguments)
		if err != nil {
			// Invalid per schema — drop so the caller can repair.
			continue
		}
		if cleaned != "" {
			c.Function.Arguments = cleaned
		}
		out = append(out, c)
	}
	return out
}