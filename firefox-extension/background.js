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

async function inject(accountId, cookies, authUser) {
  const cfg = await getConfig();
  const headers = { "Content-Type": "application/json" };
  if (cfg.adminKey) headers["Authorization"] = `Bearer ${cfg.adminKey}`;

  const resp = await fetch(`${cfg.gateway}/admin/cookies`, {
    method: "POST",
    headers,
    body: JSON.stringify({ id: accountId, cookies, auth_user: authUser }),
  });
  const text = await resp.text();
  if (!resp.ok) throw new Error(`HTTP ${resp.status}: ${text}`);
  return text;
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