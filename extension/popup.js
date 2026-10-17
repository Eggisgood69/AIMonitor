const $ = (id) => document.getElementById(id);
const NAMES = { claude: "Claude", chatgpt: "ChatGPT", gemini: "Gemini", none: "沒有 AI 分頁" };
const ST = { idle: "待命", done: "完成", working: "生成中", review: "需審核", error: "錯誤" };

async function refresh() {
  const r = await chrome.runtime.sendMessage({ type: "getStatus" }).catch(() => null);
  if (!r) return;
  const s = r.serial || {};
  $("dot").className = "dot " + (s.connected ? "on" : "off");
  $("conn").textContent = s.connected ? (s.verified ? "螢幕已連線" : "已連線，等待螢幕回應")
    : s.paired ? "已配對，螢幕未插上" : "尚未配對螢幕";
  $("adot").className = "dot " + (r.appHelper ? "on" : "");
  $("app").textContent = r.appHelper ? "桌面 App 監測：執行中" : "桌面 App 監測：未執行（Windows 才需要）";
  const f = r.frame;
  $("ai").textContent = NAMES[f.ai] || f.ai;
  let line = ST[f.st] || f.st;
  if (f.p5 !== undefined) line += ` · 5 小時額度 ${Math.round(f.p5)}%`;
  if (f.o && f.o.length) line += ` · 另有 ${f.o.length} 個分頁有狀態`;
  $("st").textContent = line;
}

$("pair").onclick = () => chrome.tabs.create({ url: "pair.html" });
$("demo").onclick = () => chrome.runtime.sendMessage({ type: "demo" });
refresh();
setInterval(refresh, 1500);
