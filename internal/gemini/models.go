package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("X-Same-Domain", "1")
	req.Header.Set("Cookie", cookieHdr)

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

	var parsedEnvelope []any
	if err := json.Unmarshal([]byte(bodyStr), &parsedEnvelope); err != nil {
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
	// Check typical nested array structure: [null, null, null, [[null, null, [[...]]]]]
	if len(payload) > 3 {
		if outerArr, ok := payload[3].([]any); ok && len(outerArr) > 0 {
			if subArr, ok := outerArr[0].([]any); ok && len(subArr) > 2 {
				if modelList, ok := subArr[2].([]any); ok {
					for _, m := range modelList {
						if fields, ok := m.([]any); ok && len(fields) >= 13 {
							modelName, _ := fields[1].(string)
							modelID, _ := fields[12].(string)
							capVal := 1
							if c, ok := fields[6].(float64); ok {
								capVal = int(c)
							}
							numVal := 1
							if n, ok := fields[5].(float64); ok {
								numVal = int(n)
							}

							if modelName != "" {
								results = append(results, GeminiModel{
									ID:            modelName,
									DisplayName:   modelName,
									ModelID:       modelID,
									Capacity:      capVal,
									CapacityField: 12,
									ModelNumber:   numVal,
								})
							}
						}
					}
				}
			}
		}
	}
	return results
}
