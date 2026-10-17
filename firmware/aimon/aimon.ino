// AI 狀態指示燈（USB 版） — ESP32-2424S012（ESP32-C3 + GC9A01 1.28" 圓形螢幕）
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
#define FW_VERSION "1.6.0"

// arduino-cli 自動產生的函式原型會插在第一個函式之前，先宣告 pollReader 參數用到的型別
struct LineReader;

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

static void handleLine(Stream* src, const String& line) {
  JsonDocument doc;
  if (deserializeJson(doc, line)) return;
  lastSource = src;
  if (doc["hello"].is<int>()) {
    sendLine(src, "{\"aimon\":2,\"fw\":\"" FW_VERSION "\"}");
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
    if (f.nOthers >= 3) break;
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
  JsonArray ds = doc["d"].as<JsonArray>();
  for (JsonArray d : ds) {
    if (f.nToday >= 3) break;
    f.today[f.nToday].brand = brandIndex(d[0] | "none");
    f.today[f.nToday].count = d[1] | 0;
    f.today[f.nToday].secs = d[2] | 0UL;
    f.nToday++;
  }
  JsonArray hs = doc["h"].as<JsonArray>();
  for (JsonVariant v : hs) { if (f.nHist >= 24) break; f.hist[f.nHist++] = v.as<float>(); }
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
    }
  }
}

static LineReader rdUsb(&Serial);
#if ARDUINO_USB_CDC_ON_BOOT
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

void setup() {
  Serial.begin(115200);
#if ARDUINO_USB_CDC_ON_BOOT
  Serial0.begin(115200);
#endif
  pinMode(PIN_BTN, INPUT_PULLUP);

  lcd.init();
  { Preferences pf; pf.begin("aimon", true); g_rot = pf.getUChar("rot", 0) & 3; pf.end(); }
  lcd.setRotation(g_rot);
  lcd.setBrightness(brightness);
  lcd.fillScreen(TFT_BLACK);
  loadFonts();
  g_ringDirect = true;
  ringInit();
  band.setColorDepth(16);
  if (!band.createSprite(W, BAND_H)) { band.setColorDepth(8); band.createSprite(W, BAND_H); }
  render();
}

void loop() {
  pollReader(rdUsb);
#if ARDUINO_USB_CDC_ON_BOOT
  pollReader(rdUart);
#endif
  pollButton();

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

  // 整個畫面：資料變了、或每秒一次（時間、倒數）。
  // 分成 6 條，每次 loop 只畫一條，中間照常更新呼吸燈 → 呼吸燈不會因為重畫數字而卡住
  static int strip = -1;            // -1 = 沒有在重畫
  static uint32_t stripMs = 0;
  if (strip < 0 && (dirty || now - lastFull >= (animating() ? 1000u : 5000u))) {
    strip = 0; stripMs = now; lastFull = now; dirty = false; g_anyKeyed = false;
  }
  if (ringAnimating() && now - lastRing >= 16) {             // 約 60 fps
    lcd.startWrite();
    statusRingOn(lcd, 0, now);
    lcd.endWrite();
    lastRing = now;
  } else if (strip >= 0) {
    OY = strip * BAND_H;
    renderStrip(stripMs);
    OY = 0;
    if (++strip * BAND_H >= H) {
      strip = -1;
      if (g_anyKeyed && !ringAnimating()) { lcd.startWrite(); statusRingOn(lcd, 0, millis()); lcd.endWrite(); }   // 靜態外環（完成、待命）
    }
  }
  delay(1);
}
