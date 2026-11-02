package server

import (
	"encoding/json"
	"net/http"
	"time"
)

type ModelCard struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelListResponse struct {
	Object string      `json:"object"`
	Data   []ModelCard `json:"data"`
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	now := time.Now().Unix()

	// Collect models dynamically from all healthy sessions
	seen := make(map[string]bool)
	var models []ModelCard

	if s.pool != nil {
		sessions := s.pool.GetSessions()
		for _, sess := range sessions {
			if !sess.IsHealthy {
				continue
			}
			for _, m := range sess.CachedModels {
				if !seen[m.ID] {
					seen[m.ID] = true
					models = append(models, ModelCard{
						ID:      m.ID,
						Object:  "model",
						Created: now,
						OwnedBy: "google",
					})
				}
			}
		}
	}

	// Always include well-known aliases as fallback
	wellKnown := []string{
		"gemini-4-argon",
		"gemini-3.1-pro",
		"gemini-3.8-flash",
		"gemini-flash-lite",
		"gemini-auto",
	}
	for _, id := range wellKnown {
		if !seen[id] {
			seen[id] = true
			models = append(models, ModelCard{
				ID:      id,
				Object:  "model",
				Created: now,
				OwnedBy: "google",
			})
		}
	}

	resp := ModelListResponse{
		Object: "list",
		Data:   models,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
