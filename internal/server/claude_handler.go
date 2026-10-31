package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"geminiplexer/internal/gemini"
	"geminiplexer/internal/pool"
	"geminiplexer/internal/toolbridge"
)

type ClaudeTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema,omitempty"`
}

type ClaudeContentBlock struct {
	Type        string `json:"type"` // "text", "tool_result", "image", "thinking", "tool_use"
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Input       any    `json:"input,omitempty"`
	Content     any    `json:"content,omitempty"`
	ToolUseID   string `json:"tool_use_id,omitempty"`
}

type ClaudeMessage struct {
	Role    string `json:"role"`    // "user", "assistant"
	Content any    `json:"content"` // string or []ClaudeContentBlock
}

type ClaudeMessageRequest struct {
	Model     string          `json:"model"`
	Messages  []ClaudeMessage `json:"messages"`
	System    any             `json:"system,omitempty"` // string or []any
	MaxTokens int             `json:"max_tokens,omitempty"`
	Stream    bool            `json:"stream,omitempty"`
	Tools     []ClaudeTool    `json:"tools,omitempty"`
}

type ClaudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type ClaudeMessageResponse struct {
	ID           string               `json:"id"`
	Type         string               `json:"type"` // "message"
	Role         string               `json:"role"` // "assistant"
	Model        string               `json:"model"`
	Content      []ClaudeContentBlock `json:"content"`
	StopReason   string               `json:"stop_reason"` // "end_turn", "tool_use", "max_tokens"
	StopSequence *string              `json:"stop_sequence"`
	Usage        ClaudeUsage          `json:"usage"`
}

func parseClaudeSystem(system any) string {
	if system == nil {
		return ""
	}
	if s, ok := system.(string); ok {
		return s
	}
	if arr, ok := system.([]any); ok {
		var sb strings.Builder
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				if t, _ := m["text"].(string); t != "" {
					sb.WriteString(t)
					sb.WriteString("\n")
				}
			}
		}
		return strings.TrimSpace(sb.String())
	}
	return ""
}

func parseClaudeContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, _ := m["text"].(string); t != "" {
					sb.WriteString(t)
				} else if c, ok := m["content"].(string); ok && c != "" {
					sb.WriteString(c)
				}
			}
		}
		return sb.String()
	default:
		b, _ := json.Marshal(content)
		return string(b)
	}
}

func (s *Server) handleClaudeMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var req ClaudeMessageRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
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
		modelName = "gemini-4-argon"
	}

	// Convert Claude tools to toolbridge.OpenAITool
	var openAITools []toolbridge.OpenAITool
	for _, ct := range req.Tools {
		openAITools = append(openAITools, toolbridge.OpenAITool{
			Type: "function",
			Function: toolbridge.FunctionDefinition{
				Name:        ct.Name,
				Description: ct.Description,
				Parameters:  ct.InputSchema,
			},
		})
	}

	// Build prompt
	systemPrompt := parseClaudeSystem(req.System)
	var turns []string
	for _, m := range req.Messages {
		contentStr := parseClaudeContent(m.Content)
		if m.Role == "user" {
			turns = append(turns, fmt.Sprintf("User: %s", contentStr))
		} else {
			turns = append(turns, fmt.Sprintf("Assistant: %s", contentStr))
		}
	}

	finalSystem := toolbridge.InjectTools(systemPrompt, openAITools)
	var prompt string
	if finalSystem == "" && len(req.Messages) == 1 && req.Messages[0].Role == "user" {
		prompt = parseClaudeContent(req.Messages[0].Content)
	} else {
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
		prompt = strings.TrimSpace(sb.String())
	}

	maxAttempts := len(s.pool.GetSessions())
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var (
		sess           *gemini.AccountSession
		gModel         gemini.GeminiModel
		release        func(pool.ErrorKind)
		streamChan     <-chan gemini.GenerateChunk
		bufferedChunks []gemini.GenerateChunk
		lastErr        error
	)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		sess, gModel, release, err = s.pool.Acquire(r.Context(), modelName)
		if err != nil {
			lastErr = err
			break
		}

		streamChan, err = sess.StreamGenerate(r.Context(), prompt, gModel, 1)
		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
				release(pool.StatusAuthError)
				go sess.Init(context.Background())
			} else {
				release(pool.StatusRateLimited)
			}
			continue
		}

		bufferedChunks = nil
		downgraded := false

		for chunk := range streamChan {
			bufferedChunks = append(bufferedChunks, chunk)
			if chunk.ModelID != "" {
				if strings.Contains(strings.ToLower(modelName), "pro") && strings.Contains(strings.ToLower(chunk.ModelID), "flash") {
					downgraded = true
				}
				break
			}
			if chunk.Text != "" || chunk.Reasoning != "" || chunk.IsDone {
				break
			}
		}

		if downgraded && attempt < maxAttempts-1 {
			release(pool.StatusModelDowngraded)
			continue
		}

		break
	}

	if streamChan == nil {
		http.Error(w, fmt.Sprintf("Pool error or upstream failure: %v", lastErr), http.StatusBadGateway)
		return
	}
	defer release(pool.StatusSuccess)

	msgID := "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		// 1. message_start
		startMsg := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            msgID,
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         modelName,
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  len(prompt) / 4,
					"output_tokens": 0,
				},
			},
		}
		b, _ := json.Marshal(startMsg)
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", string(b))
		flusher.Flush()

		// 2. content_block_start
		blockStart := map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		}
		b, _ = json.Marshal(blockStart)
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", string(b))
		flusher.Flush()

		// 3. content_block_delta
		var prevText string
		var totalOutputTokens int

		processChunk := func(chunk gemini.GenerateChunk) {
			if chunk.IsDone {
				return
			}
			if chunk.Text != "" {
				var deltaText string
				if strings.HasPrefix(chunk.Text, prevText) {
					deltaText = chunk.Text[len(prevText):]
					prevText = chunk.Text
				} else {
					deltaText = chunk.Text
					prevText = chunk.Text
				}

				if deltaText != "" {
					totalOutputTokens += len(deltaText) / 4
					deltaEv := map[string]any{
						"type":  "content_block_delta",
						"index": 0,
						"delta": map[string]any{
							"type": "text_delta",
							"text": deltaText,
						},
					}
					b, _ := json.Marshal(deltaEv)
					fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", string(b))
					flusher.Flush()
				}
			}
		}

		for _, c := range bufferedChunks {
			processChunk(c)
		}
		for chunk := range streamChan {
			processChunk(chunk)
		}

		// 4. content_block_stop
		blockStop := map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		}
		b, _ = json.Marshal(blockStop)
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", string(b))
		flusher.Flush()

		// 5. message_delta
		msgDelta := map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   "end_turn",
				"stop_sequence": nil,
			},
			"usage": map[string]any{
				"output_tokens": totalOutputTokens,
			},
		}
		b, _ = json.Marshal(msgDelta)
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", string(b))
		flusher.Flush()

		// 6. message_stop
		fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		flusher.Flush()
		return
	}

	// Non-streaming response aggregation
	var finalContent string
	for _, c := range bufferedChunks {
		if c.Text != "" {
			finalContent = c.Text
		}
	}
	for chunk := range streamChan {
		if chunk.Text != "" {
			finalContent = chunk.Text
		}
	}

	var contentBlocks []ClaudeContentBlock
	stopReason := "end_turn"

	if len(req.Tools) > 0 {
		parsedCalls, remaining, ok := toolbridge.ParseToolCalls(finalContent)
		if ok && len(parsedCalls) > 0 {
			stopReason = "tool_use"
			if remaining != "" {
				contentBlocks = append(contentBlocks, ClaudeContentBlock{
					Type: "text",
					Text: remaining,
				})
			}
			for _, pc := range parsedCalls {
				var inputMap map[string]any
				_ = json.Unmarshal([]byte(pc.Function.Arguments), &inputMap)
				contentBlocks = append(contentBlocks, ClaudeContentBlock{
					Type:  "tool_use",
					ID:    "toolu_" + strings.ReplaceAll(uuid.NewString()[:12], "-", ""),
					Name:  pc.Function.Name,
					Input: inputMap,
				})
			}
		}
	}

	if len(contentBlocks) == 0 {
		contentBlocks = append(contentBlocks, ClaudeContentBlock{
			Type: "text",
			Text: finalContent,
		})
	}

	resp := ClaudeMessageResponse{
		ID:         msgID,
		Type:       "message",
		Role:       "assistant",
		Model:      modelName,
		Content:    contentBlocks,
		StopReason: stopReason,
		Usage: ClaudeUsage{
			InputTokens:  len(prompt) / 4,
			OutputTokens: len(finalContent) / 4,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
