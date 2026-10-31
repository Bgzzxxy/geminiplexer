package server

import (
	"fmt"
	"net/http"

	"gemini-relay/internal/config"
	"gemini-relay/internal/pool"
)

type Server struct {
	cfg           *config.Config
	pool          *pool.AccountPool
	Handler       http.Handler
	ReloadHandler func() error
}

func NewServer(cfg *config.Config, p *pool.AccountPool, reloadFn ...func() error) *http.Server {
	s := &Server{
		cfg:  cfg,
		pool: p,
	}
	if len(reloadFn) > 0 {
		s.ReloadHandler = reloadFn[0]
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/messages", s.handleClaudeMessages)
	mux.HandleFunc("/messages", s.handleClaudeMessages)
	mux.HandleFunc("/claude/v1/messages", s.handleClaudeMessages)
	mux.HandleFunc("/admin/cookies", s.handleAdminCookies)
	mux.HandleFunc("/admin/reload", s.handleAdminReload)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	corsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
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
