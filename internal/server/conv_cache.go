package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"geminiplexer/internal/gemini"
)

const (
	convCacheTTL        = 30 * time.Minute
	convCacheMaxEntries = 256
)

// convCacheEntry binds Gemini Web continuation metadata to the account that
// owns the conversation, so a follow-up turn resumes the right session even
// when the pool rotates across accounts.
type convCacheEntry struct {
	AccountID string
	Meta      gemini.ContinuationMeta
	At        time.Time
}

type convCache struct {
	mu      sync.Mutex
	entries map[string]convCacheEntry
	order   []string // LRU
}

func newConvCache() *convCache {
	return &convCache{entries: make(map[string]convCacheEntry)}
}

// key hashes the model + full message history + tools into a stable identity
// for a conversation, mirroring gemini-stealth-bridge's HashHistoryWithTools.
func convCacheKey(model string, messages []ChatMessage, tools any) string {
	h := sha256.New()
	h.Write([]byte(model))
	enc := json.NewEncoder(h)
	_ = enc.Encode(messages)
	_ = enc.Encode(tools)
	return hex.EncodeToString(h.Sum(nil))
}

// lookupPrefix finds continuation metadata for a conversation by trying the
// longest matching prefix of the message history. A turn is stored under
// (history + assistant reply), so the next turn (history + assistant reply +
// new user message) matches that longer prefix and resumes the conversation.
func (c *convCache) lookupPrefix(model string, messages []ChatMessage, tools any) (convCacheEntry, bool) {
	// Try longest prefix first (full history), then shorter prefixes.
	for n := len(messages); n >= 1; n-- {
		key := convCacheKey(model, messages[:n], tools)
		if e, ok := c.get(key); ok {
			return e, true
		}
	}
	return convCacheEntry{}, false
}

// nextKey computes the key to store after this turn completes: the current
// history plus the assistant reply, so the following turn matches this prefix.
func nextConvKey(model string, messages []ChatMessage, tools any, assistantContent string) string {
	next := make([]ChatMessage, 0, len(messages)+1)
	next = append(next, messages...)
	next = append(next, ChatMessage{Role: "assistant", Content: assistantContent})
	return convCacheKey(model, next, tools)
}

func (c *convCache) get(key string) (convCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return convCacheEntry{}, false
	}
	if time.Since(e.At) >= convCacheTTL {
		c.deleteLocked(key)
		return convCacheEntry{}, false
	}
	c.promoteLocked(key)
	return e, true
}

func (c *convCache) put(key string, e convCacheEntry) {
	if e.AccountID == "" || e.Meta.Empty() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e.At = time.Now()
	if _, ok := c.entries[key]; !ok && len(c.order) >= convCacheMaxEntries {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
	c.entries[key] = e
	c.promoteLocked(key)
}

func (c *convCache) promoteLocked(key string) {
	for i, k := range c.order {
		if k == key {
			copy(c.order[i:], c.order[i+1:])
			c.order[len(c.order)-1] = key
			return
		}
	}
	c.order = append(c.order, key)
}

func (c *convCache) deleteLocked(key string) {
	delete(c.entries, key)
	for i, k := range c.order {
		if k == key {
			copy(c.order[i:], c.order[i+1:])
			c.order = c.order[:len(c.order)-1]
			return
		}
	}
}