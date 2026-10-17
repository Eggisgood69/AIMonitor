// 在 claude.ai / chatgpt.com / gemini.google.com 頁面裡偵測生成狀態，回報給背景程式。
// 各網站改版時，通常只要調整下面 SITES 裡的選擇器。
(() => {
  const host = location.hostname;
  const AI = host.includes("claude.ai") ? "claude" : host.includes("gemini.google.com") ? "gemini" : "chatgpt";

  const SITES = {
    claude: {
      stop: ['button[aria-label="Stop response"]', '[data-is-streaming="true"]'],
    },
    chatgpt: {
      stop: ['[data-testid="stop-button"]', 'button[aria-label*="Stop streaming"]', 'button[aria-label*="停止串流"]'],
    },
    gemini: {
      stop: ['button[aria-label*="Stop response"]', 'button[aria-label*="停止回應"]', 'mat-icon[fonticon="stop"]', '[data-mat-icon-name="stop"]'],
    },
  };
  const site = SITES[AI];

  // 通用規則：各種語系的「停止」按鈕、「允許/確認」按鈕、錯誤訊息
  const STOP_RE = /^(stop|stop (generating|response|streaming)|停止|停止回應|停止產生|停止生成|停止串流|中止)\b/i;
  const REVIEW_RE = /^(allow|allow once|allow always|always allow|allow for this chat|approve|confirm|允許|允許一次|一律允許|永遠允許|允許這次|核准|確認)$/i;
  const ERR_RE = /(went wrong|error|limit|unable to|failed|try again|無法|錯誤|上限|失敗|發生問題|再試一次)/i;

  function visible(el) {
    if (!el || !el.isConnected) return false;
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) return false;
    const cs = getComputedStyle(el);
    return cs.visibility !== "hidden" && cs.display !== "none" && cs.opacity !== "0";
  }

  function isGenerating() {
    for (const sel of site.stop) {
      for (const el of document.querySelectorAll(sel)) {
        if (sel.includes("data-is-streaming") || visible(el)) return true;
      }
    }
    for (const b of document.querySelectorAll("button")) {
      const label = (b.getAttribute("aria-label") || b.getAttribute("data-testid") || "").trim();
      if (label && STOP_RE.test(label) && visible(b)) return true;
    }
    return false;
  }

  function needsReview() {
    for (const b of document.querySelectorAll("button")) {
      if (b.disabled) continue;
      const t = (b.innerText || b.getAttribute("aria-label") || "").trim();
      if (t.length > 0 && t.length < 24 && REVIEW_RE.test(t) && visible(b)) {
        // 排除在輸入框旁邊的按鈕
        if (b.closest("form, fieldset, [contenteditable]")) continue;
        return true;
      }
    }
    return false;
  }

  function errorElements() {
    const out = [];
    for (const el of document.querySelectorAll('[role="alert"], [class*="error" i], [data-testid*="error" i]')) {
      const t = (el.innerText || "").trim();
      if (t && t.length < 400 && ERR_RE.test(t) && visible(el)) out.push(t);
    }
    return out;
  }

  function errorLabel(t) {
    if (/limit|上限|quota/i.test(t)) return "Usage limit";
    if (/network|網路|connection/i.test(t)) return "Network error";
    return "Error";
  }

  // ---------------- 狀態機 ----------------
  let gen = false, genSince = 0, genOffAt = 0;
  let review = false, reviewSince = 0;
  let err = false, errMsg = "", errAt = 0, errBaseline = errorElements().length;
  let finishedAt = 0;
  let last = "";

  function tick() {
    const now = Date.now();
    const g = isGenerating();
    // 停止按鈕會閃爍，要連續消失 1.5 秒才算真的結束
    if (g) {
      if (!gen) { gen = true; genSince = now; err = false; errBaseline = errorElements().length; }
      genOffAt = 0;
    } else if (gen) {
      if (!genOffAt) genOffAt = now;
      if (now - genOffAt > 1500) { gen = false; finishedAt = now; if (AI === "claude") setTimeout(claudeUsage, 2000); }
    }

    const r = needsReview();
    if (r && !review) reviewSince = now;
    review = r;

    const errs = errorElements();
    if (errs.length > errBaseline && !err) { err = true; errAt = now; errMsg = errorLabel(errs[errs.length - 1]); }
    if (errs.length < errBaseline) errBaseline = errs.length;

    const state = { ai: AI, gen, genSince, review, reviewSince, err, errMsg, errAt, finishedAt, title: document.title };
    const key = JSON.stringify([gen, review, err, errMsg, finishedAt]);
    if (key !== last || now - lastSent > 15000) {
      last = key; lastSent = now;
      try { chrome.runtime.sendMessage({ type: "tabState", state }); } catch (e) { /* 擴充功能重新載入後舊頁面會失效，忽略 */ }
    }
  }
  let lastSent = 0;

  const mo = new MutationObserver(() => { if (!pending) { pending = true; setTimeout(() => { pending = false; tick(); }, 300); } });
  let pending = false;
  mo.observe(document.documentElement, { subtree: true, childList: true, attributes: true, attributeFilter: ["aria-label", "data-is-streaming", "data-testid", "disabled"] });
  setInterval(tick, 1000);
  tick();

  // ---------------- Claude 額度（與 claude.ai「用量」頁面相同的資料）----------------
  async function claudeUsage() {
    try {
      let org = (document.cookie.match(/(?:^|;\s*)lastActiveOrg=([^;]+)/) || [])[1];
      if (!org) {
        const r = await fetch("/api/organizations", { credentials: "include" });
        if (!r.ok) return;
        const orgs = await r.json();
        org = orgs && orgs[0] && orgs[0].uuid;
      }
      if (!org) return;
      const r = await fetch(`/api/organizations/${org}/usage`, { credentials: "include" });
      if (!r.ok) return;
      const d = await r.json();
      const pick = (w) => (w && typeof w.utilization === "number") ? { p: w.utilization, r: w.resets_at ? Math.floor(Date.parse(w.resets_at) / 1000) : 0 } : null;
      const fh = pick(d.five_hour), sd = pick(d.seven_day);
      if (!fh && !sd) return;
      chrome.runtime.sendMessage({
        type: "quota", ai: "claude",
        quota: { p5: fh ? fh.p : -1, r5: fh ? fh.r : 0, pw: sd ? sd.p : -1, rw: sd ? sd.r : 0 },
      });
    } catch (e) { /* 端點不存在或改版：就不顯示額度 */ }
  }
  if (AI === "claude") {
    claudeUsage();
    setInterval(claudeUsage, 120000);
  }
})();
