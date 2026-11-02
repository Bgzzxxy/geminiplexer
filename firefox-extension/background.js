// Gemini Cookie Injector — background service worker (Firefox MV2)
// Membaca cookie sesi Google via API browser.cookies (bisa baca HttpOnly),
// lalu POST ke geminiplexer gateway /admin/cookies.

const COOKIE_LIST = [
  "__Secure-1PSID",
  "__Secure-1PSIDTS",
  "SIDCC",
  "__Secure-1PSIDCC",
  "SID",
  "HSID",
  "SSID",
  "APISID",
  "SAPISID",
];

const REQUIRED = ["__Secure-1PSID", "__Secure-1PSIDTS", "SIDCC"];

const DEFAULT_CONFIG = {
  gateway: "http://localhost:4981",
  adminKey: "",
  autoInject: true,
  targetAccount: "auto", // "auto" | "account_1" .. "account_5"
};

async function getConfig() {
  const stored = await browser.storage.local.get("config");
  return { ...DEFAULT_CONFIG, ...(stored.config || {}) };
}

// Baca cookie dari Google. url diarahkan ke gemini.google.com (domain-scoped
// cookies .google.com ikut terbaca), fallback ke accounts.google.com.
async function harvestCookies() {
  const jar = {};
  for (const name of COOKIE_LIST) {
    let c = await browser.cookies.get({ url: "https://gemini.google.com/", name });
    if ((!c || !c.value) && name !== "__Secure-1PSID") {
      c = await browser.cookies.get({ url: "https://accounts.google.com/", name });
    }
    if (c && c.value) jar[name] = c.value;
  }

  const ordered = [
    "__Secure-1PSID",
    "__Secure-1PSIDTS",
    "SIDCC",
    "__Secure-1PSIDCC",
    "SID",
    "HSID",
    "SSID",
    "APISID",
    "SAPISID",
  ];
  const parts = [];
  for (const n of ordered) if (jar[n]) parts.push(`${n}=${jar[n]}`);
  return { header: parts.join("; "), names: Object.keys(jar) };
}

// Petakan authuser dari URL menjadi account id: /u/0 -> account_1, dst.
// Format account_<N> di accounts.yaml memakai auth_user = N-1 (account_1 -> '').
function resolveAccount(url) {
  const m = url.match(/gemini\.google\.com\/u\/(\d+)/);
  const idx = m ? parseInt(m[1], 10) : 0;
  return `account_${idx + 1}`;
}

function authUserFor(accountId) {
  const n = parseInt(accountId.replace("account_", ""), 10);
  if (isNaN(n) || n <= 1) return "";
  return String(n - 1);
}

function xhrRequest(method, url, headers, body) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(method, url, true);
    for (const [k, v] of Object.entries(headers)) {
      xhr.setRequestHeader(k, v);
    }
    xhr.timeout = 12000;
    xhr.onload = () => {
      const text = xhr.responseText || "";
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(text);
      } else {
        reject(new Error(`HTTP ${xhr.status}: ${text.slice(0, 300)}`));
      }
    };
    xhr.onerror = () => reject(new Error(`network error fetching ${url}`));
    xhr.ontimeout = () => reject(new Error(`timeout (12s) fetching ${url}`));
    xhr.send(body);
  });
}

async function inject(accountId, cookies, authUser) {
  const cfg = await getConfig();
  const headers = { "Content-Type": "application/json" };
  if (cfg.adminKey) headers["Authorization"] = `Bearer ${cfg.adminKey}`;

  const url = `${cfg.gateway}/admin/cookies`;

  // Preflight: confirm the gateway is reachable before POST so the user gets
  // a clear "gateway not running" message instead of a generic network error.
  try {
    await xhrRequest("GET", `${cfg.gateway}/health`, {}, null);
  } catch (e) {
    throw new Error(`Gateway tidak reachable di ${cfg.gateway} (${e.message}). Pastikan API jalan & URL benar.`);
  }

  // XHR (not fetch): Firefox MV2 extensions are CORS-enforced for fetch() from
  // moz-extension:// pages, but XHR honors the <all_urls> host permission and
  // bypasses CORS. fetch() here surfaces as "NetworkError ... fetch Resource".
  return xhrRequest("POST", url, headers, JSON.stringify({ id: accountId, cookies, auth_user: authUser }));
}

async function harvestAndInject(url, explicitAccountId) {
  const { header, names } = await harvestCookies();

  const missing = REQUIRED.filter((r) => !names.includes(r));
  if (missing.length > 0) {
    throw new Error(
      "Tidak ada sesi Google aktif (cookie hilang: " + missing.join(", ") + "). Login dulu ke Gemini."
    );
  }

  const cfg = await getConfig();
  const accountId =
    explicitAccountId ||
    (cfg.targetAccount && cfg.targetAccount !== "auto"
      ? cfg.targetAccount
      : resolveAccount(url || ""));

  const authUser = authUserFor(accountId);
  const result = await inject(accountId, header, authUser);

  return {
    ok: true,
    accountId,
    cookieCount: names.length,
    result,
  };
}

browser.runtime.onMessage.addListener((msg, sender) => {
  if (msg.type === "harvest-and-inject") {
    return harvestAndInject(msg.url, msg.accountId)
      .then((r) => ({ ok: true, ...r }))
      .catch((e) => ({ ok: false, error: e.message }));
  }
  if (msg.type === "get-prefs") {
    return getConfig();
  }
  if (msg.type === "save-prefs") {
    return browser.storage.local.set({ config: msg.config }).then(() => ({ ok: true }));
  }
  return undefined;
});