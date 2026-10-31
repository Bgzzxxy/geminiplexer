package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"gemini-to-api/internal/pool"
	"gemini-to-api/internal/toolbridge"
)

type ChatMessage struct {
	Role       string                `json:"role"`
	Content    any                   `json:"content"`
	ToolCalls  []toolbridge.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string                `json:"tool_call_id,omitempty"`
}

type ChatCompletionRequest struct {
	Model           string                  `json:"model"`
	Messages        []ChatMessage           `json:"messages"`
	Stream          bool                    `json:"stream"`
	ReasoningEffort string                  `json:"reasoning_effort,omitempty"`
	Tools           []toolbridge.OpenAITool `json:"tools,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatChoice struct {
	Index        int                    `json:"index"`
	Message      ChatMessage            `json:"message"`
	FinishReason string                 `json:"finish_reason"`
}

type ChatCompletionResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   Usage        `json:"usage"`
}

type Delta struct {
	Role             string                `json:"role,omitempty"`
	Content          string                `json:"content,omitempty"`
	ReasoningContent string                `json:"reasoning_content,omitempty"`
	ToolCalls        []toolbridge.ToolCall `json:"tool_calls,omitempty"`
}

type StreamChoice struct {
	Index        int    `json:"index"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}

type ChatCompletionChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

func parseReasoningEffort(level string) int {
	switch strings.ToLower(level) {
	case "medium":
		return 2
	case "high":
		return 3
	default:
		return 1 // low
	}
}

func formatMessageContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, _ := m["type"].(string); t == "text" {
					if text, _ := m["text"].(string); text != "" {
						sb.WriteString(text)
					}
				}
			}
		}
		return sb.String()
	default:
		b, _ := json.Marshal(content)
		return string(b)
	}
}

func flattenMessages(messages []ChatMessage, tools []toolbridge.OpenAITool) string {
	var systemPrompt string
	var turns []string

	for _, msg := range messages {
		contentStr := formatMessageContent(msg.Content)
		switch msg.Role {
		case "system":
			if systemPrompt == "" {
				systemPrompt = contentStr
			} else {
				systemPrompt += "\n" + contentStr
			}
		case "user":
			turns = append(turns, fmt.Sprintf("User: %s", contentStr))
		case "assistant":
			turns = append(turns, fmt.Sprintf("Assistant: %s", contentStr))
		case "tool":
			turns = append(turns, fmt.Sprintf("Tool Result: %s", contentStr))
		}
	}

	finalSystem := toolbridge.InjectTools(systemPrompt, tools)

	var sb strings.Builder
	if finalSystem != "" {
		sb.WriteString("System Instruction:\n")
		sb.WriteString(finalSystem)
		sb.WriteString("\n\n---\nConversation History:\n")
	}

	for _, t := range turns {
		sb.WriteString(t)
		sb.WriteString("\n\n")
	}

	return strings.TrimSpace(sb.String())
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var req ChatCompletionRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		http.Error(w, "messages cannot be empty", http.StatusBadRequest)
		return
	}

	if s.pool == nil {
		http.Error(w, "Account pool not initialized", http.StatusServiceUnavailable)
		return
	}

	modelName := req.Model
	if modelName == "" {
		modelName = "gemini-auto"
	}

	sess, gModel, release, err := s.pool.Acquire(r.Context(), modelName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Pool error: %v", err), http.StatusServiceUnavailable)
		return
	}
	defer release(pool.StatusSuccess)

	prompt := flattenMessages(req.Messages, req.Tools)
	thinkingLevel := parseReasoningEffort(req.ReasoningEffort)

	streamChan, err := sess.StreamGenerate(r.Context(), prompt, gModel, thinkingLevel)
	if err != nil {
		release(pool.StatusRateLimited)
		http.Error(w, fmt.Sprintf("Gemini upstream error: %v", err), http.StatusBadGateway)
		return
	}

	reqID := "chatcmpl-" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	created := time.Now().Unix()

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		for chunk := range streamChan {
			if chunk.IsDone {
				break
			}

			delta := Delta{}
			if chunk.Reasoning != "" {
				delta.ReasoningContent = chunk.Reasoning + "\n"
			}
			if chunk.Text != "" {
				delta.Content = chunk.Text
			}

			if delta.Content != "" || delta.ReasoningContent != "" {
				cResp := ChatCompletionChunk{
					ID:      reqID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   modelName,
					Choices: []StreamChoice{
						{
							Index: 0,
							Delta: delta,
						},
					},
				}
				b, _ := json.Marshal(cResp)
				fmt.Fprintf(w, "data: %s\n\n", string(b))
				flusher.Flush()
			}
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	// Non-streaming response aggregation
	var fullText strings.Builder
	for chunk := range streamChan {
		if chunk.Text != "" {
			fullText.WriteString(chunk.Text)
		}
	}

	finalContent := fullText.String()
	var toolCalls []toolbridge.ToolCall
	if len(req.Tools) > 0 {
		if parsedCalls, remaining, ok := toolbridge.ParseToolCalls(finalContent); ok {
			toolCalls = parsedCalls
			finalContent = remaining
		}
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	resp := ChatCompletionResponse{
		ID:      reqID,
		Object:  "chat.completion",
		Created: created,
		Model:   modelName,
		Choices: []ChatChoice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:      "assistant",
					Content:   finalContent,
					ToolCalls: toolCalls,
				},
				FinishReason: finishReason,
			},
		},
		Usage: Usage{
			PromptTokens:     len(prompt) / 4,
			CompletionTokens: len(finalContent) / 4,
			TotalTokens:      (len(prompt) + len(finalContent)) / 4,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
