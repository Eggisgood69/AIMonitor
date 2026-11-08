// AIMonitor 網頁偵測：收集每個 AI 分頁回報的按鈕狀態，連同「是不是你正在看的分頁」送到本機的 AIMonitor。
const API = "http://127.0.0.1:47322/ext";
const MATCH = ["https://claude.ai/*", "https://chatgpt.com/*", "https://chat.openai.com/*", "https://gemini.google.com/*"];

// 網址 → 哪家 AI（不是這四個網站 = null）
const AI_RE = /^https:\/\/(claude\.ai|chatgpt\.com|chat\.openai\.com|gemini\.google\.com)\//;
function urlAI(url) {
  const m = AI_RE.exec(url || "");
  if (!m) return null;
  return m[1] === "claude.ai" ? "claude" : m[1] === "gemini.google.com" ? "gemini" : "chatgpt";
}

// 分頁狀態放在記憶體裡，每次改動都在同一段同步程式碼裡完成（中間不 await），storage.session 只當備份：
// service worker 被 Chrome 停掉再叫醒時從備份讀回。以前每次都「讀整份 → 改一筆 → 寫回整份」，中間有 await，
// 兩個分頁同時回報時後寫的會把先寫的蓋掉（AIMonitor 以為那個分頁關了）；send() 寫回舊快照也會蓋掉新回報
let tabs = {};
const ready = chrome.storage.session.get("tabs").then((r) => {
  // 讀回備份前就收到的回報比較新，保留
  tabs = Object.assign(r.tabs || {}, tabs);
}).catch(() => {});
function save() { chrome.storage.session.set({ tabs }).catch(() => {}); }
function drop(id, s) {
  // 只刪掉檢查時看到的那一筆；檢查期間分頁又回報了（換成新物件）就不刪
  if (tabs[id] && (s === undefined || tabs[id] === s)) { delete tabs[id]; save(); }
}

let sending = null, again = false;
async function send() {
  if (sending) { again = true; return sending; }
  sending = (async () => {
    await ready;
    do {
      again = false;
      let fw = null;
      try { const w = await chrome.windows.getLastFocused(); if (w && w.focused) fw = w.id; } catch (e) {}
      const list = [];
      for (const [id, s] of Object.entries(tabs)) { // 快照：迴圈中途新增 / 更新的分頁，下一輪（again）會送
        let active = false, ai = null;
        try { const t = await chrome.tabs.get(Number(id)); active = t.active && t.windowId === fw; ai = urlAI(t.url); }
        catch (e) { drop(id, s); continue; } // 分頁已經關掉
        // 分頁現在不是 AI 網站（例如從 ChatGPT 換到別的網站時，舊頁面最後一次回報晚到、把它加回清單）：拿掉。
        // 沒有 tabs 權限時，只有這四個網站的分頁讀得到 url，別的網站是 undefined → 一樣拿掉
        if (!ai) { drop(id, s); continue; }
        if (tabs[id] !== s) { again = true; continue; } // 檢查期間又回報了：用新的那筆再送一次
        list.push({ id: Number(id), ai, stop: !!s.stop, review: !!s.review, view: s.view || "", active });
      }
      if (again) continue; // 有新回報：整份重做，不送半新半舊的清單（AIMonitor 會把缺席的分頁當成關掉）
      try {
        await fetch(API, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ v: 1, tabs: list }) });
      } catch (e) { /* AIMonitor 沒在執行 */ }
    } while (again);
  })().finally(() => { sending = null; });
  return sending;
}

chrome.runtime.onMessage.addListener((msg, sender) => {
  // 只收 AI 網站主頁面的回報（iframe、已經換到別的網站的頁面不算）
  if (!msg || msg.t !== "state" || !sender.tab || sender.frameId !== 0 || !urlAI(sender.url)) return;
  tabs[sender.tab.id] = msg.s; // 同步寫進記憶體（讀回備份前收到的也會在 ready 時保留）
  ready.then(() => { save(); send(); });
});
chrome.tabs.onRemoved.addListener(async (id) => { await ready; drop(String(id)); send(); });
chrome.tabs.onActivated.addListener(() => send());
chrome.windows.onFocusChanged.addListener(() => send());
// 離開 AI 網站（同一個分頁開別的網址）：content script 不會再回報，從清單拿掉
chrome.tabs.onUpdated.addListener(async (id, info) => {
  if (!info.url || urlAI(info.url)) return;
  await ready;
  if (tabs[id]) { drop(String(id)); send(); }
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
