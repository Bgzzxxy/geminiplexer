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
	Text           string
	Reasoning      string
	ModelID        string
	ResponseModel  string // answered model label (from pj[42])
	ResponseModelID string // answered model hex id (from pj[39])
	CID            string // conversation id (pj[1][0])
	RID            string // response id (pj[1][1])
	RCID           string // candidate id (candidate[0])
	ContextToken   string // conversation continuation token (pj[25])
	IsDone         bool
}

// ContinuationMeta carries the 4 tokens Gemini Web returns so a follow-up turn
// can continue the same conversation instead of starting stateless.
type ContinuationMeta struct {
	CID          string
	RID          string
	RCID         string
	ContextToken string
}

// Empty reports whether there is enough metadata to request continuation.
func (m ContinuationMeta) Empty() bool {
	return m.CID == "" || m.ContextToken == ""
}

// ToMeta builds the 10-element Gemini Web continuation array for inner[2].
func (m ContinuationMeta) ToMeta() []any {
	return []any{m.CID, m.RID, m.RCID, nil, nil, nil, nil, nil, nil, m.ContextToken}
}

// buildInnerPayload constructs the 81-element Gemini Web StreamGenerate inner
// payload. When meta carries continuation tokens, the conversation is resumed;
// otherwise inner[2] is left nil (a fresh, non-temporary conversation) so
// Gemini returns continuation metadata for the next turn.
//
// inner[45] (temporary/stateless chat) is intentionally left unset: forcing it
// to 1 was the root cause of "the model forgets the system prompt and context
// every turn".
func buildInnerPayload(prompt string, model GeminiModel, thinkingLevel int, meta ContinuationMeta, requestID string) string {
	inner := make([]any, 81)
	// inner[0] is the 7-element prompt message array, not a bare [prompt]:
	//   [text, 0, nil, filesSlot, nil, nil, 0]
	// filesSlot stays nil until attachment upload is implemented. This shape is
	// what Gemini Web's StreamGenerate expects; a 1-element slot caused prompt
	// truncation upstream.
	filesSlot := any(nil)
	inner[0] = []any{prompt, 0, nil, filesSlot, nil, nil, 0}
	inner[1] = []any{"en"}
	if !meta.Empty() {
		inner[2] = meta.ToMeta()
		inner[71] = []any{meta.CID, nil, nil, nil, nil, nil, nil, meta.ContextToken}
	}
	inner[6] = []any{1}
	inner[7] = 1
	inner[10] = 1
	inner[11] = 0
	inner[17] = []any{[]any{0}}
	inner[18] = 0
	inner[27] = 1
	inner[30] = []any{4}
	inner[41] = []any{1}
	inner[53] = 0
	inner[59] = requestID
	inner[61] = []any{}
	inner[68] = 1
	inner[79] = model.ModelNumber
	inner[80] = thinkingLevel

	b, _ := json.Marshal(inner)
	return string(b)
}

func (s *AccountSession) SetGenerateEndpointOverride(u string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.GenerateOverride = u
}

func (s *AccountSession) StreamGenerate(ctx context.Context, prompt string, model GeminiModel, thinkingLevel int) (<-chan GenerateChunk, error) {
	return s.StreamGenerateWithMeta(ctx, prompt, model, thinkingLevel, ContinuationMeta{})
}

func (s *AccountSession) StreamGenerateWithMeta(ctx context.Context, prompt string, model GeminiModel, thinkingLevel int, meta ContinuationMeta) (<-chan GenerateChunk, error) {
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

	// Build inner payload (81 elements); non-temporary so Gemini returns
	// continuation metadata (cid/rid/rcid/contextToken) for multi-turn use.
	innerJSON := buildInnerPayload(prompt, model, thinkingLevel, meta, requestID)
	outerJSON, _ := json.Marshal([]any{nil, innerJSON})

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
					if chunk.Text != "" || chunk.Reasoning != "" || chunk.ModelID != "" || chunk.ResponseModel != "" || chunk.ContextToken != "" {
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

// jspbField reads a 0-based index from a JSPB array, supporting both dense
// positional slots and the trailing sparse dictionary object at arr[len-1]
// (where keys are 1-based field numbers). Ported from gemini-stealth-bridge.
func jspbField(arr []any, index int) any {
	if len(arr) == 0 {
		return nil
	}
	if index >= 0 && index < len(arr) && arr[index] != nil {
		val := arr[index]
		if index < len(arr)-1 {
			return val
		}
		if _, isMap := val.(map[string]any); !isMap {
			return val
		}
	}
	if lastMap, ok := arr[len(arr)-1].(map[string]any); ok {
		return lastMap[strconv.Itoa(index+1)]
	}
	return nil
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

		// Conversation/response ids: pj[1] = [cid, rid].
		if len(payload) > 1 {
			if ids, ok := payload[1].([]any); ok {
				if len(ids) > 0 {
					if cid, ok := ids[0].(string); ok && cid != "" {
						chunk.CID = cid
					}
				}
				if len(ids) > 1 {
					if rid, ok := ids[1].(string); ok && rid != "" {
						chunk.RID = rid
					}
				}
			}
		}

		// Answered model id/label: pj[39] (hex16 id), pj[42] (label).
		if rawID, ok := jspbField(payload, 39).(string); ok && rawID != "" {
			chunk.ResponseModelID = rawID
		}
		if rawLabel, ok := jspbField(payload, 42).(string); ok && rawLabel != "" {
			chunk.ResponseModel = rawLabel
		}

		// Context token for session continuation lives at pj[25], not pj[2].
		if rawCtx, ok := jspbField(payload, 25).(string); ok && rawCtx != "" {
			chunk.ContextToken = rawCtx
		}

		// Candidates at pj[4]; each candidate[0] is the rcid.
		if len(payload) > 4 {
			if candidates, ok := payload[4].([]any); ok && len(candidates) > 0 {
				if firstCand, ok := candidates[0].([]any); ok && len(firstCand) > 1 {
					if rcid, ok := firstCand[0].(string); ok && rcid != "" {
						chunk.RCID = rcid
					}
					// Reasoning/thoughts: candidate[37] → [0] → [0] → string.
					if tf, ok := jspbField(firstCand, 37).([]any); ok && len(tf) > 0 {
						if tf0, ok := tf[0].([]any); ok && len(tf0) > 0 {
							if thoughts, ok := tf0[0].(string); ok && thoughts != "" {
								chunk.Reasoning = strings.TrimSpace(thoughts)
							}
						}
					}
					if contentParts, ok := firstCand[1].([]any); ok && len(contentParts) > 0 {
						if resText, ok := contentParts[0].(string); ok && resText != "" {
							chunk.Text = SanitizeText(resText)
						}
					}
				}
			}
		}

		// Model ID marker at payload[80] (requested model echo).
		if len(payload) > 80 {
			if mID, ok := payload[80].(string); ok && mID != "" {
				chunk.ModelID = mID
			}
		}

		if chunk.Text != "" || chunk.Reasoning != "" || chunk.ModelID != "" || chunk.ResponseModel != "" || chunk.ResponseModelID != "" || chunk.CID != "" || chunk.RID != "" || chunk.RCID != "" || chunk.ContextToken != "" {
			results = append(results, chunk)
		}
	}

	return results
}
