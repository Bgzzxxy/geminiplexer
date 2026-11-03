package pool

import (
	"context"
	"testing"
	"time"

	"geminiplexer/internal/config"
	"geminiplexer/internal/gemini"
)

func TestAcquireSkipsQuotaExhaustedAccount(t *testing.T) {
	a1 := gemini.NewAccountSession(config.AccountConfig{ID: "a1", Cookies: "__Secure-1PSID=x"})
	a2 := gemini.NewAccountSession(config.AccountConfig{ID: "a2", Cookies: "__Secure-1PSID=x"})

	now := time.Now()
	// a1's pro-family quota is freshly exhausted -> must be skipped for argon.
	a1.SetCachedQuota([]gemini.QuotaBucket{
		{Kind: "pro", IsExhausted: true, ResetAt: now.Add(time.Hour)},
	}, now)

	p := NewAccountPool([]*gemini.AccountSession{a1, a2})

	sess, _, release, err := p.Acquire(context.Background(), "gemini-4-argon")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	defer release(StatusSuccess)
	if sess.ID != "a2" {
		t.Fatalf("should skip quota-exhausted a1 and pick a2, got %s", sess.ID)
	}
}

func TestAcquireAllExhaustedFailsOpen(t *testing.T) {
	a1 := gemini.NewAccountSession(config.AccountConfig{ID: "a1", Cookies: "__Secure-1PSID=x"})
	now := time.Now()
	a1.SetCachedQuota([]gemini.QuotaBucket{
		{Kind: "pro", IsExhausted: true, ResetAt: now.Add(time.Hour)},
		{Kind: "flash", IsExhausted: true, ResetAt: now.Add(time.Hour)},
	}, now)

	p := NewAccountPool([]*gemini.AccountSession{a1})

	// All buckets exhausted -> no eligible account -> acquire errors out.
	_, _, _, err := p.Acquire(context.Background(), "gemini-4-argon")
	if err == nil {
		t.Fatal("expected no healthy accounts error when all quota exhausted")
	}
}