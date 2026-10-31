// AI 狀態指示燈（USB 版）
//   ESP32-2424S012（ESP32-C3 + GC9A01 1.28" 240×240 LCD）
//   Waveshare ESP32-S3-Touch-AMOLED-1.75（ESP32-S3 + CO5300 1.75" 466×466 AMOLED）
//   兩種板子共用這份程式，腳位與差異在 config.h（依編譯目標自動選）
//
// 插上 USB 就能用：不需要 Wi-Fi，也不需要任何設定。
// 電腦端的 Chrome 擴充功能透過 USB 序列埠（115200）每次送一行 JSON（完整畫面狀態）：
//   {"ai":"claude","st":"working","msg":"","p5":42,"pw":63,"r5":1792226358,"rw":1792477758,
//    "now":1792218000,"o":[["chatgpt","done"],["gemini","review"]]}
//   age: 這個狀態已持續的秒數（沒有額度資料時，大數字顯示這個）
//   ai : claude | chatgpt | gemini | none
//   st : idle | done | working | review | error
// 收到 {"hello":1} 會回 {"aimon":2,"fw":"版本"}。
// BOOT 鍵：短按翻頁（狀態 → 額度詳細 → 時鐘）；長按 0.7 秒送出 {"ev":"btn"}（標為已讀）。
//
// Arduino IDE 設定：Board = ESP32C3 Dev Module、USB CDC On Boot = Enabled、

#include <ArduinoJson.h>
#define LGFX_USE_V1
#include <LovyanGFX.hpp>
#include "config.h"
#include <Preferences.h>
#define FW_VERSION "1.9.0"

// arduino-cli 自動產生的函式原型會插在第一個函式之前，先宣告 pollReader 參數用到的型別
struct LineReader;

#if BOARD_AMOLED
// CO5300 466×466：控制器記憶體 472 欄，面板從第 6 欄開始（Waveshare BSP 的 set_gap(6, 0)）。
// 初始化指令照 Waveshare BSP；開機先把亮度設 0，畫好黑畫面才亮起來
struct Panel_CO5300_466 : public lgfx::Panel_CO5300 {
  const uint8_t* getInitCommands(uint8_t listno) const override {
    static constexpr uint8_t list0[] = {
      // LovyanGFX 放開重置後只等 64 ms：面板還沒好時前面的換頁指令會被吃掉，
      // 後面的設定寫到別頁 → 偶爾開機全白。先送個 NOP 再多等 150 ms
      0x00, 0x80, 150,
      0xFE, 1, 0x20,
      0x19, 1, 0x10,
      0x1C, 1, 0xA0,
      0xFE, 1, 0x00,
      0xC4, 1, 0x80,
      0x3A, 1, 0x55,                     // RGB565
      0x35, 1, 0x00,                     // TE on
      0x53, 1, 0x20,
      0x51, 1, 0x00,                     // 亮度 0
      0x63, 1, 0xFF,
      0x2A, 4, 0x00, 0x06, 0x01, 0xD7,   // 欄 6 ~ 471
      0x2B, 4, 0x00, 0x00, 0x01, 0xD1,   // 列 0 ~ 465
      0x11, 0x80, 120,                   // sleep out
      0x29, 0x80, 20,                    // display on
      0xFF, 0xFF,
    };
    return listno == 0 ? list0 : nullptr;
  }
};

class LGFX : public lgfx::LGFX_Device {
  Panel_CO5300_466 _panel;
  lgfx::Bus_SPI    _bus;
public:
  LGFX() {
    { auto c = _bus.config();
      c.spi_host = SPI2_HOST; c.spi_mode = 0;
      c.freq_write = LCD_SPI_HZ; c.freq_read = 20000000;
      c.spi_3wire = true; c.use_lock = true; c.dma_channel = 0;   // 不用 DMA：QSPI 經 DMA 的小筆寫入（外環一段一段）會畫壞
      c.pin_sclk = PIN_LCD_SCLK; c.pin_mosi = -1; c.pin_miso = -1; c.pin_dc = -1;
      c.pin_io0 = PIN_LCD_D0; c.pin_io1 = PIN_LCD_D1; c.pin_io2 = PIN_LCD_D2; c.pin_io3 = PIN_LCD_D3;
      _bus.config(c); _panel.setBus(&_bus); }
    { auto c = _panel.config();
      c.pin_cs = PIN_LCD_CS; c.pin_rst = PIN_LCD_RST; c.pin_busy = -1;
      c.panel_width = LCD_W; c.panel_height = LCD_W; c.memory_width = 472; c.memory_height = LCD_W;
      c.offset_x = 6; c.offset_y = 0;
      c.offset_rotation = 0; c.readable = false; c.invert = false; c.rgb_order = false;
      c.dlen_16bit = false; c.bus_shared = false;
      _panel.config(c); }
    setPanel(&_panel);
  }
};
#else
class LGFX : public lgfx::LGFX_Device {
  lgfx::Panel_GC9A01 _panel;
  lgfx::Bus_SPI      _bus;
  lgfx::Light_PWM    _light;
public:
  LGFX() {
    { auto c = _bus.config();
      c.spi_host = SPI2_HOST; c.spi_mode = 0;
      c.freq_write = LCD_SPI_HZ; c.freq_read = 20000000;
      c.spi_3wire = true; c.use_lock = true; c.dma_channel = SPI_DMA_CH_AUTO;
      c.pin_sclk = PIN_LCD_SCLK; c.pin_mosi = PIN_LCD_MOSI; c.pin_miso = -1; c.pin_dc = PIN_LCD_DC;
      _bus.config(c); _panel.setBus(&_bus); }
    { auto c = _panel.config();
      c.pin_cs = PIN_LCD_CS; c.pin_rst = -1; c.pin_busy = -1;
      c.panel_width = 240; c.panel_height = 240; c.offset_x = 0; c.offset_y = 0;
      c.offset_rotation = 0; c.readable = false; c.invert = true; c.rgb_order = false;
      c.dlen_16bit = false; c.bus_shared = false;
      _panel.config(c); }
    { auto c = _light.config();
      c.pin_bl = PIN_LCD_BL; c.invert = false; c.freq = 12000; c.pwm_channel = 0;
      _light.config(c); _panel.setLight(&_light); }
    setPanel(&_panel);
  }
};
#endif

static LGFX lcd;
static LGFX_Sprite band(&lcd);
#include "ui.h"

static uint8_t brightness = BRIGHTNESS;
static int g_rot = 0;
static bool dirty = true;

// 畫一條（OY 那一條，高 BAND_H）並送到螢幕
static bool g_anyKeyed = false;    // 這一輪有沒有任何一條留空了外環
static void renderStrip(uint32_t ms) {
  g_ringKeyed = false;
  drawScene(ms);
  lcd.startWrite();
  // 外環區域留空（透明），不覆蓋螢幕上正在呼吸的外環 → 不會出現一條一條亮度不同
  if (g_ringKeyed) { band.pushSprite(0, OY, RING_KEY); g_anyKeyed = true; }
  else band.pushSprite(0, OY);
  lcd.endWrite();
}

static void render() {          // 一次畫完整個畫面（開機用）
  uint32_t ms = millis();
  for (OY = 0; OY < H; OY += BAND_H) renderStrip(ms);
  OY = 0;
  lcd.startWrite(); statusRingOn(lcd, 0, millis()); lcd.endWrite();
}

// ---------------- 序列埠：同時聽 USB（Serial）與 UART0（Serial0）----------------
// 不同批次的板子有的走 ESP32-C3 內建 USB，有的經過 CH340 轉 UART0，兩條都收。
struct LineReader {
  Stream* s; String buf;
  explicit LineReader(Stream* st) : s(st) {}
};

static void sendLine(Stream* s, const char* line) {
  if (!s) return;
  s->println(line);
}

static Stream* lastSource = nullptr;

// ---------------- 外接 WS2812（RX 腳）----------------
// 顏色、亮度跟外環一樣（ringParams），每 16 ms 更新一次，值沒變就不送。
static int g_ledPct = 0;          // 0 = 關閉，1-100 = 最大亮度 %
static bool g_ledPin = !LISTEN_UART0;   // LED 腳可以用了（C3：RX 腳改成 LED 用，UART0 不再接收）

static void ledSetPct(Stream* src, int pct) {
#if !LISTEN_UART0
  (void)src;                               // LED 有自己的腳，不跟序列埠共用
#elif ARDUINO_USB_CDC_ON_BOOT
  if (pct > 0 && src != &Serial) return;   // 走 UART0（CH340 板）連線時，RX 腳是通訊用的，不能接管
  if (pct > 0 && !g_ledPin) { Serial0.end(); g_ledPin = true; }
#else
  (void)src; (void)pct; return;            // Serial 本身就是 UART0，RX 腳不能用
#endif
  g_ledPct = pct < 0 ? 0 : (pct > 100 ? 100 : pct);
}

static void ledTick(uint32_t ms) {
  static uint32_t lastMs = 0, lastVal = 0xFFFFFFFF;
  if (!g_ledPin || ms - lastMs < 16) return;
  lastMs = ms;
  uint8_t r = 0, g = 0, b = 0;
  if (g_ledPct > 0 && linkAlive() && g_frame.st != S_IDLE) {     // 待命、沒連線：熄燈
    uint32_t col; float k;
    ringParams(ms, col, k);
    // LED 用純色：螢幕的 Apple 系統色在 LED 上綠色會偏藍、紅色會偏粉紅
    if (col == C_GREEN) col = 0x00FF00;
    else if (col == C_RED) col = 0xFF0000;
    float s = k * k * g_ledPct / 100.0f;                          // gamma 2：低亮度時呼吸才平順
    if (g_saver) s = s * LED_SAVER_PCT / 100.0f;
    r = (uint8_t)(((col >> 16) & 0xFF) * s + 0.5f);
    g = (uint8_t)(((col >> 8) & 0xFF) * s + 0.5f);
    b = (uint8_t)((col & 0xFF) * s + 0.5f);
  }
  uint32_t v = ((uint32_t)r << 16) | ((uint32_t)g << 8) | b;
  if (v == lastVal) return;
  lastVal = v;
  neopixelWrite(PIN_LED, r, g, b);
}

// ---------------- 負荷統計（隨 hello 回報給電腦端，寫進 AIMonitor 紀錄）----------------
static uint32_t g_maxLoopMs = 0;    // 上次回報之後，最長的一次 loop（畫一條畫面時 USB 沒人讀）
static uint32_t g_maxStripMs = 0;   // 上次回報之後，最長的一條畫面重畫
static uint32_t g_maxRingMs = 0;    // 上次回報之後，最長的一格外環（AMOLED 一格要十幾 ms）
static uint32_t g_badLines = 0;     // 開機以來解析失敗的行（資料被截斷 / 丟掉）
static uint32_t g_longLines = 0;    // 開機以來超過長度上限、整行丟掉的次數

#ifdef PIN_LCD_TE
// AMOLED：面板每開始掃描新的一張，TE 腳送一個脈衝（約 60 Hz）。外環在這之後馬上寫（約 3 ms，比面板掃描快），
// 不然面板掃到一半時寫入，會半圈新、半圈舊（外圈出現一段錯開的缺口）
static volatile uint32_t g_teUs = 0;
static volatile uint32_t g_teN = 0;       // 上次回報之後的脈衝數（回報給電腦端，確認 TE 有訊號）
static void IRAM_ATTR onTE() { g_teUs = micros(); g_teN++; }
#endif

static void sendHello(Stream* src) {
  char b[192];
  snprintf(b, sizeof b, "{\"aimon\":2,\"fw\":\"" FW_VERSION "\",\"hw\":\"" BOARD_ID "\",\"up\":%lu,\"loop\":%lu,\"strip\":%lu,\"bad\":%lu,\"long\":%lu,\"heap\":%lu}",
           (unsigned long)(millis() / 1000), (unsigned long)g_maxLoopMs, (unsigned long)g_maxStripMs,
           (unsigned long)g_badLines, (unsigned long)g_longLines, (unsigned long)ESP.getFreeHeap());
#ifdef PIN_LCD_TE
  snprintf(b + strlen(b) - 1, sizeof b - strlen(b) + 1, ",\"te\":%lu,\"ring\":%lu}", (unsigned long)g_teN, (unsigned long)g_maxRingMs);
  g_teN = 0;
#endif
  sendLine(src, b);
  g_maxLoopMs = g_maxStripMs = g_maxRingMs = 0;
}

static void handleLine(Stream* src, const String& line) {
  JsonDocument doc;
  if (deserializeJson(doc, line)) { g_badLines++; return; }
  lastSource = src;
  if (doc["hello"].is<int>()) {
    sendHello(src);
    return;
  }
  if (doc["bright"].is<int>()) {
    brightness = doc["bright"].as<int>();
    lcd.setBrightness(brightness);
  }
  if (!doc["st"].is<const char*>()) return;

  Frame f;
  f.brand = brandIndex(doc["ai"] | "none");
  f.st = parseSt(doc["st"] | "idle");
  f.msg = (const char*)(doc["msg"] | "");
  f.note = (const char*)(doc["note"] | "");
  { const char* nk = doc["nk"] | ""; f.noteKind = !strcmp(nk, "reset") ? 2 : (f.note.length() ? 1 : 0); }
  if (doc["p5"].is<float>()) f.p5 = doc["p5"].as<float>();
  if (doc["pw"].is<float>()) f.pw = doc["pw"].as<float>();
  f.r5 = doc["r5"] | 0UL;
  f.rw = doc["rw"] | 0UL;
  JsonArray o = doc["o"].as<JsonArray>();
  for (JsonArray item : o) {
    if (f.nOthers >= 5) break;
    f.others[f.nOthers].brand = brandIndex(item[0] | "none");
    f.others[f.nOthers].st = parseSt(item[1] | "idle");
    f.nOthers++;
  }
  uint32_t now = doc["now"] | 0UL;
  if (now > 1700000000UL) g_epochOffset = (int32_t)(now - millis() / 1000);

  // "age"：此狀態已持續幾秒（由電腦端計算，切換分頁也正確）
  g_stateSinceBase = doc["age"] | 0UL;
  g_stateSinceMs = millis();
  if (f.st != g_frame.st || f.brand != g_frame.brand) g_stChangedMs = millis();
  // 有狀態（或狀態改變）就算「有事」→ 離開 / 延後螢幕保護
  if (f.st != S_IDLE || f.st != g_frame.st || !linkAlive()) g_lastActiveMs = millis();
  // 新的額度重置提醒：叫醒螢幕並回到狀態頁
  if (f.note.length() && (f.note != g_frame.note || f.noteKind != g_frame.noteKind)) {
    if (f.noteKind != g_frame.noteKind || !g_frame.note.length()) { g_lastActiveMs = millis(); g_page = 0; }
  }
  if (doc["tz"].is<int>()) g_tz = doc["tz"].as<int>();
  if (doc["saver"].is<int>()) g_saverMin = doc["saver"].as<int>();   // 螢幕保護分鐘數（0 = 關閉）
  if (doc["rot"].is<int>()) {                                           // 螢幕方向 0-3（×90°，由電腦端設定）
    int r = doc["rot"].as<int>() & 3;
    if (r != g_rot) {
      g_rot = r;
      lcd.setRotation(g_rot);
      lcd.fillScreen(TFT_BLACK);
      Preferences pf; pf.begin("aimon", false); pf.putUChar("rot", g_rot); pf.end();   // 記住，下次開機一插電就是這個方向
    }
  }
  if (doc["bri"].is<int>()) {                                           // 亮度（由電腦端設定）
    int b = doc["bri"].as<int>();
    if (b >= 5 && b <= 255 && b != brightness) { brightness = b; if (!g_saver) lcd.setBrightness(brightness); }
  }
  if (doc["led"].is<int>()) ledSetPct(src, doc["led"].as<int>());   // 外接 LED 亮度 0-100
  JsonArray ds = doc["d"].as<JsonArray>();
  for (JsonArray d : ds) {
    if (f.nToday >= 3) break;
    f.today[f.nToday].brand = brandIndex(d[0] | "none");
    f.today[f.nToday].count = d[1] | 0;
    f.today[f.nToday].secs = d[2] | 0UL;
    f.nToday++;
  }
  // 需要你處理的狀態：自動回到狀態頁
  if ((f.st == S_REVIEW || f.st == S_ERROR || (f.st == S_WORKING && g_frame.st != S_WORKING)) && g_page != 0) g_page = 0;
  g_frame = f;
  g_lastFrameMs = millis();
  if (!g_everConnected) g_everConnected = true;
  dirty = true;
}

static void pollReader(LineReader& r) {
  while (r.s->available()) {
    char c = r.s->read();
    if (c == '\n' || c == '\r') {
      if (r.buf.length()) handleLine(r.s, r.buf);
      r.buf = "";
    } else if (r.buf.length() < 1500) {
      r.buf += c;
    } else {
      r.buf = "";   // 異常長行，丟掉
      g_longLines++;
    }
  }
}

static LineReader rdUsb(&Serial);
#if ARDUINO_USB_CDC_ON_BOOT && LISTEN_UART0
static LineReader rdUart(&Serial0);
#endif

// ---------------- BOOT 鍵 ----------------
static uint32_t g_pageMs = 0;

static void pollButton() {
  static bool was = false; static uint32_t downAt = 0; static bool longDone = false; static bool wokeSaver = false;
  bool down = digitalRead(PIN_BTN) == LOW;
  uint32_t now = millis();
  if (down && !was) {
    downAt = now; longDone = false;
    wokeSaver = g_saver;                                  // 螢幕保護中：這一下只負責叫醒
    g_lastActiveMs = now;
  }
  if (down && !longDone && now - downAt > 700) {          // 長按：標為已讀
    sendLine(lastSource ? lastSource : &Serial, "{\"ev\":\"btn\"}");
    if (g_frame.st == S_DONE || g_frame.st == S_ERROR) { g_frame.st = S_IDLE; g_frame.msg = ""; }
    g_page = 0;
    dirty = true;
    longDone = true;
  }
  if (!down && was && !longDone && now - downAt > 30 && !wokeSaver) {   // 短按：翻頁
    g_page = (g_page + 1) % N_PAGES;
    g_pageMs = now;
    dirty = true;
  }
  if (g_page != 0 && now - g_pageMs > 20000) { g_page = 0; dirty = true; }   // 20 秒沒操作回到狀態頁
  was = down;
}

// 外環這一格：0 = 還不用畫，1 = 現在畫，2 = 快到了、等面板（這次 loop 不要重畫其他東西）
// stripWaiting：重畫進行中，而且上一格外環之後還沒畫過任何一條
static int ringFrameState(uint32_t now, uint32_t lastRing, bool stripWaiting) {
  if (!ringAnimating()) return 0;
#ifdef PIN_LCD_TE
  const uint32_t te = micros() - g_teUs;
  if (g_teUs && te < 100000) {                    // 有 TE：跟著面板的節奏
    // AMOLED 一格外環要十幾 ms，加上等 TE 會占滿整個 16.7 ms：重畫中每一格外環之後先畫一條，
    // 不然重畫永遠排不到（外環照常呼吸，中間的字卻一直不更新）
    if (stripWaiting) return 0;
    if (now - lastRing < 10) return 0;            // 剛畫過，這段時間可以重畫一條
    return te < 2000 ? 1 : 2;
  }
#endif
  return now - lastRing >= 16 ? 1 : 0;            // 約 60 fps
}

void setup() {
  // 一行畫面資料約 400-600 位元組，預設接收緩衝只有 256：畫一條畫面的期間沒人讀，滿了後面就被丟掉 → 那行解析失敗
  Serial.setRxBufferSize(2048);
  Serial.begin(115200);
#if ARDUINO_USB_CDC_ON_BOOT && LISTEN_UART0
  Serial0.setRxBufferSize(1024);
  Serial0.begin(115200);
#endif
  pinMode(PIN_BTN, INPUT_PULLUP);
#ifdef PIN_LCD_TE
  pinMode(PIN_LCD_TE, INPUT);
  attachInterrupt(PIN_LCD_TE, onTE, RISING);
#endif

  lcd.init();
  { Preferences pf; pf.begin("aimon", true); g_rot = pf.getUChar("rot", 0) & 3; pf.end(); }
  lcd.setRotation(g_rot);
  lcd.fillScreen(TFT_BLACK);
  lcd.setBrightness(brightness);
  loadFonts();
  // 外環直接畫到螢幕，sprite 裡留空（透明色）。AMOLED 不行：透明色切出來的每一段要偶數對齊，
  // 不對齊的整段會被丟掉 → 外環也畫進 sprite（每條重畫只要幾 ms，跟呼吸燈差一格看不出來）
  g_ringDirect = !BOARD_AMOLED;
  g_ringLive = BOARD_AMOLED;
  ringInit();
  gaugeInit();
  band.setColorDepth(16);
  if (!band.createSprite(W, BAND_H)) { band.setColorDepth(8); band.createSprite(W, BAND_H); }
  render();
}

void loop() {
  const uint32_t loopStart = millis();
  pollReader(rdUsb);
#if ARDUINO_USB_CDC_ON_BOOT && LISTEN_UART0
  if (!g_ledPin) pollReader(rdUart);
#endif
  pollButton();
  ledTick(millis());

  static uint32_t lastFull = 0, lastRing = 0;
  static bool lastAlive = false;
  uint32_t now = millis();
  bool alive = linkAlive();
  if (alive != lastAlive) { lastAlive = alive; dirty = true; if (!alive) g_lastActiveMs = now; }

  // 螢幕保護：進入時調暗背光，離開時恢復
  bool want = saverWanted();
  if (want != g_saver) {
    g_saver = want;
    lcd.setBrightness(g_saver ? SAVER_BRIGHT : brightness);
    dirty = true;
  }

#if BOARD_AMOLED
  // 防烙印：AMOLED 長時間顯示一樣的字會留下殘影 → 文字每 2 分鐘換個位置（9 個位置輪流，最多偏 2 個設計像素）
  {
    static const int8_t SHIFT[9][2] = {{0, 0}, {2, 0}, {2, 2}, {0, 2}, {-2, 2}, {-2, 0}, {-2, -2}, {0, -2}, {2, -2}};
    const int8_t* sh = SHIFT[(now / 120000UL) % 9];
    if (SI(sh[0]) != g_shX || SI(sh[1]) != g_shY) { g_shX = SI(sh[0]); g_shY = SI(sh[1]); dirty = true; }
  }
#endif

  // 整個畫面：資料變了、或每秒一次（時間、倒數）。
  // 分成 6 條，每次 loop 只畫一條，中間照常更新呼吸燈 → 呼吸燈不會因為重畫數字而卡住
  static int strip = -1;            // -1 = 沒有在重畫
  static uint32_t stripMs = 0;
  if (strip < 0 && (dirty || now - lastFull >= (animating() ? 1000u : 5000u))) {
    strip = 0; stripMs = now; lastFull = now; dirty = false; g_anyKeyed = false;
  }
  static bool stripSinceRing = false;   // 上一格外環之後畫過一條了
  const int ringState = ringFrameState(now, lastRing, strip >= 0 && !stripSinceRing);
  if (ringState == 1) {
    lcd.startWrite();
    statusRingOn(lcd, 0, now);
    lcd.endWrite();
    lastRing = now;
    stripSinceRing = false;
    if (millis() - now > g_maxRingMs) g_maxRingMs = millis() - now;
  } else if (ringState == 0 && strip >= 0) {
    OY = strip * BAND_H;
    const uint32_t t0 = millis();
    renderStrip(stripMs);
    stripSinceRing = true;
    if (millis() - t0 > g_maxStripMs) g_maxStripMs = millis() - t0;
    OY = 0;
    if (++strip * BAND_H >= H) {
      strip = -1;
      if (g_anyKeyed && !ringAnimating()) { lcd.startWrite(); statusRingOn(lcd, 0, millis()); lcd.endWrite(); }   // 靜態外環（完成、待命）
    }
  }
  if (millis() - loopStart > g_maxLoopMs) g_maxLoopMs = millis() - loopStart;
  delay(1);
}
