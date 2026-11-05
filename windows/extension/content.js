// AIMonitor 網頁偵測：在 AI 分頁裡看「停止」「允許」按鈕，回報給背景程式（background.js 再送到本機的 AIMonitor）。
// 分頁切到背景時這段程式照樣在跑，所以背景分頁也偵測得到。只看按鈕，不讀對話內容。
// 判斷規則跟 AIMonitor 的畫面偵測（monitor.ps1）一樣；生成中 / 完成的判斷（停止鈕消失 1.5 秒）在 AIMonitor 那邊做。
(() => {
  if (window.__aimonExt) return; // 安裝時補注入到已開著的分頁，避免重複
  window.__aimonExt = true;
  const host = location.hostname;
  const AI = host.includes("claude.ai") ? "claude" : host.includes("gemini.google.com") ? "gemini" : "chatgpt";

  const STOP_RE = /^(stop( (response|responding|generating|streaming|generation))?|停止.{0,6}|中止.{0,4}|停止 .{0,8})$/i;
  const STOP_NOT_RE = /(分享|共用|錄|播放|朗讀|語音|任務|伺服器|share|record|play|read aloud|voice|sharing|task|server)/i;
  const REVIEW_RE = /^(allow( .{1,30})?|always allow|approve|允許.{0,8}|一律允許|永遠允許|核准)$/i;

  function visible(el) {
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) return false;
    const cs = getComputedStyle(el);
    return cs.visibility !== "hidden" && cs.display !== "none";
  }
  // 按鈕的名稱：跟 Windows 讀到的一樣，先看 aria-label，沒有就看文字
  const label = (b) => (b.getAttribute("aria-label") || b.innerText || "").replace(/\s+/g, " ").trim();

  function scan() {
    let stop = false, review = false;
    for (const b of document.querySelectorAll('button, [role="button"]')) {
      const n = label(b);
      if (!n || n.length > 40) continue;
      if (!stop && STOP_RE.test(n) && !STOP_NOT_RE.test(n) && visible(b)) stop = true;
      if (!review && !b.disabled && REVIEW_RE.test(n) && visible(b)) review = true;
      if (stop && review) break;
    }
    // Claude 生成中的訊息帶 data-is-streaming（停止鈕暫時不見時也還在）
    if (!stop && AI === "claude" && document.querySelector('[data-is-streaming="true"]')) stop = true;
    return { ai: AI, stop, review, view: location.origin + location.pathname };
  }

  let last = "", lastSent = 0, pending = false;
  function tick() {
    pending = false;
    const s = scan(), key = JSON.stringify(s), now = Date.now();
    // 有變化就送；沒變化每 5 秒送一次（AIMonitor 很久沒收到就當作分頁不在了）
    if (key === last && now - lastSent < 5000) return;
    last = key; lastSent = now;
    try { chrome.runtime.sendMessage({ t: "state", s }).catch(() => {}); } catch (e) { /* 擴充功能更新或移除後，舊頁面的這段程式會失效 */ }
  }
  // 按鈕出現 / 消失時馬上看（背景分頁的計時器會被放慢，DOM 變化不會）
  new MutationObserver(() => { if (!pending) { pending = true; setTimeout(tick, 200); } })
    .observe(document.documentElement, { subtree: true, childList: true, attributes: true, attributeFilter: ["aria-label", "data-is-streaming", "disabled", "style", "class"] });
  setInterval(tick, 1000);
  tick();
})();
