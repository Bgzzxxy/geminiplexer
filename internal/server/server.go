package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"geminiplexer/internal/config"
	"geminiplexer/internal/pool"
)

type Server struct {
	cfg           *config.Config
	pool          *pool.AccountPool
	Handler       http.Handler
	ReloadHandler func() error
	keyGroupMap   map[string]string // API key → group mapping
	convs         *convCache        // conversation-keyed continuation cache
}

func NewServer(cfg *config.Config, p *pool.AccountPool, reloadFn ...func() error) *http.Server {
	s := &Server{
		cfg:         cfg,
		pool:        p,
		keyGroupMap: make(map[string]string),
		convs:       newConvCache(),
	}
	if len(reloadFn) > 0 {
		s.ReloadHandler = reloadFn[0]
	}
	// Build key → group lookup
	for _, ak := range cfg.APIKeys {
		if ak.Key != "" {
			s.keyGroupMap[ak.Key] = ak.Group
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/responses", s.handleResponses)
	mux.HandleFunc("/v1/messages", s.handleClaudeMessages)
	mux.HandleFunc("/messages", s.handleClaudeMessages)
	mux.HandleFunc("/claude/v1/messages", s.handleClaudeMessages)
	mux.HandleFunc("/admin/cookies", s.handleAdminCookies)
	mux.HandleFunc("/admin/reload", s.handleAdminReload)
	mux.HandleFunc("/api/telemetry", s.handleAPITelemetry)

	// Health check - always reachable without auth
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Readiness check - reports whether any accounts are healthy
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		healthy := 0
		total := 0
		if p != nil {
			sessions := p.GetSessions()
			total = len(sessions)
			for _, sess := range sessions {
				if sess.IsHealthy {
					healthy++
				}
			}
		}
		status := "ready"
		code := http.StatusOK
		if healthy == 0 {
			status = "not_ready"
			code = http.StatusServiceUnavailable
		}
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{
			"status":           status,
			"total_accounts":   total,
			"healthy_accounts": healthy,
		})
	})

	RegisterDashboard(mux)

	corsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, x-goog-api-key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		mux.ServeHTTP(w, r)
	})

	s.Handler = corsHandler

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	if cfg.Host == "" {
		addr = fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	}

	return &http.Server{
		Addr:    addr,
		Handler: corsHandler,
	}
}

// resolveGroupFromRequest extracts API key from request and returns the mapped group.
// Returns empty string if no key or no group mapping (= access all accounts).
func (s *Server) resolveGroupFromRequest(r *http.Request) string {
	key := ""
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		key = strings.TrimPrefix(auth, "Bearer ")
	}
	if key == "" {
		key = r.Header.Get("x-api-key")
	}
	if key == "" {
		key = r.Header.Get("x-goog-api-key")
	}
	if key == "" {
		return ""
	}
	return s.keyGroupMap[key]
}
