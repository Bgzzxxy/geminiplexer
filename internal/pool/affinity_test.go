package pool

import (
	"context"
	"testing"
	"time"

	"geminiplexer/internal/config"
	"geminiplexer/internal/gemini"
)

func TestAcquireAffinityPrefersOwner(t *testing.T) {
	a1 := gemini.NewAccountSession(config.AccountConfig{ID: "a1", Cookies: "__Secure-1PSID=x"})
	a2 := gemini.NewAccountSession(config.AccountConfig{ID: "a2", Cookies: "__Secure-1PSID=x"})
	// Make a1 the LRU (older LastUsed) so a tie would pick a1 by default.
	a1.LastUsed = a1.LastUsed.Add(-time.Hour)
	p := NewAccountPool([]*gemini.AccountSession{a1, a2})

	sess, _, release, err := p.AcquireForGroupWithAffinity(context.Background(), "gemini-auto", "", "a2")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	defer release(StatusSuccess)
	if sess.ID != "a2" {
		t.Fatalf("affinity should prefer a2, got %s", sess.ID)
	}
}

func TestAcquireFallsBackWhenAffinityUnavailable(t *testing.T) {
	a1 := gemini.NewAccountSession(config.AccountConfig{ID: "a1", Cookies: "__Secure-1PSID=x"})
	p := NewAccountPool([]*gemini.AccountSession{a1})

	// Affinity points to a non-existent account; must fall back to a1.
	sess, _, release, err := p.AcquireForGroupWithAffinity(context.Background(), "gemini-auto", "", "ghost")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	defer release(StatusSuccess)
	if sess.ID != "a1" {
		t.Fatalf("fallback should pick a1, got %s", sess.ID)
	}
}