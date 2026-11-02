package gemini

import (
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

	"geminiplexer/internal/transport"
)

const (
	geminiUserStatusRPC = "otAQ7b"
	DefaultBatchExecURL = "https://gemini.google.com/_/BardChatUi/data/batchexecute"
)

type GeminiModel struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	ModelID       string `json:"model_id"`
	Capacity      int    `json:"capacity"`
	CapacityField int    `json:"capacity_field"`
	ModelNumber   int    `json:"model_number"`
}

func (s *AccountSession) SetBatchExecEndpointOverride(u string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.BatchExecOverride = u
}

func (s *AccountSession) FetchModels(ctx context.Context) ([]GeminiModel, error) {
	s.mu.Lock()
	token := s.Token
	cookieHdr := s.getFullCookieHeaderLocked()
	buildLabel := s.BuildLabel
	sessionID := s.SessionID
	authUser := s.AuthUser
	client := s.Client
	endpoint := DefaultBatchExecURL
	if s.BatchExecOverride != "" {
		endpoint = s.BatchExecOverride
	} else if authUser != "" {
		endpoint = fmt.Sprintf("https://gemini.google.com/u/%s/_/BardChatUi/data/batchexecute", authUser)
	}
	s.mu.Unlock()

	if token == "" {
		return nil, fmt.Errorf("session token (SNlM0e) not initialized")
	}

	query := url.Values{
		"rpcids":      {geminiUserStatusRPC},
		"hl":          {"en"},
		"_reqid":      {strconv.Itoa(rand.Intn(90000) + 10000)},
		"rt":          {"c"},
		"source-path": {"/app"},
	}
	if authUser != "" {
		query.Set("source-path", "/u/"+authUser+"/app")
	}
	if buildLabel != "" {
		query.Set("bl", buildLabel)
	}
	if sessionID != "" {
		query.Set("f.sid", sessionID)
	}

	form := url.Values{
		"at":    {token},
		"f.req": {`[[["otAQ7b","[]",null,"generic"]]]`},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create batchexecute request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	req.Header.Set("Origin", "https://gemini.google.com")
	req.Header.Set("Referer", "https://gemini.google.com/")
	transport.SetChromeHeaders(req)
	req.Header.Set("X-Same-Domain", "1")
	req.Header.Set("Cookie", cookieHdr)
	if auth := s.SAPISIDHash("https://gemini.google.com"); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute batchexecute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("batchexecute returned status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read batchexecute body: %w", err)
	}

	bodyStr := strings.TrimSpace(string(bodyBytes))
	// Strip Google anti-XSSI prefix if present
	if strings.HasPrefix(bodyStr, ")]}'") {
		bodyStr = strings.TrimPrefix(bodyStr, ")]}'")
		bodyStr = strings.TrimSpace(bodyStr)
	}

	parsedEnvelope, err := parseBatchEnvelope(bodyStr)
	if err != nil {
		// Fallback to default model if unmarshal of outer envelope fails
		return defaultModels(), nil
	}

	models := parseModelsFromEnvelope(parsedEnvelope)
	if len(models) == 0 {
		return defaultModels(), nil
	}

	s.mu.Lock()
	s.CachedModels = models
	s.mu.Unlock()

	log.Printf("[Models] %s: discovered %d live models (ids=%s)", s.ID, len(models), joinModelIDs(models))

	return models, nil
}

func defaultModels() []GeminiModel {
	return []GeminiModel{
		{
			ID:            "gemini-auto",
			DisplayName:   "Gemini Auto",
			ModelID:       "auto",
			Capacity:      1,
			CapacityField: 12,
			ModelNumber:   1,
		},
	}
}

func parseModelsFromEnvelope(envelope []any) []GeminiModel {
	var models []GeminiModel
	for _, item := range envelope {
		itemArr, ok := item.([]any)
		if !ok || len(itemArr) < 3 {
			continue
		}
		// Look for wrb.fr, otAQ7b
		rpcName, _ := itemArr[1].(string)
		if rpcName != geminiUserStatusRPC {
			continue
		}
		bodyData, ok := itemArr[2].(string)
		if !ok || bodyData == "" {
			continue
		}

		var payload []any
		if err := json.Unmarshal([]byte(bodyData), &payload); err != nil {
			continue
		}

		// Traverse payload looking for model string markers
		models = extractModelsFromPayload(payload)
	}
	return models
}

func extractModelsFromPayload(payload []any) []GeminiModel {
	var results []GeminiModel
	// Gemini Web's otAQ7b payload carries the live model registry at index 15:
	//   payload[0]=1, [1]=[flags], ..., [15]=[[modelEntry], [modelEntry], ...]
	// Each modelEntry: [0]=hex model id, [1]=category ("Flash"), [9]/[17]=model
	// number, [11]=display name ("3.8 Flash").
	if len(payload) <= 15 {
		return results
	}
	rawModels, ok := payload[15].([]any)
	if !ok || len(rawModels) == 0 {
		return results
	}

	for _, mAny := range rawModels {
		md, ok := mAny.([]any)
		if !ok || len(md) == 0 {
			continue
		}
		hexID, _ := md[0].(string)
		if hexID == "" {
			continue
		}
		category := stringAtIdx(md, 1) // "Flash", "Pro", "Argon", "Flash-Lite"
		display := stringAtIdx(md, 11) // "3.8 Flash"
		if display == "" {
			display = stringAtIdx(md, 1)
		}
		modelNumber := 1
		if n, ok := intAtIdx(md, 17); ok {
			modelNumber = n
		} else if n, ok := intAtIdx(md, 9); ok {
			modelNumber = n
		}

		friendlyID := mapCategoryToFriendlyID(category)
		if friendlyID == "" {
			continue
		}

		results = append(results, GeminiModel{
			ID:            friendlyID,
			DisplayName:   display,
			ModelID:       hexID,
			Capacity:      2,
			CapacityField: 12,
			ModelNumber:   modelNumber,
		})
	}
	return results
}

// mapCategoryToFriendlyID maps Gemini Web's category slug to Geminiplexer's
// stable public model IDs so existing clients keep working while the hex model
// id and model number come live from Google.
func mapCategoryToFriendlyID(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "argon":
		return "gemini-4-argon"
	case "pro":
		return "gemini-3.1-pro"
	case "flash", "3.8 flash", "gemini flash":
		return "gemini-3.8-flash"
	case "flash-lite", "3.5 flash-lite", "flash lite":
		return "gemini-flash-lite"
	default:
		return ""
	}
}

func stringAtIdx(arr []any, idx int) string {
	if idx >= 0 && idx < len(arr) {
		if s, ok := arr[idx].(string); ok {
			return s
		}
	}
	return ""
}

// joinModelIDs renders model IDs (with hex ModelID) for log diagnostics.
func joinModelIDs(models []GeminiModel) string {
	parts := make([]string, 0, len(models))
	for _, m := range models {
		parts = append(parts, m.ID+"/"+m.ModelID)
	}
	return strings.Join(parts, ", ")
}

// parseBatchEnvelope parses a batchexecute response body, which is prefixed by
// a length line (e.g. "14688\n") followed by one or more JSON arrays, and may
// carry Google's )]}' anti-XSSI prefix. Mirrors gemini-stealth-bridge's
// ParseBatch: skip the length/digit line, then decode the first JSON array.
func parseBatchEnvelope(body string) ([]any, error) {
	s := strings.TrimSpace(body)
	if strings.HasPrefix(s, ")]}'") {
		s = strings.TrimSpace(strings.TrimPrefix(s, ")]}'"))
	}
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		// Skip pure numeric length-prefix lines.
		if _, err := strconv.Atoi(t); err == nil {
			continue
		}
		var arr []any
		if err := json.Unmarshal([]byte(t), &arr); err != nil {
			continue
		}
		return arr, nil
	}
	return nil, fmt.Errorf("no JSON envelope found in batchexecute body")
}

func intAtIdx(arr []any, idx int) (int, bool) {
	if idx >= 0 && idx < len(arr) {
		if f, ok := arr[idx].(float64); ok && float64(int(f)) == f {
			return int(f), true
		}
	}
	return 0, false
}
