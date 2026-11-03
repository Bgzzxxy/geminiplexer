package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"geminiplexer/internal/transport"
)

const (
	geminiQuotaRPC = "qpEbW"

	// qpEbW payloads request the per-action quota buckets. Action IDs decode to
	// {4: Gemini Pro/Advanced, 11: Gemini Flash} (same map gsb uses).
	quotaPayloadFlash    = "[[[1,11],[2,11],[6,11]]]"
	quotaPayloadAdvanced = "[[[1,4],[6,6],[1,15]]]"

	// QuotaFreshness is how long cached quota data is trusted for preflight
	// decisions before it is considered stale (and therefore not skipped on).
	QuotaFreshness = 10 * time.Minute
)

// quotaActionKind maps the qpEbW action id to a model-family kind.
var quotaActionKind = map[int]string{
	4:  "pro",
	11: "flash",
}

var quotaActionLabel = map[int]string{
	4:  "Gemini Pro",
	11: "Gemini Flash",
}

// QuotaBucket represents one per-family quota status returned by qpEbW.
type QuotaBucket struct {
	Kind        string // "pro" or "flash"
	Label       string
	Remaining   int
	Total       int
	ResetAt     time.Time
	Unlimited   bool
	IsExhausted bool
}

// CheckQuota queries the Gemini Web quota endpoint (qpEbW) for both Flash and
// Pro/Advanced buckets. Returns nil when quota cannot be determined (non-fatal).
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
	if client == nil {
		return nil, fmt.Errorf("session client not initialized")
	}

	var all []QuotaBucket
	for _, payload := range []string{quotaPayloadFlash, quotaPayloadAdvanced} {
		buckets, err := s.doQuotaBatch(ctx, client, endpoint, token, cookieHdr, buildLabel, sessionID, authUser, payload)
		if err != nil {
			return nil, err
		}
		all = append(all, buckets...)
	}

	if len(all) > 0 {
		s.SetCachedQuota(all, time.Now())
		log.Printf("[Quota] %s: %d buckets (%s)", s.ID, len(all), formatQuota(all))
	}
	return all, nil
}

func (s *AccountSession) doQuotaBatch(ctx context.Context, client *http.Client, endpoint, token, cookieHdr, buildLabel, sessionID, authUser, payload string) ([]QuotaBucket, error) {
	query := url.Values{
		"rpcids":      {geminiQuotaRPC},
		"hl":          {"en"},
		"_reqid":      {strconv.FormatInt(s.NextReqID(), 10)},
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
		"f.req": {fmt.Sprintf(`[[[%q,%q,null,"generic"]]]`, geminiQuotaRPC, payload)},
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

	var parsedEnvelope []any
	parsedEnvelope, err = parseBatchEnvelope(bodyStr)
	if err != nil {
		return nil, fmt.Errorf("parse quota envelope: %w", err)
	}

	return parseQuotaFromEnvelope(parsedEnvelope), nil
}

// SetCachedQuota records fresh quota buckets and their sample time, updating the
// earliest reset time used by X-Resets-At.
func (s *AccountSession) SetCachedQuota(buckets []QuotaBucket, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CachedQuota = buckets
	s.QuotaCheckedAt = at
	s.QuotaResetAt = time.Time{}
	for _, b := range buckets {
		if b.IsExhausted && !b.ResetAt.IsZero() {
			if s.QuotaResetAt.IsZero() || b.ResetAt.Before(s.QuotaResetAt) {
				s.QuotaResetAt = b.ResetAt
			}
		}
	}
}

// GetQuotaResetAt returns when the earliest quota bucket resets.
func (s *AccountSession) GetQuotaResetAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.QuotaResetAt
}

// QuotaExhaustedFor reports whether the bucket feeding modelID ("flash*" ->
// Flash, everything else -> Pro) is known exhausted within the freshness window.
// The third return is false when quota is unknown or stale, in which case the
// caller should NOT skip the account (advisory, not strict).
func (s *AccountSession) QuotaExhaustedFor(modelID string, now time.Time, maxAge time.Duration) (bool, time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.QuotaCheckedAt.IsZero() || now.Sub(s.QuotaCheckedAt) > maxAge {
		return false, s.QuotaResetAt, false
	}
	kind := quotaKindForModel(modelID)
	for _, b := range s.CachedQuota {
		if b.Kind == kind && b.IsExhausted {
			return true, b.ResetAt, true
		}
	}
	return false, s.QuotaResetAt, true
}

func quotaKindForModel(modelID string) string {
	if strings.Contains(strings.ToLower(modelID), "flash") {
		return "flash"
	}
	return "pro"
}

func parseQuotaFromEnvelope(envelope []any) []QuotaBucket {
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
		return extractQuotaBuckets(payload)
	}
	return nil
}

// extractQuotaBuckets parses the precise qpEbW structure: payload = [ items ],
// where each item = [idList, ?, usagePct, resetArr, total, remaining] and
// idList[1] is the action id (4=Pro, 11=Flash).
func extractQuotaBuckets(payload []any) []QuotaBucket {
	if len(payload) == 0 {
		return nil
	}
	items, ok := payload[0].([]any)
	if !ok {
		return nil
	}
	var out []QuotaBucket
	for _, itemAny := range items {
		item, ok := itemAny.([]any)
		if !ok || len(item) < 6 {
			continue
		}
		idList, _ := item[0].([]any)
		if len(idList) < 2 {
			continue
		}
		actionID, ok := toInt(idList[1])
		if !ok {
			continue
		}
		kind, known := quotaActionKind[actionID]
		if !known {
			continue
		}
		total, _ := toInt(item[4])
		remaining, _ := toInt(item[5])
		var resetAt time.Time
		if rArr, ok := item[3].([]any); ok && len(rArr) > 0 {
			if ts, ok := toInt64(rArr[0]); ok && ts > 0 {
				resetAt = time.Unix(ts, 0)
			}
		}
		unlimited := total == 0 && remaining == 0
		out = append(out, QuotaBucket{
			Kind:        kind,
			Label:       quotaActionLabel[actionID],
			Remaining:   remaining,
			Total:       total,
			ResetAt:     resetAt,
			Unlimited:   unlimited,
			IsExhausted: !unlimited && remaining <= 0,
		})
	}
	return out
}

func formatQuota(buckets []QuotaBucket) string {
	parts := make([]string, 0, len(buckets))
	for _, b := range buckets {
		parts = append(parts, fmt.Sprintf("%s=%d/%d", b.Label, b.Remaining, b.Total))
	}
	return strings.Join(parts, ", ")
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
