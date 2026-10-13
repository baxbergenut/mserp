importScripts("core.js");
const API = "https://erp.msexpressinc.net/api/dispatcher-extension";
const ready = chrome.storage.local.setAccessLevel({ accessLevel: "TRUSTED_CONTEXTS" });
let queue = Promise.resolve();
const isBoard = sender => sender.tab && /^https:\/\/scm\.jbhunt\.com\/carrier\//.test(sender.url || "");
const isSettings = sender => sender.url === chrome.runtime.getURL("settings.html");
async function api(path, body, token) {
  const response = await fetch(API + path, { method: "POST", credentials: "omit", cache: "no-store", redirect: "error",
    headers: { "Content-Type": "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: JSON.stringify(body), signal: AbortSignal.timeout(10000) });
  const result = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(result.error || "ERP access check failed. Activate again or contact your administrator.");
  return result;
}
async function handle(message, sender) {
  await ready;
  if (!isSettings(sender) && !isBoard(sender)) throw new Error("Unsupported page");
  const { settings, token, dispatcherName } = await chrome.storage.local.get(["settings", "token", "dispatcherName"]);
  const config = DispatchWatch.normalize(settings);
  if (message.type === "settings") return { settings: config, activated: !!token, dispatcherName: dispatcherName || "" };
  if (message.type === "options" && isBoard(sender)) { await chrome.runtime.openOptionsPage(); return {}; }
  if (message.type === "save" && isSettings(sender)) { await chrome.storage.local.set({ settings: DispatchWatch.normalize(message.settings) }); return {}; }
  if (message.type === "activate" && isSettings(sender)) {
    const result = await api("/activate", { code: String(message.code || "").trim() });
    await chrome.storage.local.set({ token: result.token, dispatcherName: result.dispatcherName });
    await chrome.storage.session.remove("watchOwner");
    return { dispatcherName: result.dispatcherName };
  }
  if (message.type === "forget" && isSettings(sender)) { await chrome.storage.local.remove(["token", "dispatcherName"]); await chrome.storage.session.remove("watchOwner"); return {}; }
  if (!isBoard(sender)) throw new Error("Use Play on the J.B. Hunt grid");
  const { watchOwner } = await chrome.storage.session.get("watchOwner");
  const tabId = sender.tab.id;
  if (message.type === "stop") {
    if (watchOwner?.tabId === tabId) await chrome.storage.session.remove("watchOwner");
    return {};
  }
  if (message.type !== "start" && message.type !== "poll") throw new Error("Unknown action");
  if (!token) throw new Error("Activate the extension from its toolbar icon first.");
  if (watchOwner && watchOwner.tabId !== tabId && Date.now() - watchOwner.lastSeen < 120000) throw new Error("Already running in another J.B. Hunt tab. Pause it there first.");
  if (message.type === "poll" && watchOwner?.tabId !== tabId) throw new Error("Monitoring stopped. Press Play to restart.");
  await api("/check", {}, token);
  await chrome.storage.session.set({ watchOwner: { tabId, lastSeen: Date.now() } });
  return { settings: config, dispatcherName };
}
chrome.runtime.onMessage.addListener((message, sender, reply) => {
  const run = queue.then(() => handle(message, sender));
  queue = run.catch(() => {});
  run.then(result => reply({ ok: true, ...result }), e => reply({ ok: false, error: e.message || "Connection failed" }));
  return true;
});
chrome.tabs.onRemoved.addListener(tabId => {
  queue = queue.then(async () => { const { watchOwner } = await chrome.storage.session.get("watchOwner"); if (watchOwner?.tabId === tabId) await chrome.storage.session.remove("watchOwner"); }).catch(() => {});
});
