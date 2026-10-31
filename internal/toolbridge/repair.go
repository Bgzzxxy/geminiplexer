package toolbridge

import (
	"encoding/json"
	"fmt"
)

func BuildRepairPrompt(invalidJSON, parseError string) string {
	return fmt.Sprintf("Your previous tool call response was invalid JSON.\nError: %s\nOriginal output:\n%s\nPlease fix the JSON and return ONLY the valid markdown JSON block with \"name\" and \"arguments\".", parseError, invalidJSON)
}

func ValidateToolCallJSON(jsonStr string) (string, error) {
	var v any
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return "", err
	}
	clean, _ := json.Marshal(v)
	return string(clean), nil
}
