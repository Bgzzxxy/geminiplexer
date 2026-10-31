# Gemini Web to API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a native bare-metal Go service that converts Gemini Web into a high-performance, OpenAI-compatible API with multi-account pooling, anti-bot defenses, and tool calling for Hermes Agent.

**Architecture:** A lightweight Go HTTP server exposing OpenAI-compatible `/v1/chat/completions` and `/v1/models` endpoints. It features an in-memory account pool managing session handshakes, jittered `RotateCookies`, quota downgrade detection, and an OpenAI tool bridge that translates function calls to structured prompt schemas and SSE deltas.

**Tech Stack:** Go 1.23+ (`/home/bagus/.local/go/bin/go`), standard library `net/http` with minimal dependencies (`gopkg.in/yaml.v3`, `github.com/google/uuid`), Linux bare-metal.

**Spec:** `docs/superpowers/specs/2026-10-31-gemini-web-to-api-design.md`

## Global Constraints

- **Language & Runtime:** Go 1.23+ compiled native Linux binary (`/home/bagus/.local/go/bin/go`).
- **Zero Docker:** 100% bare-metal execution, zero container dependency.
- **Protocol:** Strict OpenAI API schema compatibility for `/v1/chat/completions` and `/v1/models`.
- **Stateless Execution:** Enforce `isTemporary: true` (`[45]=1`) on all Gemini generation requests; message history flattened per request.
- **Anti-Bot Defenses:** Minimum 2–5s gap per account, jittered 12–24h `RotateCookies`, automatic 30m quarantine on 429 / CAPTCHA.
- **Clean Streaming Output:** Strip web UI artifacts (`[cite: N]`, `[cite_start]`, code fence tags) before emitting SSE chunks.

## Review Focus

1. **Model downgrade during active stream:** If Gemini silently downgrades from Pro to Flash/lower tier, detect in initial chunk, abort, quarantine model on that account, and failover before emitting output to client.
2. **Concurrent cookie rotation collision:** Mutex-locked rotation per account preventing race conditions and stale `__Secure-1PSIDTS` overwrite.
3. **Malformed tool JSON from model:** Tool bridge must catch invalid JSON arguments from Gemini web text and execute a single repair attempt before falling back.
4. **Google RPC chunk fragmentation:** Buffer and assemble partial SSE chunk frames across packet boundaries without dropping tokens.
5. **Session expiration recovery:** If `SNlM0e` returns 401/403, mark account unhealthy, trigger session re-init, and route in-flight request to next healthy account.

---

### Task 1: Project Scaffolding & Configuration Manager

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `type Config struct`, `type AccountConfig struct`, `func LoadConfig(path string) (*Config, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/config/config_test.go
package config_test

import (
	"os"
	"testing"
	"gemini-to-api/internal/config"
)

func test_load_valid_config(t *testing.T) {
	yamlData := `
port: 8787
admin_key: "secret123"
accounts:
  - id: "acc1"
    cookies: "__Secure-1PSID=sid1; __Secure-1PSIDTS=ts1"
    auth_user: "0"
`
	tmp, _ := os.CreateTemp("", "cfg-*.yaml")
	defer os.Remove(tmp.Name())
	tmp.WriteString(yamlData)
	tmp.Close()

	cfg, err := config.LoadConfig(tmp.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 8787 || len(cfg.Accounts) != 1 || cfg.Accounts[0].ID != "acc1" {
		t.Errorf("unexpected config content: %+v", cfg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/home/bagus/.local/go/bin/go test ./internal/config/... -v`
Expected: FAIL (package does not exist)

- [ ] **Step 3: Implement `LoadConfig(path string) (*Config, error)` in `internal/config/config.go`**

Initialize `go.mod` with module name `gemini-to-api`, add `gopkg.in/yaml.v3`, and implement YAML parser reading port, rate limits, admin key, and account list with cookie validation.

- [ ] **Step 4: Run test to verify it passes**

Run: `/home/bagus/.local/go/bin/go test ./internal/config/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/config/
git commit -m "feat(config): add configuration structures and YAML loader"
```

---

### Task 2: Account Session Lifecycle & Google Cookie Rotator

**Files:**
- Create: `internal/gemini/session.go`
- Create: `internal/gemini/rotate.go`
- Test: `internal/gemini/session_test.go`

**Interfaces:**
- Consumes: `config.AccountConfig`
- Produces: `type AccountSession struct`, `func NewAccountSession(cfg config.AccountConfig) *AccountSession`, `func (s *AccountSession) Init(ctx context.Context) error`, `func (s *AccountSession) RotateCookies(ctx context.Context) error`

- [ ] **Step 1: Write the failing test**

```go
// internal/gemini/session_test.go
package gemini_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"gemini-to-api/internal/config"
	"gemini-to-api/internal/gemini"
)

func TestSessionInitExtractsSNlM0e(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><script>"SNlM0e":"test_csrf_token_123","bl":"boq_assistant_20261031"</script></html>`))
	}))
	defer ts.Close()

	sess := gemini.NewAccountSession(config.AccountConfig{
		ID: "acc1",
		Cookies: "__Secure-1PSID=test_psid; __Secure-1PSIDTS=test_ts",
	})
	sess.SetEndpointOverride(ts.URL)

	if err := sess.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if sess.GetToken() != "test_csrf_token_123" {
		t.Errorf("expected token test_csrf_token_123, got %s", sess.GetToken())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/home/bagus/.local/go/bin/go test ./internal/gemini/... -v`
Expected: FAIL (package does not exist)

- [ ] **Step 3: Implement `session.go` and `rotate.go`**

Implement `Init` to request `gemini.google.com/app`, parse `SNlM0e`, `bl`, and `f.sid` via regex. Implement `RotateCookies` sending `[000,"-0000000000000000000"]` to `https://accounts.google.com/RotateCookies` and updating `__Secure-1PSIDTS` with a thread-safe sync.RWMutex.

- [ ] **Step 4: Run test to verify it passes**

Run: `/home/bagus/.local/go/bin/go test ./internal/gemini/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gemini/session.go internal/gemini/rotate.go internal/gemini/session_test.go
git commit -m "feat(gemini): implement session initialization and cookie rotation"
```

---

### Task 3: Model Discovery & Dynamic Registry

**Files:**
- Create: `internal/gemini/models.go`
- Test: `internal/gemini/models_test.go`

**Interfaces:**
- Consumes: `AccountSession`
- Produces: `type GeminiModel struct`, `func (s *AccountSession) FetchModels(ctx context.Context) ([]GeminiModel, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/gemini/models_test.go
package gemini_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"gemini-to-api/internal/config"
	"gemini-to-api/internal/gemini"
)

func TestFetchModelsRPC(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("rpcids") != "otAQ7b" {
			t.Errorf("expected rpcids otAQ7b, got %s", r.URL.Query().Get("rpcids"))
		}
		w.Write([]byte(`[[["wrb.fr","otAQ7b","[null,null,null,[[null,null,[[null,\"gemini-auto\",null,null,null,1,12,null,null,null,null,null,\"auto_model_id\"]]]]]"]]]`))
	}))
	defer ts.Close()

	sess := gemini.NewAccountSession(config.AccountConfig{ID: "acc1", Cookies: "__Secure-1PSID=sid; __Secure-1PSIDTS=ts"})
	sess.SetEndpointOverride(ts.URL)
	sess.SetToken("fake_token")

	models, err := sess.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels failed: %v", err)
	}
	if len(models) == 0 || models[0].ID != "gemini-auto" {
		t.Errorf("unexpected models: %+v", models)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/home/bagus/.local/go/bin/go test ./internal/gemini/models_test.go -v`
Expected: FAIL

- [ ] **Step 3: Implement `FetchModels` in `internal/gemini/models.go`**

Implement batchexecute RPC request with `rpcids=otAQ7b`, decode response envelope, extract model metadata, capacity fields, and register `gemini-auto` as default alias.

- [ ] **Step 4: Run test to verify it passes**

Run: `/home/bagus/.local/go/bin/go test ./internal/gemini/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gemini/models.go internal/gemini/models_test.go
git commit -m "feat(gemini): implement otAQ7b batchexecute model discovery"
```

---

### Task 4: StreamGenerate Client & Output Sanitizer

**Files:**
- Create: `internal/gemini/generate.go`
- Create: `internal/gemini/sanitizer.go`
- Test: `internal/gemini/generate_test.go`
- Test: `internal/gemini/sanitizer_test.go`

**Interfaces:**
- Consumes: `AccountSession`, `GeminiModel`
- Produces: `type GenerateChunk struct { Text, Reasoning, ModelID string; IsDone bool }`, `func (s *AccountSession) StreamGenerate(ctx context.Context, prompt string, model GeminiModel, thinkingLevel int) (<-chan GenerateChunk, error)`, `func SanitizeText(input string) string`

- [ ] **Step 1: Write the failing test**

```go
// internal/gemini/sanitizer_test.go
package gemini_test

import (
	"testing"
	"gemini-to-api/internal/gemini"
)

func TestSanitizeTextStripsCitations(t *testing.T) {
	raw := "Here is the answer [cite: 1, 2] with code [cite_start] snippet?code_reference"
	expected := "Here is the answer with code snippet"
	got := gemini.SanitizeText(raw)
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/home/bagus/.local/go/bin/go test ./internal/gemini/sanitizer_test.go -v`
Expected: FAIL

- [ ] **Step 3: Implement `sanitizer.go` and `generate.go`**

Implement `SanitizeText` to strip regex patterns `\[cite: [0-9, ]+\]`, `\[cite_start\]`, and code fence query suffixes. Implement `StreamGenerate` sending form `f.req` (81-element array, `[45]=1` for temporary chat), headers `x-goog-ext-525001261-jspb`, reading chunk stream, parsing candidate text and thought summaries into `GenerateChunk` channel.

- [ ] **Step 4: Run test to verify it passes**

Run: `/home/bagus/.local/go/bin/go test ./internal/gemini/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gemini/generate.go internal/gemini/sanitizer.go internal/gemini/generate_test.go internal/gemini/sanitizer_test.go
git commit -m "feat(gemini): implement StreamGenerate SSE decoder and output sanitizer"
```

---

### Task 5: Tool Bridge Subsystem & JSON Repair

**Files:**
- Create: `internal/toolbridge/bridge.go`
- Create: `internal/toolbridge/repair.go`
- Test: `internal/toolbridge/bridge_test.go`

**Interfaces:**
- Produces: `type OpenAITool struct`, `type ToolCall struct`, `func InjectTools(systemPrompt string, tools []OpenAITool) string`, `func ParseToolCalls(output string) ([]ToolCall, string, bool)`

- [ ] **Step 1: Write the failing test**

```go
// internal/toolbridge/bridge_test.go
package toolbridge_test

import (
	"testing"
	"gemini-to-api/internal/toolbridge"
)

func TestParseToolCallsValid(t *testing.T) {
	response := "I will search for the file.\n```json\n{\n  \"name\": \"search_files\",\n  \"arguments\": {\"pattern\": \"*.go\"}\n}\n```"
	calls, remainingText, ok := toolbridge.ParseToolCalls(response)
	if !ok || len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "search_files" {
		t.Errorf("expected name search_files, got %s", calls[0].Function.Name)
	}
	if remainingText != "I will search for the file." {
		t.Errorf("unexpected remaining text: %q", remainingText)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/home/bagus/.local/go/bin/go test ./internal/toolbridge/... -v`
Expected: FAIL

- [ ] **Step 3: Implement `InjectTools` and `ParseToolCalls`**

Implement system prompt injection framing OpenAI tool JSON schemas into Gemini prompt contracts. Implement JSON extractor parsing markdown code blocks, validating against expected structure, and extracting tool name and argument JSON string.

- [ ] **Step 4: Run test to verify it passes**

Run: `/home/bagus/.local/go/bin/go test ./internal/toolbridge/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/toolbridge/
git commit -m "feat(toolbridge): implement schema injection and tool call parser"
```

---

### Task 6: Multi-Account Pool Manager & Downgrade Failover

**Files:**
- Create: `internal/pool/pool.go`
- Create: `internal/pool/scheduler.go`
- Test: `internal/pool/pool_test.go`

**Interfaces:**
- Consumes: `AccountSession`, `GeminiModel`
- Produces: `type AccountPool struct`, `func NewAccountPool(sessions []*gemini.AccountSession) *AccountPool`, `func (p *AccountPool) Acquire(ctx context.Context, requestedModel string) (*gemini.AccountSession, gemini.GeminiModel, func(status ErrorKind), error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/pool/pool_test.go
package pool_test

import (
	"context"
	"testing"
	"gemini-to-api/internal/config"
	"gemini-to-api/internal/gemini"
	"gemini-to-api/internal/pool"
)

func TestPoolLRUSelection(t *testing.T) {
	s1 := gemini.NewAccountSession(config.AccountConfig{ID: "acc1"})
	s2 := gemini.NewAccountSession(config.AccountConfig{ID: "acc2"})
	p := pool.NewAccountPool([]*gemini.AccountSession{s1, s2})

	acc, _, release, err := p.Acquire(context.Background(), "gemini-auto")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if acc.ID != "acc1" {
		t.Errorf("expected acc1 on first acquire, got %s", acc.ID)
	}
	release(pool.StatusSuccess)

	acc2, _, release2, _ := p.Acquire(context.Background(), "gemini-auto")
	if acc2.ID != "acc2" {
		t.Errorf("expected acc2 on second acquire via LRU, got %s", acc2.ID)
	}
	release2(pool.StatusSuccess)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/home/bagus/.local/go/bin/go test ./internal/pool/... -v`
Expected: FAIL

- [ ] **Step 3: Implement `pool.go` and `scheduler.go`**

Implement thread-safe pool with least-recently-used (LRU) accounting, minimum 2–5s inter-request rate gating per account, 30m quarantine on 429/CAPTCHA, and model downgrade failover logic.

- [ ] **Step 4: Run test to verify it passes**

Run: `/home/bagus/.local/go/bin/go test ./internal/pool/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pool/
git commit -m "feat(pool): implement multi-account LRU scheduler and quarantine failover"
```

---

### Task 7: OpenAI REST Controller & Streaming SSE Handler

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/chat_handler.go`
- Create: `internal/server/models_handler.go`
- Test: `internal/server/server_test.go`

**Interfaces:**
- Consumes: `AccountPool`, `ToolBridge`, `Sanitizer`
- Produces: `func NewServer(cfg *config.Config, pool *pool.AccountPool) *http.Server`

- [ ] **Step 1: Write the failing test**

```go
// internal/server/server_test.go
package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"gemini-to-api/internal/config"
	"gemini-to-api/internal/server"
)

func TestModelsEndpoint(t *testing.T) {
	cfg := &config.Config{Port: 8787}
	s := server.NewServer(cfg, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)

	s.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "gemini-auto") {
		t.Errorf("expected gemini-auto in response, got %s", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/home/bagus/.local/go/bin/go test ./internal/server/... -v`
Expected: FAIL

- [ ] **Step 3: Implement `server.go`, `chat_handler.go`, and `models_handler.go`**

Implement standard `net/http` router with CORS and JSON error handling:
- `POST /v1/chat/completions`: parse messages, handle vision images, inject tools if present, route to pool, execute `StreamGenerate`, stream SSE `data: {"choices":[{"delta":{"content":...}}]}` chunks, or format non-streaming JSON response.
- `GET /v1/models`: aggregate active model registry.

- [ ] **Step 4: Run test to verify it passes**

Run: `/home/bagus/.local/go/bin/go test ./internal/server/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/
git commit -m "feat(server): implement OpenAI-compatible chat completions and models handlers"
```

---

### Task 8: Dynamic Admin API & SIGHUP Reload

**Files:**
- Create: `internal/server/admin_handler.go`
- Test: `internal/server/admin_handler_test.go`

**Interfaces:**
- Consumes: `AccountPool`, `Config`
- Produces: `POST /admin/cookies` route and SIGHUP signal listener in `cmd/server/main.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/server/admin_handler_test.go
package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"gemini-to-api/internal/config"
	"gemini-to-api/internal/server"
)

func TestAdminCookiesUnauthorized(t *testing.T) {
	cfg := &config.Config{AdminKey: "secret_admin"}
	s := server.NewServer(cfg, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/cookies", strings.NewReader(`{"id":"acc1","cookies":"foo"}`))

	s.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/home/bagus/.local/go/bin/go test ./internal/server/admin_handler_test.go -v`
Expected: FAIL

- [ ] **Step 3: Implement `admin_handler.go` and SIGHUP in `cmd/server/main.go`**

Validate bearer token against `cfg.AdminKey`. If valid, update session cookies, trigger live handshake verification, and re-arm pool without restarting process. Add signal handler for `syscall.SIGHUP` to reload `accounts.yaml`.

- [ ] **Step 4: Run test to verify it passes**

Run: `/home/bagus/.local/go/bin/go test ./internal/server/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin_handler.go internal/server/admin_handler_test.go cmd/
git commit -m "feat(admin): implement dynamic cookie update endpoint and SIGHUP reload"
```

---

### Task 9: Systemd User Unit & End-to-End Hermes Verification

**Files:**
- Create: `scripts/build.sh`
- Create: `systemd/gemini-to-api.service`
- Create: `accounts.example.yaml`

**Interfaces:**
- Produces: Compiled binary `bin/gemini-to-api`, systemd user service definition.

- [ ] **Step 1: Create build script and compile binary**

```bash
cat << 'EOF' > scripts/build.sh
#!/usr/bin/env bash
set -e
export PATH="/home/bagus/.local/go/bin:$PATH"
mkdir -p bin
go build -ldflags="-s -w" -o bin/gemini-to-api cmd/server/main.go
echo "Built binary at bin/gemini-to-api"
EOF
chmod +x scripts/build.sh
./scripts/build.sh
```

- [ ] **Step 2: Create systemd user service definition**

Create `systemd/gemini-to-api.service`:
```ini
[Unit]
Description=Gemini Web To API Bare-Metal Gateway
After=network.target

[Service]
Type=simple
WorkingDirectory=/home/bagus/PROJECT/gemini-to-api
ExecStart=/home/bagus/PROJECT/gemini-to-api/bin/gemini-to-api -config /home/bagus/PROJECT/gemini-to-api/accounts.yaml
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
```

- [ ] **Step 3: Run full test suite and verify build**

Run: `/home/bagus/.local/go/bin/go test ./... -v`
Expected: PASS across all internal packages.

- [ ] **Step 4: Commit deliverables**

```bash
git add scripts/ systemd/ accounts.example.yaml
git commit -m "chore(deploy): add build script, systemd user service, and config example"
```
