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
)

// OpenAI Responses API types (POST /v1/responses)

type ResponsesRequest struct {
	Model           string `json:"model"`
	Input           any    `json:"input"` // string or []InputItem
	Stream          bool   `json:"stream"`
	Reasoning       any    `json:"reasoning,omitempty"` // object {effort:"high"} or string
	Instructions    string `json:"instructions,omitempty"`
	PreviousID      string `json:"previous_response_id,omitempty"`
}

// parseResponsesReasoning extracts thinking level from the reasoning field.
// Accepts {"effort": "high"} object or plain "high" string.
func parseResponsesReasoning(v any) int {
	switch t := v.(type) {
	case string:
		return parseReasoningEffort(t)
	case map[string]any:
		if effort, ok := t["effort"].(string); ok {
			return parseReasoningEffort(effort)
		}
	}
	return 1
}

type InputItem struct {
	Type    string `json:"type"` // "message"
	Role    string `json:"role"` // "user", "assistant", "system"
	Content any    `json:"content"`
}

type ResponseObject struct {
	ID        string       `json:"id"`
	Object    string       `json:"object"` // "response"
	CreatedAt int64        `json:"created_at"`
	Model     string       `json:"model"`
	Output    []OutputItem `json:"output"`
	Usage     ResponseUsage `json:"usage"`
	Status    string       `json:"status"` // "completed", "failed"
}

type OutputItem struct {
	Type    string          `json:"type"` // "message"
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Content []ContentBlock  `json:"content"`
	Status  string          `json:"status"` // "completed"
}

type ContentBlock struct {
	Type string `json:"type"` // "output_text"
	Text string `json:"text"`
}

type ResponseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Stream event types for Responses API
type ResponseStreamEvent struct {
	Type     string `json:"type"`
	Response *ResponseObject `json:"response,omitempty"`
	ItemID   string `json:"item_id,omitempty"`
	OutputIndex int `json:"output_index,omitempty"`
	ContentIndex int `json:"content_index,omitempty"`
	Delta    string `json:"delta,omitempty"`
	Item     *OutputItem `json:"item,omitempty"`
	Part     *ContentBlock `json:"part,omitempty"`
}

func flattenResponsesInput(input any, instructions string) string {
	var sb strings.Builder

	if instructions != "" {
		sb.WriteString("System Instruction:\n")
		sb.WriteString(instructions)
		sb.WriteString("\n\n---\n")
	}

	switch v := input.(type) {
	case string:
		sb.WriteString(v)
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			role, _ := m["role"].(string)
			content := ""
			switch c := m["content"].(type) {
			case string:
				content = c
			case []any:
				// Array of content parts
				for _, part := range c {
					if pm, ok := part.(map[string]any); ok {
						if t, _ := pm["type"].(string); t == "input_text" || t == "text" {
							if text, _ := pm["text"].(string); text != "" {
								content += text
							}
						}
					}
				}
			}
			switch role {
			case "system":
				sb.WriteString("System: ")
			case "user":
				sb.WriteString("User: ")
			case "assistant":
				sb.WriteString("Assistant: ")
			}
			sb.WriteString(content)
			sb.WriteString("\n\n")
		}
	}

	return strings.TrimSpace(sb.String())
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "Failed to read request body")
		return
	}

	var req ResponsesRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "Invalid JSON: "+err.Error())
		return
	}

	if req.Input == nil {
		writeAPIError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "input is required")
		return
	}

	if s.pool == nil {
		writeAPIError(w, http.StatusServiceUnavailable, ErrCodeAuthUnavailable, "No accounts loaded")
		return
	}

	modelName := req.Model
	if modelName == "" {
		modelName = "gemini-auto"
	}

	prompt := flattenResponsesInput(req.Input, req.Instructions)
	thinkingLevel := parseResponsesReasoning(req.Reasoning)
	group := s.resolveGroupFromRequest(r)

	log.Printf("[Responses] model=%s, prompt_len=%d, stream=%v, group=%q", modelName, len(prompt), req.Stream, group)

	start := time.Now()
	responseID := "resp_" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")

	// Acquire session
	sess, gModel, release, err := s.pool.AcquireForGroup(r.Context(), modelName, group)
	if err != nil {
		if strings.Contains(err.Error(), "no healthy accounts") {
			writeAPIError(w, http.StatusServiceUnavailable, ErrCodeAuthUnavailable, err.Error())
		} else {
			writeAPIError(w, http.StatusBadGateway, ErrCodeServerError, err.Error())
		}
		return
	}

	streamChan, err := sess.StreamGenerate(r.Context(), prompt, gModel, thinkingLevel)
	if err != nil {
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
			release(pool.StatusAuthError)
			go sess.SelfHeal(context.Background())
			writeAPIError(w, http.StatusUnauthorized, ErrCodeUpstreamAuthError, "Google session expired")
		} else {
			release(pool.StatusRateLimited)
			writeAPIError(w, http.StatusBadGateway, ErrCodeServerError, err.Error())
		}
		return
	}
	defer release(pool.StatusSuccess)

	if req.Stream {
		s.handleResponsesStream(w, r, streamChan, responseID, modelName, sess.ID, prompt, start)
		return
	}

	// Non-streaming: aggregate full response
	var fullText string
	for chunk := range streamChan {
		if chunk.Text != "" {
			fullText = chunk.Text
		}
	}

	msgID := "msg_" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	resp := ResponseObject{
		ID:        responseID,
		Object:    "response",
		CreatedAt: time.Now().Unix(),
		Model:     modelName,
		Status:    "completed",
		Output: []OutputItem{
			{
				Type:   "message",
				ID:     msgID,
				Role:   "assistant",
				Status: "completed",
				Content: []ContentBlock{
					{
						Type: "output_text",
						Text: fullText,
					},
				},
			},
		},
		Usage: ResponseUsage{
			InputTokens:  len(prompt) / 4,
			OutputTokens: len(fullText) / 4,
			TotalTokens:  (len(prompt) + len(fullText)) / 4,
		},
	}

	GlobalTelemetry.RecordRequest(RequestLogItem{
		ID:        responseID,
		Timestamp: time.Now().Format("15:04:05"),
		Protocol:  "Responses",
		Model:     modelName,
		Account:   sess.ID,
		LatencyMs: time.Since(start).Milliseconds(),
		Status:    http.StatusOK,
	}, int64(len(fullText)/4))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleResponsesStream(w http.ResponseWriter, r *http.Request, streamChan <-chan gemini.GenerateChunk, responseID, modelName, accountID, prompt string, start time.Time) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, ErrCodeServerError, "Streaming not supported")
		return
	}

	msgID := "msg_" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	created := time.Now().Unix()

	// response.created
	sendSSE(w, flusher, "response.created", map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":         responseID,
			"object":     "response",
			"created_at": created,
			"model":      modelName,
			"status":     "in_progress",
			"output":     []any{},
		},
	})

	// response.output_item.added
	sendSSE(w, flusher, "response.output_item.added", map[string]any{
		"type":         "response.output_item.added",
		"output_index": 0,
		"item": map[string]any{
			"type":   "message",
			"id":     msgID,
			"role":   "assistant",
			"status": "in_progress",
			"content": []any{},
		},
	})

	// response.content_part.added
	sendSSE(w, flusher, "response.content_part.added", map[string]any{
		"type":          "response.content_part.added",
		"item_id":       msgID,
		"output_index":  0,
		"content_index": 0,
		"part": map[string]any{
			"type": "output_text",
			"text": "",
		},
	})

	var prevText string
	var fullText string
	for chunk := range streamChan {
		if chunk.IsDone {
			continue
		}
		if chunk.Text != "" {
			delta := chunk.Text
			if strings.HasPrefix(chunk.Text, prevText) {
				delta = chunk.Text[len(prevText):]
				prevText = chunk.Text
			} else {
				prevText = chunk.Text
			}
			fullText = chunk.Text

			if delta != "" {
				sendSSE(w, flusher, "response.output_text.delta", map[string]any{
					"type":          "response.output_text.delta",
					"item_id":       msgID,
					"output_index":  0,
					"content_index": 0,
					"delta":         delta,
				})
			}
		}
	}

	// response.output_text.done
	sendSSE(w, flusher, "response.output_text.done", map[string]any{
		"type":          "response.output_text.done",
		"item_id":       msgID,
		"output_index":  0,
		"content_index": 0,
		"text":          fullText,
	})

	// response.content_part.done
	sendSSE(w, flusher, "response.content_part.done", map[string]any{
		"type":          "response.content_part.done",
		"item_id":       msgID,
		"output_index":  0,
		"content_index": 0,
		"part": map[string]any{
			"type": "output_text",
			"text": fullText,
		},
	})

	// response.output_item.done
	sendSSE(w, flusher, "response.output_item.done", map[string]any{
		"type":         "response.output_item.done",
		"output_index": 0,
		"item": map[string]any{
			"type":   "message",
			"id":     msgID,
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]any{
				{"type": "output_text", "text": fullText},
			},
		},
	})

	// response.completed
	sendSSE(w, flusher, "response.completed", map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":         responseID,
			"object":     "response",
			"created_at": created,
			"model":      modelName,
			"status":     "completed",
			"output": []map[string]any{
				{
					"type":   "message",
					"id":     msgID,
					"role":   "assistant",
					"status": "completed",
					"content": []map[string]any{
						{"type": "output_text", "text": fullText},
					},
				},
			},
			"usage": map[string]int{
				"input_tokens":  len(prompt) / 4,
				"output_tokens": len(fullText) / 4,
				"total_tokens":  (len(prompt) + len(fullText)) / 4,
			},
		},
	})

	GlobalTelemetry.RecordRequest(RequestLogItem{
		ID:        responseID,
		Timestamp: time.Now().Format("15:04:05"),
		Protocol:  "Responses",
		Model:     modelName,
		Account:   accountID,
		LatencyMs: time.Since(start).Milliseconds(),
		Status:    http.StatusOK,
	}, int64(len(fullText)/4))
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(b))
	flusher.Flush()
}
