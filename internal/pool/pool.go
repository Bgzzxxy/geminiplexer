package pool

import (
	"context"
	"fmt"
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
	return p.AcquireForGroupWithAffinity(ctx, requestedModel, group, "")
}

// AcquireForGroupWithAffinity prefers the named account (a conversation-owner
// hint) when it is healthy and eligible, so multi-turn conversations stay on
// the Gemini session that holds their continuation metadata. Falls back to
// normal LRU selection otherwise.
func (p *AccountPool) AcquireForGroupWithAffinity(ctx context.Context, requestedModel string, group string, preferAccountID string) (*gemini.AccountSession, gemini.GeminiModel, func(status ErrorKind), error) {
	p.mu.Lock()

	now := time.Now()
	var bestSession *gemini.AccountSession

	eligible := func(s *gemini.AccountSession) bool {
		if !s.IsHealthy || s.CooldownUntil.After(now) {
			return false
		}
		if group != "" && s.Group != group {
			return false
		}
		// Quota preflight: skip accounts whose requested model family is known
		// exhausted within the freshness window, so we don't burn a request that
		// will be silently downgraded (or rejected) upstream.
		if exhausted, _, known := s.QuotaExhaustedFor(requestedModel, now, gemini.QuotaFreshness); exhausted && known {
			return false
		}
		return true
	}

	// Pass 1: prefer an eligible, non-busy conversation-owner account.
	if preferAccountID != "" {
		for _, s := range p.sessions {
			if s.ID == preferAccountID && eligible(s) && !s.IsBusy() {
				bestSession = s
				break
			}
		}
	}

	// Pass 2: LRU over eligible, non-busy accounts.
	if bestSession == nil {
		for _, s := range p.sessions {
			if !eligible(s) || s.IsBusy() {
				continue
			}
			if bestSession == nil || s.LastUsed.Before(bestSession.LastUsed) {
				bestSession = s
			}
		}
	}

	// Pass 3: affinity even if busy (will block on the semaphore).
	if bestSession == nil && preferAccountID != "" {
		for _, s := range p.sessions {
			if s.ID == preferAccountID && eligible(s) {
				bestSession = s
				break
			}
		}
	}

	// Pass 4: any eligible account (will block).
	if bestSession == nil {
		for _, s := range p.sessions {
			if !eligible(s) {
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
	// Correct ModelNumber values (md17) from the live otAQ7b registry:
	//   argon=8, pro=3, flash=1, flash-lite=6.
	// The previous hardcoded map used md9 values (4/4/20/1), which made Gemini
	// Web unable to route the request and silently downgraded every call to
	// Flash-Lite.
	return gemini.DefaultModel(gemini.CanonicalModelID(requestedModel))
}
