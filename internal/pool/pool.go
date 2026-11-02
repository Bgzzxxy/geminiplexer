package pool

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"geminiplexer/internal/gemini"
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
	return p.AcquireForGroup(ctx, requestedModel, "")
}

// AcquireForGroup acquires a session filtered by group. Empty group = all accounts.
func (p *AccountPool) AcquireForGroup(ctx context.Context, requestedModel string, group string) (*gemini.AccountSession, gemini.GeminiModel, func(status ErrorKind), error) {
	p.mu.Lock()

	now := time.Now()
	var bestSession *gemini.AccountSession

	// Pick healthy session not in cooldown, not busy, with oldest LastUsed (LRU)
	for _, s := range p.sessions {
		if !s.IsHealthy {
			continue
		}
		if s.CooldownUntil.After(now) {
			continue
		}
		if s.IsBusy() {
			continue
		}
		if group != "" && s.Group != group {
			continue
		}
		if bestSession == nil || s.LastUsed.Before(bestSession.LastUsed) {
			bestSession = s
		}
	}

	// Fallback: if all non-cooldown accounts are busy, pick any healthy non-cooldown (will block)
	if bestSession == nil {
		for _, s := range p.sessions {
			if !s.IsHealthy {
				continue
			}
			if s.CooldownUntil.After(now) {
				continue
			}
			if group != "" && s.Group != group {
				continue
			}
			if bestSession == nil || s.LastUsed.Before(bestSession.LastUsed) {
				bestSession = s
			}
		}
	}

	if bestSession == nil {
		p.mu.Unlock()
		return nil, gemini.GeminiModel{}, nil, fmt.Errorf("no healthy accounts available in pool (all in cooldown or uninitialized)")
	}

	// Rate-limit gap check: guarantee at least 2s gap per account
	var gap time.Duration
	if time.Since(bestSession.LastUsed) < 2*time.Second {
		gap = 2*time.Second - time.Since(bestSession.LastUsed)
	}
	bestSession.LastUsed = now.Add(gap)
	p.mu.Unlock()

	if gap > 0 {
		select {
		case <-time.After(gap):
		case <-ctx.Done():
			return nil, gemini.GeminiModel{}, nil, ctx.Err()
		}
	}

	// Acquire concurrency semaphore (blocks if account is busy)
	if err := bestSession.AcquireWithContext(ctx); err != nil {
		return nil, gemini.GeminiModel{}, nil, fmt.Errorf("acquire concurrency semaphore: %w", err)
	}

	model := resolveModel(requestedModel)

	release := func(status ErrorKind) {
		// Always release the concurrency semaphore
		bestSession.Release()

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

func resolveModel(requestedModel string) gemini.GeminiModel {
	switch strings.ToLower(requestedModel) {
	case "gemini-4-argon", "gemini-argon", "argon", "4-argon":
		return gemini.GeminiModel{
			ID:            "gemini-4-argon",
			DisplayName:   "Gemini 4 Argon",
			ModelID:       "95a4c0abcba24364",
			ModelNumber:   4,
			Capacity:      2,
			CapacityField: 12,
		}
	case "gemini-3.1-pro", "gemini-pro", "pro":
		return gemini.GeminiModel{
			ID:            "gemini-3.1-pro",
			DisplayName:   "Gemini 3.1 Pro",
			ModelID:       "e6fa609c3fa255c0",
			ModelNumber:   4,
			Capacity:      2,
			CapacityField: 12,
		}
	case "gemini-3.8-flash", "gemini-flash", "flash":
		return gemini.GeminiModel{
			ID:            "gemini-3.8-flash",
			DisplayName:   "Gemini 3.8 Flash",
			ModelID:       "56fdd199312815e2",
			ModelNumber:   20,
			Capacity:      2,
			CapacityField: 12,
		}
	case "gemini-flash-lite", "flash-lite", "gemini-3.5-flash-lite":
		return gemini.GeminiModel{
			ID:            "gemini-flash-lite",
			DisplayName:   "Gemini Flash-Lite",
			ModelID:       "8c46e95b1a07cecc",
			ModelNumber:   1,
			Capacity:      2,
			CapacityField: 12,
		}
	default:
		return gemini.GeminiModel{
			ID:            "gemini-auto",
			DisplayName:   "Gemini Auto",
			ModelID:       "auto",
			Capacity:      1,
			CapacityField: 12,
			ModelNumber:   1,
		}
	}
}
