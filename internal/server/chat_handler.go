package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"geminiplexer/internal/gemini"
	"geminiplexer/internal/pool"
	"geminiplexer/internal/toolbridge"
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

	if finalSystem == "" && len(messages) == 1 && messages[0].Role == "user" {
		return formatMessageContent(messages[0].Content)
	}

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
		writeAPIError(w, http.StatusServiceUnavailable, ErrCodeAuthUnavailable, "No accounts loaded in pool")
		return
	}

	modelName := req.Model
	if modelName == "" {
		modelName = "gemini-auto"
	}

	prompt := flattenMessages(req.Messages, req.Tools)
	thinkingLevel := parseReasoningEffort(req.ReasoningEffort)
	group := s.resolveGroupFromRequest(r)
	log.Printf("[Chat] handleChatCompletions: model=%s, prompt_len=%d, stream=%v, group=%q", modelName, len(prompt), req.Stream, group)

	// Conversation-keyed continuation: hash (model + history + tools) to look up
	// the account + metadata that own this conversation.
	var convMeta gemini.ContinuationMeta
	affinityAccount := ""
	if s.convs != nil {
		if e, ok := s.convs.lookupPrefix(modelName, req.Messages, req.Tools); ok {
			convMeta = e.Meta
			affinityAccount = e.AccountID
		}
	}

	maxAttempts := len(s.pool.GetSessions())
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	start := time.Now()
	var (
		sess           *gemini.AccountSession
		gModel         gemini.GeminiModel
		release        func(pool.ErrorKind)
		streamChan     <-chan gemini.GenerateChunk
		bufferedChunks []gemini.GenerateChunk
		lastErr        error
	)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Reset per-attempt state. Without this, a downgrade→continue leaves
		// stale sess/streamChan from the prior iteration while the next Acquire
		// may return a nil release (all accounts cooling), so the `streamChan
		// == nil` guard is bypassed and `defer release(...)` panics on nil.
		sess = nil
		release = nil
		streamChan = nil

		sess, gModel, release, err = s.pool.AcquireForGroupWithAffinity(r.Context(), modelName, group, affinityAccount)
		if err != nil {
			lastErr = err
			break
		}

		streamChan, err = sess.StreamGenerateWithMeta(r.Context(), prompt, gModel, thinkingLevel, convMeta)
		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
				release(pool.StatusAuthError)
				// Trigger self-heal in background
				go sess.SelfHeal(context.Background())
			} else {
				release(pool.StatusRateLimited)
			}
			continue
		}

		// Buffer initial chunk to inspect ModelID/ResponseModel for downgrade detection
		bufferedChunks = nil
		downgraded := false

		for chunk := range streamChan {
			log.Printf("[Chat] received chunk: text=%q, isDone=%v, modelID=%q, responseModel=%q, cid=%q, rid=%q, rcid=%q, ctxTok=%q", chunk.Text, chunk.IsDone, chunk.ModelID, chunk.ResponseModel, chunk.CID, chunk.RID, chunk.RCID, chunk.ContextToken)
			bufferedChunks = append(bufferedChunks, chunk)

			// Check for model downgrade using ResponseModel (pj[39]) or ModelID (pj[80])
			responseModelStr := chunk.ResponseModel
			if responseModelStr == "" {
				responseModelStr = chunk.ModelID
			}
			if responseModelStr != "" {
				requestedLower := strings.ToLower(modelName)
				responseLower := strings.ToLower(responseModelStr)
				// Downgrade: requested pro/argon but got flash/lite
				if (strings.Contains(requestedLower, "pro") || strings.Contains(requestedLower, "argon")) &&
					(strings.Contains(responseLower, "flash") || strings.Contains(responseLower, "lite")) {
					downgraded = true
					log.Printf("[Chat] model downgrade detected: requested=%s, got=%s (account=%s)", modelName, responseModelStr, sess.ID)
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

		// Succeeded with healthy model
		break
	}

	if streamChan == nil {
		// Classify the error
		errMsg := "Pool exhausted or upstream failure"
		if lastErr != nil {
			errMsg = lastErr.Error()
		}
		if strings.Contains(errMsg, "401") || strings.Contains(errMsg, "403") {
			writeAPIError(w, http.StatusUnauthorized, ErrCodeUpstreamAuthError, "Google session expired: "+errMsg)
		} else if strings.Contains(errMsg, "no healthy accounts") {
			writeAPIError(w, http.StatusServiceUnavailable, ErrCodeAuthUnavailable, errMsg)
		} else {
			writeAPIError(w, http.StatusBadGateway, ErrCodeServerError, errMsg)
		}
		return
	}
	defer release(pool.StatusSuccess)

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

		// When tools are requested, aggregate stream to detect and structure tool calls properly
		if len(req.Tools) > 0 {
			var fullText strings.Builder
			for _, c := range bufferedChunks {
				if c.Reasoning != "" {
					chunkResp := ChatCompletionChunk{
						ID:      reqID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   modelName,
						Choices: []StreamChoice{
							{
								Index: 0,
								Delta: Delta{ReasoningContent: c.Reasoning + "\n"},
							},
						},
					}
					b, _ := json.Marshal(chunkResp)
					fmt.Fprintf(w, "data: %s\n\n", string(b))
					flusher.Flush()
				}
				if c.Text != "" {
					fullText.Reset()
					fullText.WriteString(c.Text)
				}
			}

			for chunk := range streamChan {
				if chunk.Reasoning != "" {
					chunkResp := ChatCompletionChunk{
						ID:      reqID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   modelName,
						Choices: []StreamChoice{
							{
								Index: 0,
								Delta: Delta{ReasoningContent: chunk.Reasoning + "\n"},
							},
						},
					}
					b, _ := json.Marshal(chunkResp)
					fmt.Fprintf(w, "data: %s\n\n", string(b))
					flusher.Flush()
				}
				if chunk.Text != "" {
					fullText.Reset()
					fullText.WriteString(chunk.Text)
				}
			}

			finalContent := fullText.String()
			parsedCalls, remaining, ok := toolbridge.ParseToolCalls(finalContent)
			var toolCalls []toolbridge.ToolCall
			if ok {
				// Validate + coerce each call's arguments against its JSON Schema.
				toolCalls = toolbridge.CoerceCallsAgainstTools(parsedCalls, req.Tools)
				finalContent = remaining
			} else if strings.Contains(finalContent, "```json") || strings.Contains(finalContent, "\"name\":") {
				// Repair attempt
				repairPrompt := toolbridge.BuildRepairPrompt(finalContent, "JSON validation failed")
				repairChan, err := sess.StreamGenerate(r.Context(), repairPrompt, gModel, thinkingLevel)
				if err == nil {
					var repairText string
					for rc := range repairChan {
						if rc.Text != "" {
							repairText = rc.Text
						}
					}
					if fixedCalls, fixedRem, ok := toolbridge.ParseToolCalls(repairText); ok {
						toolCalls = fixedCalls
						finalContent = fixedRem
					}
				}
			}

			if len(toolCalls) > 0 {
				// Stream tool calls
				for i, tc := range toolCalls {
					cResp := ChatCompletionChunk{
						ID:      reqID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   modelName,
						Choices: []StreamChoice{
							{
								Index: 0,
								Delta: Delta{
									Role:      "assistant",
									ToolCalls: []toolbridge.ToolCall{tc},
								},
							},
						},
					}
					b, _ := json.Marshal(cResp)
					fmt.Fprintf(w, "data: %s\n\n", string(b))
					flusher.Flush()
					_ = i
				}

				// Final tool_calls stop chunk
				cResp := ChatCompletionChunk{
					ID:      reqID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   modelName,
					Choices: []StreamChoice{
						{
							Index:        0,
							Delta:        Delta{},
							FinishReason: "tool_calls",
						},
					},
				}
				b, _ := json.Marshal(cResp)
				fmt.Fprintf(w, "data: %s\n\n", string(b))
				flusher.Flush()
			} else {
				// No tool calls detected: stream the final text content
				if finalContent != "" {
					cResp := ChatCompletionChunk{
						ID:      reqID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   modelName,
						Choices: []StreamChoice{
							{
								Index: 0,
								Delta: Delta{Content: finalContent},
							},
						},
					}
					b, _ := json.Marshal(cResp)
					fmt.Fprintf(w, "data: %s\n\n", string(b))
					flusher.Flush()
				}
				cResp := ChatCompletionChunk{
					ID:      reqID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   modelName,
					Choices: []StreamChoice{
						{
							Index:        0,
							Delta:        Delta{},
							FinishReason: "stop",
						},
					},
				}
				b, _ := json.Marshal(cResp)
				fmt.Fprintf(w, "data: %s\n\n", string(b))
				flusher.Flush()
			}

			GlobalTelemetry.RecordRequest(RequestLogItem{
				ID:        reqID,
				Timestamp: time.Now().Format("15:04:05"),
				Protocol:  "OpenAI",
				Model:     modelName,
				Account:   sess.ID,
				LatencyMs: time.Since(start).Milliseconds(),
				Status:    http.StatusOK,
				ToolCalls: len(toolCalls) > 0,
			}, int64(len(finalContent)/4))

			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}

		var prevText string
		processChunk := func(chunk gemini.GenerateChunk) {
			if chunk.IsDone {
				return
			}

			delta := Delta{}
			if chunk.Reasoning != "" {
				delta.ReasoningContent = chunk.Reasoning + "\n"
			}
			if chunk.Text != "" {
				// Emit incremental delta, not duplicate cumulative prefix
				if strings.HasPrefix(chunk.Text, prevText) {
					delta.Content = chunk.Text[len(prevText):]
					prevText = chunk.Text
				} else {
					delta.Content = chunk.Text
					prevText = chunk.Text
				}
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

		for _, c := range bufferedChunks {
			processChunk(c)
		}
		for chunk := range streamChan {
			processChunk(chunk)
		}

		GlobalTelemetry.RecordRequest(RequestLogItem{
			ID:        reqID,
			Timestamp: time.Now().Format("15:04:05"),
			Protocol:  "OpenAI",
			Model:     modelName,
			Account:   sess.ID,
			LatencyMs: time.Since(start).Milliseconds(),
			Status:    http.StatusOK,
			ToolCalls: false,
		}, int64(len(prevText)/4))

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	// Non-streaming response aggregation: take the latest cumulative snapshot
	var finalContent string
	var finalMeta gemini.ContinuationMeta
	mergeMeta := func(c gemini.GenerateChunk) {
		if c.CID != "" {
			finalMeta.CID = c.CID
		}
		if c.RID != "" {
			finalMeta.RID = c.RID
		}
		if c.RCID != "" {
			finalMeta.RCID = c.RCID
		}
		if c.ContextToken != "" {
			finalMeta.ContextToken = c.ContextToken
		}
	}
	for _, c := range bufferedChunks {
		if c.Text != "" {
			finalContent = c.Text
		}
		mergeMeta(c)
	}
	for chunk := range streamChan {
		if chunk.Text != "" {
			finalContent = chunk.Text
		}
		mergeMeta(chunk)
	}

	// Persist the continuation metadata for the next turn in this conversation.
	// Store under (history + assistant reply) so the following turn matches via
	// prefix lookup.
	if s.convs != nil && sess != nil && !finalMeta.Empty() {
		s.convs.put(nextConvKey(modelName, req.Messages, req.Tools, finalContent), convCacheEntry{AccountID: sess.ID, Meta: finalMeta})
	}

	var toolCalls []toolbridge.ToolCall
	if len(req.Tools) > 0 {
		parsedCalls, remaining, ok := toolbridge.ParseToolCalls(finalContent)
		if ok {
			// Validate + coerce each call's arguments against its JSON Schema.
			toolCalls = toolbridge.CoerceCallsAgainstTools(parsedCalls, req.Tools)
			finalContent = remaining
		} else if strings.Contains(finalContent, "```json") || strings.Contains(finalContent, "\"name\":") {
			// Model attempted tool call but produced invalid JSON: 1-shot repair attempt
			repairPrompt := toolbridge.BuildRepairPrompt(finalContent, "JSON validation failed")
			repairChan, err := sess.StreamGenerate(r.Context(), repairPrompt, gModel, thinkingLevel)
			if err == nil {
				var repairText string
				for rc := range repairChan {
					if rc.Text != "" {
						repairText = rc.Text
					}
				}
				if fixedCalls, fixedRem, ok := toolbridge.ParseToolCalls(repairText); ok {
					toolCalls = fixedCalls
					finalContent = fixedRem
				}
			}
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

	GlobalTelemetry.RecordRequest(RequestLogItem{
		ID:        reqID,
		Timestamp: time.Now().Format("15:04:05"),
		Protocol:  "OpenAI",
		Model:     modelName,
		Account:   sess.ID,
		LatencyMs: time.Since(start).Milliseconds(),
		Status:    http.StatusOK,
		ToolCalls: len(toolCalls) > 0,
	}, int64(len(finalContent)/4))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
