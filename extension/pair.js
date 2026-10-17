const $ = (id) => document.getElementById(id);
const FILTERS = [
  { usbVendorId: 0x303a },  // Espressif 內建 USB（ESP32-C3 USB JTAG/serial）
  { usbVendorId: 0x1a86 },  // CH340 / CH343
  { usbVendorId: 0x10c4 },  // CP210x
];

function show(text, cls) { $("status").textContent = text; $("status").className = cls || ""; }

async function pick(all) {
  if (!("serial" in navigator)) {
    show("這個瀏覽器不支援 Web Serial，請用電腦版 Chrome 或 Edge。", "bad");
    return;
  }
  try {
    await navigator.serial.requestPort(all ? {} : { filters: FILTERS });
  } catch (e) {
    show("沒有選擇裝置。", "bad");
    return;
  }
  show("連線中…");
  await chrome.runtime.sendMessage({ type: "kick" });
  const r = await chrome.runtime.sendMessage({ target: "offscreen", type: "reconnect" }).catch(() => null);
  setTimeout(check, 1200);
  if (r && r.ok) show("已連線！", "ok");
}

async function check() {
  const st = await chrome.runtime.sendMessage({ type: "getStatus" }).catch(() => null);
  const s = st && st.serial;
  if (s && s.connected) {
    show(s.verified ? "已連線，螢幕有回應 ✓" : "已連線（等待螢幕回應…若一直沒回應，確認韌體已燒錄）", "ok");
    $("after").hidden = false;
  } else if (s && s.paired) {
    show("已配對，但目前開不了序列埠：可能被 Arduino IDE 等程式佔用。" + (s.error ? "\n" + s.error : ""), "bad");
  }
}

$("pick").onclick = () => pick(false);
$("pickAll").onclick = () => pick(true);
$("demo").onclick = () => chrome.runtime.sendMessage({ type: "demo" });
check();
