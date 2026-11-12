#pragma once
// AMOLED 的 WiFi：設定好之後不用一直插著電腦，螢幕接 USB 充電器也收得到畫面。
//   設定：電腦經 USB 送 {"wscan":1}（掃描，回 {"wscan":[[ssid, rssi, 0 開放 / 1 要密碼 / 2 企業版不支援], ...]}）、
//         {"wset":{"s":ssid,"p":密碼,"k":配對金鑰}}（存進 NVS 並連線；s 空白 = 忘記 WiFi、關掉）
//   連線：連上 WiFi 後開 TCP 47323。電腦連進來第一行要是 {"auth":"<配對金鑰>"}，對了才收其他的（回 {"auth":1}），
//         之後跟 USB 一樣每行一個 JSON（hello、畫面）。同時只接一台電腦
//   找螢幕：電腦用 USB 時記下螢幕回報的 IP；IP 換了用 mDNS 名稱 aimon-<id 後 6 碼>.local
//   狀態：{"wst":{"st":0 沒設定 / 1 連線中 / 2 已連線 / 3 密碼可能不對 / 4 找不到這個 WiFi,"ssid","ip","rssi","r":斷線原因}}，
//         回 hello 之後送一次、狀態變了也送（USB 最近有收到資料才送：接充電器時寫 Serial 會卡住）
//   更新韌體（1.12.0 起，hello 的 "ota":1）：驗證過的 WiFi 連線送 {"ota":{"n":大小,"sha":"SHA256 hex"}}，
//         螢幕回 {"ota":"rdy"} 後電腦直接送 n 個位元組的韌體（不是 JSON）。寫進另一個 app 區（app0 / app1 輪流），
//         SHA256 對、映像檔驗證過才切換開機分割區，回 {"ota":"ok"} 後重開；失敗回 {"ota":"err","why":"..."}、維持舊版。
//         USB 一鍵更新只寫 app0，所以 USB 燒錄時要清掉 otadata（flasher.go），否則從 app1 開機、還是舊版。
//         新版開機後跟電腦打完招呼才確認有效（aimon.ino otaConfirm），確認前當機或 2 分鐘沒電腦 → 退回舊版
// 1.28 吋（C3）不做：單核心、記憶體少，WiFi 會拖到外環動畫

#define WIFI_TCP_PORT 47323

static String g_wSsid, g_wPass, g_wKey;   // NVS（Preferences "aimon"：wss / wpw / wky）
static char g_wHost[16];                  // aimon-<id 後 6 碼>
static WiFiServer g_wServer(WIFI_TCP_PORT);
static WiFiClient g_wClient;
static LineReader g_wReader(&g_wClient);
static bool g_wAuth = false;              // 這個連線送對了配對金鑰
static uint32_t g_wRxMs = 0, g_wAcceptMs = 0;
static bool g_wServing = false, g_wMdns = false;
static volatile uint8_t g_wReason = 0;    // 上次斷線的原因（wifi_err_reason_t），連上時清掉
static volatile bool g_wDropped = false;  // 剛斷線 / 這次沒連上：快一點重試
static uint32_t g_wBeginMs = 0;
static bool g_wScanning = false;
static Stream* g_wScanTo = nullptr;       // 掃描結果送給誰（USB 或 WiFi 連線）
static uint32_t g_usbRxMs = 0;            // 上次從 USB 收到資料（aimon.ino loop 更新）
static char g_wHintBuf[32];

static void wifiOta(Stream* src, JsonObject o);   // 經 WiFi 更新韌體（下面）

static bool wifiUp() { return WiFi.status() == WL_CONNECTED; }

static int wifiState() {
  if (!g_wSsid.length()) return 0;
  if (wifiUp()) return 2;
  const uint8_t r = g_wReason;
  if (r == WIFI_REASON_AUTH_FAIL || r == WIFI_REASON_4WAY_HANDSHAKE_TIMEOUT || r == WIFI_REASON_HANDSHAKE_TIMEOUT ||
      r == WIFI_REASON_MIC_FAILURE || r == WIFI_REASON_AUTH_EXPIRE) return 3;
  if (r == WIFI_REASON_NO_AP_FOUND) return 4;
  return 1;
}

static void wifiSendStatus(Stream* s) {
  if (!s) return;
  JsonDocument d;
  JsonObject o = d["wst"].to<JsonObject>();
  o["st"] = wifiState();
  o["ssid"] = g_wSsid;
  if (wifiUp()) { o["ip"] = WiFi.localIP().toString(); o["rssi"] = WiFi.RSSI(); }
  o["r"] = (int)g_wReason;
  String out;
  serializeJson(d, out);
  sendLine(s, out.c_str());
}

// 掃描結果：同名的只留訊號最強的，最多 20 個，訊號強的在前面
static void wifiSendScan(Stream* s, int n) {
  if (!s) return;
  JsonDocument d;
  JsonArray a = d["wscan"].to<JsonArray>();
  if (n < 0) d["err"] = 1;
  int idx[64], m = 0;
  for (int i = 0; i < n && m < 64; i++) if (WiFi.SSID(i).length()) idx[m++] = i;
  for (int i = 1; i < m; i++)                                   // 依訊號排序（插入排序，數量很少）
    for (int j = i; j > 0 && WiFi.RSSI(idx[j]) > WiFi.RSSI(idx[j - 1]); j--) { const int t = idx[j]; idx[j] = idx[j - 1]; idx[j - 1] = t; }
  int cnt = 0;
  for (int i = 0; i < m && cnt < 20; i++) {
    const String ss = WiFi.SSID(idx[i]);
    bool dup = false;
    for (int j = 0; j < i && !dup; j++) dup = WiFi.SSID(idx[j]) == ss;
    if (dup) continue;
    const wifi_auth_mode_t au = WiFi.encryptionType(idx[i]);
    JsonArray e = a.add<JsonArray>();
    e.add(ss);
    e.add(WiFi.RSSI(idx[i]));
    e.add(au == WIFI_AUTH_OPEN ? 0 : (au == WIFI_AUTH_WPA2_ENTERPRISE ? 2 : 1));
    cnt++;
  }
  String out;
  serializeJson(d, out);
  sendLine(s, out.c_str());
}

// 打開 STA（還沒開的話）。主機名稱要在開之前設
static void wifiOn() {
  if (WiFi.getMode() & WIFI_MODE_STA) return;
  WiFi.persistent(false);                 // 設定我們自己存（NVS），不要讓 WiFi 函式庫再存一份
  WiFi.setAutoReconnect(false);           // 重連由 wifiTick 管（掃描時不能同時在連線）
  WiFi.setHostname(g_wHost);
  WiFi.mode(WIFI_STA);
  WiFi.setSleep(false);                   // 省電模式下收資料會延遲 100-300 ms（燈號變了晚一點才亮）
}

static void wifiBegin() {
  wifiOn();
  WiFi.begin(g_wSsid.c_str(), g_wPass.length() ? g_wPass.c_str() : nullptr);
  g_wBeginMs = millis();
  g_wDropped = false;
}

static void wifiSave() {
  Preferences pf;
  pf.begin("aimon", false);
  pf.putString("wss", g_wSsid);
  pf.putString("wpw", g_wPass);
  pf.putString("wky", g_wKey);
  pf.end();
}

static void wifiInit() {
  const uint64_t mac = ESP.getEfuseMac();
  char id[16];
  snprintf(id, sizeof id, "%04x%08lx", (unsigned)((mac >> 32) & 0xFFFF), (unsigned long)(mac & 0xFFFFFFFFUL));
  snprintf(g_wHost, sizeof g_wHost, "aimon-%s", id + 6);   // 電腦端用 hello 的 id 算出同一個名稱
  {
    Preferences pf;
    pf.begin("aimon", true);
    g_wSsid = pf.getString("wss", "");
    g_wPass = pf.getString("wpw", "");
    g_wKey = pf.getString("wky", "");
    pf.end();
  }
  WiFi.onEvent([](arduino_event_id_t e, arduino_event_info_t info) {
    if (e == ARDUINO_EVENT_WIFI_STA_DISCONNECTED) { g_wReason = info.wifi_sta_disconnected.reason; g_wDropped = true; }
    else if (e == ARDUINO_EVENT_WIFI_STA_GOT_IP) g_wReason = 0;
  });
  if (g_wSsid.length()) wifiBegin();
}

static void wifiCloseClient() {
  g_wClient.stop();
  g_wAuth = false;
  g_wReader.buf = "";
}

// handleLine 最前面：WiFi 連線還沒送對配對金鑰時，只收 {"auth":...}，其他（或金鑰不對）直接斷線。回傳 false = 這行到此為止
static bool wifiGate(Stream* src, JsonDocument& doc) {
  if (src != &g_wClient || g_wAuth) return true;
  const char* k = doc["auth"] | "";
  if (g_wKey.length() && g_wKey == k) {
    g_wAuth = true;
    g_wRxMs = millis();
    sendLine(src, "{\"auth\":1}");
  } else {
    wifiCloseClient();
  }
  return false;
}

// WiFi 設定指令（USB 或已驗證的 WiFi 連線）。回傳 true = 處理掉了
static bool wifiCommand(Stream* src, JsonDocument& doc) {
  if (doc["wscan"].is<int>()) {
    g_wScanTo = src;
    if (g_wScanning) return true;
    wifiOn();
    if (g_wSsid.length() && !wifiUp()) WiFi.disconnect();   // 正在嘗試連線時不能掃描
    if (WiFi.scanNetworks(true, false, false, 250) == WIFI_SCAN_FAILED) {
      wifiSendScan(src, -1);
      if (!g_wSsid.length()) WiFi.mode(WIFI_OFF);
    } else {
      g_wScanning = true;
    }
    return true;
  }
  if (doc["wset"].is<JsonObject>()) {
    JsonObject w = doc["wset"];
    const char* s = w["s"] | "";
    const char* p = w["p"] | "";
    const char* k = w["k"] | "";
    if (strlen(s) > 32 || strlen(p) > 64 || strlen(k) > 64) return true;
    if (!*s) {                                               // 忘記 WiFi
      g_wSsid = g_wPass = g_wKey = "";
      wifiSave();
      if (g_wMdns) { MDNS.end(); g_wMdns = false; }
      g_wReason = 0;
      wifiSendStatus(src);
      if (src != &g_wClient) wifiCloseClient();             // 從 WiFi 連線送來的：回覆送出後連線自然會斷
      WiFi.disconnect(true);
      WiFi.mode(WIFI_OFF);
      return true;
    }
    g_wSsid = s;
    g_wPass = p;
    if (*k) g_wKey = k;
    wifiSave();
    g_wReason = 0;
    wifiBegin();
    wifiSendStatus(src);
    return true;
  }
  if (doc["ota"].is<JsonObject>()) { wifiOta(src, doc["ota"]); return true; }
  return false;
}

// ---------------- 經 WiFi 更新韌體 ----------------
// 收的時候整個 loop 停在這裡（畫面只顯示進度，外環不動）：約 1.3 MB，區網內 10-20 秒
static void otaFail(Stream* src, const char* why) {
  Update.abort();
  char b[96];
  snprintf(b, sizeof b, "{\"ota\":\"err\",\"why\":\"%s\"}", why);
  sendLine(src, b);
  g_otaPct = -1;
  dirty = true;
  wifiCloseClient();                                        // 剩下沒讀的是韌體資料，不能再當成 JSON 讀
}

static void otaShow(int pct) {
  g_otaPct = pct;
  g_saver = false;
  g_fadeDir = 0;
  g_fadeLv = 1000;
  showBright();
  render();
}

static void wifiOta(Stream* src, JsonObject o) {
  const uint32_t n = o["n"] | 0UL;
  const char* sha = o["sha"] | "";
  if (src != &g_wClient) { sendLine(src, "{\"ota\":\"err\",\"why\":\"wifi only\"}"); return; }   // USB 用 ROM 燒錄（flasher.go）
  if (strlen(sha) != 64) { otaFail(src, "bad sha"); return; }
  if (g_micOn) { otaFail(src, "mic on"); return; }
  if (!n || !Update.begin(n, U_FLASH)) { otaFail(src, "no space"); return; }
  sendLine(src, "{\"ota\":\"rdy\"}");
  otaShow(0);
  mbedtls_sha256_context ctx;
  mbedtls_sha256_init(&ctx);
  mbedtls_sha256_starts(&ctx, 0);
  static uint8_t buf[4096];
  uint32_t got = 0, lastRx = millis();
  int shown = 0;
  bool ok = true;
  const char* why = "";
  while (got < n) {
    const int av = g_wClient.available();
    if (av <= 0) {
      if (!g_wClient.connected()) { ok = false; why = "closed"; break; }
      if (millis() - lastRx > 15000) { ok = false; why = "timeout"; break; }
      delay(1);
      continue;
    }
    const int r = g_wClient.read(buf, min((uint32_t)sizeof buf, min((uint32_t)av, n - got)));
    if (r <= 0) continue;
    lastRx = millis();
    mbedtls_sha256_update(&ctx, buf, r);
    if (Update.write(buf, r) != (size_t)r) { ok = false; why = "write"; break; }
    got += r;
    const int pct = (int)((uint64_t)got * 100 / n);
    if (pct >= shown + 10 && got < n) { shown = pct - pct % 10; otaShow(shown); }   // 整張重畫約 150 ms，每 10% 一次就好
  }
  uint8_t dig[32];
  mbedtls_sha256_finish(&ctx, dig);
  mbedtls_sha256_free(&ctx);
  if (!ok) { otaFail(src, why); return; }
  char hex[65];
  for (int i = 0; i < 32; i++) snprintf(hex + i * 2, 3, "%02x", dig[i]);
  if (strcasecmp(hex, sha)) { otaFail(src, "sha mismatch"); return; }
  if (!Update.end()) { otaFail(src, "image invalid"); return; }   // 映像檔驗證（晶片、校驗和），通過才切換開機分割區
  otaShow(100);
  sendLine(src, "{\"ota\":\"ok\"}");
  g_wClient.flush();
  delay(300);
  g_wClient.stop();
  delay(100);
  esp_restart();
}

// 沒連上電腦時畫面最下面那行（ui.h g_netHint）
static void wifiHint() {
  const int st = wifiState();
  const char* h = nullptr;
  if (st == 2) { snprintf(g_wHintBuf, sizeof g_wHintBuf, "WIFI %s", WiFi.localIP().toString().c_str()); h = g_wHintBuf; }
  else if (st == 1) h = "WIFI CONNECTING";
  else if (st == 3) h = "WIFI CHECK PASSWORD";
  else if (st == 4) h = "WIFI NOT FOUND";
  static String last = "-";
  const String now = h ? h : "";
  if (now == last) return;
  last = now;
  g_netHint = h;
  if (!linkAlive()) dirty = true;
}

// 每次 loop：收 WiFi 連線的資料；每 20 ms 看一次狀態、新連線、要不要重連
static void wifiTick(uint32_t now) {
  if (g_wClient.available()) { g_wRxMs = now; pollReader(g_wReader); }
  static uint32_t last = 0;
  if (now - last < 20) return;
  last = now;

  if (g_wScanning) {
    const int16_t n = WiFi.scanComplete();
    if (n != WIFI_SCAN_RUNNING) {
      g_wScanning = false;
      wifiSendScan(g_wScanTo, n);
      WiFi.scanDelete();
      if (!g_wSsid.length()) WiFi.mode(WIFI_OFF);
      else g_wBeginMs = 0;                                   // 馬上重連
    }
  }

  const bool up = g_wSsid.length() && wifiUp();
  if (g_wSsid.length() && !up && !g_wScanning && now - g_wBeginMs > (g_wDropped ? 5000u : 20000u)) wifiBegin();
  if (up && !g_wServing) { g_wServer.begin(); g_wServer.setNoDelay(true); g_wServing = true; }
  if (up && !g_wMdns) {
    if (MDNS.begin(g_wHost)) MDNS.addService("aimon", "tcp", WIFI_TCP_PORT);
    g_wMdns = true;
  }

  // 新連線：已經有一台驗證過、30 秒內還有資料的電腦連著就不換（區網裡別的東西連進來不會把電腦擠掉）；
  // 舊的連線沒聲音了（電腦睡眠、斷網）就換成新的
  if (g_wServing && g_wServer.hasClient()) {
    WiFiClient c = g_wServer.available();
    if (g_wClient.connected() && g_wAuth && now - g_wRxMs < 30000) c.stop();
    else {
      wifiCloseClient();
      g_wClient = c;
      g_wClient.setNoDelay(true);
      g_wRxMs = g_wAcceptMs = now;
    }
  }
  // 斷線、5 秒內沒送金鑰、60 秒沒資料（電腦每 15 秒打一次招呼）：關掉
  if (g_wClient && (!g_wClient.connected() || (!g_wAuth && now - g_wAcceptMs > 5000) || (g_wAuth && now - g_wRxMs > 60000)))
    wifiCloseClient();

  // 狀態變了：告訴電腦（設定視窗）、更新沒連線時的提示
  static int lastSt = -1;
  static uint32_t lastIp = 0;
  const int st = wifiState();
  const uint32_t ip = up ? (uint32_t)WiFi.localIP() : 0;
  if (st != lastSt || ip != lastIp) {
    lastSt = st;
    lastIp = ip;
    if (now - g_usbRxMs < 60000) wifiSendStatus(rdUsb.s);
    if (g_wAuth) wifiSendStatus(&g_wClient);
  }
  wifiHint();
}
