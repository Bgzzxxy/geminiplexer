package toolbridge_test

import (
	"strings"
	"testing"
	"gemini-relay/internal/toolbridge"
)

func TestParseToolCallsValid(t *testing.T) {
	response := "I will search for the file.\n```json\n{\n  \"name\": \"search_files\",\n  \"arguments\": {\"pattern\": \"*.go\"}\n}\n```"
	calls, remainingText, ok := toolbridge.ParseToolCalls(response)
	if !ok || len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "search_files" {
		t.Errorf("expected name search_files, got %s", calls[0].Function.Name)
	}
	if remainingText != "I will search for the file." {
		t.Errorf("unexpected remaining text: %q", remainingText)
	}
}

func TestInjectTools(t *testing.T) {
	tools := []toolbridge.OpenAITool{
		{
			Type: "function",
			Function: toolbridge.FunctionDefinition{
				Name:        "get_weather",
				Description: "Get the current weather",
				Parameters:  map[string]any{"type": "object"},
			},
		},
	}

	prompt := toolbridge.InjectTools("You are a helpful assistant.", tools)
	if !strings.Contains(prompt, "get_weather") || !strings.Contains(prompt, "TOOL CALLING FORMAT") {
		t.Errorf("expected prompt to include tool definition, got: %s", prompt)
	}
}
