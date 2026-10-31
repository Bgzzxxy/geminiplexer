<div align="center">

# ⚡ Geminiplexer
### High-Performance Bare-Metal Reverse-Engineered OpenAI & Anthropic Gateway for Google Gemini Web

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go)](https://golang.org)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20Windows%20%7C%20macOS-E95420?style=for-the-badge&logo=linux)](https://github.com/Bgzzxxy/geminiplexer)
[![Docker Free](https://img.shields.io/badge/Docker-Zero%20Dependency-2496ED?style=for-the-badge)](https://github.com/Bgzzxxy/geminiplexer)
[![OpenAI Protocol](https://img.shields.io/badge/API-OpenAI%20Compatible-412991?style=for-the-badge&logo=openai)](https://platform.openai.com)
[![Anthropic Protocol](https://img.shields.io/badge/API-Anthropic%20Claude%20Compatible-D97757?style=for-the-badge&logo=anthropic)](https://docs.anthropic.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg?style=for-the-badge)](LICENSE)

<p align="center">
  <b>Transforms Google Gemini Web into a lightning-fast, production-ready OpenAI and Anthropic-compatible REST API.</b><br/>
  <i>Engineered natively in pure Go with multi-account pooling, anti-bot defenses, downgrade quarantine failover, cross-platform support (Linux, Windows, macOS), and native tool calling.</i>
</p>

---

</div>

## 📑 Table of Contents

- [Why This Exists](#-why-this-exists)
- [Key Features](#-key-features)
- [Architecture](#-architecture)
- [Supported Models & Google RPC Routing](#-supported-models--google-rpc-routing)
- [Cross-Platform Quick Start](#-cross-platform-quick-start)
  - [Linux](#-linux)
  - [Windows](#-windows)
  - [macOS](#-macos)
- [Configuration Guide (`accounts.yaml`)](#-configuration-guide-accountsyaml)
- [Background Service Management](#-background-service-management)
- [9router Integration](#-9router-integration)
- [API Usage Examples](#-api-usage-examples)
  - [Anthropic Python SDK (Claude Compatible)](#anthropic-python-sdk-claude-compatible)
  - [OpenAI Python SDK](#openai-python-sdk)
  - [Tool Calling via cURL](#tool-calling-via-curl)
- [Technical Knowledge Base](#-technical-knowledge-base)
- [Security & Disclaimer](#-security--disclaimer)

---

## 🎯 Why This Exists

Google Gemini Web offers state-of-the-art flagship reasoning models like **Gemini 4 Argon** and **Gemini 3.1 Pro**, but they are not accessible via standard OpenAI client libraries or Anthropic Claude clients without expensive subscriptions or restrictive rate limits.

Existing third-party solutions often depend on heavy Docker containers, Node.js runtimes with high memory footprints, lack multi-account quota pooling, or silently fail when Google throttles a model tier.

**Geminiplexer** solves this with a native, static binary:
- **Dual Protocol**: First-class support for both OpenAI (`/v1/chat/completions`) and Anthropic Claude (`/v1/messages` and `/messages`) protocols.
- **Zero Docker & Multiplatform**: Compiles natively for Linux, Windows (`.exe`), and macOS (Apple Silicon & Intel) with under 20 MB RAM usage.
- **Multi-Account Pooling**: Pools multiple Google accounts with Least-Recently-Used (LRU) scheduling and anti-burst rate limiting.
- **Quota Downgrade Protection**: Instantly detects when Google attempts to silently downgrade a Pro/Argon request to Flash, quarantines that account, and fails over to an account with fresh quota.
- **Native Tool Calling**: Injects tool definition schemas and extracts structured JSON tool arguments (`finish_reason: "tool_calls"` on OpenAI, `type: "tool_use"` on Anthropic).

---

## ✨ Key Features

| Capability | Description |
|---|---|
| **Dual Protocol Bridge** | Supports OpenAI `/v1/chat/completions` and Anthropic Claude `/v1/messages` (SSE streaming & non-streaming) plus `/v1/models`. |
| **Cross-Platform Native** | Pre-built single binaries for Linux (amd64, arm64), macOS (Apple Silicon M-Series, Intel), and Windows (amd64, arm64). |
| **Model Downgrade Failover** | Buffers initial chunks to inspect Google's internal `ModelID`. If a downgrade is detected due to session quotas, the account is quarantined for 15m and the request seamlessly retried on another pool account. |
| **Multi-Account Pool & LRU** | Load balances requests across multiple Google accounts while maintaining an inter-request cooldown (2–5s) to eliminate automated traffic bursts. |
| **Scheduled Jittered Cookie Rotation** | Automated session refreshes via Google's `RotateCookies` endpoint (`__Secure-1PSIDTS`) with random jitter (±10%) to prevent bot signature detection. |
| **Universal Function / Tool Calling** | Full support for tools on both OpenAI and Anthropic schemas with automated 1-shot JSON argument repair. |
| **Real-time Incremental SSE** | Emits clean token deltas instead of duplicate cumulative snapshot text. |
| **Clean Sanitized Text** | Automatically strips Google Web citation tags (`[cite: N]`, `[cite_start]`) and code execution fence artifacts (`?code_reference`, `?code_stdout`). |
| **Dynamic Administration** | Protected `POST /admin/cookies` and `POST /admin/reload` routes for runtime session updates without restarting the server. |

---

## 🏛️ Architecture

```
                 ┌─────────────────────────────────────────────────────┐
                 │     Client / 9router / Claude Code / OpenAI SDK     │
                 └──────────────┬───────────────────────┬──────────────┘
                                │                       │
            POST /v1/chat/completions            POST /v1/messages
            (OpenAI Format)                      (Anthropic Format)
                                │                       │
                                └───────────┬───────────┘
                                            ▼
                 ┌─────────────────────────────────────────────────────┐
                 │                Geminiplexer Gateway                 │
                 │   - Dual Protocol Translators (OpenAI & Anthropic)  │
                 │   - Tool Bridge (JSON Schema Engine)                │
                 │   - Output Sanitizer & Incremental Delta Slicer     │
                 └──────────────────────────┬──────────────────────────┘
                                            │
                                            ▼
                 ┌─────────────────────────────────────────────────────┐
                 │               Account Pool Manager                  │
                 │   - LRU Account Scheduling & Gating (2-5s)          │
                 │   - Jittered Cookie Rotator (12-24h)                │
                 │   - Quota Downgrade Inspector & Failover            │
                 └──────────┬───────────────┬───────────────┬──────────┘
                            │               │               │
                      Account #1      Account #2      Account #N
                     (Cookie A)      (Cookie B)      (Cookie C)
                            │               │               │
                            └───────────────┼───────────────┘
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

## 🚀 Cross-Platform Quick Start

You can either download pre-built binaries from **[GitHub Releases](https://github.com/Bgzzxxy/geminiplexer/releases)** or build from source using Go.

### 🐧 Linux

```bash
# Build from source
git clone https://github.com/Bgzzxxy/geminiplexer.git
cd geminiplexer
./scripts/build.sh

# Configure and run
cp accounts.example.yaml accounts.yaml
./bin/gemini-to-api -config accounts.yaml
```

### 🪟 Windows

1. Download `geminiplexer-windows-amd64.zip` from Releases and extract it.
2. Edit `accounts.yaml` with your cookies.
3. Double-click **`start.bat`** (or run `geminiplexer.exe -config accounts.yaml` in PowerShell/CMD).

### 🍎 macOS (Apple Silicon & Intel)

```bash
# Extract pre-built binary or compile
tar -xzvf geminiplexer-darwin-arm64.tar.gz
cd geminiplexer

# Run
cp accounts.example.yaml accounts.yaml
./geminiplexer -config accounts.yaml
```

---

## ⚙️ Configuration Guide (`accounts.yaml`)

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

### How to Extract Cookies:
1. Open [gemini.google.com](https://gemini.google.com) in your browser and sign in.
2. Open DevTools (`F12` or `Ctrl+Shift+I`) -> Navigate to the **Network** tab.
3. Reload the page and select any request to `gemini.google.com` (such as `batchexecute`).
4. Copy the entire value of the **`Cookie`** request header (must contain `__Secure-1PSID` and `__Secure-1PSIDTS`).
5. If the URL contains an account slot such as `/u/2/app`, set `auth_user: "2"`. For the primary account (`/app`), leave `auth_user: ""`.

---

## 🛠️ Background Service Management

### Linux (`systemd`)
```bash
mkdir -p ~/.config/systemd/user
cp systemd/gemini-to-api.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now gemini-to-api.service
```

### macOS (`launchd`)
```bash
cp launchd/com.geminiplexer.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.geminiplexer.plist
```

---

## 🌐 9router Integration

The gateway integrates cleanly with [9router](https://github.com/decolua/9router) as an OpenAI-compatible or Anthropic-compatible provider.

- **Base URL**: `http://127.0.0.1:4981/v1` (or `http://127.0.0.1:4981`)
- **Prefix**: `gemini-to-api`
- **Available Models**:
  - `gemini-to-api/gemini-4-argon` (or combo alias `gemini-4-argon`, `argon`)
  - `gemini-to-api/gemini-3.1-pro` (or combo alias `gemini-3.1-pro`, `pro`)
  - `gemini-to-api/gemini-3.8-flash`
  - `gemini-to-api/gemini-flash-lite`
  - `gemini-to-api/gemini-auto`

---

## 🧪 API Usage Examples

### Anthropic Python SDK (Claude Compatible)

```python
import anthropic

client = anthropic.Anthropic(
    base_url="http://127.0.0.1:4981", # or http://127.0.0.1:4981/v1
    api_key="not-needed"
)

message = client.messages.create(
    model="gemini-4-argon",
    max_tokens=1024,
    messages=[
        {"role": "user", "content": "Explain quantum computing in one sentence."}
    ]
)

print(message.content[0].text)
```

### OpenAI Python SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:4981/v1",
    api_key="not-needed"
)

response = client.chat.completions.create(
    model="gemini-4-argon",
    messages=[
        {"role": "user", "content": "Write a high-performance Go worker pool."}
    ],
    stream=True
)

for chunk in response:
    content = chunk.choices[0].delta.content or ""
    print(content, end="", flush=True)
```

### Tool Calling via cURL

```bash
# Anthropic Tool Calling
curl -X POST http://127.0.0.1:4981/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-4-argon",
    "messages": [{"role": "user", "content": "What is the weather in Tokyo?"}],
    "tools": [{
      "name": "get_weather",
      "description": "Get current weather for a city",
      "input_schema": {
        "type": "object",
        "properties": {"city": {"type": "string"}},
        "required": ["city"]
      }
    }]
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
