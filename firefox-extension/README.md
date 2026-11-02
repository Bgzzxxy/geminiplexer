# Gemini Cookie Injector — Firefox Extension

Ambil cookies sesi Gemini/Google (termasuk cookie HttpOnly seperti
`__Secure-1PSID`) lalu **injeksi otomatis** ke gateway geminiplexer.

## Cara kerja

1. Extension membaca cookie via API `browser.cookies` — ini satu-satunya cara
   baca cookie yang ber-flag HttpOnly (JS `document.cookie` tidak bisa).
2. Cookie dikirim sebagai `POST /admin/cookies` ke gateway, dengan
   `Authorization: Bearer <admin_key>`.
3. Gateway meng-update sesi di memory **dan menulis balik ke `accounts.yaml`**
   (persist), sehingga injeksi bertahan setelah restart.

## Instalasi (temporary / tanpa signing)

1. Buka `about:debugging` di Firefox → tab **This Firefox**.
2. Klik **Load Temporary Add-on…**.
3. Pilih `manifest.json` di folder `firefox-extension/` (atau file
   `gemini-cookie-injector.xpi`).

> Temporary add-on hilang saat Firefox ditutup. Untuk permanen, pakai
> `about:config → xpinstall.signatures.required = false` lalu install `.xpi`,
> atau daftarkan ke addons.mozilla.org (signing resmi).

## Konfigurasi

Klik ikon extension → isi:

| Field | Nilai |
|---|---|
| Gateway URL | `http://localhost:4981` (bisa IP lokal kalau gateway di mesin lain) |
| Admin key | nilai `admin_key` dari `accounts.yaml` |
| Akun target | `auto` (dari `authuser` URL) atau `account_1`…`account_5` |
| Auto-inject | aktif = otomatis injek saat buka Gemini dalam keadaan login |

## Penggunaan

1. Login ke Gemini/Google di Firefox dengan akun yang ingin dipakai.
2. Buka `https://gemini.google.com` (atau `/u/N/` untuk akun ke-N).
3. Auto-inject berjalan otomatis, atau klik **Injek sekarang**.
4. Verifikasi: `curl http://localhost:4981/readyz` → `healthy_accounts` naik.

## Pemetaan akun (auto)

URL `gemini.google.com/app` → `account_1`; `/u/1/` → `account_2`; dst.
`auth_user` di `accounts.yaml` = `N-1` (account_1 → kosong).

## Izin

Extension meminta `cookies` + `<all_urls>` supaya bisa baca cookie `.google.com`
dan POST ke gateway di host apa pun (localhost / LAN). Ini tool pribadi —
instal hanya di profil yang kamu percayai.

## Build .xpi

```bash
cd firefox-extension
zip -r ../gemini-cookie-injector.xpi \
  manifest.json background.js content_script.js popup.html popup.js icon48.png icon96.png
```