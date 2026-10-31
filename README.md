<div align="center">

# ⚡ Gemini Web To API
### High-Performance Bare-Metal Reverse-Engineered OpenAI Gateway for Google Gemini Web

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go)](https://golang.org)
[![Platform](https://img.shields.io/badge/Platform-Linux%20Bare--Metal-E95420?style=for-the-badge&logo=linux)](https://kernel.org)
[![Docker Free](https://img.shields.io/badge/Docker-Zero%20Dependency-2496ED?style=for-the-badge)](https://github.com/Bgzzxxy/gemini-web-to-api)
[![Protocol](https://img.shields.io/badge/API-OpenAI%20Compatible-412991?style=for-the-badge&logo=openai)](https://platform.openai.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg?style=for-the-badge)](LICENSE)

<p align="center">
  <b>Transforms Google Gemini Web into a lightning-fast, production-ready OpenAI-compatible REST API.</b><br/>
  <i>Engineered natively in pure Go with multi-account pooling, anti-bot defenses, downgrade quarantine failover, and native tool calling for autonomous AI agents like Hermes Agent and 9router.</i>
</p>

---

</div>

## 📑 Table of Contents

- [Why This Exists](#-why-this-exists)
- [Key Features](#-key-features)
- [Architecture](#-architecture)
- [Supported Models & Google RPC Routing](#-supported-models--google-rpc-routing)
- [Quick Start](#-quick-start)
- [Configuration Guide (`accounts.yaml`)](#-configuration-guide-accountsyaml)
- [Systemd User Service Daemon](#-systemd-user-service-daemon)
- [9router & Hermes Agent Integration](#-9router--hermes-agent-integration)
- [API Usage Examples](#-api-usage-examples)
- [Technical Knowledge Base](#-technical-knowledge-base)
- [Security & Disclaimer](#-security--disclaimer)

---

## 🎯 Why This Exists

Google Gemini Web offers state-of-the-art flagship reasoning models like **Gemini 4 Argon** and **Gemini 3.1 Pro**, but they are not accessible via standard OpenAI client libraries or autonomous agents without expensive enterprise subscriptions or official Studio rate limits.

Existing third-party solutions often depend on heavy Docker containers, Node.js runtimes with high memory footprints, lack multi-account quota pooling, or silently fail when Google throttles a model tier.

**Gemini Web To API** solves this with a native, static 6.2 MB Linux binary:
- **Zero Docker**: Runs natively bare-metal (< 20 MB RAM footprint).
- **Multi-Account Pooling**: Pools multiple Google accounts with Least-Recently-Used (LRU) scheduling and anti-burst rate limiting.
- **Quota Downgrade Protection**: Instantly detects when Google attempts to silently downgrade a Pro/Argon request to Flash, quarantines that account, and fails over to an account with fresh quota.
- **Native Tool Calling**: Injects tool definition schemas and extracts structured JSON tool arguments (`finish_reason: "tool_calls"`) for agentic workflows.

---

## ✨ Key Features

| Capability | Description |
|---|---|
| **Universal OpenAI Bridge** | Drop-in replacement for `/v1/chat/completions` (streaming SSE & non-streaming) and `/v1/models`. |
| **Model Downgrade Failover** | Buffers initial chunks to inspect Google's internal `ModelID`. If a downgrade is detected due to session quotas, the account is quarantined for 15m and the request seamlessly retried on another pool account. |
| **Multi-Account Pool & LRU** | Load balances requests across multiple Google accounts while maintaining an inter-request cooldown (2–5s) to eliminate automated traffic bursts. |
| **Scheduled Jittered Cookie Rotation** | Automated session refreshes via Google's `RotateCookies` endpoint (`__Secure-1PSIDTS`) with random jitter (±10%) to prevent bot signature detection. |
| **OpenAI Tool / Function Calling** | Full support for `tools`, `tool_choice`, and `tool_calls` with automated 1-shot JSON argument repair. |
| **Real-time Incremental SSE** | Emits clean token deltas instead of duplicate cumulative snapshot text. |
| **Clean Sanitized Text** | Automatically strips Google Web citation tags (`[cite: N]`, `[cite_start]`) and code execution fence artifacts (`?code_reference`, `?code_stdout`). |
| **Dynamic Administration** | Protected `POST /admin/cookies` route for runtime session updates without restarting the server, alongside `SIGHUP` config reloads. |

---

## 🏛️ Architecture

```
                          ┌───────────────────────────────────────┐
                          │   Client / Hermes Agent / 9router     │
                          └──────────────────┬────────────────────┘
                                             │ HTTP /v1/chat/completions (tools, SSE)
                                             ▼
                          ┌───────────────────────────────────────┐
                          │       Gemini-to-API Gateway           │
                          │   - OpenAI Request Validator          │
                          │   - Tool Bridge (JSON Schema Engine)  │
                          │   - Output Sanitizer & Delta Slicer   │
                          └──────────────────┬────────────────────┘
                                             │
                                             ▼
                          ┌───────────────────────────────────────┐
                          │        Account Pool Manager           │
                          │   - LRU Account Scheduling            │
                          │   - Anti-Burst Rate Gating (2-5s)     │
                          │   - Jittered Cookie Rotator (12-24h)  │
                          │   - Downgrade Inspector & Failover    │
                          └──────┬───────────┬───────────┬────────┘
                                 │           │           │
                           Account #1  Account #2  Account #N
                          (Cookie A)  (Cookie B)  (Cookie C)
                                 │           │           │
                                 └───────────┼───────────┘
                                             ▼
                                  Google Gemini Web Engine
```

---

## 🧠 Supported Models & Google RPC Routing

All models are dynamically discovered via batchexecute RPC `otAQ7b` and mapped directly to Google's internal routing identities:

| Public Model Name | Internal Google Model ID | Category | Target Workload |
|---|---|---|---|
| **`gemini-4-argon`** | `95a4c0abcba24364` | `4 Argon` | Complex tasks, elite coding, high-reasoning |
| **`gemini-3.1-pro`** | `e6fa609c3fa255c0` | `3.1 Pro` | Advanced reasoning & deep analysis |
| **`gemini-3.8-flash`** | `56fdd199312815e2` | `3.8 Flash` | High-speed all-around responses |
| **`gemini-flash-lite`**| `8c46e95b1a07cecc` | `3.5 Flash-Lite` | Ultra-low latency queries |
| **`gemini-auto`** | `auto` | Auto | Google Web default automatic routing |

---

## 🚀 Quick Start

### 1. Prerequisites
- Linux OS (x86_64 or ARM64)
- Go 1.21+ (if compiling from source)
- Valid Google account(s) logged in to [gemini.google.com](https://gemini.google.com)

### 2. Clone & Build
```bash
git clone https://github.com/Bgzzxxy/gemini-web-to-api.git
cd gemini-web-to-api

# Build static binary
./scripts/build.sh
```

### 3. Configure Accounts
```bash
cp accounts.example.yaml accounts.yaml
```

Edit `accounts.yaml` with your cookies:
```yaml
host: "127.0.0.1"
port: 4981
admin_key: "your_secret_admin_token"

accounts:
  - id: "primary"
    auth_user: "" # Leave empty for default account (/app)
    cookies: "__Secure-1PSID=...; __Secure-1PSIDTS=..."
    refresh_interval_min: 1440

  - id: "secondary"
    auth_user: "1" # Slot index for multi-account (/u/1/app)
    cookies: "__Secure-1PSID=...; __Secure-1PSIDTS=..."
    refresh_interval_min: 1440
```

### 4. Run Directly
```bash
./bin/gemini-to-api -config accounts.yaml
```

---

## ⚙️ Configuration Guide (`accounts.yaml`)

### How to Extract Cookies:
1. Open [gemini.google.com](https://gemini.google.com) in your browser and sign in.
2. Open DevTools (`F12` or `Ctrl+Shift+I`) -> Navigate to the **Network** tab.
3. Reload the page and select any request to `gemini.google.com` (such as `batchexecute`).
4. Copy the entire value of the **`Cookie`** request header.
5. If the URL contains an account slot such as `/u/2/app`, set `auth_user: "2"`. For the primary account (`/app`), leave `auth_user: ""`.

---

## 🐧 Systemd User Service Daemon

Run the gateway reliably as a background service managed by Linux `systemd`:

```bash
# 1. Install unit file
mkdir -p ~/.config/systemd/user
cp systemd/gemini-to-api.service ~/.config/systemd/user/

# 2. Reload & Enable
systemctl --user daemon-reload
systemctl --user enable --now gemini-to-api.service

# 3. Check status & logs
systemctl --user status gemini-to-api.service
journalctl --user -u gemini-to-api.service -f
```

---

## 🌐 9router & Hermes Agent Integration

The gateway integrates cleanly with [9router](https://github.com/ntthanh2603/9router) and autonomous AI coding agents.

### In 9router:
- **Base URL**: `http://127.0.0.1:4981/v1`
- **Prefix**: `gemini-to-api`
- **Available Models**:
  - `gemini-to-api/gemini-4-argon` (or combo alias `gemini-4-argon`, `argon`)
  - `gemini-to-api/gemini-3.1-pro` (or combo alias `gemini-3.1-pro`, `pro`)
  - `gemini-to-api/gemini-3.8-flash`
  - `gemini-to-api/gemini-flash-lite`

### In Hermes Agent (`~/.hermes/config.yaml`):
```yaml
model:
  default: "gemini-4-argon"
  provider: "custom"
  base_url: "http://127.0.0.1:20128/v1" # or direct: http://127.0.0.1:4981/v1
  api_key: ${OPENAI_API_KEY}
```

---

## 🧪 API Usage Examples

### Python (OpenAI Official SDK)

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:4981/v1",
    api_key="not-needed"
)

response = client.chat.completions.create(
    model="gemini-4-argon",
    messages=[
        {"role": "system", "content": "You are an elite coding assistant."},
        {"role": "user", "content": "Write a high-performance Go worker pool."}
    ],
    stream=True
)

for chunk in response:
    content = chunk.choices[0].delta.content or ""
    print(content, end="", flush=True)
```

### Function / Tool Calling (cURL)

```bash
curl -X POST http://127.0.0.1:4981/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-4-argon",
    "messages": [
      {"role": "user", "content": "What is the weather in Tokyo?"}
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "get_weather",
          "description": "Get current weather for a city",
          "parameters": {
            "type": "object",
            "properties": {
              "city": {"type": "string"}
            },
            "required": ["city"]
          }
        }
      }
    ]
  }'
```

---

## 📖 Technical Knowledge Base

For in-depth reverse-engineering documentation, protobuf envelopes, length-prefix framing protocol, and wire-format RPC specs, consult **[KNOWLEDGE_BASE.md](KNOWLEDGE_BASE.md)**.

---

## 🔒 Security & Disclaimer

> [!NOTE]
> This project is intended for **research, educational, and local development purposes only**.

- **Zero Secret Leakage**: Your session cookies never leave your local machine. No external proxy, telemetry, or remote telemetry servers are contacted.
- **Not Affiliated with Google**: This software uses reverse-engineered web interfaces and is not endorsed by or affiliated with Google LLC. Please adhere to Google's Terms of Service. Use responsibly.

---

## 📄 License

Distributed under the **MIT License**. See `LICENSE` for more information.
