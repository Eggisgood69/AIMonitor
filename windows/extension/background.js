// AIMonitor 網頁偵測：收集每個 AI 分頁回報的按鈕狀態，連同「是不是你正在看的分頁」送到本機的 AIMonitor。
const API = "http://127.0.0.1:47322/ext";
const MATCH = ["https://claude.ai/*", "https://chatgpt.com/*", "https://chat.openai.com/*", "https://gemini.google.com/*"];

async function getTabs() { return (await chrome.storage.session.get("tabs")).tabs || {}; }
async function setTabs(t) { await chrome.storage.session.set({ tabs: t }); }

let sending = null, again = false;
async function send() {
  if (sending) { again = true; return sending; }
  sending = (async () => {
    do {
      again = false;
      const tabs = await getTabs();
      let fw = null;
      try { const w = await chrome.windows.getLastFocused(); if (w && w.focused) fw = w.id; } catch (e) {}
      const list = [];
      for (const [id, s] of Object.entries(tabs)) {
        let active = false;
        try { const t = await chrome.tabs.get(Number(id)); active = t.active && t.windowId === fw; }
        catch (e) { delete tabs[id]; continue; } // 分頁已經關掉
        list.push({ id: Number(id), ai: s.ai, stop: !!s.stop, review: !!s.review, view: s.view || "", active });
      }
      await setTabs(tabs);
      try {
        await fetch(API, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ v: 1, tabs: list }) });
      } catch (e) { /* AIMonitor 沒在執行 */ }
    } while (again);
  })().finally(() => { sending = null; });
  return sending;
}

chrome.runtime.onMessage.addListener((msg, sender) => {
  if (!msg || msg.t !== "state" || !sender.tab) return;
  (async () => {
    const tabs = await getTabs();
    tabs[sender.tab.id] = msg.s;
    await setTabs(tabs);
    send();
  })();
});
chrome.tabs.onRemoved.addListener(async (id) => { const t = await getTabs(); delete t[id]; await setTabs(t); send(); });
chrome.tabs.onActivated.addListener(() => send());
chrome.windows.onFocusChanged.addListener(() => send());
// 離開 AI 網站（同一個分頁開別的網址）：content script 不會再回報，從清單拿掉
chrome.tabs.onUpdated.addListener(async (id, info) => {
  if (!info.url) return;
  const t = await getTabs();
  if (t[id] && !/^https:\/\/(claude\.ai|chatgpt\.com|chat\.openai\.com|gemini\.google\.com)\//.test(info.url)) { delete t[id]; await setTabs(t); send(); }
});

// 剛安裝 / 更新時，已經開著的 AI 分頁不會自動載入 content script：補注入，不用重新整理頁面
chrome.runtime.onInstalled.addListener(async () => {
  send(); // 沒開 AI 分頁也讓 AIMonitor 知道擴充功能裝好了
  for (const t of await chrome.tabs.query({ url: MATCH })) {
    try { await chrome.scripting.executeScript({ target: { tabId: t.id }, files: ["content.js"] }); } catch (e) {}
  }
});
// 每 30 秒報一次平安（沒有 AI 分頁時也送空清單）：AIMonitor 3 分鐘沒收到就當作擴充功能不在了
chrome.runtime.onStartup.addListener(() => send());
chrome.alarms.create("beat", { periodInMinutes: 0.5 });
chrome.alarms.onAlarm.addListener(() => send());
