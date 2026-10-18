// ChatGPT / Gemini 用量讀取（注入到 AIMonitor 內建瀏覽器的背景視窗）
// 讀到的結果透過 window.aimonUsage(JSON) 回傳給程式。
(function () {
  const host = location.hostname;
  const send = (o) => { try { window.aimonUsage(JSON.stringify(o)); } catch (e) {} };

  // ---------------- ChatGPT ----------------
  if (host === "chatgpt.com" || host === "chat.openai.com") {
    window.__aimonPoll = async function () {
      try {
        const s = await (await fetch("/api/auth/session", { credentials: "include", cache: "no-store" })).json();
        if (!s || !s.accessToken) { send({ ai: "chatgpt", login: false }); return; }
        const headers = { Authorization: "Bearer " + s.accessToken };
        if (s.account && s.account.id) headers["ChatGPT-Account-Id"] = s.account.id;
        const diag = [];
        // 方案用量（5 小時 / 每週 / 30 天，與 Codex 共用的額度時窗）：原始回應交給程式解析（chatgpt.go，有單元測試）
        let wham = "";
        try {
          const r = await fetch("/backend-api/wham/usage", { headers, credentials: "include", cache: "no-store" });
          const t = await r.text();
          diag.push("wham/usage " + r.status + " " + t.slice(0, 400));
          if (r.ok) wham = t;
        } catch (e) { diag.push("wham err " + e); }
        // 聊天訊息上限（非公開，格式不固定；先記錄下來）
        try {
          const r = await fetch("/backend-api/conversation_limit", { headers, credentials: "include", cache: "no-store" });
          diag.push("conversation_limit " + r.status + " " + (await r.text()).slice(0, 300));
        } catch (e) {}
        send({ ai: "chatgpt", login: true, wham, diag: diag.join("\n") });
      } catch (e) {
        send({ ai: "chatgpt", error: String(e) });
      }
    };
    setTimeout(() => window.__aimonPoll(), 4000);
    return;
  }

  // ---------------- Gemini ----------------
  // 用量頁：https://gemini.google.com/usage（從左下角齒輪 →「用量限制」也會到這裡）。畫面格式（2026-10）：
  //   目前用量 ……… 已使用 21%   重設時間：下午8:55
  //   每週上限 ……… 已使用 21%   重設時間：10月21日下午8:55
  if (host === "gemini.google.com") {
    const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
    const USAGE_RE = /usage limits?|用量限制|使用限制|使用量限制|使用量上限|使用上限|用量上限/i;
    const PCT_RE = /(?:已使用|已用|used)\s*(\d{1,3}(?:\.\d+)?)\s*%|(\d{1,3}(?:\.\d+)?)\s*%\s*(?:used|已使用)/i;
    const SETTINGS_START = /^(settings|設定)/i;
    const MENU_RE = /main menu|主選單|展開選單|開啟選單|menu|選單/i;
    const steps = [];
    const onUsagePage = () => /^\/(u\/\d+\/)?usage/.test(location.pathname);
    // 背景視窗不要開任何新視窗（例如「瞭解詳情」連到說明中心）
    try { window.open = () => null; } catch (e) {}
    document.addEventListener("click", (e) => {
      const a = e.target && e.target.closest && e.target.closest("a[href]");
      if (!a) return;
      let u; try { u = new URL(a.href, location.href); } catch (x) { return; }
      if (a.target === "_blank" || u.hostname !== location.hostname) { e.preventDefault(); e.stopPropagation(); }
    }, true);
    function visibleEls(sel) { return [...document.querySelectorAll(sel)].filter((e) => e.getClientRects().length); }
    function label(e) { return ((e.getAttribute("aria-label") || "") + " " + (e.innerText || "")).trim(); }

    // 找出所有「已使用 xx%」的元素，取它們的共同上層 = 用量畫面
    const BARE_PCT = /^(\d{1,3}(?:\.\d+)?)\s*%$/;
    function findPanel() {
      // 不限標籤：Gemini 用的是自訂元件（Angular），文字可能在任何元素裡
      const hits = [...document.body.querySelectorAll("*")].filter((e) => {
        if (/^(SCRIPT|STYLE|NOSCRIPT|TEMPLATE)$/.test(e.tagName)) return false;
        const t = (e.innerText || "").trim();
        return t.length < 40 && PCT_RE.test(t) && ![...e.children].some((c) => PCT_RE.test(c.innerText || ""));
      });
      // 備案：用量頁上只有「21%」這種單獨的數字
      if (!hits.length && onUsagePage()) {
        for (const e of document.body.querySelectorAll("*")) {
          const t = (e.innerText || "").trim();
          if (t.length < 8 && BARE_PCT.test(t) && ![...e.children].some((c) => BARE_PCT.test((c.innerText || "").trim()))) hits.push(e);
        }
      }
      if (!hits.length) return null;
      let a = hits[0];
      const contains = (x) => hits.every((h) => x.contains(h));
      while (a && !contains(a)) a = a.parentElement;
      // 往上幾層，把「目前用量 / 每週上限 / 重設時間」的標題也包進來
      for (let i = 0; i < 6 && a && a.parentElement; i++) {
        const t = a.innerText || "";
        if (/(週|周|week)/i.test(t) && /(重設|重置|reset)/i.test(t) && (hits.length > 1 || /(目前|current)/i.test(t))) break;
        const up = a.parentElement;
        if ((up.innerText || "").length > 4000) break;
        a = up;
      }
      return a;
    }

    // 「下午8:55」「10月21日下午8:55」「8:55 PM」「Oct 21, 8:55 PM」→ Unix 秒
    function parseReset(s) {
      if (!s) return 0;
      const now = new Date();
      let m = s.match(/(上午|下午|中午|凌晨|晚上)?\s*(\d{1,2})[:：](\d{2})\s*(AM|PM|上午|下午)?/i);
      if (!m) return 0;
      let h = +m[2]; const min = +m[3]; const ap = (m[1] || "") + (m[4] || "");
      if (/下午|晚上|PM/i.test(ap) && h < 12) h += 12;
      if (/上午|凌晨|AM/i.test(ap) && h === 12) h = 0;
      const d = new Date(now); d.setSeconds(0, 0); d.setHours(h, min);
      let md = s.match(/(\d{1,2})\s*月\s*(\d{1,2})\s*日/);
      if (!md) {
        const en = s.match(/(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]*\.?\s+(\d{1,2})/i);
        if (en) md = [0, "janfebmaraprmayjunjulaugsepoctnovdec".indexOf(en[1].slice(0, 3).toLowerCase()) / 3 + 1, en[2]];
      }
      if (md) { d.setMonth(+md[1] - 1, +md[2]); if (d < now - 86400000) d.setFullYear(d.getFullYear() + 1); }
      else if (/明天|tomorrow/i.test(s) || d < now) d.setDate(d.getDate() + 1);
      return Math.floor(d.getTime() / 1000);
    }

    // 逐行解析：遇到 % 就歸到最近的區塊標題（目前用量 / 每週上限），重設時間也跟著區塊走
    function parse(text) {
      const lines = text.split(/\n+/).map((s) => s.trim()).filter(Boolean);
      const blocks = [];          // {kind, p, r}
      let cur = null;
      const newBlock = (kind) => { cur = { kind, p: -1, r: 0 }; blocks.push(cur); };
      for (const ln of lines) {
        if (/^(目前用量|目前|current usage|current)/i.test(ln) || /5\s*-?\s*(hour|小時)/i.test(ln)) newBlock("5");
        else if (/(每週|每周|週上限|weekly|per week)/i.test(ln) && ln.length < 30) newBlock("w");
        else if (/(每日|每天|daily|per day)/i.test(ln) && ln.length < 30) newBlock("d");
        const pm = ln.match(PCT_RE) || ln.match(BARE_PCT);
        if (pm) { if (!cur || cur.p >= 0) newBlock(""); cur.p = parseFloat(pm[1] ?? pm[2]); }
        if (/(重設|重置|reset)/i.test(ln) && cur && !cur.r) cur.r = parseReset(ln.replace(/^.*?(重設時間|重置時間|resets?( at| on)?)[:：]?\s*/i, ""));
      }
      const withP = blocks.filter((b) => b.p >= 0);
      let s = withP.find((b) => b.kind === "5") || withP.find((b) => b.kind === "d") || withP.find((b) => b.kind === "");
      let w = withP.find((b) => b.kind === "w");
      if (!w) w = withP.find((b) => b !== s && b.kind === "");
      return { p5: s ? s.p : -1, r5: s ? s.r : 0, pw: w ? w.p : -1, rw: w ? w.r : 0, slabel: s && s.kind === "d" ? "今天" : "目前" };
    }

    function loggedIn() {
      return !!document.querySelector('img[src*="googleusercontent.com"], [aria-label*="Google 帳戶"], [aria-label*="Google Account"], a[href*="SignOutOptions"]');
    }
    function loggedOut() {
      if (/accounts\.google\.com/.test(location.href)) return true;
      return visibleEls("a, button").some((e) => /^(sign in|登入)$/i.test((e.innerText || "").trim()));
    }
    function findSettings() {
      return visibleEls('button, [role="button"], a, [role="menuitem"]').find((e) => {
        const al = e.getAttribute("aria-label") || "", l = label(e);
        return /settings|設定/i.test(al) || (l.length < 40 && SETTINGS_START.test(l)) || /^settings$/i.test((e.innerText || "").trim());
      });
    }
    function findUsageItem() {
      return visibleEls('[role="menuitem"], button, [role="button"], a, [role="link"], li').find((e) => {
        const l = label(e);
        if (e.tagName === "A" && e.hostname && e.hostname !== location.hostname) return false;
        return l.length < 40 && USAGE_RE.test(l) && !/瞭解|learn more/i.test(l);
      });
    }
    function labelsForDiag() {
      return visibleEls('button, [role="button"], [role="menuitem"], a').map((e) => label(e).replace(/\s+/g, " ").slice(0, 30)).filter(Boolean).slice(0, 50).join(" | ");
    }
    async function waitFor(fn, ms) {
      for (let t = 0; t < ms; t += 300) { const r = fn(); if (r) return r; await sleep(300); }
      return null;
    }
    async function openUsage() {
      let item = findUsageItem();
      if (!item) {
        let s = findSettings();
        steps.push("齒輪:" + (s ? "有" : "無"));
        if (!s) {
          const m = visibleEls('button, [role="button"]').find((e) => MENU_RE.test(e.getAttribute("aria-label") || ""));
          steps.push("選單:" + (m ? "有" : "無"));
          if (m) { m.click(); await sleep(900); }
          s = findSettings();
        }
        if (s) { s.click(); item = await waitFor(findUsageItem, 3000); }
      }
      steps.push("用量限制項目:" + (item ? "有" : "無"));
      if (item) { item.click(); await waitFor(findPanel, 8000); }
    }

    let openedAt = 0;
    const ss = (k, v) => { try { if (v === undefined) return sessionStorage.getItem(k); sessionStorage.setItem(k, v); } catch (e) { return null; } };
    window.__aimonPoll = async function () {
      steps.length = 0;
      // 用量頁開著太久，數字可能是舊的：重新整理，載入後會自動再讀一次
      if (openedAt && Date.now() - openedAt > 60000) { location.reload(); return; }
      // 不在用量頁：直接開用量頁（如果帳號沒有用量頁，Google 會轉回首頁，這時改用選單）
      if (!onUsagePage() && !findPanel() && !loggedOut()) {
        const tried = +(ss("aimonUsageTry") || 0);
        if (Date.now() - tried > 10 * 60000) { ss("aimonUsageTry", String(Date.now())); location.assign("https://gemini.google.com/usage"); return; }
        steps.push("/usage 被轉回首頁");
      }
      try {
        if (!loggedIn() && loggedOut() && !findPanel()) { send({ ai: "gemini", login: false }); return; }
        let panel = findPanel();
        if (!panel && onUsagePage()) panel = await waitFor(findPanel, 8000);
        if (!panel && !onUsagePage()) { await openUsage(); panel = findPanel(); if (panel) openedAt = Date.now(); }
        else if (!openedAt) openedAt = Date.now();
        if (!panel) {
          const body = ((document.body && document.body.innerText) || "").replace(/\s+/g, " ").slice(0, 500);
          send({ ai: "gemini", login: true, quota: null, diag: "找不到用量畫面（" + steps.join("，") + "，網址 " + location.pathname + "）。頁面文字：" + body + "　按鈕：" + labelsForDiag() });
          document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
          return;
        }
        const text = panel.innerText || "";
        const r = parse(text);
        send({ ai: "gemini", login: true, quota: (r.p5 >= 0 || r.pw >= 0) ? r : null, diag: text.slice(0, 600) });
      } catch (e) {
        send({ ai: "gemini", error: String(e) });
      }
    };
    setTimeout(() => window.__aimonPoll(), 6000);
  }
})();
