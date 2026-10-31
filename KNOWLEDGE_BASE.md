# Knowledge Base: Gemini Web Internal Reverse Engineering

In-depth technical documentation covering the internal architecture, RPC payload schemas, Google anti-bot mitigations, and streaming protocols of Google Gemini Web (`gemini.google.com`).

---

## 1. Google Web Authentication Mechanics

### 1.1 Required Cookies
To access Gemini Web without interactive browser login, at least two primary cookies from the `.google.com` domain are required:
- `__Secure-1PSID`: Long-lived user account session identity token. Survives across weeks/months, but aggressive cross-IP usage may trigger CAPTCHA challenges.
- `__Secure-1PSIDTS`: Dynamic timestamped session token. Relates to recent activity and has a shorter lifespan (12–24 hours). When this expires, Google redirects unauthenticated requests to the accounts login page.

### 1.2 Initial Handshake & Token Extraction (`SNlM0e`)
Google embeds a CSRF security token directly within the HTML document of `https://gemini.google.com/app` (or `https://gemini.google.com/u/{slot}/app` for secondary accounts):
- **Extraction Regex Patterns**:
  - Primary pattern: `"SNlM0e":"([^"]+)"`
  - Fallback pattern: `\["SNlM0e","([^"]+)"\]`
- The token begins with `AIaPT3...` and is **mandatory** as the `at` form parameter in all subsequent `batchexecute` and `StreamGenerate` RPC requests.
- **User-Agent Requirement**: Requests must send a realistic, modern desktop Chrome User-Agent header (Windows or Linux). Default HTTP client identifiers (such as `Go-http-client` or `python-requests`) trigger immediate HTTP 403 Forbidden responses.

### 1.3 Automated Session Refresh (`RotateCookies`)
- **Endpoint**: `POST https://accounts.google.com/RotateCookies`
- **Body**: Static JSON string: `[000,"-0000000000000000000"]`
- **Headers**: Must supply current `__Secure-1PSID` and `__Secure-1PSIDTS` cookies.
- **Response**: Google returns a `Set-Cookie` header containing a fresh `__Secure-1PSIDTS` token.
- **Safety Precaution**: Cookie rotation should not occur too frequently (safe default: 12–24 hours). Rapid rotation attempts are flagged by Google fraud detection as automated abuse. The Go service protects rotations with a per-session mutex (`rotateMu`) to eliminate concurrent race collisions.

---

## 2. Model Discovery & Internal Model IDs (`otAQ7b`)

Google Gemini Web's model registry is queried through the internal RPC `otAQ7b`:
- **Endpoint**: `POST https://gemini.google.com/_/BardChatUi/data/batchexecute?rpcids=otAQ7b&hl=en&rt=c`
- **Payload**: `f.req=[[["otAQ7b","[]",null,"generic"]]]`

On modern accounts, selectable model metadata is stored in `payload[15]`. Each element contains:
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

### Verified Wire Format Mappings:
| Public Model Name | Internal Google Model ID | Model Number | Capacity | Capacity Field |
|---|---|---|---|---|
| **Gemini 4 Argon** | `95a4c0abcba24364` | `4` | `2` | `12` |
| **Gemini 3.1 Pro** | `e6fa609c3fa255c0` | `4` | `2` | `12` |
| **Gemini 3.8 Flash** | `56fdd199312815e2` | `20` | `2` | `12` |
| **Gemini Flash-Lite** | `8c46e95b1a07cecc` | `1` | `2` | `12` |
| **Gemini Auto** | `auto` | `1` | `1` | `12` |

---

## 3. `StreamGenerate` Protocol (Inference)

### 3.1 Endpoint & Headers
- **URL**: `POST https://gemini.google.com/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate?at={SNlM0e}&hl=en&_reqid=12345&rt=c`
- **Model Routing Header (`x-goog-ext-525001261-jspb`)**:
  When requesting specific flagship models (e.g., Argon or Pro), this header must be sent as a serialized 17-element JSON array:
  ```json
  [1, null, null, null, "{model_id}", null, null, 0, [4, 5, 6, 8], null, null, {capacity}, null, null, {model_number}, {thinking_level}, "{generation_uuid}"]
  ```
  *(Note: For `gemini-auto`, omitting this header allows Gemini Web to perform automatic latency-optimized routing).*

### 3.2 Form Payload Structure `f.req` (81 Elements)
Payload `f.req` is encoded as `outer = [null, json_string(inner)]`:
- `inner[0]`: Message content array `[prompt]` (or multimodal file references).
- `inner[1]`: Language `["en"]`.
- `inner[2]`: Conversation metadata `["", "", "", null, null, null, null, null, null, ""]`.
- `inner[45]`: `1` -> **Temporary / Incognito Chat Mode**. Prevents queries from polluting the Google account's user history and maintains privacy.
- `inner[59]`: Unique request UUID.
- `inner[79]`: Model Number (`4` for Argon/Pro, `20` for Flash, `1` for Auto/Lite).
- `inner[80]`: Thinking / Reasoning Effort Level (`1` = Low, `2` = Medium, `3` = High).

### 3.3 Response Framing & Chunk Extraction
Google's streaming response uses layered length-prefixed chunking:
```
)]}'\n
177\n
[["wrb.fr",null,"[null,[\"c_...\",\"r_...\"],{\"18\":\"...\",\"44\":true}]"]]\n
5309\n
[["wrb.fr",null,"[null,[\"c_...\",\"r_...\"],null,null,[[\"rc_...\",[\"Hello!\"],...]]]]"]]\n
```

#### Parsing Rules:
1. The first line `)]}'` is Google's anti-XSSI security prefix -> discard.
2. Lines containing strictly digits (e.g., `177`, `5309`) indicate **frame byte lengths** -> discard.
3. Concatenate JSON chunks into an assembly buffer (`frameBuf`) until a valid JSON envelope parses successfully (`json.Unmarshal == nil`).
4. **Immediately reset the assembly buffer** after successfully parsing an envelope, even if that envelope only contained metadata, to prevent buffer corruption across subsequent frames.
5. **Cumulative vs. Delta Output**: Gemini Web streams response candidates cumulatively (`candidate[0][1][0]`). The gateway tracks previously emitted text and computes incremental deltas:
   `delta = currentText[len(previousText):]`

---

## 4. Multi-Account Pool & Downgrade Quarantine

```
                        Incoming Request
                               │
                               ▼
                   [Least Recently Used (LRU)]
                               │
                Is Account in Active Cooldown?
                 ├── Yes ──► Try Next Pool Account
                 └── No
                               │
                 Anti-Burst Gap Delay (2s - 5s)
                               │
                               ▼
                       Execute Request
                               │
                 Inspect Initial Chunk ModelID
                               │
                   Was Model Silently Downgraded?
                 ├── Yes ──► Set StatusModelDowngraded (15m Cooldown)
                 │           Failover to Next Account in Pool
                 └── No
                               │
                               ▼
                      Stream Output to Client
```

1. **Session Isolation**: Each account retains its own distinct cookie jar, session identifier, and CSRF token without cross-contamination.
2. **Account Slot Multi-Login**: When multiple Google accounts share a browser session, URL slot paths (`/u/0/`, `/u/1/`, `/u/2/`, `/u/3/`, `/u/4/`) map cleanly to the `auth_user` configuration field.
3. **Automated Recovery**: When an account returns HTTP 401/403 (session invalidated), the pool marks it as `StatusAuthError`, triggers an asynchronous re-initialization handshake via `sess.Init()`, and immediately routes the in-flight request to the next available healthy account.
