// 注入到 WebView2 每個頁面（只在 claude.ai 上作用）
// 1. window.__aimonPoll()：用登入狀態查 Claude 用量，回傳給程式
// 2. 在 /api/organizations 這個輕量頁面上畫出狀態頁
(function () {
  if (location.hostname !== "claude.ai") return;

  window.__aimonPoll = async function () {
    try {
      const r = await fetch("/api/organizations", { credentials: "include", cache: "no-store" });
      if (!r.ok) { await window.aimonUsage(JSON.stringify({ login: false, status: r.status })); return; }
      let orgs = await r.json();
      if (!Array.isArray(orgs) || !orgs.length) { await window.aimonUsage(JSON.stringify({ login: false, status: 0 })); return; }
      // 有聊天功能的組織優先；逐一嘗試，直到拿到用量
      orgs = orgs.slice().sort((a, b) => ((b.capabilities || []).includes("chat") ? 1 : 0) - ((a.capabilities || []).includes("chat") ? 1 : 0));
      const tried = [];
      for (const o of orgs) {
        if (!o || !o.uuid) continue;
        const u = await fetch("/api/organizations/" + o.uuid + "/usage", { credentials: "include", cache: "no-store" });
        const text = await u.text();
        let usage = null;
        try { usage = JSON.parse(text); } catch (_) {}
        if (u.ok && usage && (usage.five_hour || usage.seven_day)) {
          await window.aimonUsage(JSON.stringify({ login: true, status: u.status, usage }));
          return;
        }
        tried.push({ org: (o.name || "").slice(0, 40), caps: o.capabilities || [], status: u.status, body: text.slice(0, 300) });
      }
      await window.aimonUsage(JSON.stringify({ login: true, status: -1, usage: null, diag: tried }));
    } catch (e) {
      try { await window.aimonUsage(JSON.stringify({ error: String(e) })); } catch (_) {}
    }
  };
  setTimeout(() => window.__aimonPoll(), 1500);

  if (location.pathname !== "/api/organizations") return;

  // ---------------- 狀態頁 ----------------
  const NAMES = { claude: "Claude", chatgpt: "ChatGPT", gemini: "Gemini", none: "—" };
  const ST = { idle: "待命", done: "完成", working: "生成中", review: "需審核", error: "錯誤" };
  const COL = { idle: "#8e8e93", done: "#30d158", working: "#30d158", review: "#ffd60a", error: "#ff453a" };
  const SRC = { app: "桌面 App", web: "瀏覽器", cc: "終端機" };

  function build() {
    document.documentElement.innerHTML = `
<head><meta charset="utf-8"><title>AI 狀態指示燈</title>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body { margin: 0; background: #000; color: #f5f5f7; font: 14px/1.5 -apple-system, "Segoe UI Variable", "Segoe UI", "Microsoft JhengHei UI", sans-serif; -webkit-user-select: none; user-select: none; }
  main { padding: 22px 20px 28px; max-width: 520px; margin: 0 auto; }
  header { display: flex; align-items: center; gap: 12px; margin-bottom: 18px; }
  .logo { width: 40px; height: 40px; border-radius: 50%; border: 3px solid #30d158; display: grid; place-items: center; }
  .logo::after { content: ""; width: 14px; height: 14px; border-radius: 50%; border: 3px solid #f0855a; }
  h1 { font-size: 20px; font-weight: 650; margin: 0; letter-spacing: -0.01em; }
  .ver { color: #6e6e73; font-size: 12px; }
  .card { background: #1c1c1e; border-radius: 14px; padding: 4px 16px; margin-bottom: 14px; }
  .row { display: flex; align-items: center; gap: 12px; padding: 12px 0; border-bottom: 1px solid #2c2c2e; }
  .row:last-child { border-bottom: 0; }
  .k { color: #98989f; width: 92px; flex: none; }
  .v { flex: 1; min-width: 0; }
  .dot { display: inline-block; width: 9px; height: 9px; border-radius: 50%; margin-right: 7px; vertical-align: 1px; background: #636366; }
  .hint { color: #8e8e93; font-size: 12.5px; margin-top: 2px; }
  .bar { height: 6px; border-radius: 3px; background: #3a2a24; overflow: hidden; margin-top: 6px; }
  .bar i { display: block; height: 100%; background: #f0855a; border-radius: 3px; }
  button { font: inherit; font-weight: 600; border: 0; border-radius: 10px; padding: 10px 14px; cursor: pointer; background: #2c2c2e; color: #f5f5f7; }
  button.primary { background: #0a84ff; color: #fff; }
  button:hover { filter: brightness(1.15); }
  .btns { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 4px; }
  .items .it { padding: 10px 0; border-bottom: 1px solid #2c2c2e; }
  .items .it:last-child { border-bottom: 0; }
  .muted { color: #6e6e73; }
  details { margin-top: 6px; color: #8e8e93; font-size: 12px; }
  details code { color: #c7c7cc; word-break: break-all; }
  .sw { display: flex; align-items: center; justify-content: space-between; }
  .toggle { width: 44px; height: 26px; border-radius: 13px; background: #39393d; position: relative; cursor: pointer; flex: none; }
  .toggle.on { background: #30d158; }
  .toggle::after { content: ""; position: absolute; top: 2px; left: 2px; width: 22px; height: 22px; border-radius: 50%; background: #fff; transition: left .15s; }
  .toggle.on::after { left: 20px; }
  select { font: inherit; color: #f5f5f7; background: #2c2c2e; border: 0; border-radius: 8px; padding: 7px 28px 7px 12px; cursor: pointer;
           -webkit-appearance: none; appearance: none;
           background-image: linear-gradient(45deg, transparent 50%, #8e8e93 50%), linear-gradient(135deg, #8e8e93 50%, transparent 50%);
           background-position: calc(100% - 15px) 52%, calc(100% - 10px) 52%; background-size: 5px 5px; background-repeat: no-repeat; }
  input[type=range] { width: 160px; accent-color: #0a84ff; }
  .srow { display: flex; align-items: center; gap: 10px; padding: 9px 0; }
  .srow .nm { width: 64px; flex: none; }
  .srow .bar { flex: 1; margin-top: 0; }
  .srow .val { width: 112px; flex: none; text-align: right; color: #c7c7cc; font-size: 13px; }
  .week { display: flex; align-items: flex-end; gap: 8px; height: 74px; padding: 6px 0 0; }
  .week .col { flex: 1; display: flex; flex-direction: column; align-items: center; gap: 4px; height: 100%; justify-content: flex-end; }
  .week .stack { width: 100%; max-width: 26px; display: flex; flex-direction: column-reverse; border-radius: 4px; overflow: hidden; background: #2c2c2e; }
  .week .lbl { font-size: 11px; color: #8e8e93; }
  .week .col.today .lbl { color: #f5f5f7; font-weight: 600; }
  .ok { color: #30d158; } .bad { color: #ff453a; }
  footer { color: #6e6e73; font-size: 12px; text-align: center; margin-top: 8px; }
</style></head>
<body><main>
  <header><div class="logo"></div><div><h1>AI 狀態指示燈</h1><div class="ver" id="ver"></div></div></header>

  <div class="card">
    <div class="row"><div class="k">螢幕</div><div class="v" id="serial"></div></div>
    <div class="row"><div class="k">韌體</div><div class="v" id="fw"></div></div>
    <div class="row"><div class="k">目前顯示</div><div class="v" id="frame"></div></div>
  </div>

  <div class="card">
    <div class="row"><div class="k">Claude</div><div class="v" id="quota"></div></div>
    <div class="row"><div class="k">ChatGPT</div><div class="v" id="q_chatgpt"></div></div>
    <div class="row"><div class="k">Gemini</div><div class="v" id="q_gemini"></div></div>
  </div>

  <div class="card" id="stats"></div>

  <div class="card items" id="items"></div>

  <div class="card">
    <div class="row sw"><div>螢幕保護<div class="hint">待命一段時間後顯示時鐘並調暗</div></div>
      <select id="saver">
        <option value="0">關閉</option><option value="1">1 分鐘</option><option value="3">3 分鐘</option>
        <option value="5">5 分鐘</option><option value="10">10 分鐘</option><option value="15">15 分鐘</option>
        <option value="30">30 分鐘</option><option value="60">1 小時</option>
      </select></div>
    <div class="row sw"><div>額度重置提醒<div class="hint">用量達到門檻後，重置前 5 分鐘和重置時通知你<br><a href="#" data-cmd="toast-test" style="color:#0a84ff;text-decoration:none">測試通知</a></div></div>
      <select id="remind">
        <option value="0">關閉</option><option value="50">用量 ≥ 50%</option><option value="80">用量 ≥ 80%</option>
        <option value="90">用量 ≥ 90%</option><option value="100">用完時</option>
      </select></div>
    <div class="row sw"><div>螢幕方向<div class="hint">依擺放方式選到畫面是正的為止，螢幕會馬上轉過去</div></div>
      <select id="rot">
        <option value="0">0°</option><option value="1">90°</option>
        <option value="2">180°</option><option value="3">270°</option>
      </select></div>
    <div class="row sw"><div>螢幕亮度</div><input type="range" id="bri" min="10" max="255" step="5"></div>
    <div class="row sw"><div>連接 Claude Code<div class="hint">終端機的 Agent 模式也會亮燈（生成中、等你允許、完成）。<br>會在 Claude Code 設定檔加入通知設定，修改前自動備份。</div><div class="hint" id="ccst"></div></div><div class="toggle" id="cc"></div></div>
    <div class="row sw"><div>開機自動執行</div><div class="toggle" id="auto"></div></div>
  </div>

  <div class="btns">
    <button id="demo">播放測試畫面</button>
    <button id="hide">隱藏視窗</button>
    <button id="quit">結束程式</button>
  </div>
  <footer>關閉視窗只是隱藏，程式會繼續在背景執行。<br>點右下角系統匣的圓形圖示（或再次雙擊 AIMonitor）就能打開這個視窗。</footer>
</main></body>`;
    document.getElementById("demo").onclick = () => window.aimonCmd("demo");
    document.getElementById("hide").onclick = () => window.aimonCmd("hide");
    document.getElementById("quit").onclick = () => { if (confirm("結束後螢幕會停止更新，確定要結束嗎？")) window.aimonCmd("quit"); };
    document.getElementById("bri").onchange = (e) => window.aimonCmd("bri:" + e.target.value);
    document.addEventListener("click", (e) => {
      const b = e.target.closest("[data-cmd]");
      if (!b) return;
      e.preventDefault();
      const c = b.getAttribute("data-cmd");
      if (c === "fwupdate" && !confirm("更新時螢幕會重新開機，大約 30 秒。過程中請不要拔掉 USB。要開始嗎？")) return;
      window.aimonCmd(c).then(render);
    });
    document.getElementById("rot").onchange = (e) => { window.aimonCmd("rot:" + e.target.value).then(render); };
    document.getElementById("remind").onchange = (e) => { window.aimonCmd("remind:" + e.target.value).then(render); };
    document.getElementById("saver").onchange = (e) => { window.aimonCmd("saver:" + e.target.value).then(render); };
    document.getElementById("cc").onclick = (e) => {
      const on = e.currentTarget.classList.contains("on");
      window.aimonCmd(on ? "cc-off" : "cc-on").then(render);
    };
    document.getElementById("auto").onclick = (e) => {
      const on = e.currentTarget.classList.contains("on");
      window.aimonCmd(on ? "autostart-off" : "autostart-on").then(render);
    };
  }

  function fmtLeft(epoch) {
    if (!epoch) return "";
    let s = epoch - Math.floor(Date.now() / 1000);
    if (s <= 0) return "";
    const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
    return d ? `${d} 天 ${h} 小時` : h ? `${h} 小時 ${m} 分` : `${m} 分`;
  }

  async function render() {
    let s;
    try { s = JSON.parse(await window.aimonStatus()); } catch (e) { return; }
    const $ = (id) => document.getElementById(id);
    $("ver").textContent = "版本 " + s.version;

    const ser = s.serial || {};
    $("serial").innerHTML = ser.connected
      ? `<span class="dot" style="background:#30d158"></span>已連線（${ser.port}）${ser.verified ? "" : `<div class="hint">尚未收到螢幕回覆（已收到 ${ser.rx || 0} bytes）</div>`}`
      : `<span class="dot" style="background:#ff453a"></span>未偵測到<div class="hint">用 USB 線插上螢幕即可，會自動連線。若燒錄工具或 Chrome 擴充功能正在使用序列埠，請先關閉。</div>`;

    const f = s.frame || {};
    $("frame").innerHTML = f.ai && f.ai !== "none"
      ? `<span class="dot" style="background:${COL[f.st]}"></span>${NAMES[f.ai] || f.ai} · ${ST[f.st] || f.st}`
      : `<span class="muted">沒有偵測到 AI App 或 AI 網頁</span>`;

    const q = s.quota;
    if (s.login === "out") {
      $("quota").innerHTML = `<span class="dot" style="background:#ffd60a"></span>尚未登入
        <div class="hint">登入一次就會記住，之後自動在背景查詢額度。建議用 Email 登入（Google 登入在這個視窗可能會被擋）。</div>
        <div class="btns" style="margin-top:10px"><button class="primary" id="login">登入 Claude</button></div>`;
      $("login").onclick = () => window.aimonCmd("login");
    } else if (q && q.p5 >= 0) {
      const left = fmtLeft(q.r5);
      $("quota").innerHTML = `5 小時 <b>${Math.round(q.p5)}%</b>${left ? `<span class="muted"> · ${left}後重置</span>` : ""}
        <div class="bar"><i style="width:${Math.min(100, q.p5)}%"></i></div>
        ${q.pw >= 0 ? `<div class="hint">本週 ${Math.round(q.pw)}%</div>` : ""}`;
    } else if (s.usageErr) {
      $("quota").innerHTML = `<span class="dot" style="background:#ff453a"></span>讀取失敗
        <div class="hint">已登入，但讀不到用量。請把這段截圖給開發者：</div>
        <details open><summary>詳細資訊</summary><code>${s.usageErr.replace(/[<&]/g, "")}</code></details>
        <div class="btns" style="margin-top:8px"><button id="retry">重新讀取</button><button id="login">重新登入</button></div>`;
      $("retry").onclick = () => window.aimonCmd("poll");
      $("login").onclick = () => window.aimonCmd("login");
    } else {
      $("quota").innerHTML = `<span class="muted">${s.login === "in" ? "讀取中…" : "檢查登入狀態中…"}</span>`;
    }

    // ChatGPT / Gemini
    const P = s.providers || {};
    for (const ai of ["chatgpt", "gemini"]) {
      const p = P[ai] || {}, name = NAMES[ai], el = $("q_" + ai);
      const q = p.quota;
      if (!p.available) { el.innerHTML = `<span class="muted">無法使用（內建瀏覽器未啟動）</span>`; continue; }
      if (q && (q.p5 >= 0 || q.pw >= 0)) {
        const wl = p.wlabel || "本週";
        const main = q.p5 >= 0 ? q.p5 : q.pw, label = q.p5 >= 0 ? (p.slabel || "5 小時") : wl;
        const left = fmtLeft(q.p5 >= 0 ? q.r5 : q.rw);
        el.innerHTML = `${label} <b>${Math.round(main)}%</b>${left ? `<span class="muted"> · ${left}後重置</span>` : ""}
          <div class="bar"><i style="width:${Math.min(100, main)}%"></i></div>
          ${q.p5 >= 0 && q.pw >= 0 ? `<div class="hint">${wl} ${Math.round(q.pw)}%</div>` : ""}
          ${ai === "chatgpt" ? `<div class="hint">這是 Codex 的用量；ChatGPT 一般聊天沒有公開的用量數字</div>` : ""}`;
      } else if (p.login === "out") {
        el.innerHTML = `<span class="dot" style="background:#ffd60a"></span>尚未登入
          <div class="btns" style="margin-top:8px"><button class="primary" data-cmd="login:${ai}">登入 ${name}</button></div>`;
      } else if (p.err) {
        el.innerHTML = `<span class="dot" style="background:#ff453a"></span>${p.err}
          <details><summary>詳細資訊（截圖給開發者）</summary><code>${(p.diag || "").replace(/[<&]/g, "")}</code></details>
          <div class="btns" style="margin-top:8px"><button data-cmd="poll:all">重新讀取</button><button data-cmd="login:${ai}">開啟 ${name}</button></div>`;
      } else {
        el.innerHTML = `<span class="muted">${p.login === "in" ? "讀取中…" : "檢查登入狀態中…"}</span>
          <div class="btns" style="margin-top:8px"><button data-cmd="login:${ai}">登入 / 開啟 ${name}</button></div>`;
      }
    }

    // 螢幕韌體
    const fw = s.firmware || {}, dev = ser.fw || "";
    const less = (a, b) => { const x = (a || "0").split(".").map(Number), y = (b || "0").split(".").map(Number); for (let i = 0; i < 3; i++) { if ((x[i] || 0) !== (y[i] || 0)) return (x[i] || 0) < (y[i] || 0); } return false; };
    if (fw.running) {
      $("fw").innerHTML = `${fw.msg || "更新中…"} ${fw.progress}%<div class="bar"><i style="width:${fw.progress}%;background:#0a84ff"></i></div><div class="hint">請不要拔掉 USB</div>`;
    } else if (fw.err) {
      $("fw").innerHTML = `<span class="bad">更新失敗：${fw.err}</span><div class="btns" style="margin-top:8px"><button data-cmd="fwupdate">再試一次</button></div>`;
    } else if (fw.msg) {
      $("fw").innerHTML = `<span class="ok">${fw.msg}</span>`;
    } else if (!ser.connected) {
      $("fw").innerHTML = `<span class="muted">內建 ${fw.bundled}</span>`;
    } else if (!dev || less(dev, fw.bundled)) {
      $("fw").innerHTML = `${dev ? dev : "舊版"} <span class="muted">→ 可更新到 ${fw.bundled}</span>
        <div class="btns" style="margin-top:8px"><button class="primary" data-cmd="fwupdate">更新螢幕韌體</button></div>`;
    } else {
      $("fw").innerHTML = `${dev} <span class="muted">· 已是最新 · <a href="#" data-cmd="fwupdate" style="color:inherit">重新寫入</a></span>`;
    }
    const bri = $("bri");
    if (document.activeElement !== bri) bri.value = s.brightness || 160;

    const items = s.items || [];
    $("items").innerHTML = items.length
      ? items.map((it) => {
          const [src] = it.key.split(":");
          const nm = src === "cc" ? ((it.buttons || [])[0] || "Claude Code").replace(/[<&]/g, "") : (NAMES[it.ai] || it.ai);
          return `<div class="it"><span class="dot" style="background:${COL[it.st]}"></span>${nm}
            <span class="muted">· ${SRC[src] || src}${it.focused ? " · 前景" : ""}</span> — ${ST[it.st] || it.st}
            <details><summary>偵測到的按鈕（除錯用）</summary><code>${(it.buttons || []).map((b) => b.replace(/[<&]/g, "")).join(" | ") || "（沒有）"}</code></details></div>`;
        }).join("")
      : `<div class="it muted">目前沒有偵測到 AI App 或 AI 網頁。<br>打開 Claude 桌面 App，或在瀏覽器切到 claude.ai / chatgpt.com / gemini.google.com 分頁。</div>`;
    $("items").innerHTML += `<details style="padding:8px 0 10px"><summary>偵測狀態（除錯用）</summary><code>${(s.monitor || "偵測程式尚未回報").replace(/[<&]/g, "")}</code></details>`;

    renderStats(s);
    const cc = s.claudeCode || {};
    $("cc").classList.toggle("on", !!cc.installed);
    let ccs = "";
    if (cc.err) ccs = `<span class="bad">${cc.err}</span>`;
    else if (cc.msg && cc.msg.startsWith("失敗")) ccs = `<span class="bad">${cc.msg}</span>`;
    else if (cc.installed) ccs = cc.sessions ? `<span class="ok">● 偵測到 ${cc.sessions} 個 Claude Code 工作階段</span>` : (cc.msg || "已連接，等待 Claude Code 啟動（已開著的請重新啟動）");
    $("ccst").innerHTML = ccs;
    $("auto").classList.toggle("on", !!s.autostart);
    const sv = $("saver");
    if (document.activeElement !== sv) {
      const v = String(s.saverMin ?? 5);
      if (![...sv.options].some((o) => o.value === v)) sv.add(new Option(v + " 分鐘", v));
      sv.value = v;
    }
    const ro = $("rot");
    if (document.activeElement !== ro) ro.value = String((s.rotation || 0) & 3);
    const rm = $("remind");
    if (document.activeElement !== rm) {
      const v = String(s.remindAt ?? 90);
      if (![...rm.options].some((o) => o.value === v)) rm.add(new Option("用量 ≥ " + v + "%", v));
      rm.value = v;
    }
  }

  const BRAND = { claude: "#f0855a", chatgpt: "#e5e5ea", gemini: "#64a8ff" };
  function fmtDur(sec) {
    sec = Math.round(sec || 0);
    if (sec < 60) return sec + " 秒";
    if (sec < 3600) return Math.round(sec / 60) + " 分";
    return Math.floor(sec / 3600) + " 小時 " + Math.floor((sec % 3600) / 60) + " 分";
  }
  let lastStats = "";
  function renderStats(s) {
    const key = JSON.stringify([s.today, s.week]);
    if (key === lastStats) return;
    lastStats = key;
    const today = s.today || [];
    let h = `<div class="row" style="border:0;padding-bottom:2px"><div class="k">今日統計</div><div class="v hint" style="margin:0">生成次數與生成時間（不含等你按允許的時間）</div></div>`;
    if (!today.length) {
      h += `<div class="hint" style="padding:4px 0 12px">今天還沒有偵測到生成。</div>`;
    } else {
      const max = Math.max(1, ...today.map((t) => t[2]));
      let tc = 0, ts = 0;
      for (const [ai, c, sec] of today) {
        tc += c; ts += sec;
        h += `<div class="srow"><div class="nm">${NAMES[ai] || ai}</div><div class="bar"><i style="width:${Math.max(3, (sec / max) * 100)}%;background:${BRAND[ai] || "#8e8e93"}"></i></div><div class="val">${c} 次 · ${fmtDur(sec)}</div></div>`;
      }
      h += `<div class="hint" style="padding:2px 0 8px">合計 ${tc} 次 · ${fmtDur(ts)}</div>`;
    }
    const week = s.week || [];
    if (week.some((d) => d.secs)) {
      const tot = (d) => Object.values(d.secs || {}).reduce((a, b) => a + b, 0);
      const wmax = Math.max(1, ...week.map(tot));
      const WD = "日一二三四五六";
      h += `<div class="hint" style="margin-top:4px">最近 7 天（生成時間）</div><div class="week">` + week.map((d, i) => {
        const segs = ["gemini", "chatgpt", "claude"].map((ai) => {
          const v = (d.secs || {})[ai] || 0;
          return v ? `<div style="height:${(v / wmax) * 52}px;background:${BRAND[ai]}" title="${NAMES[ai]} ${fmtDur(v)}"></div>` : "";
        }).join("");
        return `<div class="col${i === week.length - 1 ? " today" : ""}" title="${d.day} · ${fmtDur(tot(d))}"><div class="stack">${segs}</div><div class="lbl">${i === week.length - 1 ? "今天" : WD[d.wd]}</div></div>`;
      }).join("") + `</div><div style="height:10px"></div>`;
    }
    document.getElementById("stats").innerHTML = h;
  }

  function start() {
    // Cloudflare 驗證頁之類的非 JSON 內容：先讓它跑完（完成後會自動重新載入）
    const txt = (document.body && document.body.innerText) || "";
    if (!/^\s*[\[{]/.test(txt)) return;
    build(); render(); setInterval(render, 1000);
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start);
  else start();
})();
