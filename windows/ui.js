// 注入到 WebView2 每個頁面（只在 claude.ai 上作用）
// 1. window.__aimonPoll()：用登入狀態查 Claude 用量，回傳給程式
// 2. 在 /api/organizations 這個輕量頁面上畫出狀態頁
(function () {
  if (location.hostname !== "claude.ai") return;
  // 放進 innerHTML 的外來字串（螢幕回報的型號 / 版本 / ID、埠名稱）一律跳脫：只會成為文字或屬性值
  const H = (t) => String(t ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);

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
  // 原廠圖示（路徑取自 Simple Icons，CC0；內嵌 SVG，不從外部載入）
  const LOGOS = {
    claude: `<svg viewBox="0 0 24 24"><path fill="#D97757" d="m4.7144 15.9555 4.7174-2.6471.079-.2307-.079-.1275h-.2307l-.7893-.0486-2.6956-.0729-2.3375-.0971-2.2646-.1214-.5707-.1215-.5343-.7042.0546-.3522.4797-.3218.686.0608 1.5179.1032 2.2767.1578 1.6514.0972 2.4468.255h.3886l.0546-.1579-.1336-.0971-.1032-.0972L6.973 9.8356l-2.55-1.6879-1.3356-.9714-.7225-.4918-.3643-.4614-.1578-1.0078.6557-.7225.8803.0607.2246.0607.8925.686 1.9064 1.4754 2.4893 1.8336.3643.3035.1457-.1032.0182-.0728-.164-.2733-1.3539-2.4467-1.445-2.4893-.6435-1.032-.17-.6194c-.0607-.255-.1032-.4674-.1032-.7285L6.287.1335 6.6997 0l.9957.1336.419.3642.6192 1.4147 1.0018 2.2282 1.5543 3.0296.4553.8985.2429.8318.091.255h.1579v-.1457l.1275-1.706.2368-2.0947.2307-2.6957.0789-.7589.3764-.9107.7468-.4918.5828.2793.4797.686-.0668.4433-.2853 1.8517-.5586 2.9021-.3643 1.9429h.2125l.2429-.2429.9835-1.3053 1.6514-2.0643.7286-.8196.85-.9046.5464-.4311h1.0321l.759 1.1293-.34 1.1657-1.0625 1.3478-.8804 1.1414-1.2628 1.7-.7893 1.36.0729.1093.1882-.0183 2.8535-.607 1.5421-.2794 1.8396-.3157.8318.3886.091.3946-.3278.8075-1.967.4857-2.3072.4614-3.4364.8136-.0425.0304.0486.0607 1.5482.1457.6618.0364h1.621l3.0175.2247.7892.522.4736.6376-.079.4857-1.2142.6193-1.6393-.3886-3.825-.9107-1.3113-.3279h-.1822v.1093l1.0929 1.0686 2.0035 1.8092 2.5075 2.3314.1275.5768-.3218.4554-.34-.0486-2.2039-1.6575-.85-.7468-1.9246-1.621h-.1275v.17l.4432.6496 2.3436 3.5214.1214 1.0807-.17.3521-.6071.2125-.6679-.1214-1.3721-1.9246L14.38 17.959l-1.1414-1.9428-.1397.079-.674 7.2552-.3156.3703-.7286.2793-.6071-.4614-.3218-.7468.3218-1.4753.3886-1.9246.3157-1.53.2853-1.9004.17-.6314-.0121-.0425-.1397.0182-1.4328 1.9672-2.1796 2.9446-1.7243 1.8456-.4128.164-.7164-.3704.0667-.6618.4008-.5889 2.386-3.0357 1.4389-1.882.929-1.0868-.0062-.1579h-.0546l-6.3385 4.1164-1.1293.1457-.4857-.4554.0608-.7467.2307-.2429 1.9064-1.3114Z"/></svg>`,
    chatgpt: `<svg viewBox="0 0 24 24"><path fill="#FFFFFF" d="M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997Z"/></svg>`,
    gemini: `<svg viewBox="0 0 24 24"><defs><linearGradient id="gmg" x1="0" y1="1" x2="1" y2="0"><stop offset="0" stop-color="#4796E3"/><stop offset=".5" stop-color="#9177C7"/><stop offset="1" stop-color="#CA6673"/></linearGradient></defs><path fill="url(#gmg)" d="M11.04 19.32Q12 21.51 12 24q0-2.49.93-4.68.96-2.19 2.58-3.81t3.81-2.55Q21.51 12 24 12q-2.49 0-4.68-.93a12.3 12.3 0 0 1-3.81-2.58 12.3 12.3 0 0 1-2.58-3.81Q12 2.49 12 0q0 2.49-.96 4.68-.93 2.19-2.55 3.81a12.3 12.3 0 0 1-3.81 2.58Q2.49 12 0 12q2.49 0 4.68.96 2.19.93 3.81 2.55t2.55 3.81"/></svg>`,
  };
  LOGOS.chatgpt = LOGOS.chatgpt.replace('fill="#FFFFFF"', 'style="fill:var(--gpt)"');   // 淺色主題時白色看不到，跟著主題換色
  // 同一頁放好幾個 Gemini 圖示時，漸層的 id 要各自不同：url(#gmg) 會指到第一個，它在隱藏的分頁裡就整個畫不出來
  let logoN = 0;
  const logo = (ai) => { const id = "gmg" + ++logoN; return (LOGOS[ai] || "").replace(/gmg/g, id); };
  const ST = { idle: "待命", done: "完成", working: "生成中", review: "需審核", error: "錯誤" };
  const COL = { idle: "var(--mut)", done: "var(--grn)", working: "var(--grn)", review: "var(--yel)", error: "var(--red)" };
  const SRC = { app: "桌面 App", web: "瀏覽器", ext: "網頁", cc: "Agent" };   // Claude Code 分不出是 App 的 Code 分頁還是終端機

  function build() {
    document.documentElement.innerHTML = `
<head><meta charset="utf-8"><title>AI 狀態指示燈</title>
<style>
  /* 淺色 / 深色兩套顏色（右上角或設定分頁切換，記在 localStorage）。版面參考使用者給的淺色元件圖：
     白色圓角卡片外面再一圈淡灰（box-shadow 外框）、膠囊形分頁、綠色漸層開關、黑色主要按鈕 */
  :root, html[data-theme=dark] { color-scheme: dark;
    --bg:#000; --tile:#121214; --card:#1c1c1e; --card2:#2c2c2e; --fg:#f5f5f7; --fg2:#c7c7cc; --mut:#8e8e93; --mut2:#6e6e73;
    --line:rgba(255,255,255,.08); --track:#2c2c2e; --track2:#242426; --gpt:#e5e5ea; --blue:#0a84ff; --yel:#ffd60a; --red:#ff453a; --grn:#30d158;
    --pop:#2c2c2e; --shadow:0 0 0 0 transparent; --segon:#3a3a3c; --btn:#2c2c2e; --kbtn:#f5f5f7; --kbtnfg:#000; --grid:#2c2c2e; --grid2:#1c1c1e;
    --upd:#0a2a4a; --upderr:#3a1715; --hero:#1c1c1e; }
  html[data-theme=light] { color-scheme: light;
    --bg:#f4f4f2; --tile:#e9e9e6; --card:#fff; --card2:#f1f1ef; --fg:#1d1d1f; --fg2:#3a3a3c; --mut:#86868b; --mut2:#98989d;
    --line:#ececea; --track:#e6e6e3; --track2:#ededea; --gpt:#1d1d1f; --blue:#007aff; --yel:#b88a00; --red:#e5342b; --grn:#24a148;
    --pop:#fff; --shadow:0 1px 2px rgba(0,0,0,.04), 0 4px 14px rgba(0,0,0,.05); --segon:#fff; --btn:#fff; --kbtn:#111; --kbtnfg:#fff; --grid:#e8e8e5; --grid2:#f0f0ee;
    --upd:#e3eeff; --upderr:#fde8e6; --hero:#111; }
  * { box-sizing: border-box; }
  body { margin: 0; background: var(--bg); color: var(--fg); font: 14px/1.5 -apple-system, "Segoe UI Variable", "Segoe UI", "Microsoft JhengHei UI", sans-serif; -webkit-user-select: none; user-select: none; }
  main { padding: 22px 20px 28px; max-width: 520px; margin: 0 auto; }
  header { display: flex; align-items: center; gap: 12px; margin-bottom: 16px; }
  .logo { width: 40px; height: 40px; border-radius: 50%; border: 3px solid #30d158; display: grid; place-items: center; flex: none; }
  .logo::after { content: ""; width: 14px; height: 14px; border-radius: 50%; border: 3px solid #f0855a; }
  h1 { font-size: 20px; font-weight: 650; margin: 0; letter-spacing: -0.01em; }
  .ver { color: var(--mut); font-size: 12px; }
  .copy { margin-top: 10px; }
  .themebtn { margin-left: auto; width: 36px; height: 36px; padding: 0; border-radius: 50%; display: grid; place-items: center; background: var(--card); color: var(--fg); box-shadow: var(--shadow); flex: none; }
  .themebtn svg { width: 18px; height: 18px; }
  .card { background: var(--card); border-radius: 18px; padding: 2px 16px; margin: 7px 0 21px; box-shadow: 0 0 0 7px var(--tile), var(--shadow); }
  .row { display: flex; align-items: center; gap: 12px; padding: 11px 0; border-bottom: 1px solid var(--line); }
  .row:last-child { border-bottom: 0; }
  .k { color: var(--mut); width: 92px; flex: none; }
  .k.ai { display: flex; align-items: center; gap: 8px; color: var(--fg); font-weight: 600; }
  .k.ai svg { width: 18px; height: 18px; flex: none; }
  .v { flex: 1; min-width: 0; }
  .dot { display: inline-block; width: 9px; height: 9px; border-radius: 50%; margin-right: 7px; vertical-align: 1px; background: var(--mut2); }
  .hint { color: var(--mut); font-size: 12.5px; margin-top: 2px; }
  .bar { height: 6px; border-radius: 3px; background: var(--track); overflow: hidden; margin-top: 6px; }
  .bar i { display: block; height: 100%; background: #f0855a; border-radius: 3px; }
  button { font: inherit; font-weight: 600; border: 0; border-radius: 999px; padding: 9px 16px; cursor: pointer; background: var(--btn); color: var(--fg); box-shadow: var(--shadow), inset 0 0 0 1px var(--line); }
  button.primary { background: var(--kbtn); color: var(--kbtnfg); box-shadow: none; }
  button:hover { filter: brightness(1.08); }
  html[data-theme=light] button:hover { filter: brightness(.97); }
  .btns { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 4px; }
  section > .btns button { flex: 1; padding: 12px; }
  .items .it { padding: 9px 0; border-bottom: 1px solid var(--line); }
  .items .it:last-child { border-bottom: 0; }
  .muted { color: var(--mut2); }
  details { margin-top: 6px; color: var(--mut); font-size: 12px; }
  details code { color: var(--fg2); word-break: break-all; }
  .sw { display: flex; align-items: center; justify-content: space-between; }
  .sw > div:first-child { flex: 1; min-width: 0; }
  .toggle { width: 46px; height: 28px; border-radius: 14px; background: var(--track); position: relative; cursor: pointer; flex: none; transition: background .15s; }
  .toggle.on { background: linear-gradient(90deg, #3aa757, #8fd16a); }
  .toggle::after { content: ""; position: absolute; top: 3px; left: 3px; width: 22px; height: 22px; border-radius: 50%; background: #fff; box-shadow: 0 1px 3px rgba(0,0,0,.25); transition: left .15s; }
  .toggle.on::after { left: 21px; }
  select { font: inherit; font-weight: 500; color: var(--fg); background: var(--card2); border: 0; border-radius: 999px; padding: 6px 30px 6px 14px; cursor: pointer;
           -webkit-appearance: none; appearance: none;
           background-image: linear-gradient(45deg, transparent 50%, var(--mut) 50%), linear-gradient(135deg, var(--mut) 50%, transparent 50%);
           background-position: calc(100% - 16px) 52%, calc(100% - 11px) 52%; background-size: 5px 5px; background-repeat: no-repeat; }
  select option { background: var(--card); color: var(--fg); }
  input[type=range] { width: 170px; accent-color: #3aa757; }
  .srow { display: flex; align-items: center; gap: 10px; padding: 9px 0; }
  .srow .nm { width: 20px; height: 20px; flex: none; display: grid; place-items: center; }
  .srow .nm svg { width: 20px; height: 20px; }
  .srow .bar { flex: 1; margin-top: 0; }
  .srow .val { width: 112px; flex: none; text-align: right; color: var(--fg2); font-size: 13px; }
  /* 最近 7 天：圖拉高、長條細一點、上面寫總時間；滑鼠指著的那天放大、其他天變暗、跳出三家各多久（使用者覺得原本太擠） */
  .whead { display: flex; justify-content: space-between; align-items: center; margin-top: 4px; }
  .leg { display: flex; gap: 10px; font-size: 11.5px; color: var(--mut); }
  .leg i { display: inline-block; width: 8px; height: 8px; border-radius: 2px; margin-right: 4px; }
  .week { position: relative; display: flex; align-items: flex-end; gap: 14px; height: 150px; padding: 10px 6px 0; margin-top: 6px; }
  .week .base { position: absolute; left: 0; right: 0; bottom: 22px; border-top: 1px solid var(--line); pointer-events: none; }
  .week .col { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: flex-end; height: 100%; gap: 5px; transition: opacity .15s; cursor: default; }
  .week .stack { width: 18px; display: flex; flex-direction: column-reverse; border-radius: 5px 5px 2px 2px; overflow: hidden; transition: width .15s, box-shadow .15s; }
  .week .val { font-size: 10.5px; color: var(--mut); white-space: nowrap; }
  .week .lbl { font-size: 11px; color: var(--mut); height: 16px; }
  .week .col.today .lbl, .week .col.today .val { color: var(--fg); }
  .week .col.today .lbl { font-weight: 600; }
  .week:hover .col { opacity: .35; }
  .week .col:hover { opacity: 1; }
  .week .col:hover .stack { width: 28px; box-shadow: 0 0 0 2px var(--line); }
  .week .col:hover .val { color: var(--fg); font-weight: 600; font-size: 12px; }
  .wtip { position: absolute; top: 4px; z-index: 3; display: none; background: var(--pop); border: 0.5px solid var(--line); border-radius: 12px;
          padding: 8px 12px; font-size: 12px; box-shadow: 0 8px 24px rgba(0,0,0,.18); white-space: nowrap; pointer-events: none; }
  .wtip .d { color: var(--mut); margin-bottom: 4px; }
  .wtip .r { display: flex; justify-content: space-between; gap: 18px; }
  .wtip i { display: inline-block; width: 8px; height: 8px; border-radius: 2px; margin-right: 6px; }
  /* 額度卡片：週額度也一條細長條（淡一點）＋重置倒數；滑鼠指著顯示精確的重置日期時間 */
  .bar.wk { height: 3px; margin-top: 4px; background: var(--track2); }
  .bar.wk i { opacity: .55; }
  .wline { margin-top: 8px; font-size: 12.5px; color: var(--fg2); }
  .wline b { color: var(--fg); font-weight: 600; }
  .qbox { position: relative; }
  .rtip { position: absolute; right: 0; bottom: calc(100% + 6px); z-index: 3; display: none; background: var(--pop); border: 0.5px solid var(--line);
          border-radius: 12px; padding: 8px 12px; font-size: 12.5px; box-shadow: 0 8px 24px rgba(0,0,0,.18); white-space: nowrap; pointer-events: none; }
  .qbox:hover .rtip { display: block; }
  .rtip .tk { color: var(--mut); display: inline-block; min-width: 44px; margin-right: 6px; }
  .ok { color: var(--grn); } .bad { color: var(--red); }
  footer { color: var(--mut2); font-size: 12px; text-align: center; margin-top: 8px; }
  .ask { position: fixed; inset: 0; z-index: 10; display: grid; place-items: center; padding: 20px; background: rgba(0,0,0,.45); }
  .ask .box { width: 100%; max-width: 360px; padding: 20px 18px 14px; border-radius: 20px; background: var(--card); box-shadow: 0 12px 40px rgba(0,0,0,.3); }
  .ask .msg { margin-bottom: 14px; line-height: 1.6; }
  .ask .btns { justify-content: flex-end; }
  .upd { display: flex; align-items: center; gap: 10px; padding: 10px 14px; margin-bottom: 14px; border-radius: 16px; background: var(--upd); }
  .upd.err { background: var(--upderr); }
  .upd .v { color: var(--fg); }
  .upd button { padding: 7px 14px; flex: none; }
  .upd .bar { background: rgba(127,127,127,.2); }
  .tabs { display: flex; gap: 2px; padding: 4px; margin-bottom: 18px; border-radius: 999px; background: var(--tile); }
  .tabs button { flex: 1; padding: 7px 0; border-radius: 999px; background: transparent; color: var(--mut); font-weight: 500; box-shadow: none; }
  .tabs button.on { background: var(--segon); color: var(--fg); font-weight: 600; box-shadow: 0 1px 3px rgba(0,0,0,.1); }
  [data-pane] { display: none; }
  details.list { margin: 0; color: inherit; font-size: inherit; }
  details.list summary { padding: 11px 0; cursor: pointer; color: var(--fg2); }
  details.list[open] summary { border-bottom: 1px solid var(--line); }
  [data-pane].on { display: block; }
  input.txt { font: inherit; width: 100%; color: var(--fg); background: var(--card2); border: 0; border-radius: 12px; padding: 10px 14px; margin-top: 8px; -webkit-user-select: text; user-select: text; }
  input.txt:focus { outline: 2px solid var(--blue); }
  .wap { cursor: pointer; }
  .wap:hover { background: var(--card2); }
  .wap .nm { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .wap .sig { flex: none; color: var(--mut); font-size: 12.5px; }
  .qbar { display: flex; justify-content: space-between; gap: 8px; padding: 2px 0 8px; }
  .seg { display: flex; gap: 2px; padding: 3px; border-radius: 999px; background: var(--card2); }
  .seg button { padding: 4px 12px; border-radius: 999px; background: transparent; color: var(--mut); font-weight: 600; font-size: 12.5px; display: flex; align-items: center; box-shadow: none; }
  .seg button.on { background: var(--segon); color: var(--fg); box-shadow: 0 1px 3px rgba(0,0,0,.1); }
  .seg button svg { width: 16px; height: 16px; }
  .qleg { display: flex; gap: 14px; font-size: 12px; color: var(--mut); padding-bottom: 4px; }
  .qleg i { display: inline-block; width: 16px; height: 0; border-top: 2px solid; vertical-align: 3px; margin-right: 5px; }
  #qplot { position: relative; padding-bottom: 10px; }
  #qplot svg { display: block; width: 100%; }
  #qplot text { font: 11px -apple-system, "Segoe UI", sans-serif; fill: var(--mut); }
  .qtip { position: absolute; top: 0; pointer-events: none; background: var(--pop); border: 0.5px solid var(--line); border-radius: 10px; padding: 6px 9px; font-size: 12px; line-height: 1.5; white-space: nowrap; box-shadow: 0 4px 12px rgba(0,0,0,.18); }
  /* 狀態分頁最上面：螢幕正在顯示的 AI（黑色卡片，跟螢幕一樣的額度圓環）。兩種主題都是深色底，顏色固定 */
  .hero { --gpt:#e5e5ea; --grn:#30d158; --yel:#ffd60a; --red:#ff453a; --mut:#8e8e93; --mut2:#8e8e93;
          display: flex; align-items: center; gap: 18px; background: var(--hero); color: #fff; border-radius: 22px; padding: 16px 18px; margin: 0 0 18px; }
  html[data-theme=dark] .hero, :root:not([data-theme]) .hero { margin: 7px 0 21px; box-shadow: 0 0 0 7px var(--tile); }
  .hero .ring { position: relative; width: 92px; height: 92px; flex: none; }
  .hero .ring svg { position: absolute; inset: 0; }
  .hero .num { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; font-size: 22px; font-weight: 650; line-height: 1.1; }
  .hero .num small { font-size: 10.5px; font-weight: 500; color: #9a9a9f; margin-top: 3px; }
  .hero .nm { display: flex; align-items: center; gap: 8px; font-size: 16px; font-weight: 650; }
  .hero .nm svg { width: 22px; height: 22px; }
  .hero .pill { display: inline-flex; align-items: center; border-radius: 999px; padding: 2px 10px; margin: 8px 0 4px; font-size: 12px; font-weight: 600; background: rgba(255,255,255,.1); }
  .hero .pill .dot { width: 7px; height: 7px; margin-right: 6px; vertical-align: 0; }
  .hero .sub { font-size: 12.5px; color: #9a9a9f; }
</style></head>
<body><main>
  <header><div class="logo"></div><div><h1>AI 狀態指示燈</h1><div class="ver" id="ver"></div></div><button class="themebtn" id="theme"></button></header>
  <div id="upd"></div>
  <nav class="tabs"><button data-tab="status">狀態</button><button data-tab="stats">統計</button><button data-tab="wifi">WiFi</button><button data-tab="settings">設定</button></nav>

  <section data-pane="status">
  <div class="hero" id="hero"></div>
  <div class="card">
    <div class="row"><div class="k">螢幕</div><div class="v" id="serial"></div></div>
    <div class="row"><div class="k">韌體</div><div class="v" id="fw"></div></div>
  </div>

  <div class="card">
    <div class="row"><div class="k ai">${LOGOS.claude}Claude</div><div class="v" id="quota"></div></div>
    <div class="row"><div class="k ai">${LOGOS.chatgpt}ChatGPT</div><div class="v" id="q_chatgpt"></div></div>
    <div class="row"><div class="k ai">${LOGOS.gemini}Gemini</div><div class="v" id="q_gemini"></div></div>
  </div>

  <div class="card items" id="items"></div>
  </section>

  <section data-pane="stats">
  <div class="card" id="stats"></div>
  <div class="card" id="qcard">
    <div class="row" style="border:0;padding-bottom:2px"><div class="k">額度用量</div></div>
    <div class="qbar"><div class="seg" id="qai"></div><div class="seg"><button data-qd="1">24 小時</button><button data-qd="7">7 天</button></div></div>
    <div class="qleg" id="qleg"></div>
    <div id="qplot"></div>
  </div>
  </section>

  <section data-pane="wifi">
  <div id="wifibox"></div>
  <div class="hint" style="padding:0 4px 12px">連上後螢幕接充電器也能顯示（跟電腦同一個網路）。只支援 2.4 GHz；麥克風、更新韌體要插 USB。</div>
  </section>

  <section data-pane="settings">
  <div class="card">
    <div class="row sw"><div>連接 Claude Code<div class="hint" id="ccst"></div></div><div class="toggle" id="cc"></div></div>
    <div class="row sw"><div>網頁版<div class="hint">要裝瀏覽器擴充功能</div><div class="hint" id="extst"></div></div><div class="toggle" id="ext"></div></div>
  </div>
  <div class="card" id="scrset"></div>
  <div class="card">
    <div class="row sw"><div>螢幕保護<div class="hint">待命後顯示時鐘</div></div>
      <select id="saver">
        <option value="0">關閉</option><option value="30s">30 秒</option><option value="1">1 分鐘</option><option value="3">3 分鐘</option>
        <option value="5">5 分鐘</option><option value="10">10 分鐘</option><option value="15">15 分鐘</option>
        <option value="30">30 分鐘</option><option value="60">1 小時</option>
      </select></div>
    <div class="row sw"><div>額度重置提醒<div class="hint"><a href="#" data-cmd="toast-test" style="color:var(--blue);text-decoration:none">測試</a></div></div>
      <select id="remind">
        <option value="0">關閉</option><option value="50">用量 ≥ 50%</option><option value="80">用量 ≥ 80%</option>
        <option value="90">用量 ≥ 90%</option><option value="100">用完時</option>
      </select></div>
    <div class="row sw"><div>外觀</div><div class="seg"><button data-theme-set="light">淺色</button><button data-theme-set="dark">深色</button></div></div>
    <div class="row sw"><div>開機自動執行</div><div class="toggle" id="auto"></div></div>
    <div class="row sw"><div>檢查更新<div class="hint" id="updst"></div></div><div class="toggle" id="updchk"></div></div>
  </div>

  <div class="btns">
    <button id="demo">播放測試畫面</button>
    <button id="hide">隱藏視窗</button>
    <button id="quit">結束程式</button>
  </div>
  <div class="items" id="dbgbox"></div>
  </section>
  <footer>關閉視窗只會隱藏，程式繼續在背景執行（系統匣的圓形圖示可以再打開）<div class="copy" id="copy"></div></footer>
</main></body>`;
    const showTab = (t) => {
      if (!document.querySelector(`[data-pane="${t}"]`)) t = "status";
      for (const el of document.querySelectorAll("[data-tab]")) el.classList.toggle("on", el.dataset.tab === t);
      for (const el of document.querySelectorAll("[data-pane]")) el.classList.toggle("on", el.dataset.pane === t);
      try { localStorage.setItem("aimonTab", t); } catch (_) {}
    };
    for (const el of document.querySelectorAll("[data-tab]")) el.onclick = () => showTab(el.dataset.tab);
    let tab = "status";
    try { tab = localStorage.getItem("aimonTab") || tab; } catch (_) {}
    showTab(tab);
    let th = "dark";
    try { th = localStorage.getItem("aimonTheme") || th; } catch (_) {}
    setTheme(th);
    document.getElementById("theme").onclick = () => setTheme(document.documentElement.dataset.theme === "light" ? "dark" : "light");
    for (const b of document.querySelectorAll("[data-theme-set]")) b.onclick = () => setTheme(b.dataset.themeSet);
    document.getElementById("demo").onclick = () => window.aimonCmd("demo");
    document.getElementById("hide").onclick = () => window.aimonCmd("hide");
    document.getElementById("quit").onclick = async () => { if (await ask("結束後螢幕會停止更新，確定要結束嗎？", "結束")) window.aimonCmd("quit"); };
    document.addEventListener("click", async (e) => {
      const b = e.target.closest("[data-cmd]");
      if (!b) return;
      e.preventDefault();
      const c = b.getAttribute("data-cmd");
      if (c === "appupdate" && !(await ask("會下載新版、驗證後自動換掉目前的程式並重新開啟（設定會保留）。要開始嗎？", "更新"))) return;
      // 按鈕送的是 fwupdate:<埠>（指定哪塊螢幕），以前只比對 "fwupdate"，確認框不會出現、一按就開始燒
      if (c === "fwupdate" || c.startsWith("fwupdate:")) {
        const port = c.slice("fwupdate:".length), who = port ? `${scrNames[port] || "螢幕"}（${port}）` : "螢幕";
        if (!(await ask(`要更新${who}的韌體嗎？更新時螢幕會重新開機，大約 30 秒。過程中請不要拔掉 USB。`, "更新"))) return;
      }
      window.aimonCmd(c).then(render);
    });
    // 每塊螢幕的方向 / 亮度 / 外接 LED（scrset 裡的欄位帶 data-scr = 螢幕的 key）
    document.addEventListener("change", (e) => {
      const el = e.target.closest("[data-scr]");
      if (el) window.aimonCmd(`scr:${el.dataset.scr}:${el.dataset.k}:${el.value}`).then(render);
    });
    document.getElementById("remind").onchange = (e) => { window.aimonCmd("remind:" + e.target.value).then(render); };
    document.getElementById("saver").onchange = (e) => { window.aimonCmd("saver:" + e.target.value).then(render); };
    document.getElementById("cc").onclick = (e) => {
      const on = e.currentTarget.classList.contains("on");
      window.aimonCmd(on ? "cc-off" : "cc-on").then(render);
    };
    document.getElementById("ext").onclick = (e) => {
      const on = e.currentTarget.classList.contains("on");
      window.aimonCmd(on ? "ext-off" : "ext-on").then(render);
    };
    document.getElementById("auto").onclick = (e) => {
      const on = e.currentTarget.classList.contains("on");
      window.aimonCmd(on ? "autostart-off" : "autostart-on").then(render);
    };
    document.getElementById("updchk").onclick = (e) => {
      const on = e.currentTarget.classList.contains("on");
      window.aimonCmd(on ? "updchk-off" : "updchk-on").then(render);
    };
  }

  var scrNames = {}; // 埠 → 螢幕種類（render 時更新）

  // 淺色 / 深色：顏色都是 CSS 變數，換 html 的 data-theme 就好；額度曲線的 SVG 也用變數，不用重畫
  const SUN = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="4.5"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>`;
  const MOON = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/></svg>`;
  function setTheme(t) {
    t = t === "light" ? "light" : "dark";
    document.documentElement.dataset.theme = t;
    try { localStorage.setItem("aimonTheme", t); } catch (_) {}
    const b = document.getElementById("theme");
    b.innerHTML = t === "light" ? MOON : SUN;
    b.title = b.ariaLabel = t === "light" ? "切換成深色" : "切換成淺色";
    for (const x of document.querySelectorAll("[data-theme-set]")) x.classList.toggle("on", x.dataset.themeSet === t);
  }

  // 頁面內的確認框：WebView2 內建的 confirm() 是固定寬度，視窗窄時右半邊（含「確定」）會被切掉
  function ask(msg, ok) {
    return new Promise((resolve) => {
      const d = document.createElement("div");
      d.className = "ask";
      d.innerHTML = `<div class="box"><div class="msg"></div><div class="btns"><button data-a="0">取消</button><button class="primary" data-a="1"></button></div></div>`;
      d.querySelector(".msg").textContent = msg;
      d.querySelector('[data-a="1"]').textContent = ok || "確定";
      const done = (v) => { d.remove(); document.removeEventListener("keydown", key, true); resolve(v); };
      const key = (e) => { if (e.key === "Escape") done(false); else if (e.key === "Enter") done(true); };
      d.addEventListener("click", (e) => {
        e.stopPropagation();
        const a = e.target.closest("[data-a]");
        if (a) done(a.dataset.a === "1");
        else if (e.target === d) done(false);
      });
      document.addEventListener("keydown", key, true);
      document.body.appendChild(d);
      d.querySelector('[data-a="1"]').focus();
    });
  }

  function fmtLeft(epoch) {
    if (!epoch) return "";
    let s = epoch - Math.floor(Date.now() / 1000);
    if (s <= 0) return "";
    const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
    return d ? `${d} 天 ${h} 小時` : h ? `${h} 小時 ${m} 分` : `${m} 分`;
  }
  // 精確的重置時間：今天 / 明天 15:30，再遠寫 11/14（五）17:00
  function fmtAt(epoch) {
    if (!epoch) return "";
    const t = new Date(epoch * 1000), n = new Date();
    const hm = String(t.getHours()).padStart(2, "0") + ":" + String(t.getMinutes()).padStart(2, "0");
    const d0 = new Date(n.getFullYear(), n.getMonth(), n.getDate()), d1 = new Date(t.getFullYear(), t.getMonth(), t.getDate());
    const dd = Math.round((d1 - d0) / 86400000);
    if (dd === 0) return "今天 " + hm;
    if (dd === 1) return "明天 " + hm;
    return `${t.getMonth() + 1}/${t.getDate()}（${"日一二三四五六"[t.getDay()]}）${hm}`;
  }
  // 額度卡片：短週期一條長條、長週期（本週 / 30 天）一條細的淡長條，各有重置倒數；滑鼠指著顯示精確重置時間
  let qRed = 90;   // 額度變紅的門檻（設定的「額度重置提醒」，0 = 不變紅），render 時更新
  function quotaBlock(ai, sl, p5, r5, wl, pw, rw) {
    const line = (lb, p, r, thin) => {
      const left = fmtLeft(r), tail = left ? `<span class="muted"> · ${left}後重置</span>` : "";
      const c = qRed > 0 && p >= qRed ? "var(--red)" : BRAND[ai] || "var(--mut)";
      return thin
        ? `<div class="wline">${lb} <b>${Math.round(p)}%</b>${tail}</div><div class="bar wk"><i style="width:${Math.min(100, p)}%;background:${c}"></i></div>`
        : `${lb} <b>${Math.round(p)}%</b>${tail}<div class="bar"><i style="width:${Math.min(100, p)}%;background:${c}"></i></div>`;
    };
    let h = "", tip = "";
    if (p5 >= 0) { h += line(sl, p5, r5, false); if (r5) tip += `<div><span class="tk">${sl}</span>${Math.round(p5)}% · ${fmtAt(r5)} 重置</div>`; }
    if (pw >= 0) { h += line(wl, pw, rw, p5 >= 0); if (rw) tip += `<div><span class="tk">${wl}</span>${Math.round(pw)}% · ${fmtAt(rw)} 重置</div>`; }
    return `<div class="qbox">${h}${tip ? `<div class="rtip">${tip}</div>` : ""}</div>`;
  }

  async function render() {
    let s;
    try { s = JSON.parse(await window.aimonStatus()); } catch (e) { return; }
    qRed = s.remindAt ?? 90;
    const $ = (id) => document.getElementById(id);
    $("ver").textContent = "版本 " + s.version;
    $("copy").textContent = "© " + s.releaseYear + " " + s.author;

    // 螢幕可以同時接好幾個；還沒回應的埠（可能是其他 USB 裝置）只在沒有任何螢幕時顯示
    const BOARD = { c3: "1.28 吋 LCD", s3a175: "1.75 吋 AMOLED" };
    const allScr = s.screens || [], scr = allScr.filter((x) => x.verified);
    scrNames = Object.fromEntries(scr.map((x) => [x.port, BOARD[x.hw || "c3"] || x.hw])); // 更新韌體的確認框用（textContent，不用跳脫）
    $("serial").innerHTML = scr.length
      ? scr.map((x) => `<div><span class="dot" style="background:var(--grn)"></span>${H(BOARD[x.hw || "c3"] || x.hw)}（${H(x.port)}）</div>`).join("")
      : allScr.length
        ? `<span class="dot" style="background:var(--yel)"></span>已連線（${H(allScr.map((x) => x.port).join("、"))}）<div class="hint">尚未收到螢幕回覆（已收到 ${allScr.reduce((n, x) => n + (x.rx || 0), 0)} bytes）</div>`
        : `<span class="dot" style="background:var(--red)"></span>未偵測到<div class="hint">用 USB 線插上螢幕即可，會自動連線。若燒錄工具或 Chrome 擴充功能正在使用序列埠，請先關閉。</div>`;

    renderHero(s);

    const q = s.quota;
    if (s.login === "out") {
      $("quota").innerHTML = `<span class="dot" style="background:var(--yel)"></span>尚未登入
        <div class="hint">登入一次就會記住，之後自動在背景查詢額度。建議用 Email 登入（Google 登入在這個視窗可能會被擋）。</div>
        <div class="btns" style="margin-top:10px"><button class="primary" id="login">登入 Claude</button></div>`;
      $("login").onclick = () => window.aimonCmd("login");
    } else if (q && q.p5 >= 0) {
      $("quota").innerHTML = quotaBlock("claude", "5 小時", q.p5, q.r5, "本週", q.pw, q.rw);
    } else if (s.usageErr) {
      $("quota").innerHTML = `<span class="dot" style="background:var(--red)"></span>讀取失敗
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
        // cached = 重開後還沒讀到、快取又沒存週期（1.13.35 以前存的）：不知道是哪個週期，寫「上次用量」、不顯示第二個額度
        const wl = p.wlabel || "本週";
        if (p.cached) {   // 不知道是哪個週期（舊快取）：寫「上次用量」、只顯示一條
          const main = q.p5 >= 0 ? q.p5 : q.pw;
          el.innerHTML = quotaBlock(ai, "上次用量", main, q.p5 >= 0 ? q.r5 : q.rw, "", -1, 0);
        } else if (q.p5 >= 0) el.innerHTML = quotaBlock(ai, p.slabel || "5 小時", q.p5, q.r5, wl, q.pw, q.rw);
        else el.innerHTML = quotaBlock(ai, wl, q.pw, q.rw, "", -1, 0);
      } else if (p.login === "out") {
        el.innerHTML = `<span class="dot" style="background:var(--yel)"></span>尚未登入
          <div class="btns" style="margin-top:8px"><button class="primary" data-cmd="login:${ai}">登入 ${name}</button></div>`;
      } else if (p.err) {
        el.innerHTML = `<span class="dot" style="background:var(--red)"></span>${p.err}
          <details><summary>詳細資訊（截圖給開發者）</summary><code>${(p.diag || "").replace(/[<&]/g, "")}</code></details>
          <div class="btns" style="margin-top:8px"><button data-cmd="poll:all">重新讀取</button><button data-cmd="login:${ai}">開啟 ${name}</button></div>`;
      } else {
        el.innerHTML = `<span class="muted">${p.login === "in" ? "讀取中…" : "檢查登入狀態中…"}</span>
          <div class="btns" style="margin-top:8px"><button data-cmd="login:${ai}">登入 / 開啟 ${name}</button></div>`;
      }
    }

    // 螢幕韌體（每個螢幕一行）
    const fw = s.firmware || {};
    const less = (a, b) => { const x = (a || "0").split(".").map(Number), y = (b || "0").split(".").map(Number); for (let i = 0; i < 3; i++) { if ((x[i] || 0) !== (y[i] || 0)) return (x[i] || 0) < (y[i] || 0); } return false; };
    if (fw.running) {
      $("fw").innerHTML = `${fw.port ? fw.port + "：" : ""}${fw.msg || "更新中…"} ${fw.progress}%<div class="bar"><i style="width:${fw.progress}%;background:var(--blue)"></i></div><div class="hint">請不要拔掉 USB</div>`;
    } else if (fw.err) {
      $("fw").innerHTML = `<span class="bad">更新失敗：${fw.err}</span><div class="btns" style="margin-top:8px"><button data-cmd="fwupdate${fw.port ? ":" + fw.port : ""}">再試一次</button></div>`;
    } else if (fw.msg) {
      $("fw").innerHTML = `<span class="ok">${fw.msg}</span>`;
    } else if (!scr.length) {
      $("fw").innerHTML = `<span class="muted">內建 ${fw.bundled}</span>`;
    } else {
      $("fw").innerHTML = scr.map((x) => {
        const who = scr.length > 1 ? `<span class="muted">${H(x.port)}：</span>` : "", dev = H(x.fw || "");
        if (x.net) return `<div>${who}${dev}${less(dev, fw.bundled) ? ` <span class="muted">→ 可更新到 ${fw.bundled}</span>` : ""} <span class="muted">· 經 WiFi 連線，更新韌體要插 USB</span></div>`;
        return !dev || less(dev, fw.bundled)
          ? `<div>${who}${dev ? dev : "舊版"} <span class="muted">→ 可更新到 ${fw.bundled}</span>
              <div class="btns" style="margin:6px 0 4px"><button class="primary" data-cmd="fwupdate:${H(x.port)}">更新${scr.length > 1 ? " " + H(x.port) : "螢幕韌體"}</button></div></div>`
          : `<div>${who}${dev} <span class="muted">· 已是最新 · <a href="#" data-cmd="fwupdate:${H(x.port)}" style="color:inherit">重新寫入</a></span></div>`;
      }).join("");
    }
    renderScreenSettings(scr, BOARD);
    renderWifi(s, BOARD);

    const items = s.items || [];
    const esc = (t) => String(t).replace(/[<&]/g, "");
    // Claude Code 對話、Claude App 背景對話（app:claude:bg:）用對話名稱
    const name = (it) => it.key.startsWith("cc:") || it.key.includes(":bg:") ? esc((it.buttons || [])[0] || "Claude Code") : (NAMES[it.ai] || it.ai);
    const dbgOpen = !!($("dbg") && $("dbg").open);   // 每次重畫都會重建：記住除錯資訊是否展開
    const listOpen = !!($("itemlist") && $("itemlist").open);
    const busy = items.filter((it) => it.st !== "idle").length;
    $("items").innerHTML = items.length
      ? `<details class="list" id="itemlist"${listOpen ? " open" : ""}><summary>偵測到的對話 ${items.length} 個${busy ? `<span class="muted"> · ${busy} 個進行中或待處理</span>` : ""}</summary>` + items.map((it) => {
          const [src] = it.key.split(":");
          return `<div class="it"><span class="dot" style="background:${COL[it.st]}"></span>${name(it)}
            <span class="muted">· ${SRC[src] || src}${it.focused ? " · 前景" : ""}</span> — ${ST[it.st] || it.st}</div>`;
        }).join("") + `</details>`
      : `<div class="it muted">目前沒有偵測到 AI App 或 AI 網頁。<br>打開 Claude 桌面 App；網頁版要先裝瀏覽器擴充功能（設定分頁）。</div>`;
    // 網頁版沒裝擴充功能時偵測不到：講清楚，不然會以為壞掉
    const ex = s.extension || {};
    if (ex.enabled && !ex.connected) $("items").innerHTML += `<div class="it hint">用 Gemini、claude.ai、chatgpt.com 網頁的話，要先裝瀏覽器擴充功能才偵測得到（設定分頁 → 網頁版）。</div>`;
    // 除錯資訊集中在最下面一個：畫面偵測的狀態 + 每個項目讀到的按鈕（Claude Code 沒有按鈕，不列）
    const btns = items.filter((it) => !it.key.startsWith("cc:") && !it.key.includes(":bg:"))
      .map((it) => `${name(it)}：${(it.buttons || []).map(esc).join(" | ") || "（沒有）"}`);
    $("dbgbox").innerHTML = `<details id="dbg" style="padding:8px 0 10px"${dbgOpen ? " open" : ""}><summary>除錯資訊</summary>
      <code>${esc(s.monitor || "偵測程式尚未回報")}${btns.length ? "<br><br>偵測到的按鈕<br>" + btns.join("<br>") : ""}</code></details>`;

    renderStats(s);
    qStatus = s;
    if (document.querySelector('[data-pane="stats"]').classList.contains("on")) loadQHist(false);
    const cc = s.claudeCode || {};
    $("cc").classList.toggle("on", !!cc.installed);
    let ccs = "";
    if (cc.err) ccs = `<span class="bad">${cc.err}</span>`;
    else if (cc.msg && cc.msg.startsWith("失敗")) ccs = `<span class="bad">${cc.msg}</span>`;
    else if (cc.installed) ccs = cc.sessions ? `<span class="ok">● 偵測到 ${cc.sessions} 個 Claude Code 工作階段</span>` : (cc.msg || "已連接，等待 Claude Code 啟動（已開著的請重新啟動）");
    $("ccst").innerHTML = ccs;
    renderExt(s.extension || {});
    $("auto").classList.toggle("on", !!s.autostart);
    renderAppUpdate(s.appUpdate || {}, s.version);
    const sv = $("saver");
    if (document.activeElement !== sv) {
      const v = s.saverSec ? s.saverSec + "s" : String(s.saverMin ?? 5);
      if (![...sv.options].some((o) => o.value === v)) sv.add(new Option(v + " 分鐘", v));
      sv.value = v;
    }
    const rm = $("remind");
    if (document.activeElement !== rm) {
      const v = String(s.remindAt ?? 90);
      if (![...rm.options].some((o) => o.value === v)) rm.add(new Option("用量 ≥ " + v + "%", v));
      rm.value = v;
    }
  }

  // 狀態分頁最上面：螢幕正在顯示的那家 AI。圓環 = 主要額度（Claude 5 小時；ChatGPT / Gemini 照各家的短週期，沒有就用長週期）
  let heroSig = "";
  function renderHero(s) {
    const f = s.frame || {}, ai = f.ai, el = document.getElementById("hero");
    let p = -1, lab = "", r = 0;
    if (ai === "claude") { const q = s.quota || {}; if (q.p5 >= 0) { p = q.p5; lab = "5 小時"; r = q.r5; } }
    else if (ai) {
      const pv = (s.providers || {})[ai] || {}, q = pv.quota || {};
      if (q.p5 >= 0) { p = q.p5; lab = pv.cached ? "上次用量" : pv.slabel || "5 小時"; r = q.r5; }
      else if (q.pw >= 0) { p = q.pw; lab = pv.wlabel || "本週"; r = q.rw; }
    }
    const left = fmtLeft(r);
    const sig = JSON.stringify([ai, f.st, Math.round(p * 10), lab, left, qRed]);
    if (sig === heroSig) return;
    heroSig = sig;
    if (!ai || ai === "none") { el.innerHTML = `<div class="sub" style="padding:10px 0">沒有偵測到 AI App 或 AI 網頁</div>`; return; }
    const R = 40, C = 2 * Math.PI * R, col = qRed > 0 && p >= qRed ? "var(--red)" : BRAND[ai] || "var(--mut)";
    el.innerHTML = `<div class="ring"><svg viewBox="0 0 100 100"><circle cx="50" cy="50" r="${R}" fill="none" style="stroke:#2c2c2e" stroke-width="9"/>
        ${p > 0 ? `<circle cx="50" cy="50" r="${R}" fill="none" style="stroke:${col}" stroke-width="9" stroke-linecap="round" stroke-dasharray="${(C * Math.min(100, p) / 100).toFixed(1)} ${C.toFixed(1)}" transform="rotate(-90 50 50)"/>` : ""}</svg>
        <div class="num">${p >= 0 ? Math.round(p) + "%" : "--"}${lab ? `<small>${lab}</small>` : ""}</div></div>
      <div style="min-width:0"><div class="nm">${LOGOS[ai] ? logo(ai) : ""}${NAMES[ai] || H(ai)}</div>
        <span class="pill"><span class="dot" style="background:${COL[f.st] || "var(--mut)"}"></span>${ST[f.st] || H(f.st)}</span>
        <div class="sub">螢幕正在顯示${left ? ` · ${left}後重置` : ""}</div></div>`;
  }

  // 網頁版擴充功能：開關、連線狀態、沒裝時的安裝步驟
  function renderExt(x) {
    document.getElementById("ext").classList.toggle("on", !!x.enabled);
    const link = (cmd, t) => `<a href="#" data-cmd="${cmd}" style="color:var(--blue);text-decoration:none">${t}</a>`;
    let h = "";
    if (!x.enabled) h = "";
    else if (x.connected) h = `<span class="ok">● 擴充功能已連線</span>${x.tabs ? ` · ${x.tabs} 個 AI 分頁` : " · 目前沒有開著 AI 網頁"}`;
    else h = `<span style="color:var(--yel)">● 還沒裝</span><br>
      1. 打開 <b>chrome://extensions</b>，開「開發人員模式」<br>
      2. 「載入未封裝項目」→ 選 ${link("ext-folder", "這個資料夾")}`;
    document.getElementById("extst").innerHTML = h;
  }

  // 電腦端新版：上方提示列（有新版 / 下載中 / 失敗原因），設定分頁的「檢查更新」開關與上次檢查結果
  let updSig = "";
  function renderAppUpdate(u, cur) {
    const esc = (t) => String(t || "").replace(/[<&]/g, "");
    let h = "";
    if (u.running || u.msg) {
      h = `<div class="upd"><div class="v">更新到 ${esc(u.latest)}：${esc(u.msg || "下載中…")} ${u.progress || 0}%
        <div class="bar"><i style="width:${u.progress || 0}%;background:var(--blue)"></i></div></div></div>`;
    } else if (u.err) {
      h = `<div class="upd err"><div class="v"><span class="bad">更新失敗：${esc(u.err)}</span><div class="hint">目前的版本 ${esc(cur)} 照常執行</div></div>
        ${u.latest ? `<button class="primary" data-cmd="appupdate">再試一次</button>` : ""}</div>`;
    } else if (u.latest) {
      h = `<div class="upd"><div class="v">有新版 <b>${esc(u.latest)}</b><div class="hint">目前 ${esc(cur)} · 設定會保留</div></div>
        <button class="primary" data-cmd="appupdate">更新</button></div>`;
    }
    if (h !== updSig) { updSig = h; document.getElementById("upd").innerHTML = h; }
    document.getElementById("updchk").classList.toggle("on", !!u.enabled);
    let st = "";
    if (!u.enabled) st = "";
    else if (u.checking) st = "檢查中…";
    else if (u.checkErr) st = `<span class="bad">上次檢查失敗：${esc(u.checkErr)}</span> · <a href="#" data-cmd="appcheck" style="color:var(--blue);text-decoration:none">再檢查</a>`;
    else if (u.checked) {
      const t = new Date(u.checked * 1000);
      st = `${u.latest ? `有新版 ${esc(u.latest)}` : "已是最新版"} · <a href="#" data-cmd="appcheck" style="color:var(--blue);text-decoration:none">檢查</a>`;
    } else st = `<a href="#" data-cmd="appcheck" style="color:var(--blue);text-decoration:none">立即檢查</a>`;
    document.getElementById("updst").innerHTML = st;
  }

  // 螢幕的方向（AMOLED 多一個「自動」）、亮度、外接 LED，每塊各自記住。
  // 只接一塊時直接顯示它的設定；接好幾塊時上面多一個選單選要調哪一塊，下面只顯示那一塊。
  // 螢幕有增減才重建，平常只更新沒在操作的欄位
  let scrSig = "", scrSel = "";
  function renderScreenSettings(scr, BOARD) {
    const box = document.getElementById("scrset");
    if (!scr.some((x) => x.key === scrSel)) scrSel = scr.length ? scr[0].key : "";
    const sig = scr.map((x) => `${x.key}|${x.hw}|${x.port}|${x.net}`).join(",");
    if (sig !== scrSig) {
      scrSig = sig;
      const pick = scr.length > 1
        ? `<div class="row sw"><div>螢幕</div>
            <select id="scrpick">${scr.map((x) => `<option value="${H(x.key)}">${H(BOARD[x.hw || "c3"] || x.hw)}（${H(x.port)}）</option>`).join("")}</select></div>`
        : "";
      box.innerHTML = scr.length ? pick + scr.map((x) => {
        const hw = x.hw || "c3", k = H(x.key), amoled = hw === "s3a175";
        const rot = (amoled ? `<option value="auto">自動</option>` : "") +
          [0, 1, 2, 3].map((r) => `<option value="${r}">${r * 90}°</option>`).join("");
        const pin = amoled ? "接 GPIO16" : "接 RX 腳";
        return `<div data-blk="${k}">
          <div class="row sw"><div>螢幕亮度</div><input type="range" data-scr="${k}" data-k="bri" min="10" max="255" step="5"></div>
          <div class="row sw"><div>螢幕方向</div>
            <select data-scr="${k}" data-k="rot">${rot}</select></div>
          ${amoled ? `<div class="row sw"><div>麥克風<div class="hint" ${x.net ? 'style="color:var(--yel)">WiFi 連線時不能用，要插 USB' : ">Win+H 語音輸入"}</div>
              <div class="hint bad" data-micx="${k}" style="display:none">上次當機，這次先不開（重插 USB 再試）</div></div>
            <select data-scr="${k}" data-k="mic"><option value="0">關閉</option><option value="1">開啟</option></select></div>
          <div class="row sw"><div>麥克風增益</div>
            <select data-scr="${k}" data-k="mg"><option value="8">24 dB</option><option value="9">27 dB</option><option value="10">30 dB（預設）</option>
              <option value="11">33 dB</option><option value="13">36 dB</option><option value="14">37.5 dB</option></select></div>` : ""}
          <div class="row sw"><div>外接 LED<div class="hint">WS2812，${pin}</div></div>
            <select data-scr="${k}" data-k="led"><option value="0">關閉</option><option value="10">暗</option><option value="25">中</option>
              <option value="50">亮</option><option value="100">最亮</option></select></div></div>`;
      }).join("") : `<div class="row"><div class="muted">插上螢幕後可以設定</div></div>`;
      const pk = document.getElementById("scrpick");
      if (pk) pk.onchange = (e) => { scrSel = e.target.value; renderScreenSettings(scr, BOARD); };
    }
    const pk = document.getElementById("scrpick");
    if (pk && document.activeElement !== pk) pk.value = scrSel;
    for (const b of box.querySelectorAll("[data-blk]")) b.style.display = b.dataset.blk === scrSel ? "" : "none";
    for (const x of scr) {
      const set = x.set || {};
      const mx = box.querySelector(`[data-micx="${CSS.escape(x.key)}"]`);
      if (mx) mx.style.display = x.micx && set.mic ? "" : "none";
      for (const el of box.querySelectorAll(`[data-scr="${CSS.escape(x.key)}"]`)) {
        if (document.activeElement === el) continue;
        let v = el.dataset.k === "rot" ? (set.arot && x.hw === "s3a175" ? "auto" : String(set.rot & 3))
          : el.dataset.k === "mic" ? (set.mic ? "1" : "0")
          : el.dataset.k === "mg" ? String(set.mg ?? 10)
          : String(el.dataset.k === "bri" ? set.bri || 160 : set.led || 0);
        if (el.tagName === "SELECT" && ![...el.options].some((o) => o.value === v)) el.add(new Option(v + "%", v));
        el.value = v;
      }
    }
  }

  // ---------------- WiFi 分頁（只有 AMOLED）----------------
  // 每塊 AMOLED 一張卡片：WiFi 狀態、搜尋、掃描到的 WiFi（點了輸入密碼）、忘記 WiFi。
  // 設定過 WiFi、現在沒連線的螢幕也列出來（看得到連不上的原因）。SSID 是附近任何人都能取的名字，一律 H() 跳脫
  let wifiSig = "";
  function renderWifi(s, BOARD) {
    const conns = (s.screens || []).filter((x) => x.verified && x.hw === "s3a175");
    const confs = s.wifi || [];
    const keys = [...new Set([...conns.map((x) => x.key), ...confs.map((c) => c.key)])].sort();
    const now = Math.floor(Date.now() / 1000);
    const sigTxt = (r) => (r >= -60 ? "訊號強" : r >= -72 ? "訊號中" : "訊號弱");
    let h = "";
    for (const key of keys) {
      const x = conns.find((c) => c.key === key && !c.net) || conns.find((c) => c.key === key);
      const conf = confs.find((c) => c.key === key);
      const k = H(key), w = x && x.wifi;
      const where = x ? (x.net ? H(x.port) : `USB ${H(x.port)}`) : `<span class="muted">沒有連線</span>`;
      let st = "";
      if (w && w.st === 2) st = `<span class="ok">● 已連線</span> ${H(w.ssid)}<div class="hint">${H(w.ip)} · ${sigTxt(w.rssi)}（${w.rssi} dBm）</div>`;
      else if (w && w.st === 1) st = `<span style="color:var(--yel)">● 連線中…</span> ${H(w.ssid)}`;
      else if (w && w.st === 3) st = `<span class="bad">● 連不上 ${H(w.ssid)}</span><div class="hint">密碼可能不對</div>`;
      else if (w && w.st === 4) st = `<span class="bad">● 找不到 ${H(w.ssid)}</span><div class="hint">只支援 2.4 GHz</div>`;
      else if (conf) st = `${H(conf.ssid)}${conf.err ? `<div class="hint bad">${H(conf.err)}</div>` : `<div class="hint">等待螢幕連線…</div>`}`;
      else st = `<span class="muted">未設定</span>`;
      const micOn = x && x.set && x.set.mic;
      if (x && x.net && micOn) st += `<div class="hint" style="color:var(--yel)">麥克風要插 USB 才能用</div>`;
      const oldFw = x && !x.wf;
      let body = "";
      if (oldFw) {
        body = `<div class="row"><div class="hint">韌體太舊，先到「狀態」更新</div></div>`;
      } else if (x) {
        const sc = x.scan;
        body = `<div class="row"><div class="btns">
            <button class="primary" data-cmd="wscan:${k}">${sc && sc.aps && sc.aps.length ? "重新搜尋" : "搜尋 WiFi"}</button>
            <button data-wman="${k}">手動輸入</button>
            ${conf || (w && w.st) ? `<button data-wforget="${k}">忘記 WiFi</button>` : ""}</div></div>`;
        if (sc && sc.running && now - sc.t < 20) body += `<div class="row"><div class="muted">搜尋中…</div></div>`;
        else if (sc && sc.running) body += `<div class="row"><div class="bad">螢幕沒有回應，請再搜尋一次</div></div>`;
        else if (sc && sc.err) body += `<div class="row"><div class="bad">搜尋失敗，請再試一次</div></div>`;
        else if (sc && !sc.aps.length) body += `<div class="row"><div class="muted">沒有找到 WiFi（只支援 2.4 GHz）</div></div>`;
        else if (sc) body += sc.aps.map((a) => {
          const cur = w && w.ssid === a.ssid && w.st === 2;
          const sec = a.sec === 2 ? "企業版（不支援）" : a.sec ? "需要密碼" : "開放";
          return `<div class="row wap" data-wkey="${k}" data-ssid="${H(a.ssid)}" data-sec="${a.sec}">
            <div class="nm">${cur ? `<span class="ok">✓</span> ` : ""}${H(a.ssid)}</div><div class="sig">${sec} · ${sigTxt(a.rssi)}</div></div>`;
        }).join("");
      } else {
        body = `<div class="row"><div class="hint">插 USB 才能修改</div>${conf ? `<button data-wforget="${k}" style="flex:none">忘記</button>` : ""}</div>`;
      }
      h += `<div class="card">
        <div class="row"><div class="k">螢幕</div><div class="v">1.75 吋 AMOLED（${where}）</div></div>
        <div class="row"><div class="k">WiFi</div><div class="v">${st}</div></div>${body}</div>`;
    }
    if (!keys.length) {
      const lcd = (s.screens || []).some((x) => x.verified && (x.hw || "c3") === "c3");
      h = `<div class="card"><div class="row"><div class="muted">${lcd ? "1.28 吋 LCD 沒有 WiFi。" : ""}插上 1.75 吋 AMOLED 後可以設定</div></div></div>`;
    }
    if (h !== wifiSig) { wifiSig = h; document.getElementById("wifibox").innerHTML = h; }
  }

  // 輸入 WiFi 密碼（manual = 連 WiFi 名稱也自己打，隱藏的 WiFi 用）。跟 ask() 一樣是頁面內的對話框，不會被每秒重畫蓋掉
  function wifiAsk(key, ssid, sec, manual) {
    const d = document.createElement("div");
    d.className = "ask";
    d.innerHTML = `<div class="box"><div class="msg"><b class="t"></b>
        <input class="txt" data-f="ssid" placeholder="WiFi 名稱" maxlength="32" spellcheck="false" autocomplete="off">
        <input class="txt" data-f="pass" type="password" placeholder="密碼" maxlength="64" spellcheck="false" autocomplete="off">
        <label class="hint" style="display:flex;align-items:center;gap:6px;margin-top:8px"><input type="checkbox" data-f="show">顯示密碼</label>
        <div class="hint bad" data-f="err"></div></div>
      <div class="btns"><button data-a="0">取消</button><button class="primary" data-a="1">連線</button></div></div>`;
    const f = (n) => d.querySelector(`[data-f="${n}"]`);
    d.querySelector(".t").textContent = manual ? "手動輸入 WiFi" : `連線到「${ssid}」`;
    f("ssid").value = ssid || "";
    if (!manual) f("ssid").style.display = "none";
    if (!manual && !sec) { f("pass").style.display = "none"; f("show").parentNode.style.display = "none"; }
    f("show").onchange = () => { f("pass").type = f("show").checked ? "text" : "password"; };
    const close = () => { d.remove(); document.removeEventListener("keydown", key2, true); };
    const go = async () => {
      const name = manual ? f("ssid").value.trim() : ssid;
      const r = await window.aimonCmd("wifi:" + JSON.stringify({ key, ssid: name, pass: f("pass").value }));
      if (String(r).startsWith("err:")) { f("err").textContent = String(r).slice(4); return; }
      close(); render();
    };
    const key2 = (e) => { if (e.key === "Escape") close(); else if (e.key === "Enter") { e.preventDefault(); go(); } };
    d.addEventListener("click", (e) => {
      e.stopPropagation();
      const a = e.target.closest("[data-a]");
      if (a) { if (a.dataset.a === "1") go(); else close(); }
      else if (e.target === d) close();
    });
    document.addEventListener("keydown", key2, true);
    document.body.appendChild(d);
    (manual ? f("ssid") : sec ? f("pass") : d.querySelector('[data-a="1"]')).focus();
  }

  document.addEventListener("click", async (e) => {
    const ap = e.target.closest(".wap");
    if (ap) {
      if (ap.dataset.sec === "2") { await ask("這是企業版 WiFi（要帳號密碼登入），螢幕不支援。請選別的 WiFi。", "知道了"); return; }
      wifiAsk(ap.dataset.wkey, ap.dataset.ssid, ap.dataset.sec !== "0", false);
      return;
    }
    const man = e.target.closest("[data-wman]");
    if (man) { wifiAsk(man.dataset.wman, "", true, true); return; }
    const fg = e.target.closest("[data-wforget]");
    if (fg && (await ask("要讓螢幕忘記 WiFi 嗎？之後要插著 USB 才會顯示。", "忘記"))) window.aimonCmd("wforget:" + fg.dataset.wforget).then(render);
  });

  // ---------------- 額度用量曲線（統計分頁）----------------
  // 一張線圖、一個 0-100% 的軸：短週期（5 小時；Gemini / ChatGPT 依各家標籤）實線、品牌色；長週期（本週）虛線、灰色。
  // 沒有 AIMonitor 在跑的空檔（兩點相隔超過 25 分鐘）斷開不連；額度重置提醒的門檻畫一條淡淡的參考線。滑鼠移上去看那個時間的數字
  let qAI = "claude", qDays = 1, qData = null, qT = 0, qStatus = null, qHover = null;
  function loadQHist(force) {
    if (!force && Date.now() - qT < 60000) return;
    qT = Date.now();
    const ai = qAI, days = qDays;
    window.aimonQuotaHist(ai, days).then((r) => {
      if (ai !== qAI || days !== qDays) return;
      try { qData = JSON.parse(r); } catch (_) { qData = null; }
      drawQ();
    });
  }
  function qLabels(ai) {
    const p = ((qStatus || {}).providers || {})[ai] || {};
    return ai === "claude" ? ["5 小時", "本週"] : [p.slabel || "5 小時", p.wlabel || "本週"];
  }
  function drawQ() {
    const seg = document.getElementById("qai");
    if (!seg.childElementCount) seg.innerHTML = ["claude", "chatgpt", "gemini"].map((ai) => `<button data-qai="${ai}" title="${NAMES[ai]}" aria-label="${NAMES[ai]}">${logo(ai)}</button>`).join("");
    for (const b of document.querySelectorAll("[data-qai]")) b.classList.toggle("on", b.dataset.qai === qAI);
    for (const b of document.querySelectorAll("[data-qd]")) b.classList.toggle("on", +b.dataset.qd === qDays);
    const [l5, lw] = qLabels(qAI), col = BRAND[qAI] || "var(--mut)", grey = col;   // 每週：同色調淡的虛線
    document.getElementById("qleg").innerHTML = `<span><i style="border-color:${col}"></i>${l5}</span><span style="opacity:.6"><i style="border-color:${grey};border-top-style:dashed"></i>${lw}</span>`;
    const plot = document.getElementById("qplot");
    const sm = (qData && qData.samples) || [];
    if (!sm.length) { plot.innerHTML = `<div class="hint" style="padding:18px 0">還沒有資料（讀到額度後會開始記錄）</div>`; return; }
    const W = Math.max(260, plot.clientWidth || 420), H = 150, L = 38, R = 40, T = 8, B = 20;
    const t1 = qData.now, t0 = t1 - qDays * 86400;
    const x = (t) => L + ((t - t0) / (t1 - t0)) * (W - L - R), y = (v) => T + (1 - Math.min(100, v) / 100) * (H - T - B);
    const GAP = 25 * 60;
    const path = (k) => {
      let d = "", prev = null;
      for (const p of sm) {
        if (p[k] < 0) { prev = null; continue; }
        d += `${prev && p.t - prev.t <= GAP ? "L" : "M"}${x(p.t).toFixed(1)},${y(p[k]).toFixed(1)}`;
        prev = p;
      }
      return d;
    };
    let g = "";
    for (const v of [0, 50, 100]) g += `<line x1="${L}" x2="${W - R}" y1="${y(v)}" y2="${y(v)}" style="stroke:var(--grid)" stroke-width="1"/><text x="${L - 6}" y="${y(v) + 4}" text-anchor="end">${v}${v === 100 ? "%" : ""}</text>`;
    const red = (qStatus || {}).remindAt;
    if (red > 0 && red < 100) g += `<line x1="${L}" x2="${W - R}" y1="${y(red)}" y2="${y(red)}" style="stroke:var(--red)" stroke-opacity=".45" stroke-dasharray="3 3"/>`;
    // 時間刻度：24 小時每 6 小時一格，7 天每天午夜一格（本地時間）
    const step = qDays === 1 ? 6 * 3600 : 86400, tz = new Date().getTimezoneOffset() * 60;
    for (let t = Math.ceil((t0 - tz) / step) * step + tz; t <= t1; t += step) {
      if (t - t0 < 1800) continue;
      const d = new Date(t * 1000), lab = qDays === 1 ? `${String(d.getHours()).padStart(2, "0")}:00` : `${d.getMonth() + 1}/${d.getDate()}`;
      g += `<line x1="${x(t)}" x2="${x(t)}" y1="${T}" y2="${H - B}" style="stroke:var(--grid2)"/><text x="${x(t)}" y="${H - 5}" text-anchor="middle">${lab}</text>`;
    }
    g += `<path d="${path("w")}" fill="none" style="stroke:${grey}" stroke-opacity=".55" stroke-width="2" stroke-dasharray="5 4" stroke-linejoin="round" stroke-linecap="round"/>`;
    g += `<path d="${path("p")}" fill="none" style="stroke:${col}" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/>`;
    // 線尾直接標數字（兩個太近就錯開）
    const last = (k) => { for (let i = sm.length - 1; i >= 0; i--) if (sm[i][k] >= 0) return sm[i]; return null; };
    const a = last("p"), b = last("w");
    let ya = a ? y(a.p) + 4 : 0, yb = b ? y(b.w) + 4 : 0;
    if (a && b && Math.abs(ya - yb) < 12) { if (ya <= yb) { ya -= 6; yb += 6; } else { ya += 6; yb -= 6; } }
    if (a) g += `<text x="${x(a.t) + 6}" y="${ya}" style="fill:var(--fg)">${Math.round(a.p)}%</text>`;
    if (b) g += `<text x="${x(b.t) + 6}" y="${yb}">${Math.round(b.w)}%</text>`;
    g += `<g id="qx" style="display:none"><line y1="${T}" y2="${H - B}" style="stroke:var(--mut2)"/><circle r="4" style="fill:${col};stroke:var(--card)" stroke-width="2"/><circle r="4" style="fill:${grey};stroke:var(--card)" fill-opacity=".6" stroke-width="2"/></g>`;
    g += `<rect x="${L}" y="0" width="${W - L - R}" height="${H}" fill="transparent" id="qhit"/>`;
    plot.innerHTML = `<svg viewBox="0 0 ${W} ${H}" height="${H}">${g}</svg><div class="qtip" style="display:none"></div>`;
    const hit = plot.querySelector("#qhit"), cross = plot.querySelector("#qx"), tip = plot.querySelector(".qtip");
    const show = (ev) => {
      const r = plot.querySelector("svg").getBoundingClientRect(), mx = ((ev.clientX - r.left) / r.width) * W;
      let best = sm[0];
      for (const p of sm) if (Math.abs(x(p.t) - mx) < Math.abs(x(best.t) - mx)) best = p;
      const px = x(best.t), [c1, c2] = cross.querySelectorAll("circle");
      cross.style.display = "";
      cross.querySelector("line").setAttribute("x1", px); cross.querySelector("line").setAttribute("x2", px);
      c1.style.display = best.p >= 0 ? "" : "none"; c1.setAttribute("cx", px); c1.setAttribute("cy", y(Math.max(0, best.p)));
      c2.style.display = best.w >= 0 ? "" : "none"; c2.setAttribute("cx", px); c2.setAttribute("cy", y(Math.max(0, best.w)));
      const d = new Date(best.t * 1000), hm = `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
      tip.innerHTML = `<div class="muted">${qDays === 1 ? hm : `${d.getMonth() + 1}/${d.getDate()} ${hm}`}</div>` +
        (best.p >= 0 ? `<div>${l5} <b>${Math.round(best.p)}%</b></div>` : "") + (best.w >= 0 ? `<div>${lw} <b>${Math.round(best.w)}%</b></div>` : "");
      tip.style.display = "";
      const left = (px / W) * r.width;
      tip.style.left = Math.min(r.width - tip.offsetWidth, Math.max(0, left + (left > r.width / 2 ? -tip.offsetWidth - 10 : 10))) + "px";
    };
    hit.addEventListener("mousemove", show);
    hit.addEventListener("mouseleave", () => { cross.style.display = "none"; tip.style.display = "none"; });
  }
  document.addEventListener("click", (e) => {
    const a = e.target.closest("[data-qai]"), d = e.target.closest("[data-qd]");
    if (a) { qAI = a.dataset.qai; qData = null; drawQ(); loadQHist(true); }
    if (d) { qDays = +d.dataset.qd; qData = null; drawQ(); loadQHist(true); }
  });
  window.addEventListener("resize", () => { if (qData) drawQ(); });

  const BRAND = { claude: "#f0855a", chatgpt: "var(--gpt)", gemini: "#64a8ff" };   // ChatGPT：深色主題淺灰、淺色主題黑色
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
    let h = `<div class="row" style="border:0;padding-bottom:2px"><div class="k">今日統計</div></div>`;
    if (!today.length) {
      h += `<div class="hint" style="padding:4px 0 12px">今天還沒有偵測到生成。</div>`;
    } else {
      const max = Math.max(1, ...today.map((t) => t[2]));
      let tc = 0, ts = 0;
      for (const [ai, c, sec] of today) {
        tc += c; ts += sec;
        h += `<div class="srow"><div class="nm" title="${NAMES[ai] || H(ai)}" aria-label="${NAMES[ai] || H(ai)}">${LOGOS[ai] ? logo(ai) : H(NAMES[ai] || ai)}</div><div class="bar"><i style="width:${Math.max(3, (sec / max) * 100)}%;background:${BRAND[ai] || "var(--mut)"}"></i></div><div class="val">${c} 次 · ${fmtDur(sec)}</div></div>`;
      }
      h += `<div class="hint" style="padding:2px 0 8px">合計 ${tc} 次 · ${fmtDur(ts)}</div>`;
    }
    const week = s.week || [];
    if (week.some((d) => d.secs)) {
      const tot = (d) => Object.values(d.secs || {}).reduce((a, b) => a + b, 0);
      const wmax = Math.max(1, ...week.map(tot));
      const WD = "日一二三四五六";
      const leg = ["claude", "chatgpt", "gemini"].map((ai) => `<span><i style="background:${BRAND[ai]}"></i>${NAMES[ai]}</span>`).join("");
      const short = (sec) => (sec >= 3600 ? `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m` : sec >= 60 ? `${Math.round(sec / 60)}m` : sec ? `${sec}s` : "");
      h += `<div class="whead"><span class="hint" style="margin:0">最近 7 天（生成時間）</span><span class="leg">${leg}</span></div><div class="week" id="week"><div class="base"></div><div class="wtip" id="wtip"></div>` + week.map((d, i) => {
        const segs = ["gemini", "chatgpt", "claude"].map((ai) => {
          const v = (d.secs || {})[ai] || 0;
          return v ? `<div style="height:${Math.max(2, (v / wmax) * 96)}px;background:${BRAND[ai]}"></div>` : "";
        }).join("");
        return `<div class="col${i === week.length - 1 ? " today" : ""}" data-i="${i}"><div class="val">${short(tot(d))}</div><div class="stack">${segs}</div><div class="lbl">${i === week.length - 1 ? "今天" : WD[d.wd]}</div></div>`;
      }).join("") + `</div><div style="height:10px"></div>`;
    }
    document.getElementById("stats").innerHTML = h;
    // 滑鼠指著哪一天：跳出那天三家各多久、幾次（放在長條旁邊；右半邊的天放在左側，免得超出卡片）
    const wk = document.getElementById("week"), tip = document.getElementById("wtip");
    if (wk && tip) wk.querySelectorAll(".col").forEach((c) => {
      c.onmouseenter = () => {
        const d = week[+c.dataset.i] || {}, secs = d.secs || {}, cnt = d.count || {};
        const all = Object.values(secs).reduce((a, b) => a + b, 0), allC = Object.values(cnt).reduce((a, b) => a + b, 0);
        if (!all) { tip.style.display = "none"; return; }
        const [mm, dd] = (d.day || "").split("/").map(Number);
        let t = `<div class="d">${mm}/${dd}（${"日一二三四五六"[d.wd]}）· ${fmtDur(all)} · ${allC} 次</div>`;
        for (const ai of ["claude", "chatgpt", "gemini"]) if (secs[ai]) t += `<div class="r"><span><i style="background:${BRAND[ai]}"></i>${NAMES[ai]}</span><span>${fmtDur(secs[ai])} · ${cnt[ai] || 0} 次</span></div>`;
        tip.innerHTML = t;
        tip.style.display = "block";
        const cr = c.getBoundingClientRect(), wr = wk.getBoundingClientRect();
        const right = cr.left + cr.width / 2 - wr.left > wr.width / 2;
        tip.style.left = right ? "" : (cr.right - wr.left + 4) + "px";
        tip.style.right = right ? (wr.right - cr.left + 4) + "px" : "";
      };
      c.onmouseleave = () => { tip.style.display = "none"; };
    });
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
