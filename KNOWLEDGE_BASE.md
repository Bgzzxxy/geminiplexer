# Knowledge Base: Gemini Web Internal Reverse Engineering

Dokumentasi teknis mendalam mengenai arsitektur internal, struktur payload RPC, mitigasi anti-bot Google, dan protokol streaming pada Google Gemini Web (`gemini.google.com`).

---

## 1. Mekanisme Autentikasi Google Web

### 1.1 Cookie Wajib
Untuk mengakses Gemini Web tanpa login interaktif, diperlukan minimal 2 cookie utama dari domain `.google.com`:
- `__Secure-1PSID`: Token identitas sesi akun pengguna. Bertahan lama (mingguan/bulanan), tetapi jika di-share atau digunakan di IP berbeda secara agresif dapat memicu tantangan CAPTCHA.
- `__Secure-1PSIDTS`: Timestamp session token dinamis. Bertahan relatif pendek (12–24 jam). Jika token ini kedaluwarsa, Google mengarahkan request ke halaman login.

### 1.2 Ekstraksi Handshake Awal (`SNlM0e`)
Google menyematkan token CSRF di dalam dokumen HTML `https://gemini.google.com/app` (atau `https://gemini.google.com/u/{slot}/app` untuk akun sekunder).
- **Format Regex**:
  - Pola utama: `"SNlM0e":"([^"]+)"`
  - Pola fallback: `\["SNlM0e","([^"]+)"\]`
- Token ini berawalan `AIaPT3...` dan **wajib** dikirim sebagai parameter form `at` pada seluruh RPC batchexecute dan StreamGenerate.
- **Header User-Agent**: Wajib menggunakan User-Agent browser modern Chrome Windows/Linux yang konsisten. Menggunakan User-Agent default library (misal `Go-http-client` atau `python-requests`) langsung menghasilkan blokir bot 403 Forbidden.

### 1.3 Rotasi Sesi Otomatis (`RotateCookies`)
- **Endpoint**: `POST https://accounts.google.com/RotateCookies`
- **Body**: String JSON statis persis: `[000,"-0000000000000000000"]`
- **Headers**: Wajib mengirimkan cookie `__Secure-1PSID` dan `__Secure-1PSIDTS` saat ini.
- **Respons**: Google merespons dengan header `Set-Cookie` berisi `__Secure-1PSIDTS` baru.
- **Catatan Keamanan**: Rotasi tidak boleh dilakukan terlalu sering (default aman: 12-24 jam). Jika di-spam setiap beberapa menit, Google mendeteksinya sebagai anomali otomatis dan memblokir sesi akun. Serialisasi rotasi menggunakan mutex (`rotateMu`) untuk mencegah race condition antar request.

---

## 2. Model Discovery & Internal Model IDs (`otAQ7b`)

Endpoint model registry Google Gemini Web menggunakan RPC internal `otAQ7b`:
- **Endpoint**: `POST https://gemini.google.com/_/BardChatUi/data/batchexecute?rpcids=otAQ7b&hl=en&rt=c`
- **Payload**: `f.req=[[["otAQ7b","[]",null,"generic"]]]`

Pada akun modern Google Gemini, array model berada di `payload[15]`. Setiap elemen memuat informasi spesifik:
```json
[
  "95a4c0abcba24364",       // [0] Internal Model ID
  "Argon",                  // [1] Short Category Name
  "Complex tasks",          // [2] Description
  [2, 140, 3, ...],         // [3] Capability flags
  2,                        // [4] Capacity
  null, null, false, "",
  4,                        // [9] Model Number
  "Argon",
  "4 Argon",                // [11] Display Title
  ...
]
```

### Pemetaan Model Wire Format (Verified):
| Model Publik | Internal Model ID | Model Number | Capacity | Capacity Field |
|---|---|---|---|---|
| **Gemini 4 Argon** | `95a4c0abcba24364` | `4` | `2` | `12` |
| **Gemini 3.1 Pro** | `e6fa609c3fa255c0` | `4` | `2` | `12` |
| **Gemini 3.8 Flash** | `56fdd199312815e2` | `20` | `2` | `12` |
| **Gemini Flash-Lite** | `8c46e95b1a07cecc` | `1` | `2` | `12` |
| **Gemini Auto** | `auto` | `1` | `1` | `12` |

---

## 3. Protokol `StreamGenerate` (Chat Inference)

### 3.1 Endpoint & Headers
- **URL**: `POST https://gemini.google.com/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate?at={SNlM0e}&hl=en&_reqid=12345&rt=c`
- **Header Spesial Model Routing (`x-goog-ext-525001261-jspb`)**:
  Untuk model spesifik seperti Argon atau Pro, header ini wajib dikirimkan sebagai array JSON 17 elemen:
  ```json
  [1, null, null, null, "{model_id}", null, null, 0, [4, 5, 6, 8], null, null, {capacity}, null, null, {model_number}, {thinking_level}, "{generation_uuid}"]
  ```
  *(Catatan: Untuk mode `gemini-auto`, header ini dapat dihilangkan agar Gemini Web melakukan perutean otomatis tercepat).*

### 3.2 Struktur Payload Form `f.req` (81 Elemen)
Payload `f.req` dibungkus dalam `outer = [null, json_string(inner)]`:
- `inner[0]`: Konten pesan `[prompt]` (atau array multimodal file jika menyertakan gambar).
- `inner[1]`: Bahasa `["en"]`.
- `inner[2]`: Metadata percakapan `["", "", "", null, null, null, null, null, null, ""]`.
- `inner[45]`: `1` -> **Mode Obrolan Temporer (Incognito)**. Mencegah percakapan tersimpan di histori akun Google pengguna dan menjaga akun tetap bersih.
- `inner[59]`: UUID unik request.
- `inner[79]`: Nomor model (`4` untuk Argon/Pro, `20` untuk Flash, `1` untuk Auto/Lite).
- `inner[80]`: Tingkat penalaran thinking (`1` = Low, `2` = Medium, `3` = High).

### 3.3 Framing Respons & Ekstraksi Chunk
Respons streaming Google bukanlah Server-Sent Events (SSE) standar murni, melainkan format chunked bertingkat:
```
)]}'\n
177\n
[["wrb.fr",null,"[null,[\"c_...\",\"r_...\"],{\"18\":\"...\",\"44\":true}]"]]\n
5309\n
[["wrb.fr",null,"[null,[\"c_...\",\"r_...\"],null,null,[[\"rc_...\",[\"Halo!\"],...]]]]"]]\n
```

#### Aturan Parsing Parser:
1. Baris pertama `)]}'` adalah prefiks anti-XSSI Google -> abaikan.
2. Baris yang murni berupa angka (misal `177`, `5309`) adalah **panjang byte frame (length prefix)** -> abaikan.
3. Sambungkan string JSON ke buffer (`frameBuf`) sampai membentuk JSON envelope utuh yang dapat di-unmarshal (`json.Unmarshal == nil`).
4. **Segera reset buffer** setiap kali sebuah envelope JSON berhasil di-unmarshal (terlepas dari apakah frame tersebut memuat teks atau sekadar metadata), untuk mencegah korupsi buffer pada frame berikutnya.
5. **Kumulatif vs Delta**: Gemini Web mengirimkan teks secara kumulatif (`candidate[0][1][0]`). Parser menghitung delta teks dengan memotong prefiks sebelumnya:
   `delta = currentText[len(previousText):]`

---

## 4. Mekanisme Multi-Account & Downgrade Quarantine

```
                        Request Masuk
                             │
                             ▼
                 [Least Recently Used (LRU)]
                             │
              Apakah Akun dalam Cooldown?
               ├── Ya  ──► Cari Akun Berikutnya
               └── Tidak
                             │
               Jeda Anti-Burst (2s - 5s)
                             │
                             ▼
                     Eksekusi Request
                             │
            Cek ModelID pada Frame Pertama
                             │
             Apakah Terjadi Downgrade Diam-diam?
               ├── Ya  ──► Set StatusModelDowngraded (Cooldown 15m)
               │           Failover ke Akun Berikutnya di Pool
               └── Tidak
                             │
                             ▼
                   Kirim Stream ke Klien
```

1. **Isolasi Akun**: Setiap akun memiliki cookie jar, session ID, dan token `SNlM0e` masing-masing yang tidak saling mencemari.
2. **Penanganan Slot Akun**: Jika beberapa akun Google di-login pada browser yang sama, slot URL `/u/0/`, `/u/1/`, `/u/2/`, `/u/3/`, `/u/4/` dipetakan ke field `auth_user` pada konfigurasi.
3. **Failover Otomatis**: Jika sebuah akun mengalami HTTP 401/403 (token kedaluwarsa), akun tersebut langsung ditandai `StatusAuthError`, memicu re-inisialisasi latar belakang via `sess.Init()`, dan permintaan dialihkan ke akun sehat lainnya.
