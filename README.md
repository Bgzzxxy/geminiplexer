# Gemini Web To API (Native Go Bare-Metal Gateway)

High-performance, ultra-lightweight bare-metal reverse-engineered API gateway that transforms Google Gemini Web into OpenAI-compatible endpoints (`/v1/chat/completions` and `/v1/models`). Built natively in Go (zero Docker dependency), featuring multi-account pooling, automatic session rotation, downgrade quarantine failover, and OpenAI tool calling for Hermes Agent and 9router.

---

## 🌟 Fitur Utama

- ⚡ **Native Bare-Metal Go Binary**: Ringan (< 25 MB RAM, binary statis 6.2 MB), tanpa overhead Docker/container.
- 🔄 **Multi-Account Pooling & Anti-Detection**:
  - Pool multi-akun otomatis dengan penjadwalan Least Recently Used (LRU).
  - Jeda minimum 2–5 detik antar-request per akun untuk mencegah deteksi bot Google.
  - Rotasi cookie terjadwal via Google `RotateCookies` (`__Secure-1PSIDTS`) dengan random jitter ±10%.
  - Circuit breaker otomatis: karantina akun 30 menit jika terkena rate limit / CAPTCHA.
- 🛡️ **Deteksi & Failover Downgrade Model**:
  - Buffer frame awal untuk mendeteksi downgrade diam-diam dari Google (misal: Pro/Argon turun ke Flash karena limit kuota sesi).
  - Mengarantina akun yang terkena limit selama 15 menit dan memindahkan request secara instan ke akun lain yang masih memiliki kuota segar.
- 🧠 **Dukungan Penuh Model Flagship Google**:
  - `gemini-4-argon` (`95a4c0abcba24364`)
  - `gemini-3.1-pro` (`e6fa609c3fa255c0`)
  - `gemini-3.8-flash` (`56fdd199312815e2`)
  - `gemini-flash-lite` (`8c46e95b1a07cecc`)
  - `gemini-auto` (default web routing)
- 🛠️ **Native OpenAI Tool / Function Calling**:
  - Injeksi skema alat ke konteks prompt dan ekstraksi respons JSON secara bersih.
  - Mekanisme 1-shot auto repair jika model menghasilkan format argumen JSON yang salah.
- 🌊 **Streaming SSE Real-Time**:
  - Streaming incremental token delta murni (mencegah duplikasi teks snapshot kumulatif Gemini Web).
  - Rekonstruksi paket terfragmentasi (buffer framing multiline).
- 🧹 **Sanitizer Output Bersih**:
  - Membersihkan artefak sitasi UI Google seperti `[cite: N]`, `[cite_start]`, dan akhiran code fence (`?code_reference`, `?code_stdout`).
- 🔌 **Integrasi Langsung 9router & Hermes Agent**:
  - Terintegrasi mulus sebagai custom OpenAI provider di 9router (port 20128) dan backend inferensi Hermes.

---

## 📁 Struktur Proyek

```
/home/bagus/PROJECT/gemini-to-api/
├── bin/
│   └── gemini-to-api               # Native Linux compiled binary (6.2 MB)
├── cmd/
│   └── server/
│       └── main.go                 # Entrypoint server, shutdown handler, SIGHUP reloader
├── internal/
│   ├── config/                     # YAML configuration loader
│   ├── gemini/                     # Google Gemini Web reverse engineering core
│   │   ├── session.go              # Session handshake & token extractor (SNlM0e)
│   │   ├── rotate.go               # RotateCookies endpoint client & lock
│   │   ├── models.go               # otAQ7b batchexecute model discovery
│   │   ├── generate.go             # StreamGenerate RPC SSE client & frame assembler
│   │   └── sanitizer.go            # Output cleaning & citation stripper
│   ├── pool/                       # Multi-account scheduler, LRU, quarantine & failover
│   ├── server/                     # HTTP router, chat completions handler, models handler
│   └── toolbridge/                 # OpenAI tool schema injection & JSON parser
├── scripts/
│   └── build.sh                    # Binary build script
├── systemd/
│   └── gemini-to-api.service       # Systemd user service unit definition
├── accounts.yaml                   # File konfigurasi akun lokal (gitignored)
├── accounts.example.yaml           # Contoh template konfigurasi akun
├── KNOWLEDGE_BASE.md               # Dokumentasi teknis mendalam reverse engineering
└── README.md                       # Dokumentasi utama proyek
```

---

## 🚀 Panduan Operasional

### 1. Build & Kompilasi Binary

```bash
./scripts/build.sh
```

### 2. Konfigurasi Akun (`accounts.yaml`)

File `accounts.yaml` memuat daftar akun yang di-pool:

```yaml
host: "127.0.0.1"
port: 4981
admin_key: "gemini_admin_secret_key"

accounts:
  - id: "account_1"
    auth_user: ""
    cookies: "__Secure-1PSID=...; __Secure-1PSIDTS=..."
    refresh_interval_min: 1440

  - id: "account_2"
    auth_user: "1"
    cookies: "__Secure-1PSID=...; __Secure-1PSIDTS=..."
    refresh_interval_min: 1440

  - id: "account_3"
    auth_user: "2"
    cookies: "__Secure-1PSID=...; __Secure-1PSIDTS=..."
    refresh_interval_min: 1440
```

> **Tips Ekstraksi Cookie**: Salin cookie dari DevTools F12 -> tab Network saat membuka `gemini.google.com`. Pastikan field `__Secure-1PSID` dan `__Secure-1PSIDTS` tersedia.

### 3. Menjalankan sebagai Daemon (Systemd User Service)

```bash
# Salin unit file ke direktori user systemd
mkdir -p ~/.config/systemd/user
cp systemd/gemini-to-api.service ~/.config/systemd/user/

# Reload & start
systemctl --user daemon-reload
systemctl --user enable --now gemini-to-api.service

# Cek status
systemctl --user status gemini-to-api.service

# Pantau log
journalctl --user -u gemini-to-api.service -f
```

---

## 🧪 Pengujian Endpoint

### Cek Model Tersedia
```bash
curl http://127.0.0.1:4981/v1/models
```

### Chat Completion (Non-Streaming)
```bash
curl -X POST http://127.0.0.1:4981/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-4-argon",
    "messages": [{"role": "user", "content": "Halo Gemini!"}]
  }'
```

### Streaming SSE
```bash
curl -N -X POST http://127.0.0.1:4981/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-4-argon",
    "messages": [{"role": "user", "content": "Sebutkan 1 sampai 5!"}],
    "stream": true
  }'
```

---

## 🌐 Integrasi ke 9router

Layanan telah didaftarkan ke 9router sebagai custom provider:
- **Provider Node**: `gemini-to-api`
- **Base URL**: `http://127.0.0.1:4981/v1`
- **Model di 9router**:
  - `gemini-to-api/gemini-4-argon` (atau alias combo `gemini-4-argon`, `argon`, `gemini-argon`)
  - `gemini-to-api/gemini-3.1-pro` (atau alias combo `gemini-3.1-pro`)
  - `gemini-to-api/gemini-3.8-flash`
  - `gemini-to-api/gemini-flash-lite`
  - `gemini-to-api/gemini-auto`
