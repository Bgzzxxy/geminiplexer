// Gemini Cookie Injector — content script (auto-detect + auto-inject)
// Berjalan di gemini.google.com / accounts.google.com saat halaman selesai load.

function isLoggedIn() {
  // Halaman Gemini yang login menyematkan token SNlM0e di DOM/script.
  const html = document.documentElement.outerHTML;
  if (html.indexOf("SNlM0e") >= 0 || html.indexOf("FdrFJe") >= 0) return true;

  // Fallback: halaman logged-out punya tautan "Sign in".
  const els = document.querySelectorAll("a, button");
  for (const el of els) {
    if (/sign\s*in/i.test(el.textContent || "")) return false;
  }
  return html.length > 8000;
}

async function maybeAutoInject() {
  try {
    const prefs = await browser.runtime.sendMessage({ type: "get-prefs" });
    if (!prefs || !prefs.autoInject) return;

    if (!isLoggedIn()) {
      console.log("[GeminiCookieInjector] belum login, skip auto-inject.");
      return;
    }

    const res = await browser.runtime.sendMessage({
      type: "harvest-and-inject",
      url: location.href,
    });
    console.log("[GeminiCookieInjector] auto-inject:", res);
    if (res && res.ok) {
      browser.runtime.sendMessage({
        type: "notify-last",
        payload: { ok: true, accountId: res.accountId, cookieCount: res.cookieCount },
      });
    }
  } catch (e) {
    console.warn("[GeminiCookieInjector]", e);
  }
}

// Tunggu sebentar supaya SPA selesai render, lalu coba auto-inject.
setTimeout(maybeAutoInject, 2500);