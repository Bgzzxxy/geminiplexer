package gemini

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"geminiplexer/internal/transport"
)

const (
	DefaultGenerateURL   = "https://gemini.google.com/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate"
	geminiModelHeaderKey = "x-goog-ext-525001261-jspb"
)

type GenerateChunk struct {
	Text      string
	Reasoning string
	ModelID   string
	IsDone    bool
}

func (s *AccountSession) SetGenerateEndpointOverride(u string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.GenerateOverride = u
}

func (s *AccountSession) StreamGenerate(ctx context.Context, prompt string, model GeminiModel, thinkingLevel int) (<-chan GenerateChunk, error) {
	s.mu.Lock()
	token := s.Token
	cookieHdr := s.getFullCookieHeaderLocked()
	buildLabel := s.BuildLabel
	sessionID := s.SessionID
	authUser := s.AuthUser
	client := s.Client
	endpoint := DefaultGenerateURL
	if s.GenerateOverride != "" {
		endpoint = s.GenerateOverride
	} else if authUser != "" {
		endpoint = fmt.Sprintf("https://gemini.google.com/u/%s/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate", authUser)
	}
	s.mu.Unlock()

	if token == "" {
		return nil, fmt.Errorf("session token (SNlM0e) not initialized")
	}

	requestID := strings.ToUpper(uuid.NewString())
	generationID := strings.ToUpper(uuid.NewString())

	// Build inner payload (81 elements)
	inner := make([]any, 81)
	inner[0] = []any{prompt}
	inner[1] = []any{"en"}
	inner[2] = []any{"", "", "", nil, nil, nil, nil, nil, nil, ""}
	inner[6] = []any{1}
	inner[7] = 1
	inner[10] = 1
	inner[11] = 0
	inner[17] = []any{[]any{0}}
	inner[18] = 0
	inner[27] = 1
	inner[30] = []any{4}
	inner[41] = []any{1}
	inner[45] = 1 // Stateless temporary chat mode enforced
	inner[53] = 0
	inner[59] = requestID
	inner[61] = []any{}
	inner[68] = 1
	inner[79] = model.ModelNumber
	inner[80] = thinkingLevel

	innerJSON, _ := json.Marshal(inner)
	outerJSON, _ := json.Marshal([]any{nil, string(innerJSON)})

	form := url.Values{}
	form.Set("at", token)
	form.Set("f.req", string(outerJSON))

	query := url.Values{
		"at":     {token},
		"hl":     {"en"},
		"_reqid": {strconv.Itoa(rand.Intn(90000) + 10000)},
		"rt":     {"c"},
	}
	if buildLabel != "" {
		query.Set("bl", buildLabel)
	}
	if sessionID != "" {
		query.Set("f.sid", sessionID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create stream generate request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	req.Header.Set("Origin", "https://gemini.google.com")
	referer := "https://gemini.google.com/"
	if authUser != "" {
		referer = fmt.Sprintf("https://gemini.google.com/u/%s/", authUser)
	}
	req.Header.Set("Referer", referer)
	transport.SetChromeHeaders(req)
	req.Header.Set("X-Same-Domain", "1")
	req.Header.Set("Cookie", cookieHdr)

	// Routing model headers
	if model.ModelID != "" && model.ModelID != "auto" {
		modelHdr := make([]any, 17)
		modelHdr[0] = 1
		modelHdr[4] = model.ModelID
		modelHdr[7] = 0
		modelHdr[8] = []int{4, 5, 6, 8}
		modelHdr[11] = model.Capacity
		modelHdr[14] = model.ModelNumber
		modelHdr[15] = thinkingLevel
		modelHdr[16] = generationID

		encHdr, _ := json.Marshal(modelHdr)
		req.Header.Set(geminiModelHeaderKey, string(encHdr))
		reqHeader, _ := json.Marshal([]any{requestID, 1})
		req.Header.Set("x-goog-ext-525005358-jspb", string(reqHeader))
		req.Header.Set("x-goog-ext-73010989-jspb", "[0]")
		req.Header.Set("x-goog-ext-73010990-jspb", "[0,0,0]")
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[StreamGenerate] client.Do error: %v", err)
		return nil, fmt.Errorf("execute stream generate request: %w", err)
	}

	log.Printf("[StreamGenerate] HTTP response status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("stream generate returned HTTP %d", resp.StatusCode)
	}

	ch := make(chan GenerateChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		reader := bufio.NewReader(resp.Body)
		var frameBuf strings.Builder

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				log.Printf("[StreamGenerate] reader loop exited with err=%v, line_len=%d", err, len(line))
				if err != io.EOF && ctx.Err() == nil {
					ch <- GenerateChunk{IsDone: true}
				}
				break
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			dataStr := line
			if strings.HasPrefix(line, "data:") {
				dataStr = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
			if strings.HasPrefix(dataStr, ")]}'") {
				dataStr = strings.TrimSpace(strings.TrimPrefix(dataStr, ")]}'"))
			}
			if dataStr == "" {
				continue
			}
			// Skip pure numeric length-prefix lines
			if _, err := strconv.Atoi(dataStr); err == nil {
				continue
			}

			// Append line to assembly buffer
			if frameBuf.Len() > 0 {
				frameBuf.WriteString(" ")
			}
			frameBuf.WriteString(dataStr)

			combined := frameBuf.String()
			var envelope any
			if err := json.Unmarshal([]byte(combined), &envelope); err == nil {
				// Successfully parsed valid complete JSON envelope!
				frameBuf.Reset()
				chunks := extractChunksFromEnvelope(envelope)
				log.Printf("[StreamGenerate] parsed envelope successfully, found %d chunks", len(chunks))
				for _, chunk := range chunks {
					if chunk.Text != "" || chunk.Reasoning != "" || chunk.ModelID != "" {
						select {
						case ch <- chunk:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
		ch <- GenerateChunk{IsDone: true}
	}()

	return ch, nil
}

func ParseStreamFrameForTest(dataStr string) []GenerateChunk {
	var envelope any
	if err := json.Unmarshal([]byte(dataStr), &envelope); err != nil {
		return nil
	}
	return extractChunksFromEnvelope(envelope)
}

func findWrbFrPayloads(v any) []string {
	var out []string
	switch val := v.(type) {
	case []any:
		if len(val) >= 3 {
			if tag, ok := val[0].(string); ok && tag == "wrb.fr" {
				if s, ok := val[2].(string); ok && s != "" {
					out = append(out, s)
				}
			}
		}
		for _, elem := range val {
			out = append(out, findWrbFrPayloads(elem)...)
		}
	}
	return out
}

func extractChunksFromEnvelope(envelope any) []GenerateChunk {
	payloadStrings := findWrbFrPayloads(envelope)
	var results []GenerateChunk

	for _, payloadStr := range payloadStrings {
		var payload []any
		if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
			continue
		}

		chunk := GenerateChunk{}

		// 1. Thinking / reasoning label at payload[2]["7"][5][0]
		if len(payload) > 2 {
			if fields, ok := payload[2].(map[string]any); ok {
				if parts, ok := fields["7"].([]any); ok && len(parts) >= 6 {
					if labels, ok := parts[5].([]any); ok && len(labels) > 0 {
						if labelStr, ok := labels[0].(string); ok {
							chunk.Reasoning = strings.TrimSpace(labelStr)
						}
					}
				}
				// Numbered field reply text at payload[2]["7"][0]
				if parts, ok := fields["7"].([]any); ok && len(parts) > 0 {
					if replyStr, ok := parts[0].(string); ok && replyStr != "" {
						chunk.Text = SanitizeText(replyStr)
					}
				}
			}
		}

		// 2. Candidates text at payload[4][0][1][0]
		if len(payload) > 4 {
			if candidates, ok := payload[4].([]any); ok && len(candidates) > 0 {
				if firstCand, ok := candidates[0].([]any); ok && len(firstCand) > 1 {
					if contentParts, ok := firstCand[1].([]any); ok && len(contentParts) > 0 {
						if resText, ok := contentParts[0].(string); ok && resText != "" {
							chunk.Text = SanitizeText(resText)
						}
					}
				}
			}
		}

		// Model ID marker
		if len(payload) > 80 {
			if mID, ok := payload[80].(string); ok && mID != "" {
				chunk.ModelID = mID
			}
		}

		if chunk.Text != "" || chunk.Reasoning != "" || chunk.ModelID != "" {
			results = append(results, chunk)
		}
	}

	return results
}
