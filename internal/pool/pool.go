package pool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gemini-to-api/internal/gemini"
)

type ErrorKind int

const (
	StatusSuccess ErrorKind = iota
	StatusRateLimited
	StatusAuthError
	StatusModelDowngraded
)

type AccountPool struct {
	mu       sync.Mutex
	sessions []*gemini.AccountSession
}

func NewAccountPool(sessions []*gemini.AccountSession) *AccountPool {
	return &AccountPool{
		sessions: sessions,
	}
}

func (p *AccountPool) AddSession(sess *gemini.AccountSession) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessions = append(p.sessions, sess)
}

func (p *AccountPool) GetSessions() []*gemini.AccountSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	copied := make([]*gemini.AccountSession, len(p.sessions))
	copy(copied, p.sessions)
	return copied
}

func (p *AccountPool) Acquire(ctx context.Context, requestedModel string) (*gemini.AccountSession, gemini.GeminiModel, func(status ErrorKind), error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	var bestSession *gemini.AccountSession

	// Pick healthy session not in cooldown with oldest LastUsed (LRU)
	for _, s := range p.sessions {
		if !s.IsHealthy {
			continue
		}
		if s.CooldownUntil.After(now) {
			continue
		}
		if bestSession == nil || s.LastUsed.Before(bestSession.LastUsed) {
			bestSession = s
		}
	}

	if bestSession == nil {
		return nil, gemini.GeminiModel{}, nil, fmt.Errorf("no healthy accounts available in pool (all in cooldown or uninitialized)")
	}

	// Rate-limit gap check: guarantee at least 2s gap per account
	if time.Since(bestSession.LastUsed) < 2*time.Second {
		gap := 2*time.Second - time.Since(bestSession.LastUsed)
		time.Sleep(gap)
	}

	bestSession.LastUsed = time.Now()

	model := gemini.GeminiModel{
		ID:            "gemini-auto",
		DisplayName:   "Gemini Auto",
		ModelID:       "auto",
		Capacity:      1,
		CapacityField: 12,
		ModelNumber:   1,
	}
	if requestedModel != "" && requestedModel != "gemini-auto" {
		model.ID = requestedModel
		model.DisplayName = requestedModel
	}

	release := func(status ErrorKind) {
		p.mu.Lock()
		defer p.mu.Unlock()

		switch status {
		case StatusSuccess:
			bestSession.IsHealthy = true
		case StatusRateLimited:
			bestSession.CooldownUntil = time.Now().Add(30 * time.Minute)
		case StatusAuthError:
			bestSession.IsHealthy = false
		case StatusModelDowngraded:
			// Cooldown specifically on flagship for 15m
			bestSession.CooldownUntil = time.Now().Add(15 * time.Minute)
		}
	}

	return bestSession, model, release, nil
}
