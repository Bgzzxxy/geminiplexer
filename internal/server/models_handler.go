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
	models := []ModelCard{
		{ID: "gemini-auto", Object: "model", Created: now, OwnedBy: "google"},
		{ID: "gemini-2.5-pro", Object: "model", Created: now, OwnedBy: "google"},
		{ID: "gemini-2.5-flash", Object: "model", Created: now, OwnedBy: "google"},
		{ID: "gemini-3.1-pro", Object: "model", Created: now, OwnedBy: "google"},
		{ID: "gemini-3.8-flash", Object: "model", Created: now, OwnedBy: "google"},
	}

	resp := ModelListResponse{
		Object: "list",
		Data:   models,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
