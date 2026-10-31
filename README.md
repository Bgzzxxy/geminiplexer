<div align="center">

<img src="assets/logo.svg" width="130" height="130" alt="Geminiplexer Logo" />

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

<p align="center">
  <img src="assets/banner.svg" width="100%" alt="Geminiplexer Architecture &amp; System Flow" />
</p>

## 📑 Table of Contents

- [Why Geminiplexer?](#-why-geminiplexer)
- [Feature Comparison Matrix](#-feature-comparison-matrix)
- [Client Ecosystem & Tool Compatibility](#-client-ecosystem--tool-compatibility)
- [Key Architectural Highlights](#-key-architectural-highlights)
- [Supported Models & Google RPC Routing](#-supported-models--google-rpc-routing)
- [3-Step Quick Start](#-3-step-quick-start)
  - [Step 1: Download Native Binary](#step-1-download-native-binary)
  - [Step 2: Extract Your Cookies](#step-2-extract-your-cookies)
  - [Step 3: Launch Gateway](#step-3-launch-gateway)
- [Cross-Platform Deployment](#-cross-platform-deployment)
  - [Linux (systemd user unit)](#-linux-systemd)
  - [macOS (launchd daemon)](#-macos-launchd)
  - [Windows (portable one-click batch)](#-windows-service)
- [Configuration Guide (`accounts.yaml`)](#-configuration-guide-accountsyaml)
- [9router Integration](#-9router-integration)
- [API Usage Examples](#-api-usage-examples)
  - [Anthropic Claude Messages API (Python SDK)](#anthropic-claude-messages-api-python-sdk)
  - [Claude Code CLI Integration](#claude-code-cli-integration)
  - [OpenAI Chat Completions API (Python SDK)](#openai-chat-completions-api-python-sdk)
  - [Structured Tool Calling (cURL)](#structured-tool-calling-curl)
- [Technical Knowledge Base](#-technical-knowledge-base)
- [Security & Disclaimer](#-security--disclaimer)

---

## 🎯 Why Geminiplexer?

Google Gemini Web offers state-of-the-art flagship reasoning models like **Gemini 4 Argon** and **Gemini 3.1 Pro**, but they are locked behind interactive browser sessions without standard API access.

Existing community bridges typically suffer from critical design flaws:
1. **Bloated Runtimes**: Packaged in massive 500MB+ Docker containers or heavy Python/Node.js runtimes that consume 400–800 MB of RAM for a simple proxy.
2. **Single-Account Limits**: When one account hits Google's request cap, the entire service dies.
3. **Silent Model Downgrading**: When quota runs out for Pro or Argon, Google silently falls back to Flash without telling the client. Traditional proxies report successful responses while returning inferior output.
4. **Fragile Sessions**: Cookies expire after several hours, requiring tedious manual intervention.

**Geminiplexer** replaces that with a **single, dependency-free native Go binary (< 7 MB, < 20 MB RAM)** delivering dual-protocol OpenAI and Anthropic compatibility, automated multi-account pooling, and instant downgrade quarantine failover.

---

## 📊 Feature Comparison Matrix

| Feature | ⚡ **Geminiplexer (Native Go)** | 🐳 Typical Docker/Python Proxies | ☁️ Official Paid API |
|:---|:---:|:---:|:---:|
| **Memory Footprint** | **< 20 MB RAM** | ~400 MB – 1 GB RAM | Cloud-hosted |
| **Runtime Dependencies** | **Zero (Single Static Binary)** | Docker, Node.js, Python, Chrome | None (Cloud SDK) |
| **Gemini 4 Argon Support** | **Full (ID: `95a4c0abcba24364`)** | ❌ Rare / Missing | Tier-Gated |
| **Dual Protocol Bridge** | **OpenAI + Anthropic Claude** | OpenAI Only | Vendor-locked |
| **Multi-Account Pooling** | **LRU with Anti-Burst Cooldown** | Single Account | Billing account |
| **Silent Downgrade Detection** | **Instant Quarantine (15m Failover)**| ❌ Fails Silently | N/A |
| **Session Lifetime** | **Automated Jittered Rotation** | Manual re-login | API Key |
| **Tool / Function Calling** | **Universal 2-Way Schema Bridge** | Limited / Broken | Native |
| **Cross-Platform** | **Linux, Windows, macOS Native** | Container VM Required | Universal |

---

## 🔌 Client Ecosystem & Tool Compatibility

Geminiplexer exposes standardized endpoints, making it a drop-in replacement for any AI client:

```
                  ┌────────────────────────────────────────────────────────┐
                  │                 Geminiplexer (:4981)                   │
                  └───────┬────────────────────────────────────────┬───────┘
                          │                                        │
           OpenAI: /v1/chat/completions             Anthropic: /v1/messages
                          │                                        │
           ┌──────────────┴──────────────┐          ┌──────────────┴──────────────┐
           │ • Cursor / Windsurf IDE     │          │ • Claude Code CLI           │
           │ • Open-WebUI                │          │ • Cline (VS Code)           │
           │ • 9router Multi-Gateway     │          │ • RooCode                   │
           │ • Aider & Continue.dev      │          │ • Anthropic Python / TS SDK │
           │ • OpenAI Python / Node SDK  │          │ • Obsidian Smart Connections│
           └─────────────────────────────┘          └─────────────────────────────┘
```

---

## ✨ Key Architectural Highlights

- **Dual Protocol Emulation**: Supports both OpenAI (`/v1/chat/completions`) and Anthropic Claude (`/v1/messages` and `/messages`) with real-time Server-Sent Events (SSE) streaming and non-streaming responses.
- **Model Downgrade Quarantine Failover**: Pre-buffers early response frames to inspect Google's internal `ModelID`. If Google attempts to stealth-downgrade a Pro/Argon request to Flash due to quota limits, that account is immediately quarantined for 15 minutes, and the request is transparently re-routed to an account with fresh quota.
- **LRU Multi-Account Pool**: Rotates across unlimited Google accounts with configurable inter-request cooldowns (2–5s) to eliminate burst traffic patterns that trigger bot defense triggers.
- **Continuous Cookie Rotation**: Runs a background daemon communicating with Google's `RotateCookies` endpoint to refresh `__Secure-1PSIDTS` cookies with random jitter (±10%), preventing session expiration.
- **Incremental SSE Delta Slicing**: Corrects Google Web's cumulative text snapshot streaming by computing and forwarding only novel text deltas to the client.
- **Text & Code Sanitization**: Strips internal web markup, citation anchors (`[cite: N]`), and execution delimiters (`?code_reference`, `?code_stdout`).

---

## 🧠 Supported Models & Google RPC Routing

All models are dynamically discovered via batchexecute RPC `otAQ7b` and mapped directly to Google's internal routing identities:

| Model Identifier | Internal Google ID | Tier | Best Suited For |
|---|---|---|---|
| **`gemini-4-argon`** | `95a4c0abcba24364` | Flagship Reasoning | Complex coding, deep logic, architectural planning |
| **`gemini-3.1-pro`** | `e6fa609c3fa255c0` | Pro Reasoning | Extended reasoning, mathematical analysis |
| **`gemini-3.8-flash`** | `56fdd199312815e2` | Fast Generalist | High-speed conversations, real-time completions |
| **`gemini-flash-lite`**| `8c46e95b1a07cecc` | Ultra-Low Latency | Simple classification, rapid edits |
| **`gemini-auto`** | `auto` | Dynamic | Automatic Google Web routing |

---

## ⚡ 3-Step Quick Start

### Step 1: Download Native Binary

Grab the single executable for your platform from **[GitHub Releases](https://github.com/Bgzzxxy/geminiplexer/releases)**:
- **Linux**: `geminiplexer-linux-amd64.tar.gz` / `geminiplexer-linux-arm64.tar.gz`
- **Windows**: `geminiplexer-windows-amd64.zip` / `geminiplexer-windows-arm64.zip`
- **macOS**: `geminiplexer-darwin-arm64.tar.gz` (Apple Silicon) / `geminiplexer-darwin-amd64.tar.gz` (Intel)

### Step 2: Extract Your Cookies

1. Open [gemini.google.com](https://gemini.google.com) in your browser and sign in.
2. Press `F12` (or `Ctrl+Shift+I`) to open Developer Tools, then switch to the **Network** tab.
3. Refresh the page, select any request to `gemini.google.com` (such as `batchexecute`), and copy the entire **`Cookie`** header value (must contain `__Secure-1PSID` and `__Secure-1PSIDTS`).
4. If your URL has an account slot index like `/u/1/app`, set `auth_user: "1"`. For the default account (`/app`), leave `auth_user: ""`.
5. Create `accounts.yaml` (see [Configuration Guide](#-configuration-guide-accountsyaml)).

### Step 3: Launch Gateway

```bash
# Run on default port 4981
./geminiplexer -config accounts.yaml
```

The gateway is now live at `http://127.0.0.1:4981`!

---

## 🖥️ Cross-Platform Deployment

### 🐧 Linux (`systemd`)

Run Geminiplexer as a background user service:

```bash
mkdir -p ~/.config/systemd/user
cp systemd/gemini-to-api.service ~/.config/systemd/user/geminiplexer.service
systemctl --user daemon-reload
systemctl --user enable --now geminiplexer.service

# Check status
systemctl --user status geminiplexer.service
```

### 🍎 macOS (`launchd`)

```bash
cp launchd/com.geminiplexer.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.geminiplexer.plist
```

### 🪟 Windows (`scripts/start.bat`)

Double-click `scripts/start.bat` in the extracted release folder. It automatically creates `accounts.yaml` if missing and runs the server.

---

## ⚙️ Configuration Guide (`accounts.yaml`)

```yaml
host: "127.0.0.1"
port: 4981
admin_key: "your_secret_admin_token"

accounts:
  - id: "work_account"
    auth_user: "" # Default account (/app)
    cookies: "__Secure-1PSID=...; __Secure-1PSIDTS=..."
    refresh_interval_min: 1440

  - id: "personal_account"
    auth_user: "1" # Second Google account (/u/1/app)
    cookies: "__Secure-1PSID=...; __Secure-1PSIDTS=..."
    refresh_interval_min: 1440
```

> **Hot Reloading**: You can reload account cookies dynamically without dropping existing connections by issuing `POST /admin/reload` with `Authorization: Bearer <admin_key>`.

---

## 🌐 9router Integration

Geminiplexer pairs seamlessly with [9router](https://github.com/decolua/9router) as an OpenAI-compatible or Anthropic-compatible provider node:

- **Base URL**: `http://127.0.0.1:4981/v1`
- **Prefix**: `geminiplexer` (or `gemini-to-api`)
- **Supported Models**: `geminiplexer/gemini-4-argon`, `geminiplexer/gemini-3.1-pro`, `geminiplexer/gemini-3.8-flash`

---

## 🧪 API Usage Examples

### Anthropic Claude Messages API (Python SDK)

```python
import anthropic

client = anthropic.Anthropic(
    base_url="http://127.0.0.1:4981", # or http://127.0.0.1:4981/v1
    api_key="none"
)

response = client.messages.create(
    model="gemini-4-argon",
    max_tokens=2048,
    messages=[
        {"role": "user", "content": "Design a high-throughput lock-free queue in Go."}
    ]
)

print(response.content[0].text)
```

### Claude Code CLI Integration

You can drive the official Anthropic Claude Code CLI directly using Geminiplexer:

```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:4981"
export ANTHROPIC_API_KEY="not-needed"

claude "Refactor internal/pool to use sync.RWMutex"
```

### OpenAI Chat Completions API (Python SDK)

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:4981/v1",
    api_key="none"
)

stream = client.chat.completions.create(
    model="gemini-4-argon",
    messages=[{"role": "user", "content": "Explain raft consensus in 3 bullet points."}],
    stream=True
)

for chunk in stream:
    delta = chunk.choices[0].delta.content or ""
    print(delta, end="", flush=True)
```

### Structured Tool Calling (cURL)

```bash
curl -X POST http://127.0.0.1:4981/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-4-argon",
    "messages": [{"role": "user", "content": "What is the stock price of NVDA?"}],
    "tools": [{
      "name": "get_stock_quote",
      "description": "Get current stock price for a symbol",
      "input_schema": {
        "type": "object",
        "properties": {"symbol": {"type": "string"}},
        "required": ["symbol"]
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

- **Zero Secret Leakage**: Your session cookies never leave your local machine. No external proxy, telemetry, or analytics servers are contacted.
- **Not Affiliated with Google**: This software uses reverse-engineered web interfaces and is not endorsed by or affiliated with Google LLC. Please adhere to Google's Terms of Service. Use responsibly.

---

## 📄 License

Distributed under the **MIT License**. See [LICENSE](LICENSE) for more information.
