// Gemini Cookie Injector — popup logic

const $ = (id) => document.getElementById(id);

async function loadPrefs() {
  const prefs = await browser.runtime.sendMessage({ type: "get-prefs" });
  $("gateway").value = prefs.gateway || "";
  $("adminKey").value = prefs.adminKey || "";
  $("targetAccount").value = prefs.targetAccount || "auto";
  $("autoInject").checked = !!prefs.autoInject;
}

async function savePrefs() {
  const config = {
    gateway: $("gateway").value.trim(),
    adminKey: $("adminKey").value.trim(),
    targetAccount: $("targetAccount").value,
    autoInject: $("autoInject").checked,
  };
  await browser.runtime.sendMessage({ type: "save-prefs", config });
  return config;
}

function setStatus(ok, text) {
  const el = $("status");
  el.className = ok ? "ok" : "err";
  el.textContent = text;
}

async function doInject() {
  const btn = $("injectBtn");
  btn.disabled = true;
  btn.textContent = "Memproses…";
  setStatus(false, "Membaca cookie…");

  try {
    await savePrefs();

    // Ambil URL tab aktif untuk mapping authuser (kalau mode auto)
    let url = "";
    const tabs = await browser.tabs.query({ active: true, currentWindow: true });
    if (tabs[0] && tabs[0].url) url = tabs[0].url;

    const explicit =
      $("targetAccount").value !== "auto" ? $("targetAccount").value : undefined;

    const res = await browser.runtime.sendMessage({
      type: "harvest-and-inject",
      url,
      accountId: explicit,
    });

    if (res && res.ok) {
      setStatus(
        true,
        `✓ Injek ke ${res.accountId}\n${res.cookieCount} cookie dikirim.\n${res.result}`
      );
    } else {
      setStatus(false, "✗ " + (res && res.error ? res.error : "Gagal"));
    }
  } catch (e) {
    setStatus(false, "✗ " + e.message);
  } finally {
    btn.disabled = false;
    btn.textContent = "Injek sekarang";
  }
}

document.addEventListener("DOMContentLoaded", async () => {
  await loadPrefs();
  $("injectBtn").addEventListener("click", doInject);
  // Simpan pref setiap kali field berubah (opsional, tetap simpan saat inject)
  ["gateway", "adminKey", "targetAccount", "autoInject"].forEach((f) => {
    document.getElementById(f).addEventListener("change", savePrefs);
  });
});