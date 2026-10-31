package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"geminiplexer/internal/config"
	"geminiplexer/internal/gemini"
)

type AdminCookieUpdateRequest struct {
	ID       string `json:"id"`
	Cookies  string `json:"cookies"`
	AuthUser string `json:"auth_user,omitempty"`
}

func (s *Server) handleAdminCookies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.cfg.AdminKey == "" {
		http.Error(w, "Admin endpoint disabled (no admin_key set)", http.StatusForbidden)
		return
	}

	authHdr := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHdr, "Bearer ") {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(authHdr, "Bearer ")
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AdminKey)) != 1 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	var req AdminCookieUpdateRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.ID == "" || req.Cookies == "" {
		http.Error(w, "id and cookies are required", http.StatusBadRequest)
		return
	}

	if s.pool != nil {
		sessions := s.pool.GetSessions()
		var target *gemini.AccountSession
		for _, sess := range sessions {
			if sess.ID == req.ID {
				target = sess
				break
			}
		}

		if target != nil {
			target.UpdateCookies(req.Cookies)
			// Re-initialize session in background
			go func() {
				_ = target.Init(context.Background())
			}()
		} else {
			// Add new account session to pool
			newSess := gemini.NewAccountSession(config.AccountConfig{
				ID:       req.ID,
				Cookies:  req.Cookies,
				AuthUser: req.AuthUser,
			})
			go func() {
				_ = newSess.Init(context.Background())
			}()
			s.pool.AddSession(newSess)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(fmt.Sprintf(`{"status":"ok","message":"account %s updated"}`, req.ID)))
}

func (s *Server) handleAdminReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.cfg.AdminKey != "" {
		authHdr := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHdr, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(authHdr, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AdminKey)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	if s.ReloadHandler != nil {
		if err := s.ReloadHandler(); err != nil {
			http.Error(w, fmt.Sprintf("Reload failed: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","message":"configuration reloaded"}`))
}
