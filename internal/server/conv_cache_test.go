package server

import (
	"testing"
	"time"

	"geminiplexer/internal/gemini"
)

func TestConvCacheKeyStability(t *testing.T) {
	msgs := []ChatMessage{{Role: "user", Content: "hi"}}
	k1 := convCacheKey("gemini-flash", msgs, nil)
	k2 := convCacheKey("gemini-flash", msgs, nil)
	if k1 != k2 {
		t.Fatal("key must be deterministic")
	}
	k3 := convCacheKey("gemini-flash", []ChatMessage{{Role: "user", Content: "bye"}}, nil)
	if k1 == k3 {
		t.Fatal("different histories must key differently")
	}
	k4 := convCacheKey("gemini-pro", msgs, nil)
	if k1 == k4 {
		t.Fatal("different models must key differently")
	}
}

func TestConvCachePutGet(t *testing.T) {
	c := newConvCache()
	meta := gemini.ContinuationMeta{CID: "c1", ContextToken: "tok"}
	c.put("k1", convCacheEntry{AccountID: "acc1", Meta: meta})

	e, ok := c.get("k1")
	if !ok || e.AccountID != "acc1" || e.Meta.CID != "c1" {
		t.Fatalf("get failed: %+v ok=%v", e, ok)
	}
}

func TestConvCacheRejectsIncomplete(t *testing.T) {
	c := newConvCache()
	c.put("k1", convCacheEntry{AccountID: "acc1", Meta: gemini.ContinuationMeta{CID: "c1"}}) // no ContextToken
	if _, ok := c.get("k1"); ok {
		t.Fatal("incomplete meta must not be stored")
	}
}

func TestConvCacheTTL(t *testing.T) {
	c := newConvCache()
	c.put("k1", convCacheEntry{AccountID: "acc1", Meta: gemini.ContinuationMeta{CID: "c1", ContextToken: "tok"}})
	// age the entry
	c.entries["k1"] = convCacheEntry{AccountID: "acc1", Meta: gemini.ContinuationMeta{CID: "c1", ContextToken: "tok"}, At: time.Now().Add(-31 * time.Minute)}
	if _, ok := c.get("k1"); ok {
		t.Fatal("expired entry must be evicted")
	}
}