# Design Specification: Gemini Web to API (Go Bare-Metal Multi-Account Gateway)

- **Date:** 2026-10-31
- **Status:** Draft / Pending Builder Check & Approval
- **Author:** Hermes Assistant & Bagus
- **Repository Path:** `/home/bagus/PROJECT/gemini-to-api`

---

## 1. Intent and The Why

The goal is to build a high-performance, lightweight, bare-metal Go service that reverse-engineers Google Gemini's web client (`gemini.google.com`) and exposes an OpenAI-compatible REST API (`/v1/chat/completions` and `/v1/models`). This allows autonomous coding agents like Hermes Agent and other LLM clients to utilize Gemini's flagship models without official Google AI Studio API keys, leveraging browser session cookies with intelligent multi-account pooling and advanced anti-bot safety mechanisms.

---

## 2. Goals, Non-Goals, and Anti-Goals

### 2.1 Goals
- **Native Bare-Metal Go Binary:** Zero Docker requirement. Compiles into a single static Linux binary with minimal RAM footprint and fast startup.
- **OpenAI API Compatibility:**
  - `POST /v1/chat/completions` with streaming (Server-Sent Events) and non-streaming responses.
  - `GET /v1/models` dynamically reflecting discovered Gemini web models.
- **Multi-Account Pooling & Anti-Detection:**
  - Support multiple Google accounts configured in an isolated, thread-safe pool.
  - Smart scheduling (Least Recently Used / Round Robin with inter-request jitter) to prevent traffic bursts on individual accounts.
  - Automated session refresh via Google's `RotateCookies` endpoint (`__Secure-1PSIDTS` rotation) on conservative, jittered intervals (e.g. 12-24h).
  - Circuit breaker & automatic cooldown: quarantine accounts hitting HTTP 429 or "unusual traffic" blocks, seamlessly failing over to healthy accounts.
- **Reasoning Effort Control:** Support `reasoning_effort` (`low`, `medium`, `high`) mapped directly to Gemini's internal thinking protocol (index 80 in payload and header `x-goog-ext-525001261-jspb`), streaming thought status lines as `reasoning_content`.
- **Multimodal Vision Input:** Parse image inputs (base64 data URIs and external URLs) from user messages, upload them via Google internal push endpoints, and attach them to the generation request.
- **Output Sanitization:** Strip web UI citation artifacts (e.g. `[cite: N]`, `[cite_start]`, code fence tags like `?code_reference`, `?code_stdout`) so agents receive clean text.
- **Heuristic Token Usage:** Generate plausible `usage` counts (input tokens, reasoning tokens, output tokens) for context tracking in agent loops.

### 2.2 Non-Goals
- **No Experimental Generation Features:** Explicitly excluded: Canvas, Image Generation (Imagen), Video Generation (Veo), and Music Generation.
- **No Claude/Gemini-native API endpoints:** Only OpenAI standard endpoints are required, matching Hermes Agent and standard agent tools.
- **No Docker / Containerization dependencies:** Focus 100% on bare-metal deployment and Linux systemd user services.

### 2.3 Anti-Goals (What would make this a failure)
- **Account bans or rapid lockouts:** Aggressive cookie rotation, spamming `RotateCookies`, or bursting requests without account-level rate limiting that alerts Google fraud detection.
- **Leaking artifacts or corrupted streams:** Sending Google's raw batchexecute wrappers, unparsed protobuf chunks, or web citations directly into the LLM output stream.
- **Silent failure on model downgrade:** Failing to notify or handle when Gemini Web silently degrades from a Pro model to a lower tier due to temporary quota exhaustion.

---

## 3. Architecture & Reverse Engineering Reference

Reverse-engineered from Gemini Web protocols and reference implementation (`ntthanh2603/gemini-web-to-api`):

### 3.1 Authentication & Session Init
1. **Initial Handshake:**
   - URL: `https://gemini.google.com/app` (or `/u/{authUser}/app`)
   - Headers: Standard Chrome User-Agent, `Cookie: __Secure-1PSID=...; __Secure-1PSIDTS=...`
   - Regex extraction from HTML:
     - `SNlM0e` (CSRF token, sent as `at` parameter in subsequent RPCs)
     - `bl` (build label)
     - `f.sid` (session ID)
2. **Cookie Rotation:**
   - URL: `https://accounts.google.com/RotateCookies`
   - Method: POST, Body: `[000,"-0000000000000000000"]`
   - Response: `Set-Cookie: __Secure-1PSIDTS=...`
   - Updated cookies persisted locally to preserve sessions across restarts.

### 3.2 Model Discovery (`otAQ7b`)
- Endpoint: `https://gemini.google.com/_/BardChatUi/data/batchexecute`
- Payload: `f.req=[[["otAQ7b","[]",null,"generic"]]]`
- Extracts available models, model numbers, and capacity fields.

### 3.3 Inference Execution (`StreamGenerate`)
- Endpoint: `https://gemini.google.com/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate`
- Headers:
  - `x-goog-ext-525001261-jspb`: Encodes model ID, capacity, model number, thinking level, and generation UUID.
  - `x-goog-ext-52505358-jspb`: Request UUID.
  - `x-goog-ext-73010989-jspb`: `[0]`
  - `x-goog-ext-73010990-jspb`: `[0,0,0]`
- Payload array (81 elements):
  - `[0]`: Message content array `[prompt, 0, nil, fileData, nil, nil, 0]`
  - `[1]`: Language `["en"]`
  - `[2]`: Metadata `[conversation_id, response_id, choice_id, ...]`
  - `[45]`: `1` if temporary/incognito mode
  - `[59]`: Request UUID
  - `[79]`: Model Number
  - `[80]`: Thinking Level (1 = Low, etc.)

---

## 4. Multi-Account Pool & Safety Architecture

```
                  ┌────────────────────────────────────────┐
                  │          Hermes Agent / Client         │
                  └───────────────────┬────────────────────┘
                                      │ HTTP /v1/chat/completions
                                      ▼
                  ┌────────────────────────────────────────┐
                  │       Gemini-to-API Go Service         │
                  │   - OpenAI Request Validator           │
                  │   - Sanitizer & Token Estimator        │
                  └───────────────────┬────────────────────┘
                                      │
                                      ▼
                  ┌────────────────────────────────────────┐
                  │         Account Pool Manager           │
                  │   - Account Health Status Store        │
                  │   - Jittered Interval Rotator          │
                  │   - Sticky Session / LRU Scheduler     │
                  └───────┬──────────────┬──────────────┬──┘
                          │              │              │
                    Account #1     Account #2     Account #N
                   (Cookie A)     (Cookie B)     (Cookie C)
                          │              │              │
                          └──────────────┼──────────────┘
                                         ▼
                             Google Gemini Web Engine
```

### 4.1 Safety Parameters per Account
- **Minimum Inter-Request Gap:** Minimum 2–5 seconds delay between successive prompts dispatched to the exact same Google account.
- **Jittered Rotation:** Rotation interval base (e.g. 1440 min) ± 10% random jitter.
- **Failover & Quarantine:** Upon receiving HTTP 429, session expiration, or captcha/unusual traffic response, the affected account is placed in cooldown (e.g. 30 minutes) and the request is immediately retried on the next available healthy account.
- **Zero Cross-Contamination:** Each account maintains its own dedicated HTTP client cookie jar, session tokens (`SNlM0e`), and build identifiers.

---

## 5. What's Left to the Builder

- Framework choice in Go: Standard library `net/http` or lightweight router (e.g. `chi` or `fiber`).
- Config file format (`yaml` vs `json`) and default location.
- Exact regex patterns and protobuf parser routines for StreamGenerate SSE chunk extraction.
- Systemd user service unit template for continuous daemon execution.
