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
	"time"

	"geminiplexer/internal/transport"
)

const (
	geminiQuotaRPC = "qpEbW"
)

// QuotaBucket represents a per-model quota status.
type QuotaBucket struct {
	ModelID    string
	ModelName  string
	Remaining  int
	Total      int
	ResetAt    time.Time
	IsExhausted bool
}

// CheckQuota queries the Gemini Web quota endpoint via qpEbW batchexecute RPC.
// Returns nil if quota cannot be determined (non-fatal).
func (s *AccountSession) CheckQuota(ctx context.Context) ([]QuotaBucket, error) {
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
		return nil, fmt.Errorf("session token not initialized")
	}

	query := url.Values{
		"rpcids":      {geminiQuotaRPC},
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
		"f.req": {`[[["` + geminiQuotaRPC + `","[]",null,"generic"]]]`},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create quota request: %w", err)
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
		return nil, fmt.Errorf("execute quota request: %w", err)
	}
	defer resp.Body.Close()

	s.absorbSetCookies(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("quota check returned HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read quota body: %w", err)
	}

	bodyStr := strings.TrimSpace(string(bodyBytes))
	if strings.HasPrefix(bodyStr, ")]}'") {
		bodyStr = strings.TrimSpace(strings.TrimPrefix(bodyStr, ")]}'"))
	}

	var parsedEnvelope []any
	parsedEnvelope, err = parseBatchEnvelope(bodyStr)
	if err != nil {
		return nil, fmt.Errorf("parse quota envelope: %w", err)
	}

	buckets := parseQuotaFromEnvelope(parsedEnvelope)
	log.Printf("[Quota] %s: found %d quota buckets", s.ID, len(buckets))

	// Update session quota reset time
	for _, b := range buckets {
		if b.IsExhausted && !b.ResetAt.IsZero() {
			s.mu.Lock()
			if s.QuotaResetAt.IsZero() || b.ResetAt.Before(s.QuotaResetAt) {
				s.QuotaResetAt = b.ResetAt
			}
			s.mu.Unlock()
		}
	}

	return buckets, nil
}

// GetQuotaResetAt returns when the earliest quota bucket resets.
func (s *AccountSession) GetQuotaResetAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.QuotaResetAt
}

func parseQuotaFromEnvelope(envelope []any) []QuotaBucket {
	var buckets []QuotaBucket
	for _, item := range envelope {
		itemArr, ok := item.([]any)
		if !ok || len(itemArr) < 3 {
			continue
		}
		rpcName, _ := itemArr[1].(string)
		if rpcName != geminiQuotaRPC {
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

		buckets = extractQuotaBuckets(payload)
	}
	return buckets
}

func extractQuotaBuckets(payload []any) []QuotaBucket {
	var results []QuotaBucket

	// Walk nested arrays looking for quota data structures.
	// Quota buckets contain: [model_name, remaining, total, reset_timestamp_ms, ...]
	walkForQuota(payload, &results, 0)
	return results
}

func walkForQuota(v any, results *[]QuotaBucket, depth int) {
	if depth > 10 {
		return
	}
	arr, ok := v.([]any)
	if !ok {
		return
	}

	// Try to detect a quota bucket: array with a string (model name), then numbers
	if len(arr) >= 4 {
		name, nameOk := arr[0].(string)
		remaining, remOk := toInt(arr[1])
		total, totOk := toInt(arr[2])
		if nameOk && remOk && totOk && name != "" && total > 0 {
			bucket := QuotaBucket{
				ModelName:   name,
				Remaining:   remaining,
				Total:       total,
				IsExhausted: remaining <= 0,
			}
			// Check if arr[3] is a reset timestamp (milliseconds)
			if resetMs, ok := toInt64(arr[3]); ok && resetMs > 1000000000000 {
				bucket.ResetAt = time.UnixMilli(resetMs)
			}
			*results = append(*results, bucket)
			return
		}
	}

	for _, elem := range arr {
		walkForQuota(elem, results, depth+1)
	}
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}
