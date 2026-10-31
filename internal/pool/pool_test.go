package pool_test

import (
	"context"
	"testing"
	"geminiplexer/internal/config"
	"geminiplexer/internal/gemini"
	"geminiplexer/internal/pool"
)

func TestPoolLRUSelection(t *testing.T) {
	s1 := gemini.NewAccountSession(config.AccountConfig{ID: "acc1"})
	s2 := gemini.NewAccountSession(config.AccountConfig{ID: "acc2"})
	p := pool.NewAccountPool([]*gemini.AccountSession{s1, s2})

	acc1, _, release1, err := p.Acquire(context.Background(), "gemini-auto")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if acc1.ID != "acc1" {
		t.Errorf("expected acc1 on first acquire, got %s", acc1.ID)
	}
	release1(pool.StatusSuccess)

	acc2, _, release2, err := p.Acquire(context.Background(), "gemini-auto")
	if err != nil {
		t.Fatalf("Acquire 2 failed: %v", err)
	}
	if acc2.ID != "acc2" {
		t.Errorf("expected acc2 on second acquire via LRU, got %s", acc2.ID)
	}
	release2(pool.StatusSuccess)
}

func TestPoolQuarantineOnRateLimit(t *testing.T) {
	s1 := gemini.NewAccountSession(config.AccountConfig{ID: "acc1"})
	s2 := gemini.NewAccountSession(config.AccountConfig{ID: "acc2"})
	p := pool.NewAccountPool([]*gemini.AccountSession{s1, s2})

	_, _, release, _ := p.Acquire(context.Background(), "gemini-auto")
	release(pool.StatusRateLimited) // puts acc1 in cooldown

	// Next acquire should bypass acc1 and give acc2
	accNext, _, releaseNext, err := p.Acquire(context.Background(), "gemini-auto")
	if err != nil {
		t.Fatalf("Acquire next failed: %v", err)
	}
	if accNext.ID != "acc2" {
		t.Errorf("expected acc2 after acc1 rate limited, got %s", accNext.ID)
	}
	releaseNext(pool.StatusSuccess)
}
