package toolbridge

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type FunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type OpenAITool struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

var jsonBlockRegex = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\}|\\[.*?\\])\\s*```")

func InjectTools(systemPrompt string, tools []OpenAITool) string {
	if len(tools) == 0 {
		return systemPrompt
	}

	toolsJSON, _ := json.MarshalIndent(tools, "", "  ")

	var sb strings.Builder
	if systemPrompt != "" {
		sb.WriteString(systemPrompt)
		sb.WriteString("\n\n")
	}

	sb.WriteString("# TOOL CALLING FORMAT\n")
	sb.WriteString("You have access to the following tools:\n")
	sb.WriteString(string(toolsJSON))
	sb.WriteString("\n\n")
	sb.WriteString("To call a tool, return a markdown JSON block with the function name and arguments:\n")
	sb.WriteString("```json\n{\n  \"name\": \"function_name\",\n  \"arguments\": {\"param1\": \"value1\"}\n}\n```\n")
	sb.WriteString("If no tool is needed, respond directly with normal text without code fences.\n")

	return sb.String()
}

type rawToolPayload struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

func ParseToolCalls(output string) ([]ToolCall, string, bool) {
	matches := jsonBlockRegex.FindAllStringSubmatchIndex(output, -1)
	if len(matches) == 0 {
		return nil, output, false
	}

	var calls []ToolCall
	remaining := output

	// Process matches in reverse to safely slice out JSON blocks
	for i := len(matches) - 1; i >= 0; i-- {
		startOuter, endOuter := matches[i][0], matches[i][1]
		startInner, endInner := matches[i][2], matches[i][3]

		jsonStr := output[startInner:endInner]

		// Try parsing as single tool call
		var single rawToolPayload
		if err := json.Unmarshal([]byte(jsonStr), &single); err == nil && single.Name != "" {
			argsStr := "{}"
			if single.Arguments != nil {
				if s, ok := single.Arguments.(string); ok {
					argsStr = s
				} else {
					b, _ := json.Marshal(single.Arguments)
					argsStr = string(b)
				}
			}

			calls = append([]ToolCall{{
				ID:   "call_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
				Type: "function",
				Function: FunctionCall{
					Name:      single.Name,
					Arguments: argsStr,
				},
			}}, calls...)

			remaining = remaining[:startOuter] + remaining[endOuter:]
			continue
		}

		// Try parsing as array of tool calls
		var multi []rawToolPayload
		if err := json.Unmarshal([]byte(jsonStr), &multi); err == nil && len(multi) > 0 && multi[0].Name != "" {
			for _, m := range multi {
				argsStr := "{}"
				if m.Arguments != nil {
					if s, ok := m.Arguments.(string); ok {
						argsStr = s
					} else {
						b, _ := json.Marshal(m.Arguments)
						argsStr = string(b)
					}
				}
				calls = append(calls, ToolCall{
					ID:   "call_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
					Type: "function",
					Function: FunctionCall{
						Name:      m.Name,
						Arguments: argsStr,
					},
				})
			}
			remaining = remaining[:startOuter] + remaining[endOuter:]
		}
	}

	if len(calls) == 0 {
		return nil, output, false
	}

	return calls, strings.TrimSpace(remaining), true
}
