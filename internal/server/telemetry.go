package server

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sync"
	"time"
)

type RequestLogItem struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Protocol  string `json:"protocol"` // "OpenAI" or "Anthropic"
	Model     string `json:"model"`
	Account   string `json:"account"`
	LatencyMs int64  `json:"latency_ms"`
	Status    int    `json:"status"`
	ToolCalls bool   `json:"tool_calls"`
}

type AccountStatusItem struct {
	ID             string `json:"id"`
	AuthUser       string `json:"auth_user"`
	IsHealthy      bool   `json:"is_healthy"`
	Status         string `json:"status"` // "active", "cooldown", "quarantined"
	CooldownRemain int    `json:"cooldown_remain_sec"`
	LastUsedAgo    string `json:"last_used_ago"`
}

type TelemetryStats struct {
	mu           sync.RWMutex
	StartTime    time.Time
	TotalReqs    int64
	SuccessReqs  int64
	FailedReqs   int64
	TotalTokens  int64
	RecentLogs   []RequestLogItem
	ModelCounts  map[string]int64
}

var GlobalTelemetry = &TelemetryStats{
	StartTime:   time.Now(),
	RecentLogs:  make([]RequestLogItem, 0, 50),
	ModelCounts: make(map[string]int64),
}

func (t *TelemetryStats) RecordRequest(item RequestLogItem, tokens int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.TotalReqs++
	if item.Status == http.StatusOK {
		t.SuccessReqs++
	} else {
		t.FailedReqs++
	}
	t.TotalTokens += tokens
	t.ModelCounts[item.Model]++

	// Keep last 40 logs
	t.RecentLogs = append([]RequestLogItem{item}, t.RecentLogs...)
	if len(t.RecentLogs) > 40 {
		t.RecentLogs = t.RecentLogs[:40]
	}
}

func (s *Server) handleAPITelemetry(w http.ResponseWriter, r *http.Request) {
	GlobalTelemetry.mu.RLock()
	defer GlobalTelemetry.mu.RUnlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	now := time.Now()
	sessions := s.pool.GetSessions()
	accounts := make([]AccountStatusItem, 0, len(sessions))

	for _, sess := range sessions {
		status := "active"
		remain := 0
		if !sess.IsHealthy {
			status = "unhealthy"
		} else if sess.CooldownUntil.After(now) {
			remain = int(sess.CooldownUntil.Sub(now).Seconds())
			if remain > 120 {
				status = "quarantined"
			} else {
				status = "cooldown"
			}
		}

		lastUsed := "never"
		if !sess.LastUsed.IsZero() {
			diff := time.Since(sess.LastUsed)
			if diff < time.Minute {
				lastUsed = "just now"
			} else {
				lastUsed = diff.Truncate(time.Second).String() + " ago"
			}
		}

		accounts = append(accounts, AccountStatusItem{
			ID:             sess.ID,
			AuthUser:       sess.AuthUser,
			IsHealthy:      sess.IsHealthy,
			Status:         status,
			CooldownRemain: remain,
			LastUsedAgo:    lastUsed,
		})
	}

	payload := map[string]any{
		"uptime_sec":     int(time.Since(GlobalTelemetry.StartTime).Seconds()),
		"ram_alloc_mb":   float64(m.Alloc) / 1024 / 1024,
		"ram_sys_mb":     float64(m.Sys) / 1024 / 1024,
		"goroutines":     runtime.NumGoroutine(),
		"total_requests": GlobalTelemetry.TotalReqs,
		"success_reqs":   GlobalTelemetry.SuccessReqs,
		"failed_reqs":    GlobalTelemetry.FailedReqs,
		"total_tokens":   GlobalTelemetry.TotalTokens,
		"model_counts":   GlobalTelemetry.ModelCounts,
		"accounts":       accounts,
		"recent_logs":    GlobalTelemetry.RecentLogs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}
