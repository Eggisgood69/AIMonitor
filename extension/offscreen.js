// offscreen 頁面：持有 USB 序列埠，插拔自動重連，把畫面狀態一行一行 JSON 送給螢幕。
let port = null, writer = null, reader = null;
let verified = false, connecting = false;
let lastFrame = null, lastError = "";
const enc = new TextEncoder();

async function write(obj) {
  if (!writer) return false;
  try {
    await writer.write(enc.encode(JSON.stringify(obj) + "\n"));
    return true;
  } catch (e) {
    lastError = String(e);
    await closePort();
    return false;
  }
}

async function closePort() {
  try { if (reader) await reader.cancel(); } catch (e) {}
  try { if (writer) writer.releaseLock(); } catch (e) {}
  try { if (port) await port.close(); } catch (e) {}
  port = writer = reader = null;
  verified = false;
}

async function readLoop(p) {
  const decoder = new TextDecoder();
  let buf = "";
  try {
    reader = p.readable.getReader();
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      let i;
      while ((i = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, i).trim();
        buf = buf.slice(i + 1);
        if (!line.startsWith("{")) continue;
        try {
          const m = JSON.parse(line);
          if (m.aimon) verified = true;
          if (m.ev === "btn") chrome.runtime.sendMessage({ type: "button" }).catch(() => {});
        } catch (e) {}
      }
      if (buf.length > 4096) buf = "";
    }
  } catch (e) {
    lastError = String(e);
  } finally {
    try { reader && reader.releaseLock(); } catch (e) {}
    reader = null;
  }
}

async function connect() {
  if (port || connecting) return;
  connecting = true;
  try {
    const ports = await navigator.serial.getPorts();
    for (const p of ports) {
      try {
        await p.open({ baudRate: 115200 });
        // DTR=1 / RTS=0：不會讓 ESP32 重開機或進入燒錄模式
        try { await p.setSignals({ dataTerminalReady: true, requestToSend: false }); } catch (e) {}
        port = p;
        writer = p.writable.getWriter();
        readLoop(p);
        await write({ hello: 1 });
        if (lastFrame) await write(lastFrame);
        lastError = "";
        break;
      } catch (e) {
        lastError = String(e);
        try { await p.close(); } catch (_) {}
      }
    }
  } finally {
    connecting = false;
  }
}

navigator.serial.addEventListener("connect", () => setTimeout(connect, 500));
navigator.serial.addEventListener("disconnect", (e) => { if (e.target === port) closePort(); });

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (msg.target !== "offscreen") return;
  (async () => {
    if (msg.type === "frame") {
      lastFrame = msg.frame;
      if (!port) await connect();
      await write(msg.frame);
      sendResponse({ ok: true });
    } else if (msg.type === "reconnect") {
      await closePort();
      await connect();
      sendResponse({ ok: !!port });
    } else if (msg.type === "raw") {           // 測試畫面用
      if (!port) await connect();
      sendResponse({ ok: await write(msg.frame) });
    } else if (msg.type === "status") {
      const ports = await navigator.serial.getPorts();
      let info = null;
      if (port) { try { info = port.getInfo(); } catch (e) {} }
      sendResponse({ connected: !!port, verified, paired: ports.length, info, error: lastError });
    }
  })();
  return true;
});

// 心跳：每 5 秒請背景程式重算一次（綠燈過期、經過時間），螢幕 20 秒沒收到資料會顯示 NO LINK
setInterval(() => {
  if (!port) connect();
  chrome.runtime.sendMessage({ type: "tick" }).catch(() => {});
}, 5000);
connect();

// ---------------- Windows 桌面 App 監測（選用）----------------
// windows-app/aimon-app.ps1 在 127.0.0.1:47321 提供 Claude / ChatGPT 桌面 App 的狀態
let appLast = "", appOk = false, appLastSent = 0;
async function pollApp() {
  try {
    const ctl = new AbortController();
    const to = setTimeout(() => ctl.abort(), 800);
    const r = await fetch("http://127.0.0.1:47321/state", { cache: "no-store", signal: ctl.signal });
    clearTimeout(to);
    const d = await r.json();
    const s = JSON.stringify(d.apps || []);
    if (s !== appLast || !appOk || Date.now() - appLastSent > 4000) {
      appLast = s; appOk = true; appLastSent = Date.now();
      chrome.runtime.sendMessage({ type: "appState", ok: true, apps: d.apps || [] }).catch(() => {});
    }
  } catch (e) {
    if (appOk) { appOk = false; appLast = ""; chrome.runtime.sendMessage({ type: "appState", ok: false, apps: [] }).catch(() => {}); }
  }
}
setInterval(pollApp, 1000);
