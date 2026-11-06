// 背景程式：收集各 AI 分頁的狀態，挑出「現在該顯示哪一個」，交給 offscreen 頁面經 USB 送到螢幕。
const DONE_SHOW_WHEN_VIEWED_MS = 8000;   // 你正在看的分頁生成完成時，綠燈亮 8 秒
const FOREVER = 8.64e15;                   // 「還沒看過」
const QUOTA_FRESH_MS = 15 * 60 * 1000;

let appHelper = 0;  // 最後一次收到桌面 App 監測資料的時間
let tabs = {};      // tabId -> { ai, gen, genSince, review, reviewSince, err, errMsg, errAt, finishedAt, doneUntil, windowId }
let quota = {};     // ai -> { p5, pw, r5, rw, t }
let loaded = false;

async function load() {
  if (loaded) return;
  const s = await chrome.storage.session.get(["tabs", "quota"]);
  tabs = s.tabs || {};
  quota = s.quota || {};
  loaded = true;
}
function save() { chrome.storage.session.set({ tabs, quota }); }

// ---------------- offscreen（持有 USB 序列埠連線）----------------
let creating = null;
async function ensureOffscreen() {
  const ctx = await chrome.runtime.getContexts({ contextTypes: ["OFFSCREEN_DOCUMENT"] });
  if (ctx.length) return;
  if (!creating) {
    creating = chrome.offscreen.createDocument({
      url: "offscreen.html",
      reasons: ["WORKERS"],
      justification: "Keep the USB serial connection to the status display open.",
    }).catch(() => {}).finally(() => { creating = null; });
  }
  await creating;
}

// ---------------- Claude 額度：背景定時查詢（網頁、桌面 App、手機 App 共用同一個帳號額度）----------------
const USAGE_POLL_MS = 2 * 60 * 1000;
let lastUsagePoll = 0, usageBusy = false;
async function pollClaudeUsage(force) {
  if (usageBusy || (!force && Date.now() - lastUsagePoll < USAGE_POLL_MS)) return;
  usageBusy = true; lastUsagePoll = Date.now();
  try {
    const opt = { credentials: "include", headers: { accept: "application/json" } };
    let org = (await chrome.storage.local.get("claudeOrg")).claudeOrg;
    if (!org) {
      const r = await fetch("https://claude.ai/api/organizations", opt);
      if (!r.ok) return;                       // 沒登入 claude.ai
      const orgs = await r.json();
      org = orgs && orgs[0] && orgs[0].uuid;
      if (!org) return;
      chrome.storage.local.set({ claudeOrg: org });
    }
    const r = await fetch(`https://claude.ai/api/organizations/${org}/usage`, opt);
    if (r.status === 403 || r.status === 404) { chrome.storage.local.remove("claudeOrg"); return; }
    if (!r.ok) return;
    const d = await r.json();
    const pick = (w) => (w && typeof w.utilization === "number") ? w : null;
    const fh = pick(d.five_hour), sd = pick(d.seven_day);
    if (!fh && !sd) return;
    const ts = (w) => (w && w.resets_at ? Math.floor(Date.parse(w.resets_at) / 1000) : 0);
    await load();
    quota.claude = { p5: fh ? fh.utilization : -1, r5: ts(fh), pw: sd ? sd.utilization : -1, rw: ts(sd), t: Date.now() };
    save();
    schedulePush();
  } catch (e) {
    // 網路或端點改版：保留上一筆資料
  } finally {
    usageBusy = false;
  }
}

// ---------------- 狀態計算 ----------------
async function focusInfo() {
  const [active] = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  let focused = false;
  try { const w = await chrome.windows.getLastFocused(); focused = w && w.focused; } catch (e) {}
  return { activeId: active ? active.id : null, focused };
}

function tabStatus(t, now) {
  if (t.review) return ["review", t.reviewSince];
  if (t.gen) return ["working", t.genSince];
  if (t.err) return ["error", t.errAt];
  if (t.finishedAt && now < (t.doneUntil || 0)) return ["done", t.finishedAt];
  return ["idle", t.finishedAt || t.genSince || now];
}

const PRIORITY = { review: 4, error: 3, done: 2, working: 1, idle: 0 };

async function buildFrame() {
  await load();
  const now = Date.now();
  const { activeId, focused } = await focusInfo();

  // 桌面 App 在前景時，它就是「正在看的」
  const appKey = Object.keys(tabs).find((k) => k.startsWith("app:") && tabs[k].focused);
  const viewKey = appKey || (activeId != null && focused ? String(activeId) : null);

  // 正在看的分頁 / App：完成後只亮 8 秒
  if (viewKey && tabs[viewKey]) {
    const t = tabs[viewKey];
    if (t.finishedAt && t.doneUntil === FOREVER) t.doneUntil = now + DONE_SHOW_WHEN_VIEWED_MS;
  }

  const list = Object.entries(tabs).map(([id, t]) => {
    const [st, since] = tabStatus(t, now);
    return { id: String(id), t, st, since };
  });

  let main = list.find((x) => x.id === (appKey || String(activeId)));
  if (!main) {
    // 目前不在 AI 分頁：顯示最需要注意的那一個
    main = list.slice().sort((a, b) => (PRIORITY[b.st] - PRIORITY[a.st]) || (b.since - a.since))[0];
  }

  // 沒有任何 AI 分頁（例如你在用桌面 App）：顯示 Claude 帳號額度
  if (!main && quota.claude && now - quota.claude.t < QUOTA_FRESH_MS) {
    main = { id: -1, t: { ai: "claude" }, st: "idle", since: now };
  }

  const frame = { ai: "none", st: "idle", msg: "", age: 0, now: Math.floor(now / 1000), tz: -new Date().getTimezoneOffset() * 60, o: [] };
  if (main) {
    frame.ai = main.t.ai;
    frame.st = main.st;
    frame.age = Math.max(0, Math.floor((now - main.since) / 1000));
    if (main.st === "error") frame.msg = main.t.errMsg || "";
    const q = quota[main.t.ai];
    if (q && now - q.t < QUOTA_FRESH_MS) {
      let { p5, pw, r5, rw } = q;
      if (r5 && r5 * 1000 < now) p5 = 0;   // 已過重置時間
      if (rw && rw * 1000 < now) pw = 0;
      if (p5 >= 0) { frame.p5 = Math.round(p5 * 10) / 10; frame.r5 = r5 || 0; }
      if (pw >= 0) { frame.pw = Math.round(pw * 10) / 10; frame.rw = rw || 0; }
    }
    frame.o = list
      .filter((x) => x.id !== main.id && x.st !== "idle")
      .sort((a, b) => PRIORITY[b.st] - PRIORITY[a.st])
      .slice(0, 3)
      .map((x) => [x.t.ai, x.st]);
  }
  return frame;
}

let pushTimer = null;
let demoUntil = 0;
function schedulePush() {
  if (pushTimer || Date.now() < demoUntil) return;
  pushTimer = setTimeout(async () => {
    pushTimer = null;
    await ensureOffscreen();
    const frame = await buildFrame();
    save();
    chrome.runtime.sendMessage({ target: "offscreen", type: "frame", frame }).catch(() => {});
  }, 120);
}

// ---------------- 事件 ----------------
chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  (async () => {
    await load();
    if (msg.type === "tabState" && sender.tab) {
      const id = sender.tab.id;
      const prev = tabs[id] || {};
      const s = msg.state;
      const t = { ...prev, ...s, windowId: sender.tab.windowId };
      if (s.finishedAt && s.finishedAt !== prev.finishedAt) t.doneUntil = FOREVER;  // 新完成：等你看過才熄
      if (s.gen) t.doneUntil = 0;
      tabs[id] = t;
      schedulePush();
    } else if (msg.type === "appState") {        // Windows 桌面 App 監測（windows-app/aimon-app.ps1）
      appHelper = msg.ok ? Date.now() : 0;
      const seen = new Set();
      for (const a of msg.apps || []) {
        const key = "app:" + a.ai;
        seen.add(key);
        const prev = tabs[key] || {};
        const t = { ...prev, ...a, err: false, errMsg: "", isApp: true };
        if (a.finishedAt && a.finishedAt !== prev.finishedAt) t.doneUntil = a.focused ? Date.now() + DONE_SHOW_WHEN_VIEWED_MS : FOREVER;
        if (a.gen) t.doneUntil = 0;
        tabs[key] = t;
      }
      for (const k of Object.keys(tabs)) if (k.startsWith("app:") && !seen.has(k)) delete tabs[k];
      schedulePush();
    } else if (msg.type === "quota") {
      quota[msg.ai] = { ...msg.quota, t: Date.now() };
      lastUsagePoll = Date.now();
      schedulePush();
    } else if (msg.type === "tick") {            // offscreen 每 5 秒一次，順便讓背景程式保持清醒
      pollClaudeUsage(false);
      schedulePush();
    } else if (msg.type === "button") {          // 螢幕上的 BOOT 鍵：全部標為已讀
      for (const t of Object.values(tabs)) { t.doneUntil = 0; t.err = false; }
      schedulePush();
    } else if (msg.type === "getStatus") {
      await ensureOffscreen();
      const frame = await buildFrame();
      let serial = null;
      try { serial = await chrome.runtime.sendMessage({ target: "offscreen", type: "status" }); } catch (e) {}
      sendResponse({ frame, serial, tabs: Object.keys(tabs).length, appHelper: Date.now() - appHelper < 5000 });
      return;
    } else if (msg.type === "demo") {           // 測試畫面：輪流顯示各狀態
      await ensureOffscreen();
      const now = () => Math.floor(Date.now() / 1000);
      const seq = [
        { ai: "claude", st: "review", p5: 45, pw: 63, r5: now() + 7800, age: 3 },
        { ai: "chatgpt", st: "working", age: 42, o: [["gemini", "done"]] },
        { ai: "gemini", st: "done", age: 5 },
        { ai: "claude", st: "error", msg: "Usage limit", p5: 97, r5: now() + 1500, age: 1 },
      ];
      demoUntil = Date.now() + seq.length * 3000 + 500;
      for (const f of seq) {
        chrome.runtime.sendMessage({ target: "offscreen", type: "raw", frame: { msg: "", o: [], now: now(), ...f } }).catch(() => {});
        await new Promise((r) => setTimeout(r, 3000));
      }
      demoUntil = 0;
      schedulePush();
    } else if (msg.type === "kick") {
      await ensureOffscreen();
      schedulePush();
    }
    sendResponse({ ok: true });
  })();
  return true;
});

chrome.tabs.onActivated.addListener(schedulePush);
chrome.windows.onFocusChanged.addListener(schedulePush);
chrome.tabs.onRemoved.addListener(async (id) => { await load(); delete tabs[id]; schedulePush(); });
chrome.tabs.onUpdated.addListener(async (id, info) => {
  if (!info.url) return;
  await load();
  if (tabs[id] && !/^https:\/\/(claude\.ai|chatgpt\.com|chat\.openai\.com|gemini\.google\.com)\//.test(info.url)) {
    delete tabs[id];
    schedulePush();
  }
});
chrome.runtime.onStartup.addListener(() => { ensureOffscreen(); });
chrome.runtime.onInstalled.addListener(async (d) => {
  ensureOffscreen();
  // 已經開著的 AI 分頁不用重新整理也能偵測
  const open = await chrome.tabs.query({ url: ["https://claude.ai/*", "https://chatgpt.com/*", "https://chat.openai.com/*", "https://gemini.google.com/*"] });
  for (const t of open) chrome.scripting.executeScript({ target: { tabId: t.id }, files: ["content.js"] }).catch(() => {});
  if (d.reason === "install") chrome.tabs.create({ url: "pair.html" });
});
ensureOffscreen();
pollClaudeUsage(true);
