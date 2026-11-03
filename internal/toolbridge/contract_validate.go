package toolbridge

import (
	"encoding/json"
)

// ValidateAndCoerceArgs parses rawArgs (a JSON object string), coerces values to
// the declared JSON Schema, validates the result, and returns the cleaned
// arguments JSON. It returns the original rawArgs unchanged when the schema is
// absent or the arguments cannot be parsed. On validation failure it returns an
// error naming the offending path, so the caller can trigger a repair turn.
func ValidateAndCoerceArgs(schema any, rawArgs string) (string, error) {
	args, err := parseArgsObject(rawArgs)
	if err != nil {
		return rawArgs, err
	}
	if schema == nil {
		return rawArgs, nil
	}

	sMap, ok := schema.(map[string]any)
	if !ok {
		return rawArgs, nil
	}

	CoerceArgumentsToSchema(sMap, args)
	if errStr := CheckSchema(sMap, args, "$"); errStr != "" {
		return rawArgs, &SchemaError{Path: errStr}
	}

	out, err := json.Marshal(args)
	if err != nil {
		return rawArgs, nil
	}
	return string(out), nil
}

// SchemaError reports a tool-call argument that failed JSON Schema validation.
type SchemaError struct {
	Path string
}

func (e *SchemaError) Error() string {
	return "tool argument failed schema validation: " + e.Path
}

func parseArgsObject(rawArgs string) (map[string]any, error) {
	if rawArgs == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return nil, err
	}
	if args == nil {
		return map[string]any{}, nil
	}
	return args, nil
}