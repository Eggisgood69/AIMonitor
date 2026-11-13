// AI 狀態指示燈（USB 版）
//   ESP32-2424S012（ESP32-C3 + GC9A01 1.28" 240×240 LCD）
//   Waveshare ESP32-S3-Touch-AMOLED-1.75（ESP32-S3 + CO5300 1.75" 466×466 AMOLED）
//   兩種板子共用這份程式，腳位與差異在 config.h（依編譯目標自動選）
//
// 插上 USB 就能用，不需要任何設定。AMOLED 另外可以設定 WiFi（wifi.h），之後不插電腦也收得到畫面。
// 電腦端的 Chrome 擴充功能透過 USB 序列埠（115200）每次送一行 JSON（完整畫面狀態）：
//   {"ai":"claude","st":"working","msg":"","p5":42,"pw":63,"r5":1792226358,"rw":1792477758,
//    "now":1792218000,"o":[["chatgpt","done"],["gemini","review"]]}
//   age: 這個狀態已持續的秒數（沒有額度資料時，大數字顯示這個）
//   ai : claude | chatgpt | gemini | none
//   st : idle | done | working | review | error
// 收到 {"hello":1} 會回 {"aimon":2,"fw":"版本"}。
// BOOT 鍵：短按翻頁（狀態 → 額度詳細 → 今日統計 → 時鐘）；長按 0.7 秒送出 {"ev":"btn"}（標為已讀）。
// AMOLED 另外可以左右滑換頁、按住螢幕標為已讀，並依陀螺儀自動轉向（sensors.h）。
//
// Arduino IDE 設定：Board = ESP32C3 Dev Module、USB CDC On Boot = Enabled、

#include <Wire.h>   // AMOLED 的觸控、陀螺儀（sensors.h）；要放在最前面，arduino-cli 才會把它加進來
#include <WiFi.h>   // AMOLED 的 WiFi（wifi.h）；一樣要放在這裡。1.28 吋沒呼叫，不會連結進去
#include <ESPmDNS.h>
#include <Update.h>   // 經 WiFi 更新韌體（wifi.h wifiOta）
#include <mbedtls/sha256.h>
#include <esp_ota_ops.h>
#include <ArduinoJson.h>
#define LGFX_USE_V1
#include <LovyanGFX.hpp>
#include "config.h"
#include <Preferences.h>
#include <FFat.h>        // AMOLED 自訂動畫存檔（anim.h）
#define FW_VERSION "1.12.1"

// arduino-cli 自動產生的函式原型會插在第一個函式之前，先宣告 pollReader 參數用到的型別
struct LineReader;
struct Press;

#if BOARD_AMOLED
// 時鐘頁的大字型：LovyanGFX 畫一個字會把整個字形放在堆疊上，466 的字形比預設的 8 KB 還大（會當機重開）
SET_LOOP_TASK_STACK_SIZE(16 * 1024);

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
static bool g_othersDirty = false;   // 只有其他對話的小圓點變了：只重畫那一塊（othersTick）
// 正在畫的那張、螢幕上那張是什麼狀態（-1 = 還沒畫過）。Generating 的光只在螢幕上真的是生成中畫面時才走：
// 需審核 → 生成中時，整張重畫完成之前螢幕上還是「Needs Review」（比 Generating 寬），光先畫上去會留下舊字的頭尾
static int8_t g_drawSt = -1, g_shownSt = -1;
static bool g_rectFbOnly = false;    // AMOLED：redrawRect 只畫進 g_fb、不送（整張送出前補畫用，馬上整張送）

// 亮度（每次 loop 套用，亮度只在這裡設）。AMOLED 換頁、轉向、進出螢幕保護時：淡出 → 全暗時換上新畫面 → 淡入。
// 整張送出要 20 ms，比面板掃一次（16.7 ms）久，亮著直接換會看到撕裂
static int g_fadeLv = 1000;          // 0 = 全暗，1000 = 正常亮度
static int8_t g_fadeDir = 0;         // -1 淡出中（新畫面還沒送）、1 淡入中、0 沒有轉場
static int g_fadeOutBright = 0;      // 淡出時用轉場前的亮度（離開螢幕保護時不會先亮一下）
static const int FADE_OUT_MS = 120, FADE_IN_MS = 160;

static void showBright() {
  static int last = -1;
  const int target = g_fadeDir < 0 ? g_fadeOutBright : (g_saver ? SAVER_BRIGHT : brightness);
  const int b = target * g_fadeLv / 1000 * g_fadeLv / 1000;   // 平方：亮度變化看起來比較均勻
  if (b != last) { lcd.setBrightness(b); last = b; }
}

static void fadeTick(uint32_t now) {
  static uint32_t last = 0;
  const int step = (int)(now - last);
  last = now;
  if (g_fadeDir < 0) g_fadeLv = max(0, g_fadeLv - step * 1000 / FADE_OUT_MS);
  else if (g_fadeDir > 0) {
    g_fadeLv = min(1000, g_fadeLv + step * 1000 / FADE_IN_MS);
    if (g_fadeLv == 1000) g_fadeDir = 0;
  }
  showBright();
}

// 螢幕方向。AMOLED 不用面板的硬體旋轉（CO5300 在 90° / 180° / 270° 的定址跟 LovyanGFX 對不上，
// 外環和小圓點附近會錯位），面板固定 0°，每一條畫好後自己轉向再送（pushRows）；外環是正圓，不用轉
static void applyRotation() {
  lcd.setRotation(BOARD_AMOLED ? 0 : g_rot);
}

static uint16_t* g_fb = nullptr;        // AMOLED：整張畫面（PSRAM）。nullptr = 一條一條畫、畫好一條就送（C3）
#if BOARD_AMOLED
#include "sensors.h"
#include "mic.h"
static const int FB_ROWS = (H + BAND_H - 1) / BAND_H * BAND_H;   // 最後一條也是 BAND_H 列，不會寫出界
static LGFX_Sprite g_fbSprite(&lcd);    // 整張畫面（外環畫進去用）
static bool g_framePending = false;     // 整張畫好了，等 TE 送出
static uint16_t g_rotBuf[W * BAND_H];   // 轉向後的一條（RGB565，位元組已對調，直接送）

// 把 src（第 oy 列起、h 列，寬 W，位元組已對調的 RGB565）送到螢幕，在這裡轉向。位置都是偶數（W、BAND_H、oy 都是偶數）
static void pushRows(const uint16_t* src, int oy, int h) {
  uint16_t* d = g_rotBuf;
  if (g_rot == 0) {
    lcd.pushImage(0, oy, W, h, (const lgfx::swap565_t*)src);
  } else if (g_rot == 2) {                // 180°：(x, y) → (W-1-x, H-1-y)
    for (int r = 0; r < h; r++)
      for (int x = 0; x < W; x++) d[(h - 1 - r) * W + (W - 1 - x)] = src[r * W + x];
    lcd.pushImage(0, H - oy - h, W, h, (const lgfx::swap565_t*)d);
  } else if (g_rot == 1) {                // 90°（順時針）：(x, y) → (W-1-y, x)
    for (int r = 0; r < h; r++)
      for (int x = 0; x < W; x++) d[x * h + (h - 1 - r)] = src[r * W + x];
    lcd.pushImage(W - oy - h, 0, h, H, (const lgfx::swap565_t*)d);
  } else {                                // 270°：(x, y) → (y, H-1-x)
    for (int r = 0; r < h; r++)
      for (int x = 0; x < W; x++) d[(H - 1 - x) * h + r] = src[r * W + x];
    lcd.pushImage(oy, 0, h, H, (const lgfx::swap565_t*)d);
  }
}

// 整張送出不撕裂的條件：從 TE 開始寫，寫完的時間 ≤ 約兩格（33 ms）。
// 寫得比面板掃描慢沒關係（第一格掃到的全是舊的、第二格全是新的），但超過兩格，第二格掃到下半部時還沒寫到 → 下面一段是舊的
// 所以 TE 之後只做 SPI 寫入；外環、轉向先在等 TE 之前做好（prepareFrame）
static uint16_t* g_fbRot = nullptr;     // 轉向後的整張（90° / 180° / 270°）；說話光暈時 0° 也用它（加了光的那張）
static uint16_t* g_fbOut = nullptr;     // 上次 prepareFrame 準備好要送的那張（g_fb 或 g_fbRot）
static bool g_frameReady = false;       // 外環、轉向都做好了，只等 TE

// ---------------- 說話光暈（麥克風錄音中）----------------
// 有程式在錄音時（Win+H 等），黑色背景隨說話音量淡淡亮起：圓心最亮、往外淡掉，越大聲越亮、範圍越大。
// 不重畫畫面（整張重畫要 150 ms 以上，也會搶 CPU 讓麥克風掉封包）：g_fb 保持原樣，送出前才把光「加」上去——
// 整張送出時跟轉向一起搬到 g_fbRot（composeFrame），小範圍更新在 pushRect 裡加。
// 加法混色：黑底變成光的顏色，白字加一點還是白，反鋸齒的字邊也不會出現黑邊。光的顏色是主燈那家 AI 的品牌色。
// 螢幕保護時不亮（Clawd 亂跑用的是另一張底圖，整張送出會留下殘影）
#define VG_BINS 48                        // 圓心到光的邊緣分幾格
#define VG_STEPS 128                      // 光的強度分幾階（0 = 沒有光）；32 階時慢慢淡掉看得到一階一階跳（使用者說太卡）
static const float VG_MAX = 0.85f;        // 最大聲時圓心的亮度：品牌色的 85%（22%、45%、65% 時使用者都覺得太暗）
static uint8_t g_vgQ = 0;                 // 畫面上現在的光強度（0-VG_STEPS）
static uint8_t g_vgAdd[VG_BINS + 1][3];   // 每格距離要加的 R / G / B（0-255）；最後一格 = 0
static uint16_t g_vgBlk[VG_BINS + 1][16]; // 黑底的像素加上光後的顏色（位元組對調的 RGB565），依 Bayer 位置；背景大多是黑的，直接查表
static uint8_t g_vgPalQ = 0xFF, g_vgPalBrand = 0xFF;
static uint8_t* g_vgBin = nullptr;        // 距離平方 / 64 → 第幾格
static int g_vgR2Max = 0;                 // 光最大的範圍（距離平方）
static int g_vgR2Act = 0;                 // 現在這張光照得到的範圍（距離平方），外面不加
static float g_vgLevel = 0;               // 平滑後的音量 0-1
static uint32_t g_vgMs = 0, g_vgPushMs = 0;
static uint32_t g_vgN = 0, g_vgGap = 0, g_vgLastPush = 0, g_vgMaxMs = 0;   // 量測：光暈整張送了幾次、連續變化時兩次最長間隔 ms

static void vgInit() {
  if (g_vgBin) return;
  const float rmax = S(104);              // 只到額度環內側，不碰外環
  g_vgR2Max = (int)(rmax * rmax);
  const int n = (g_vgR2Max >> 6) + 1;
  g_vgBin = (uint8_t*)malloc(n);
  if (!g_vgBin) return;
  for (int i = 0; i < n; i++) {
    const int b = (int)(sqrtf(i * 64.0f) / rmax * VG_BINS);
    g_vgBin[i] = b >= VG_BINS ? VG_BINS : b;
  }
}

// 強度 q 的光：越大聲範圍越大（光的半徑 45% → 100%），中間最亮、往外平滑淡掉
static void vgPalette(int q) {
  const uint32_t col = BRANDS[g_frame.brand].color;
  const float L = q / (float)VG_STEPS, R = 0.45f + 0.55f * L;
  for (int b = 0; b < VG_BINS; b++) {
    const float x = (b + 0.5f) / VG_BINS / R, f = x < 1 ? (1 - x * x) * (1 - x * x) : 0;
    const float k = VG_MAX * L * f;
    g_vgAdd[b][0] = (uint8_t)(((col >> 16) & 255) * k + 0.5f);
    g_vgAdd[b][1] = (uint8_t)(((col >> 8) & 255) * k + 0.5f);
    g_vgAdd[b][2] = (uint8_t)((col & 255) * k + 0.5f);
  }
  g_vgAdd[VG_BINS][0] = g_vgAdd[VG_BINS][1] = g_vgAdd[VG_BINS][2] = 0;
  static const uint8_t BAYER[16] = {0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5};
  for (int b = 0; b <= VG_BINS; b++)
    for (int i = 0; i < 16; i++) {
      const int d = BAYER[i], r = min(31, (g_vgAdd[b][0] + (d >> 1)) >> 3), g = min(63, (g_vgAdd[b][1] + (d >> 2)) >> 2), bl = min(31, (g_vgAdd[b][2] + (d >> 1)) >> 3);
      const uint16_t o = (uint16_t)((r << 11) | (g << 5) | bl);
      g_vgBlk[b][i] = (uint16_t)((o >> 8) | (o << 8));
    }
  // 這次光照得到的範圍（最後一格有加東西的距離）：外面整段直接複製，小聲時少算一大半
  int last = -1;
  for (int b = 0; b < VG_BINS; b++) if (g_vgAdd[b][0] | g_vgAdd[b][1] | g_vgAdd[b][2]) last = b;
  const float rr = (last + 1) * S(104) / VG_BINS;
  g_vgR2Act = last < 0 ? 0 : min(g_vgR2Max, (int)(rr * rr) + 1);
  g_vgPalQ = q;
  g_vgPalBrand = g_frame.brand;
}

// 一個像素（看到的畫面座標 x, y；位元組對調的 RGB565）加上光。光很暗，RGB565 一階一階會看到一圈一圈：用 4×4 的 Bayer 抖動打散
static inline uint16_t vgAddPx(uint16_t p, int x, int y) {
  const int dx = x - CX, dy = y - CY, r2 = dx * dx + dy * dy;
  if (r2 >= g_vgR2Act) return p;
  if (!p) return g_vgBlk[g_vgBin[r2 >> 6]][((y & 3) << 2) | (x & 3)];   // 黑底：查表
  const uint8_t* a = g_vgAdd[g_vgBin[r2 >> 6]];
  if (!(a[0] | a[1] | a[2])) return p;
  static const uint8_t BAYER[16] = {0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5};
  const int d = BAYER[((y & 3) << 2) | (x & 3)];
  const uint16_t v = (uint16_t)((p >> 8) | (p << 8));
  int r = (v >> 11) + ((a[0] + (d >> 1)) >> 3), g = ((v >> 5) & 63) + ((a[1] + (d >> 2)) >> 2), b = (v & 31) + ((a[2] + (d >> 1)) >> 3);
  if (r > 31) r = 31;
  if (g > 63) g = 63;
  if (b > 31) b = 31;
  const uint16_t o = (uint16_t)((r << 11) | (g << 5) | b);
  return (uint16_t)((o >> 8) | (o << 8));
}

// 音量平滑，兩條包絡取大的：
//  - 快的（變亮 VG_RISE_S、變暗 VG_FAST_FALL_S）跟著每個字的音量起伏，看起來才「跟嘴」
//  - 慢的記住剛才講話的音量，停 VG_HOLD_MS 後以 VG_FALL_S 淡掉，只用它的 VG_TAIL 倍當底：講完不會一下子全暗
// 調過的：原本只有一條、350 ms 就暗 → 使用者覺得消失太快；停 0.4 秒 + 1.2 秒淡掉 → 反應慢；
// 停 0.2 秒 + 0.6 秒 → 字與字之間不會變暗，「沒有很跟嘴」；改成兩條、底 45%、停 0.2 秒 + 0.6 秒 → 講完消失太快；
// 快的那條 20 ms 亮 / 80 ms 暗 → 講話時光一直閃；停 0.4 秒 + 1 秒 → 消失還是太快
static const float VG_RISE_S = 0.05f;
static const float VG_FAST_FALL_S = 0.25f;
static const uint32_t VG_HOLD_MS = 600;
static const float VG_FALL_S = 1.6f;
static const float VG_TAIL = 0.6f;
static uint32_t g_vgHoldMs = 0;
static float g_vgFast = 0, g_vgSlow = 0;
static const float VG_WAKE = 0.25f;       // 音量到這裡就叫醒螢幕保護（0 = -56 dBFS、1 = -36 dBFS，正常講話約 0.4）
static void vgSmooth(uint32_t now) {
  const float t = micLevel();
  const float dt = min<uint32_t>(100, now - g_vgMs) / 1000.0f;
  g_vgMs = now;
  g_vgFast += (t - g_vgFast) * min(1.0f, dt / (t >= g_vgFast ? VG_RISE_S : VG_FAST_FALL_S));
  if (g_vgFast >= g_vgSlow) { g_vgSlow = g_vgFast; g_vgHoldMs = now; }
  else if (now - g_vgHoldMs >= VG_HOLD_MS) g_vgSlow -= g_vgSlow * min(1.0f, dt / VG_FALL_S);
  if (t == 0 && g_vgFast < 0.01f) g_vgFast = 0;
  if (g_vgSlow < 0.02f) g_vgSlow = 0;
  g_vgLevel = max(g_vgFast, g_vgSlow * VG_TAIL);
}

static int vgTarget() { return g_saver || !g_vgBin ? 0 : (int)(g_vgLevel * VG_STEPS + 0.5f); }

// 這一張要加的光（prepareFrame 時決定，之後的小範圍更新照同一張的光）
static void vgSet() {
  g_vgQ = vgTarget();
  if (g_vgQ && (g_vgQ != g_vgPalQ || g_frame.brand != g_vgPalBrand)) vgPalette(g_vgQ);
}

// g_fb → g_fbRot：加上光（有的話）、轉向（有的話）。16×16 一塊一塊搬，PSRAM 快取才不會一直換
static void composeFrame() {
  const uint16_t* s = g_fb;
  uint16_t* d = g_fbRot;
  const bool vg = g_vgQ;
  if (g_rot == 0) {                       // 只有光：每一列只算圓裡那一段，其他直接複製
    for (int y = 0; y < H; y++) {
      const uint16_t* sr = s + y * W;
      uint16_t* dr = d + y * W;
      const int dy = y - CY, rem = g_vgR2Act - dy * dy;
      if (rem <= 0) { memcpy(dr, sr, W * 2); continue; }
      const int hw = (int)sqrtf((float)rem) + 1, x0 = max(0, CX - hw), x1 = min(W, CX + hw);
      memcpy(dr, sr, x0 * 2);
      // 距離平方逐點遞增；黑底直接查表，有東西的像素才算加法混色（以前每點都算，準備一張 60-107 ms，光暈每秒只更新 6-10 次，看起來很卡）
      const uint16_t* blk = nullptr;
      const int brow = (y & 3) << 2;
      int dx = x0 - CX, r2 = dx * dx + dy * dy;
      for (int x = x0; x < x1; x++, r2 += 2 * dx + 1, dx++) {
        const uint16_t p = sr[x];
        if (r2 >= g_vgR2Act) dr[x] = p;
        else if (p) dr[x] = vgAddPx(p, x, y);
        else { blk = g_vgBlk[g_vgBin[r2 >> 6]]; dr[x] = blk[brow | (x & 3)]; }
      }
      memcpy(dr + x1, sr + x1, (W - x1) * 2);
    }
    return;
  }
  for (int by = 0; by < H; by += 16)
    for (int bx = 0; bx < W; bx += 16) {
      const int ye = by + 16 > H ? H : by + 16, xe = bx + 16 > W ? W : bx + 16;
      // 這一塊離圓心最近的點在光的範圍外：整塊不用算光（光只在中間，大部分的塊都是這樣）
      const int ndx = max(0, max(CX - (xe - 1), bx - CX)), ndy = max(0, max(CY - (ye - 1), by - CY));
      const bool g = vg && ndx * ndx + ndy * ndy < g_vgR2Act;
      for (int y = by; y < ye; y++) {
        const uint16_t* sr = s + y * W;
        if (g_rot == 1) {                                           // 90°（順時針）：(x, y) → (W-1-y, x)
          uint16_t* dc = d + (W - 1 - y);
          for (int x = bx; x < xe; x++) dc[x * W] = g ? vgAddPx(sr[x], x, y) : sr[x];
        } else if (g_rot == 2) {                                    // 180°：(x, y) → (W-1-x, H-1-y)
          uint16_t* dr = d + (H - 1 - y) * W + (W - 1);
          for (int x = bx; x < xe; x++) dr[-x] = g ? vgAddPx(sr[x], x, y) : sr[x];
        } else {                                                    // 270°：(x, y) → (y, H-1-x)
          uint16_t* dc = d + y;
          for (int x = bx; x < xe; x++) dc[(H - 1 - x) * W] = g ? vgAddPx(sr[x], x, y) : sr[x];
        }
      }
    }
}

// 外環換成現在的亮度（畫的時候是十幾格之前），要轉向、加光就做好
static void prepareFrame(uint32_t ms) {
  ringDraw(g_fbSprite, 0, H, ms);
  g_ringShownMs = ms;
  vgSet();
  g_fbOut = g_fb;
  if (g_fbRot && (g_rot || g_vgQ)) { composeFrame(); g_fbOut = g_fbRot; }
  else g_vgQ = 0;                         // 配置不到 g_fbRot：沒有光
}

static void pushFrame() {
  lcd.startWrite();
  if (g_fbOut == g_fbRot && g_fbRot) lcd.pushImage(0, 0, W, H, (const lgfx::swap565_t*)g_fbRot);
  else if (g_rot == 0) lcd.pushImage(0, 0, W, H, (const lgfx::swap565_t*)g_fb);
  else for (int oy = 0; oy < H; oy += BAND_H) pushRows(g_fb + oy * W, oy, oy + BAND_H > H ? H - oy : BAND_H);   // 配置不到轉向緩衝
  lcd.endWrite();
}

#endif

// 把 band（第 OY 列起）送到螢幕
static void pushBand() {
#if BOARD_AMOLED
  if (band.getColorDepth() == 16) { pushRows((const uint16_t*)band.getBuffer(), OY, OY + BAND_H > H ? H - OY : BAND_H); return; }
#endif
  band.pushSprite(0, OY);
}

// 畫一條（OY 那一條，高 BAND_H）。有整張畫面時只畫進去，最後一起送；否則畫好就送到螢幕
static bool g_anyKeyed = false;    // 這一輪有沒有任何一條留空了外環
static void renderStrip(uint32_t ms) {
#if BOARD_AMOLED
  if (g_fb) {
    band.setBuffer(g_fb + OY * W, W, BAND_H, 16);
    drawScene(ms);
    return;
  }
#endif
  g_ringKeyed = false;
  drawScene(ms);
  lcd.startWrite();
  // 外環區域留空（透明），不覆蓋螢幕上正在呼吸的外環 → 不會出現一條一條亮度不同
  if (g_ringKeyed) { band.pushSprite(0, OY, RING_KEY); g_anyKeyed = true; }
  else pushBand();
  lcd.endWrite();
}

static void render() {          // 一次畫完整個畫面（開機用）
  uint32_t ms = millis();
  g_drawSt = g_shownSt = g_frame.st;
  for (OY = 0; OY < H; OY += BAND_H) renderStrip(ms);
  OY = 0;
#if BOARD_AMOLED
  if (g_fb) { prepareFrame(millis()); pushFrame(); return; }
#endif
  lcd.startWrite(); statusRingOn(lcd, 0, millis()); lcd.endWrite();
}

// 換方向。有整張畫面時下一張會整個蓋過去，不用先清成黑的（不會閃一下）
static void setRotation(int r, bool save) {
  if (r == g_rot) return;
  g_rot = r;
  applyRotation();
  if (!g_fb) lcd.fillScreen(TFT_BLACK);
  dirty = true;
  if (save) { Preferences pf; pf.begin("aimon", false); pf.putUChar("rot", g_rot); pf.end(); }   // 記住，下次開機一插電就是這個方向
}

// ---------------- 自動轉向（AMOLED 的陀螺儀）----------------
// 由電腦端設定開關（arot），存在 NVS。開著的時候不理電腦送的 rot；平放時維持最後的方向
static bool g_autoRot = true;
#if !BOARD_AMOLED
static bool imuOk() { return false; }
#else
static bool imuOk() { return g_hasImu; }
static int g_accX = 0, g_accY = 0, g_accZ = 0;   // 最近一次的加速度（回報給電腦端，除錯用）
static int readOrientation() {
  int16_t x, y, z;
  if (!g_hasImu || !imuAccel(x, y, z)) return -1;
  g_accX = x; g_accY = y; g_accZ = z;
  return imuOrientation(x, y);
}
static void imuTick(uint32_t now) {
  static uint32_t last = 0, since = 0;
  static int cand = -1;
  if (!g_hasImu || !g_autoRot || now - last < 100) return;
  last = now;
  const int o = readOrientation();
  if (o < 0 || o == g_rot) { cand = -1; return; }
  if (o != cand) { cand = o; since = now; return; }
  if (now - since >= 800) { setRotation(o, false); cand = -1; }   // 同一個方向維持 0.8 秒才轉（拿起來晃動不會亂轉）
}
#endif

// ---------------- 序列埠：同時聽 USB（Serial）與 UART0（Serial0）----------------
// 不同批次的板子有的走 ESP32-C3 內建 USB，有的經過 CH340 轉 UART0，兩條都收。
struct LineReader {
  Stream* s; String buf;
  explicit LineReader(Stream* st) : s(st) {}
};

static void sendLine(Stream* s, const char* line) {
  if (!s) return;
#if BOARD_AMOLED
  if (g_micOn && s == micStream()) { micWriteLine(line); return; }
#endif
  s->println(line);
}

static Stream* lastSource = nullptr;

#if BOARD_AMOLED
#include "anim.h"   // 自訂動畫（取代 Clawd；要用到上面的 sendLine）
#endif

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
static uint32_t g_tePerUs = 0;
static void IRAM_ATTR onTE() { const uint32_t t = micros(); g_tePerUs = t - g_teUs; g_teUs = t; g_teN++; }
#endif

static uint32_t g_maxPushMs = 0;    // 上次回報之後，最長的一次整張送出（AMOLED，只算 SPI 寫入）
static uint32_t g_maxPrepMs = 0;    // 最長的一次送出前準備（外環、轉向）
static uint32_t g_maxRoamGap = 0;   // 螢幕保護的 Clawd 兩步之間最長隔多久（正常 50 ms，大了就是卡頓）
static uint32_t g_maxGenGap = 0;    // 「Generating」的光兩次更新之間最長隔多久（正常 40 ms，大了就是停頓）
static uint32_t g_fullN = 0;        // 上次回報之後整張重畫了幾次

#if BOARD_AMOLED
// ---------------- 經 WiFi 更新後的新版：確認能用才算數 ----------------
// Arduino 核心預設在 setup() 之前就把新版標成有效（initArduino 看 verifyRollbackLater），新版初始化時當機的話一直重開、不會退回舊版。
// 改成：第一次跟電腦打完招呼（hello，WiFi 要先過配對金鑰）才確認；確認前當機 / 重開 → bootloader 自動開回舊版；
// 開機 2 分鐘還沒有電腦來打招呼 → 自己退回舊版重開（舊版本來就能用，電腦之後可以再更新一次）。
// USB 燒錄的韌體 otadata 是清掉的，不會是「待確認」，不受影響
extern "C" {
bool verifyRollbackLater() { return true; }
}
static bool g_otaPending = false;     // 這次開機的是剛經 WiFi 更新、還沒確認的新版
static int g_appSlot = 0;             // 從哪個 app 區開機（0 = app0、1 = app1），hello 的 app
static void otaBootCheck() {
  const esp_partition_t* p = esp_ota_get_running_partition();
  esp_ota_img_states_t st;
  if (!p) return;
  g_appSlot = p->subtype == ESP_PARTITION_SUBTYPE_APP_OTA_1 ? 1 : 0;
  g_otaPending = esp_ota_get_state_partition(p, &st) == ESP_OK && st == ESP_OTA_IMG_PENDING_VERIFY;
}
static void otaConfirm() {
  if (!g_otaPending) return;
  g_otaPending = false;
  esp_ota_mark_app_valid_cancel_rollback();
}
static void otaRollbackTick(uint32_t now) {
  if (g_otaPending && now > 120000) esp_ota_mark_app_invalid_rollback_and_reboot();
}
#endif

static void sendHello(Stream* src) {
  char b[768];
  // id：晶片 MAC，電腦端用它分辨每塊螢幕（各自的方向、亮度；換 USB 孔也認得）
  const uint64_t mac = ESP.getEfuseMac();
  snprintf(b, sizeof b, "{\"aimon\":2,\"fw\":\"" FW_VERSION "\",\"hw\":\"" BOARD_ID "\",\"id\":\"%04x%08lx\",\"up\":%lu,\"loop\":%lu,\"strip\":%lu,\"bad\":%lu,\"long\":%lu,\"heap\":%lu,\"gen\":%lu,\"full\":%lu}",
           (unsigned)((mac >> 32) & 0xFFFF), (unsigned long)(mac & 0xFFFFFFFFUL),
           (unsigned long)(millis() / 1000), (unsigned long)g_maxLoopMs, (unsigned long)g_maxStripMs,
           (unsigned long)g_badLines, (unsigned long)g_longLines, (unsigned long)ESP.getFreeHeap(),
           (unsigned long)g_maxGenGap, (unsigned long)g_fullN);
#if BOARD_AMOLED
  // tp / imu：有沒有找到觸控、陀螺儀；arot：自動轉向；acc：加速度（8192 = 1 g）；fb：整張畫面緩衝有沒有配置到
  // mic：麥克風模式中；micx：上次在麥克風模式下當機重開，這次開機不再進；mpk / mgap / mms：麥克風送了幾包、兩包最長間隔 ms、錯過幾包
  snprintf(b + strlen(b) - 1, sizeof b - strlen(b) + 1, ",\"te\":%lu,\"ring\":%lu,\"push\":%lu,\"prep\":%lu,\"fb\":%d,\"tp\":%d,\"imu\":%d,\"arot\":%d,\"rot\":%d,\"acc\":[%d,%d,%d],\"roam\":%lu,\"mic\":%d,\"micx\":%d,\"mpk\":%lu,\"mgap\":%lu,\"mms\":%lu,\"mg\":%d,\"vgn\":%lu,\"vgi\":%lu,\"vgt\":%lu,\"mfx\":%lu,\"wf\":1,\"ota\":1,\"app\":%d,\"pv\":%d,\"av\":%lu}",
           (unsigned long)g_teN, (unsigned long)g_maxRingMs, (unsigned long)g_maxPushMs, (unsigned long)g_maxPrepMs, g_fb ? 1 : 0,
           g_hasTouch ? 1 : 0, g_hasImu ? 1 : 0, g_autoRot ? 1 : 0, g_rot, g_accX, g_accY, g_accZ, (unsigned long)g_maxRoamGap,
           g_micOn ? 1 : 0, g_micBlocked ? 1 : 0, (unsigned long)g_uacPkts, (unsigned long)g_uacMaxGap, (unsigned long)g_uacMiss, g_micGain, (unsigned long)g_vgN, (unsigned long)g_vgGap, (unsigned long)g_vgMaxMs,
           (unsigned long)g_maskFixN, g_appSlot, g_otaPending ? 1 : 0, (unsigned long)g_animCrc);
  g_vgN = g_vgGap = g_vgMaxMs = 0;
  g_uacPkts = 0;
  g_uacMiss = 0;
  g_uacMaxGap = 0;
  g_teN = 0;
  g_maxRoamGap = 0;
#endif
  sendLine(src, b);
  g_maxLoopMs = g_maxStripMs = g_maxRingMs = g_maxPushMs = g_maxPrepMs = g_maxGenGap = g_fullN = 0;
}

#if BOARD_AMOLED
// wifi.h（要用到下面的 pollReader、rdUsb，放在後面才 include）
static bool wifiGate(Stream* src, JsonDocument& doc);
static bool wifiCommand(Stream* src, JsonDocument& doc);
static void wifiSendStatus(Stream* s);
#endif

static void handleLine(Stream* src, const String& line) {
  JsonDocument doc;
  if (deserializeJson(doc, line)) { g_badLines++; return; }
#if BOARD_AMOLED
  if (!wifiGate(src, doc)) return;      // WiFi 連線：先送對配對金鑰才收
#endif
  lastSource = src;
  if (doc["hello"].is<int>()) {
    sendHello(src);                      // 先回（pv = 確認前的狀態，電腦端紀錄看得到「pending」）
#if BOARD_AMOLED
    otaConfirm();                        // 經 WiFi 更新後的新版：電腦連得上 = 能用（上面）
#endif
#if BOARD_AMOLED
    wifiSendStatus(src);                 // 設定視窗的 WiFi 分頁用
#endif
    return;
  }
#if BOARD_AMOLED
  if (wifiCommand(src, doc)) return;
#endif
  if (doc["bright"].is<int>()) brightness = doc["bright"].as<int>();
#if BOARD_AMOLED
  if (doc["anim"].is<JsonObject>()) { animBegin(src, doc["anim"].as<JsonObject>()); return; }   // 傳自訂動畫過來
  if (!doc["av"].isNull()) g_animWant = doc["av"].as<uint32_t>();                           // 要顯示哪一份動畫（0 = Clawd）
  // 麥克風（電腦端的開關，跟畫面一起送；也可以單獨一行 {"mic":0}）：1 = 切成 USB 麥克風、0 = 重開回 hwcdc
  if (doc["mg"].is<int>()) micSetGain(doc["mg"].as<int>());   // 麥克風增益（先設，開麥克風時才用得到）
  if (doc["mic"].is<int>()) {
    if (!doc["mic"].as<int>()) micLeave();
    else if (!g_micOn && micEnter()) lastSource = micStream();   // 之後改從新的 COM 埠收送（loop 換掉 rdUsb）
  }
#endif
  if (!doc["st"].is<const char*>()) return;

  Frame f;
  f.brand = brandIndex(doc["ai"] | "none");
  f.st = parseSt(doc["st"] | "idle");
  f.msg = (const char*)(doc["msg"] | "");
  f.note = (const char*)(doc["note"] | "");
  { const char* nk = doc["nk"] | ""; f.noteKind = !strcmp(nk, "reset") ? 2 : (f.note.length() ? 1 : 0); }
  if (doc["p5"].is<float>()) f.p5 = doc["p5"].as<float>();
  if (doc["pw"].is<float>()) f.pw = doc["pw"].as<float>();
  if (doc["red"].is<float>()) { const float r = doc["red"].as<float>(); g_redAt = r > 0 ? r : 1e9f; }   // 變紅門檻；0 = 不變紅（提醒關閉），舊版電腦端不送 = 90
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
  if (doc["saver"].is<int>()) { g_saverMin = doc["saver"].as<int>(); g_saverSec = doc["ss"] | 0; }   // 螢幕保護分鐘數（0 = 關閉）；ss = 秒數（有送就用它，30 秒的選項）
  if (doc["arot"].is<int>()) {                                          // 自動轉向（有陀螺儀的螢幕）
    bool a = doc["arot"].as<int>() != 0;
    if (a != g_autoRot) { g_autoRot = a; Preferences pf; pf.begin("aimon", false); pf.putBool("arot", a); pf.end(); }
  }
  // 螢幕方向 0-3（×90°，由電腦端設定）。自動轉向時由陀螺儀決定
  if (doc["rot"].is<int>() && !(g_autoRot && imuOk())) setRotation(doc["rot"].as<int>() & 3, true);
  if (doc["bri"].is<int>()) {                                           // 亮度（由電腦端設定）
    int b = doc["bri"].as<int>();
    if (b >= 5 && b <= 255) brightness = b;
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
  // 剛讀到額度（之前這家沒有額度）：從 0 捲上去，不要直接跳到數字（gaugeTick 看到 g_p5Shown < 0 會直接跳）
  if (f.p5 >= 0 && (g_frame.p5 < 0 || f.brand != g_frame.brand) && g_p5Shown < 0) g_p5Shown = 0;
  g_frame = f;
  g_lastFrameMs = millis();
  if (!g_everConnected) g_everConnected = true;
  // 電腦每 1-3 秒送一次，每次都整張重畫的話 Generating 的光、Clawd 會跟著頓一下 → 內容變了才重畫。
  // AMOLED 螢幕保護另外由 loop 看 saverKey
  // 只差在其他對話的小圓點（狀態切換時常在幾秒內連送好幾張）：只重畫小圓點那一塊，不整張重畫
  static uint32_t lastKey = 0, lastOthers = 0;
  const uint32_t k = frameKey(f), ko = othersKey(f);
  if (!(BOARD_AMOLED && g_saver)) {
    if (k != lastKey) dirty = true;
    else if (ko != lastOthers) g_othersDirty = true;
  }
  lastKey = k;
  lastOthers = ko;
}

static void pollReader(LineReader& r) {
#if BOARD_AMOLED
  if (g_upSrc == r.s) { animRecv(r.s); if (g_upSrc == r.s) return; }   // 這條連線正在傳動畫：收原始資料
#endif
  while (r.s->available()) {
#if BOARD_AMOLED
    if (g_upSrc == r.s) { animRecv(r.s); if (g_upSrc == r.s) return; continue; }   // 這一行剛開始傳動畫
#endif
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
#if BOARD_AMOLED
#include "wifi.h"
#endif

#if BOARD_AMOLED
// ---------------- 換頁滑動 ----------------
// 舊畫面往滑的方向推出去、新頁從另一邊滑進來，外圈燈號固定不動（每一格把外環重畫在最上面）。
// 都在面板座標做（已轉向的整張），轉向時沿面板的另一軸移動，不用每格重新轉向
static uint16_t* g_fbOld = nullptr;     // 換頁前螢幕上那張
static uint16_t* g_fbSlide = nullptr;   // 滑動中的這一格
static LGFX_Sprite g_slideSprite(&lcd);
static const uint16_t* g_slideNew = nullptr;   // 新頁（g_fb 或 g_fbRot）
static int8_t g_sliding = 0;            // 滑動中：-1 內容往左、1 往右（看到的畫面上）
static uint32_t g_slideT0 = 0;
static const uint32_t SLIDE_MS = 280;

static bool slideReady() { return g_fbOld && g_fbSlide && (g_rot == 0 || g_fbRot); }

// 開始滑動：記下螢幕上的舊畫面，新頁整張畫好、準備好（外環、轉向）
static void startSlide(int dir, uint32_t now) {
  // 螢幕上那張：滑動中就是正在顯示的那一格；否則是上次送出的那張（0° 是 g_fb，可能已經重畫了幾條，只差在時間數字；
  // 轉向或有說話光暈時是 g_fbRot）
  const uint16_t* shown = g_sliding ? g_fbSlide : (g_fbOut ? g_fbOut : g_fb);
  memcpy(g_fbOld, shown, W * H * 2);
  g_drawSt = g_shownSt = g_frame.st;
  for (OY = 0; OY < H; OY += BAND_H) renderStrip(now);
  OY = 0;
  prepareFrame(now);
  g_slideNew = g_fbOut;
  g_sliding = dir;
  g_slideT0 = millis();
}

// 舊畫面移動 a 像素（0 → W），沿看到的畫面的左右方向
static void composeSlide(int a) {
  // 看到的左右 → 面板的哪一軸、哪個方向（跟 touchToView 相反方向的換算）
  const bool alongY = g_rot == 1 || g_rot == 3;
  const int t = (g_rot >= 2 ? -g_sliding : g_sliding) * a;   // 舊畫面在面板上的位移
  const uint16_t *o = g_fbOld, *n = g_slideNew;
  uint16_t* d = g_fbSlide;
  const int A = abs(t);
  if (alongY) {                                               // 整列整列搬
    if (t <= 0) { memcpy(d, o + A * W, (H - A) * W * 2); memcpy(d + (H - A) * W, n, A * W * 2); }
    else { memcpy(d, n + (H - A) * W, A * W * 2); memcpy(d + A * W, o, (H - A) * W * 2); }
  } else {
    for (int y = 0; y < H; y++) {
      uint16_t* r = d + y * W;
      const uint16_t *ro = o + y * W, *rn = n + y * W;
      if (t <= 0) { memcpy(r, ro + A, (W - A) * 2); memcpy(r + W - A, rn, A * 2); }
      else { memcpy(r, rn + W - A, A * 2); memcpy(r + A, ro, (W - A) * 2); }
    }
  }
}

// 滑動的一格：組好、外環蓋上去、等 TE 送出。回傳 false = 滑完了（最後一格就是新頁）
static bool slideTick() {
  const uint32_t el = millis() - g_slideT0;
  const int p = el >= SLIDE_MS ? 1000 : (int)(el * 1000 / SLIDE_MS);
  const int q = 1000 - p;
  const int e = 1000 - q * q / 1000 * q / 1000;               // ease-out（三次方）：一開始快、最後慢慢停
  composeSlide(W * e / 1000);
  const uint32_t ms = millis();
  ringDraw(g_slideSprite, 0, H, ms);
  g_ringShownMs = ms;
  const uint32_t t0 = millis();
  while (g_teUs && micros() - g_teUs > 2000 && micros() - g_teUs < 100000 && millis() - t0 < 20) {}   // 等 TE（最多一格）
  lcd.startWrite();
  lcd.pushImage(0, 0, W, H, (const lgfx::swap565_t*)g_fbSlide);
  lcd.endWrite();
  if (p < 1000) return true;
  g_sliding = 0;
  return false;
}

// ---------------- Clawd 動畫 ----------------
// 把一塊畫面（看到的畫面座標 x / y / 寬 / 高，都是偶數；src 指到這塊的左上角，每列隔 stride）轉向後送到螢幕
static void pushRect(const uint16_t* src, int stride, int x, int y, int w, int h) {
  if (w <= 0 || h <= 0 || w * h > W * BAND_H) return;   // 轉向暫存區只有一條大：超過會寫壞記憶體（曾經因此當機重開）
  uint16_t* d = g_rotBuf;
  int px = x, py = y, pw = w, ph = h;
  const bool vg = g_vgQ;                 // 說話光暈：跟上一張整張送出的光一樣（不然更新的這一塊會是黑底）
  for (int r = 0; r < h; r++)
    for (int c = 0; c < w; c++) {
      uint16_t v = src[r * stride + c];
      if (vg) v = vgAddPx(v, x + c, y + r);
      if (g_rot == 0) d[r * w + c] = v;
      else if (g_rot == 2) d[(h - 1 - r) * w + (w - 1 - c)] = v;
      else if (g_rot == 1) d[c * h + (h - 1 - r)] = v;        // 90°：(x, y) → (W-1-y, x)
      else d[(w - 1 - c) * h + r] = v;                         // 270°：(x, y) → (y, H-1-x)
    }
  if (g_rot == 2) { px = W - x - w; py = H - y - h; }
  else if (g_rot == 1) { px = W - y - h; py = x; pw = h; ph = w; }
  else if (g_rot == 3) { px = y; py = H - x - w; pw = h; ph = w; }
  lcd.startWrite();
  lcd.pushImage(px, py, pw, ph, (const lgfx::swap565_t*)d);
  lcd.endWrite();
}

static void pushFbRect(int x, int y, int w, int h) { pushRect(g_fb + y * W + x, W, x, y, w, h); }

// 說話光暈只變了亮度：只重送中間光照得到的圓（舊的範圍 ∪ 新的範圍，小聲時光會縮小，舊的那圈要蓋回原樣）。
// 以前一段算一段送（22-35 ms，沒對準 TE），送的途中面板掃過去，上面新、下面舊的亮度 → 講話時看得到撕裂（使用者回報）。
// 現在分兩步：先把整塊加光、轉向後寫進 g_fbRot（面板座標，不送），再照掃描線的位置一口氣送出（時機見 vgPushGlow）。
// 外環、其他地方不動，也不用整張準備
static void vgComposeGlow(int r) {        // 看到的畫面上半徑 r 的圓 → g_fbRot（面板座標）
  const int act = g_vgR2Act, r2 = r * r;
  for (int yy = max(0, CY - r); yy < min(H, CY + r + 1); yy++) {
    const int dy = yy - CY, rem = r2 - dy * dy;
    if (rem < 0) continue;
    const int hw = (int)sqrtf((float)rem) + 1, x0 = max(0, CX - hw), x1 = min(W, CX + hw + 1);
    const uint16_t* sr = g_fb + yy * W;
    uint16_t* o;
    int step;
    if (g_rot == 0) { o = g_fbRot + yy * W + x0; step = 1; }
    else if (g_rot == 2) { o = g_fbRot + (H - 1 - yy) * W + (W - 1 - x0); step = -1; }
    else if (g_rot == 1) { o = g_fbRot + x0 * W + (W - 1 - yy); step = W; }   // 90°：(x, y) → (W-1-y, x)
    else { o = g_fbRot + (H - 1 - x0) * W + yy; step = -W; }                  // 270°：(x, y) → (y, H-1-x)
    const int brow = (yy & 3) << 2;
    int dx = x0 - CX, d2 = dx * dx + dy * dy;
    for (int x = x0; x < x1; x++, o += step, d2 += 2 * dx + 1, dx++) {
      const uint16_t p = sr[x];
      if (d2 >= act || !g_vgQ) *o = p;
      else if (p) *o = vgAddPx(p, x, yy);
      else *o = g_vgBlk[g_vgBin[d2 >> 6]][brow | (x & 3)];
    }
  }
}


// 面板座標一小塊（g_fbRot 裡的 x, y, w, h）搬到 g_rotBuf 送出
static void vgPushPanelRect(int x, int y, int w, int h) {
  for (int i = 0; i < h; i++) memcpy(g_rotBuf + i * w, g_fbRot + (y + i) * W + x, w * 2);
  lcd.pushImage(x, y, w, h, (const lgfx::swap565_t*)g_rotBuf);
}

static void vgPushGlow() {
  const int prev = g_vgR2Act;
  vgSet();
  // 熄滅：範圍歸零，調色盤的快取也要作廢。以前只清範圍，之後聲音回到熄滅前同一階時 vgSet 以為算過、不重算，
  // 範圍一直是 0，光就亮不起來（要等音量換到別階；字與字之間淡到 0 時看起來時有時無）
  if (!g_vgQ) { g_vgR2Act = 0; g_vgPalQ = 0xFF; }
  const int r2 = max(prev, g_vgR2Act);
  if (r2 <= 0) return;
  const int r = (int)sqrtf((float)r2) + 2;
  // 算的範圍比送的大一圈：送的是面板座標上一段段的方塊（角會超出圓一點），那些點也要是算過的
  vgComposeGlow(r + 10);
  const int a0 = max(0, (CY - r) & ~1), a1 = min(H, (CY + r + 2) & ~1), n = (a1 - a0 + 7) / 8;
  // 什麼時候開始送（2026-11-10 量到：半徑 98 送 4.6 ms，大約跟面積成正比，最大的圓要 20 ms 左右，比面板掃過這一塊的 15 ms 還慢）。
  // 以前一律從 TE 開始送（搶在掃描線前面）：圓大的時候送到一半被掃描線追過，上面新、下面舊 → 一條切線。
  // 現在跟在掃描線後面送：等掃描線過了這塊的頂端才開始；送得比掃描快的話（圓小）再多等一下，免得追過掃描線。
  // 這樣只要在下一次掃描追上來之前送完就好（一格 16.7 ms + 這塊的掃描時間，夠用）
  static uint32_t rate = 160;                       // 送一個像素幾 µs（× 1024），每次量了更新
  const uint32_t per = (g_tePerUs > 12000 && g_tePerUs < 25000) ? g_tePerUs : 16700;
  const uint32_t rowUs = per / (H + 14);            // 掃一列的時間
  const uint32_t scanUs = (uint32_t)(a1 - a0) * rowUs;
  const uint32_t estUs = (uint32_t)(3.14159f * r * r) * rate >> 10;
  uint32_t start = (uint32_t)a0 * rowUs + (estUs < scanUs ? scanUs - estUs : 0) + 1500;
  if (start > per - 2000) start = per - 2000;
  const uint32_t t0 = millis();
  if (g_teUs) {
    while (millis() - t0 < 40) {
      const uint32_t te = micros() - g_teUs;
      if (te >= 100000 || (te >= start && te < start + 1500)) break;
    }
  }
  const uint32_t tp = micros();
  uint32_t px = 0;
  lcd.startWrite();
  // 每段 8 列，寬度看這段裡離圓心最近的那列。段太高的話兩端的方塊角會碰到外環（半徑 214.5，呼吸中的亮度跟 g_fbRot 裡的不同）
  for (int k = 0; k < n; k++) {
    const int a = a0 + k * 8, h = min(8, a1 - a);
    const int near = (a <= CY && CY < a + h) ? 0 : min(abs(a - CY), abs(a + h - 1 - CY));
    const int rem = r2 - near * near;
    if (rem <= 0) continue;
    const int hw = (int)sqrtf((float)rem) + 2;
    const int b0 = max(0, (CX - hw) & ~1), b1 = min(W, (CX + hw + 2) & ~1);
    const int len = max(34, b1 - b0);     // 太窄（64 位元組以下）會畫壞
    const int bs = min(b0, W - len);
    px += len * h;
    vgPushPanelRect(bs, a, len, h);
  }
  lcd.endWrite();
  if (px > 2000) rate = (rate * 3 + (uint32_t)((uint64_t)(micros() - tp) * 1024 / px)) / 4;
}

// 螢幕保護的 Clawd：在整個圓裡亂跑（走到隨機的點、停一下再走，擋到時鐘也沒關係）。
// 整張畫好時把沒有 Clawd 的畫面存成底圖（g_fbBg），之後每一步在小暫存區（g_roamTmp）組好「底圖 + 新位置的 Clawd」，
// 範圍是舊位置 ∪ 新位置，直接送到螢幕。不碰 g_fb，所以整張重畫（時間更新）的期間牠也照常走，不會卡一下
static uint16_t* g_fbBg = nullptr;
static uint16_t* g_roamTmp = nullptr;      // W × BAND_H
static bool g_roamBg = false;              // 底圖是現在這張螢幕保護畫面
static int g_rx = -1, g_ry = 0;            // Clawd 左上角（看到的畫面座標）
static int g_rtx = 0, g_rty = 0;           // 正走向的點
static int g_rdx = -1, g_rdy = 0;          // 螢幕上那隻畫在哪（-1 = 還沒畫）
static uint32_t g_rPause = 0, g_rLast = 0;
static ClawdPose g_rPose;

static void roamPickTarget() {
  // Clawd 中心離圓心不超過這個距離：整隻都在額度弧（107.5）裡面
  const int u = CLAWD_SU, rmax = SI(100) - 7 * u;
  const float a = random(0, 3600) * 0.1f * DEG_TO_RAD, r = sqrtf(random(0, 1000) / 1000.0f) * rmax;
  g_rtx = CX + (int)(r * cosf(a)) - 6 * u;
  g_rty = CY + (int)(r * sinf(a)) - (7 * u) / 2;
}

// 往目標走一步（約 40 px/秒），走到了停 0.5-3 秒再挑下一個點
static void roamStep(uint32_t now) {
  const int dt = g_rLast ? (int)min<uint32_t>(now - g_rLast, 200) : 0;
  g_rLast = now;
  if (g_rx < 0) { g_rx = CX - 6 * CLAWD_SU; g_ry = CY - SI(70); roamPickTarget(); }
  ClawdPose p;
  if (now < g_rPause) {
    if ((g_rPause - now) % 1500 < 130) p.blink = true;
  } else {
    const int ex = g_rtx - g_rx, ey = g_rty - g_ry;
    const float d = sqrtf((float)(ex * ex + ey * ey)), step = 40.0f * dt / 1000.0f;
    if (d <= step) {
      g_rx = g_rtx; g_ry = g_rty;
      g_rPause = now + random(500, 3000);
      roamPickTarget();
    } else {
      static float fx = 0, fy = 0;                          // 每步不到 1 px 的部分累積起來
      fx += ex * step / d; fy += ey * step / d;
      const int mx = (int)fx, my = (int)fy;
      g_rx += mx; g_ry += my; fx -= mx; fy -= my;
      p.legsUp = (now / 160) % 2 ? 0b0101 : 0b1010;
    }
  }
  g_rPose = p;
}

// 把 Clawd 畫進畫面緩衝（左上角 x, y）
static void roamDrawAt(int x, int y, int ry, int rh) {
  OY = ry;
  band.setBuffer(g_fb + ry * W, W, rh, 16);
  drawClawd(x, y, CLAWD_SU, g_rPose);
  OY = 0;
}

// 整張螢幕保護畫面畫好了（還沒送出）：存成底圖，Clawd 畫上去
static void roamFrameDone() {
  g_roamBg = g_saver && g_fbBg && nowEpoch();
  if (!g_roamBg) { g_rdx = -1; return; }
  memcpy(g_fbBg, g_fb, W * H * 2);
  if (g_rx < 0) roamStep(millis());
  const int m = SI(3), ry = max(0, (g_ry - m) & ~1), rh = min(H - ry, (7 * CLAWD_SU + 2 * m + 2) & ~1);
  roamDrawAt(g_rx, g_ry, ry, rh);
  g_rdx = g_rx; g_rdy = g_ry;
}

// 螢幕保護畫面上會變的東西（分鐘、額度、重置倒數、連線、防烙印位置）：變了才整張重畫
static uint32_t saverKey() {
  uint32_t k = nowEpoch() / 60;
  k = k * 31 + (linkAlive() ? 1 : 0);
  k = k * 31 + g_frame.brand;
  k = k * 31 + (uint32_t)(int)(g_frame.p5 + 0.5f);
  k = k * 31 + g_frame.r5 / 60;
  k = k * 31 + (uint32_t)((g_shX + 16) * 64 + g_shY + 16);
  return k;
}

static void roamTick(uint32_t now) {
  if (!g_roamBg || !g_saver || !g_roamTmp) return;
  { static uint32_t prev = 0; if (prev && now - prev < 5000 && now - prev > g_maxRoamGap) g_maxRoamGap = now - prev; prev = now; }
  roamStep(now);
  // 要更新的範圍：舊位置 ∪ 新位置（加上防烙印的挪動、偶數對齊）
  const int m = SI(3), cw = 12 * CLAWD_SU, ch = 7 * CLAWD_SU;
  const int ox = g_rdx < 0 ? g_rx : g_rdx, oy = g_rdx < 0 ? g_ry : g_rdy;
  int x0 = max(0, (min(ox, g_rx) - m) & ~1), y0 = max(0, (min(oy, g_ry) - m) & ~1);
  int x1 = min(W, (max(ox, g_rx) + cw + m + 1) & ~1), y1 = min(H, (max(oy, g_ry) + ch + m + 1) & ~1);
  if ((x1 - x0) * (y1 - y0) > W * BAND_H) { x0 = max(0, (g_rx - m) & ~1); y0 = max(0, (g_ry - m) & ~1); x1 = min(W, (g_rx + cw + m + 1) & ~1); y1 = min(H, (g_ry + ch + m + 1) & ~1); }
  const int rw = x1 - x0, rh = y1 - y0;
  for (int r = 0; r < rh; r++) memcpy(g_roamTmp + r * rw, g_fbBg + (y0 + r) * W + x0, rw * 2);   // 底圖
  OY = y0;
  band.setBuffer(g_roamTmp, rw, rh, 16);
  drawClawd(g_rx - x0, g_ry, CLAWD_SU, g_rPose);
  OY = 0;
  pushRect(g_roamTmp, rw, x0, y0, rw, rh);
  g_rdx = g_rx; g_rdy = g_ry;
}

// 每 50 ms 重畫 Clawd 那一塊（畫面緩衝裡也更新，整張送出時一致）
static void clawdTick(uint32_t now) {
  static uint32_t last = 0;
  int bx, by, bw, bh;
  if (!g_fb || now - last < 50) return;
  if (g_saver) { last = now; roamTick(now); return; }
  if (animOn() && now / g_animMs == last / g_animMs) return;   // 自訂動畫：換格時才重畫
  if (!clawdBox(bx, by, bw, bh) || bw * bh > W * BAND_H) return;
  last = now;
  if (animOn()) {   // 自訂動畫：比 Clawd 的範圍高（往上伸進圓頂），只重畫它那一塊、不先塗黑（四個角在額度環上）
    const int ax = (CX - ANIM_W / 2 + g_shX) & ~1, ay = (ANIM_Y + g_shY) & ~1, aw = ANIM_W + 2, ah = (ANIM_H + 2) & ~1;
    OY = ay;
    band.setBuffer(g_fb + ay * W, W, ah, 16);
    animDraw(now);
    OY = 0;
    pushFbRect(ax, ay, aw, ah);
    return;
  }
  OY = by;
  band.setBuffer(g_fb + by * W, W, bh, 16);
  band.fillRect(bx, 0, bw, bh, C_BG);
  clawdDraw(now);
  OY = 0;
  pushFbRect(bx, by, bw, bh);
}
#endif

// 只重畫畫面上的一塊（範圍是偶數）。每次最多畫 BAND_H 列（外環、額度環的查表一次只畫一條的高度）：
// AMOLED 畫進畫面緩衝（整張送出時一致），每畫好一段就送那一段（轉向暫存區 g_rotBuf 只有一條大，整塊一次送會寫爆它、當機重開）；
// 1.28 吋借用分條的 sprite（沒在畫那一條時才呼叫），外環區域是透明色（RING_KEY），送出時跳過、不蓋掉螢幕上的外環
// （不用樣板：Arduino 自動產生的函式原型會把樣板弄壞）
static void redrawRect(int bx, int by, int bw, int bh, void (*draw)()) {
#if BOARD_AMOLED
  if (!g_fb) return;
#endif
  for (int y0 = by; y0 < by + bh; y0 += BAND_H) {
    const int rh = min(BAND_H, by + bh - y0);
    OY = y0;
#if BOARD_AMOLED
    band.setBuffer(g_fb + y0 * W, W, rh, 16);
#endif
    band.fillRect(bx, 0, bw, rh, C_BG);
    draw();
    OY = 0;
#if BOARD_AMOLED
    if (!g_rectFbOnly) pushFbRect(bx, y0, bw, rh);
#else
    lcd.startWrite();
    lcd.setClipRect(bx, y0, bw, rh);
    band.pushSprite(&lcd, 0, y0, RING_KEY);
    lcd.clearClipRect();
    lcd.endWrite();
#endif
  }
}

// 「Generating」的光掃過：每 40 ms 只重畫那一列（不整張重畫）。
// 整張重畫期間（每條之間）也照常更新，整張送出前再用現在的時間畫一次（force），光才不會停住或往回跳
static int g_genY = 0;
static uint32_t g_genMs = 0;
static void genDraw() { statusLine(S_WORKING, g_genY, g_genMs); }
static void genTick(uint32_t now, bool force) {
  static uint32_t last = 0;
  int bx, by, bw, bh;
  if ((force ? g_drawSt : g_shownSt) != S_WORKING) return;   // 整張送出前（force）看要送的那張，平常看螢幕上那張
  if ((!force && now - last < 40) || !genBox(bx, by, bw, bh, g_genY)) return;
  if (last && now - last < 1000 && now - last > g_maxGenGap) g_maxGenGap = now - last;   // 剛開始生成（上次是很久以前）不算
  last = now;
  g_genMs = now;
  redrawRect(bx, by, bw, bh, genDraw);
}

// 額度環的漸進動畫：電腦送來的 5 小時額度變了，畫面上的值（g_p5Shown）在 GAUGE_ANIM_MS 內跑到新值（先快後慢），
// 每一步只重畫環上變化的那一小段（整個畫面重畫一條的方式）與數字。看不到時（其他頁、螢幕保護）直接跳到新值
static uint32_t g_sceneMs = 0;
static void sceneDraw() { drawScene(g_sceneMs); }
static const uint32_t GAUGE_ANIM_MS = 3000;   // 額度變動的動畫多久（使用者覺得 1 秒太快、一閃就沒了；2 秒後又試 3 秒）
static void gaugeTick(uint32_t now) {
  static uint32_t last = 0;
  if (now - last < 33) return;
  last = now;
  // 途中目標變了才從現在的位置重新開始；只有小數變（44.6 → 44.7，顯示的整數一樣）就不重新計時
  static float from = -1, to = -1;
  static uint32_t t0 = 0;
  const float target = g_frame.p5;
  // 沒有額度：動畫的目標也要清掉。以前只清 g_p5Shown，40% → 沒資料 → 40% 時目標還是 40、時間早就跑完，
  // 讀回來第一格就直接跳到 40（應該從 0 捲上去）
  if (target < 0) { g_p5Shown = -1; to = -1; return; }
  const float old = g_p5Shown;
  if (old < 0 || g_saver || g_page != 0 || !linkAlive()) { g_p5Shown = to = target; return; }   // 看不到：直接跳過去（目標也記下，之後變回原值才會重新計時）
  if (old == target) return;                                // 已經停在目標上
  // （以前差不到 0.05 就直接設成目標、卻沒重畫，數字停在捲了 95% 的地方，要等下次整張重畫才合上去）
  // 固定 GAUGE_ANIM_MS 跑完，差多少都一樣慢。曲線兩頭稍慢、中間均速（ease-in-out sine）：
  // 原本用 ease-out cubic，最後 30% 的時間只走 3%，數字捲到快到的地方看起來卡住。
  if (target != to) {
    if (to < 0 || (int)floorf(target + 0.5f) != (int)floorf(to + 0.5f)) { from = old; t0 = now; }
    else if (now - t0 >= GAUGE_ANIM_MS) from = old;             // 已經停了：小數直接補上
    to = target;
  }
  const float t = min(1.0f, (now - t0) / (float)GAUGE_ANIM_MS), e = 0.5f - 0.5f * cosf(t * PI);
  g_p5Shown = t >= 1 ? to : from + (to - from) * e;
  // 紅色看目標值（ui.h），捲動途中不換色；門檻跨過時由 frameKey 整張重畫（切換 AI 時跟著淡入）。以前捲過 90 才整張重畫成紅色，切換時看得到跳色、撕裂
  int bx, by, bw, bh;
  g_sceneMs = now;
  gaugeArcBox(old, g_p5Shown, bx, by, bw, bh);
  redrawRect(bx, by, bw, bh, sceneDraw);
  pctBox(bx, by, bw, bh);                                   // 數字像里程表一樣捲：每一步都重畫
  redrawRect(bx, by, bw, bh, pctDraw);
}

// 其他對話的小圓點變了：只重畫那一塊（不在整張重畫中才呼叫；整張重畫途中變的，畫完後再補一次也沒關係）
static void othersTick(uint32_t now) {
  if (!g_othersDirty) return;
  g_othersDirty = false;
  int bx, by, bw, bh;
  if (dirty || !othersBox(bx, by, bw, bh)) return;          // 馬上要整張重畫 / 看不到
  g_sceneMs = now;
  redrawRect(bx, by, bw, bh, sceneDraw);
}

// ---------------- BOOT 鍵、觸控 ----------------
// BOOT 鍵：短按 = 下一頁，長按 = 標為已讀。
// AMOLED 觸控：左右滑 = 換頁（往左滑下一頁、往右滑上一頁），按住 = 標為已讀，點一下只叫醒螢幕保護
static uint32_t g_pageMs = 0;
static int8_t g_slideDir = 0;      // AMOLED：這次換頁要滑動（-1 內容往左 = 下一頁，1 往右 = 上一頁）；0 = 淡出淡入

static void markRead() {
  sendLine(lastSource ? lastSource : &Serial, "{\"ev\":\"btn\"}");
  if (g_frame.st == S_DONE || g_frame.st == S_ERROR) { g_frame.st = S_IDLE; g_frame.msg = ""; }
  g_page = 0;
  dirty = true;
}

static void turnPage(int dir, uint32_t now) {   // dir：-1 下一頁、1 上一頁
  g_page = (g_page + (dir < 0 ? 1 : N_PAGES - 1)) % N_PAGES;
  g_pageMs = now;
  g_slideDir = dir;
  dirty = true;
}

struct Press { bool was = false, longDone = false, wokeSaver = false; uint32_t downAt = 0; };

static void pressTick(Press& p, bool down, uint32_t now) {
  if (down && !p.was) {
    p.downAt = now; p.longDone = false;
    p.wokeSaver = g_saver;                                // 螢幕保護中：這一下只負責叫醒
    g_lastActiveMs = now;
  }
  if (down && !p.longDone && now - p.downAt > 700) {      // 長按：標為已讀
    markRead();
    p.longDone = true;
  }
  if (!down && p.was && !p.longDone && now - p.downAt > 30 && !p.wokeSaver) turnPage(-1, now);   // 短按：下一頁
  p.was = down;
}

#if BOARD_AMOLED
// 觸控的位移（面板座標）換成看到的畫面上的左右 / 上下（轉向時面板座標跟畫面不同）
static void touchToView(int px, int py, int& vx, int& vy) {
  switch (g_rot) {
    case 1: vx = py; vy = -px; break;     // 90°：面板 (W-1-y, x)
    case 2: vx = -px; vy = -py; break;
    case 3: vx = -py; vy = px; break;     // 270°：面板 (y, H-1-x)
    default: vx = px; vy = py;
  }
}

static void pollTouch(uint32_t now) {
  // 每 20 ms 讀一次；連續 3 次沒讀到才算放開（手指滑動時偶爾會漏一筆）
  static uint32_t lastTp = 0, downAt = 0;
  static bool down = false, moved = false, longDone = false, woke = false;
  static uint8_t miss = 0;
  static int x0, y0, x1, y1;
  if (!g_hasTouch) return;
  if (now - lastTp >= 20) {
    lastTp = now;
    int x, y;
    if (touchRead(x, y)) {
      miss = 0;
      if (!down) {
        down = true; moved = longDone = false; downAt = now;
        x0 = x1 = x; y0 = y1 = y;
        woke = g_saver;                                   // 螢幕保護中：這一下只負責叫醒
        g_lastActiveMs = now;
      } else {
        x1 = x; y1 = y;
        if (abs(x1 - x0) + abs(y1 - y0) > SI(12)) moved = true;
      }
    } else if (down && ++miss >= 3) {
      down = false;
      // 放開：主要是左右移動、夠遠、1 秒內 = 滑動換頁
      int vx, vy;
      touchToView(x1 - x0, y1 - y0, vx, vy);
      if (!longDone && !woke && now - downAt < 1000 && abs(vx) > SI(30) && abs(vx) > abs(vy) * 3 / 2)
        turnPage(vx < 0 ? -1 : 1, now);
    }
  }
  if (down && !moved && !longDone && !woke && now - downAt > 700) { markRead(); longDone = true; }   // 按住不動：標為已讀
}
#endif

static void pollButton() {
  static Press btn;
  uint32_t now = millis();
  pressTick(btn, digitalRead(PIN_BTN) == LOW, now);
#if BOARD_AMOLED
  pollTouch(now);
#endif
  if (g_page != 0 && now - g_pageMs > 20000) { g_page = 0; g_slideDir = 0; dirty = true; }   // 20 秒沒操作回到狀態頁（淡出淡入）
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
#if BOARD_AMOLED
  otaBootCheck();
  micBootCheck();   // 從麥克風模式重開：先把 USB 交回 hwcdc
#endif
  // 一行畫面資料約 400-600 位元組，預設接收緩衝只有 256：畫一條畫面的期間沒人讀，滿了後面就被丟掉 → 那行解析失敗
  Serial.setRxBufferSize(BOARD_AMOLED ? 24576 : 2048);   // AMOLED：自訂動畫一次傳 16 KB（anim.h），緩衝滿了 hwcdc 會直接丟資料
#if BOARD_AMOLED
  Serial.setTxBufferSize(1536);   // hello 一行 400 多位元組，快超過預設的緩衝（超過的後半截會被丟掉）
#endif
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
  { Preferences pf; pf.begin("aimon", true); g_rot = pf.getUChar("rot", 0) & 3; g_autoRot = pf.getBool("arot", true); pf.end(); }
#if BOARD_AMOLED
  sensorsInit();
  if (g_autoRot) { int o = readOrientation(); if (o >= 0) g_rot = o; }   // 一開機就是擺放的方向
#endif
  applyRotation();
  lcd.fillScreen(TFT_BLACK);
  showBright();
  loadFonts();
  // 外環直接畫到螢幕，sprite 裡留空（透明色）。AMOLED 不行：透明色切出來的每一段要偶數對齊，
  // 不對齊的整段會被丟掉 → 外環也畫進 sprite（每條重畫只要幾 ms，跟呼吸燈差一格看不出來）
  g_ringDirect = !BOARD_AMOLED;
  g_ringLive = BOARD_AMOLED;
  ringInit();
  gaugeInit();
#if BOARD_AMOLED
  // 整張畫面放 PSRAM（466×480×2 ≈ 447 KB）：一條一條畫進去，全部畫好才一次送出 → 換頁、數字更新不會一條一條出現
  g_fb = (uint16_t*)heap_caps_calloc(W * FB_ROWS, 2, MALLOC_CAP_SPIRAM);
  if (g_fb) {
    g_fbSprite.setBuffer(g_fb, W, H, 16); band.setBuffer(g_fb, W, BAND_H, 16);
    g_fbRot = (uint16_t*)heap_caps_malloc(W * H * 2, MALLOC_CAP_SPIRAM);
    // 換頁滑動：舊畫面、滑動中的一格（各 434 KB）
    g_fbOld = (uint16_t*)heap_caps_malloc(W * H * 2, MALLOC_CAP_SPIRAM);
    g_fbSlide = (uint16_t*)heap_caps_malloc(W * H * 2, MALLOC_CAP_SPIRAM);
    g_fbBg = (uint16_t*)heap_caps_malloc(W * H * 2, MALLOC_CAP_SPIRAM);   // 螢幕保護 Clawd 亂跑用的底圖
    g_roamTmp = (uint16_t*)heap_caps_malloc(W * BAND_H * 2, MALLOC_CAP_SPIRAM);
    if (g_fbSlide) g_slideSprite.setBuffer(g_fbSlide, W, H, 16);
    vgInit();
    animLoad();   // 上次存的自訂動畫
  }
#endif
  if (!g_fb) {
    band.setColorDepth(16);
    if (!band.createSprite(W, BAND_H)) { band.setColorDepth(8); band.createSprite(W, BAND_H); }
  }
  render();
#if BOARD_AMOLED
  wifiInit();       // 畫面先出來再開 WiFi（開 WiFi 要 100-300 ms）
#endif
}

void loop() {
  const uint32_t loopStart = millis();
#if BOARD_AMOLED
  if (g_micOn && rdUsb.s != micStream()) rdUsb.s = micStream();
  micTick(loopStart);
  if (rdUsb.s->available()) g_usbRxMs = loopStart;   // USB 有電腦在（接充電器時不要寫 Serial）
  wifiTick(loopStart);
  animTimeout(loopStart);
  otaRollbackTick(loopStart);
#endif
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

  // 螢幕保護：進入時調暗背光，離開時恢復（亮度在下面的 showBright / fadeTick 套用）
  bool want = saverWanted();
  if (want != g_saver) {
    g_saver = want;
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
  static bool stripSinceRing = false;   // 上一格外環之後畫過一條了
#if BOARD_AMOLED
  imuTick(now);
  // 換頁、轉向、進出螢幕保護、切換 AI：開始淡出，整個重畫（重畫到一半的那張可能還是舊畫面）。
  // 切換 AI 以前直接整張送，看得到撕裂（快速切換視窗時）→ 也淡出淡入。
  // 淡入的同時從上一家的額度捲到這一家的（使用者喜歡這個效果；淡出時不走，淡入一開始就走）
  {
    static int lastPage = -1, lastRot = 0, lastBrand = 0;
    static bool lastSaver = false;
    if (lastPage < 0) { lastPage = g_page; lastRot = g_rot; lastSaver = g_saver; lastBrand = g_frame.brand; }
    const bool brandChanged = g_frame.brand != lastBrand && !g_saver;
    lastBrand = g_frame.brand;
    if (g_page != lastPage || g_rot != lastRot || g_saver != lastSaver || brandChanged) {
      if (g_fb) {
        if (g_slideDir && g_page != lastPage && g_rot == lastRot && g_saver == lastSaver && g_fadeDir == 0 && slideReady()) {
          // 滑動 / BOOT 鍵換頁：新頁整張畫好，從滑的方向滑進來
          strip = -1; g_framePending = g_frameReady = false; dirty = false; lastFull = now;
          startSlide(g_slideDir, now);
        } else {
          if (g_fadeDir >= 0) g_fadeOutBright = lastSaver ? SAVER_BRIGHT : brightness;
          g_fadeDir = -1;
          g_sliding = 0;
          strip = -1; g_framePending = g_frameReady = false; dirty = true;
        }
      }
      g_slideDir = 0;
      lastPage = g_page; lastRot = g_rot; lastSaver = g_saver;
    }
  }
  if (g_sliding) {
    if (!slideTick()) { lastRing = millis(); stripSinceRing = false; }
    if (millis() - loopStart > g_maxLoopMs) g_maxLoopMs = millis() - loopStart;
    delay(1);
    return;
  }
  fadeTick(now);
  // 說話光暈：音量變了就整張送一次（不用重畫，只在送出前加光），最多每 33 ms 一次。
  // 整張重畫中、轉場中先不送（重畫到一半的那張不能送，送完那張時自然會帶上光）
  vgSmooth(now);
  // 對著麥克風講話也算「有動靜」：螢幕保護中一講話就離開（跟碰螢幕一樣走淡出淡入），講話期間不會進螢幕保護；
  // 安靜了照設定的分鐘數再回去。看慢的那條包絡（記住剛才的音量，約 -51 dBFS 以上），環境噪音、很短的雜音不會觸發
  if (g_vgSlow > VG_WAKE) g_lastActiveMs = now;
  if (g_fb && g_fbRot && !g_framePending && strip < 0 && g_fadeDir == 0 && vgTarget() != g_vgQ && now - g_vgPushMs >= 33) {
    if (g_saver) { g_framePending = true; g_frameReady = false; }   // 螢幕保護（光要關掉）：整張送，Clawd 亂跑另有底圖
    else { const uint32_t t0 = millis(); vgPushGlow(); if (millis() - t0 > g_vgMaxMs) g_vgMaxMs = millis() - t0; }
    if (now - g_vgLastPush < 500 && now - g_vgLastPush > g_vgGap) g_vgGap = now - g_vgLastPush;
    g_vgLastPush = now;
    g_vgN++;
    g_vgPushMs = now;
  }
  // 整張畫好了：先準備好（外環、轉向），在 TE 之後送出（外環也在裡面，這一格外環就不另外畫）。沒有 TE 訊號就直接送。
  // 轉場中：等全暗了才送（看不到，不用等 TE），送完開始淡入
  if (g_framePending) {
    if (g_saver && g_fadeDir == 0) clawdTick(now);   // 等著送出時 Clawd 也照常走
    if (!g_frameReady) {
      const uint32_t t0 = millis();
      if (g_fadeDir == 0) {                 // 這張開始重畫時的光、額度位置已經舊了（只畫進 g_fb，馬上整張送）
        g_rectFbOnly = true;
        genTick(t0, true); gaugeTick(t0);
        g_rectFbOnly = false;
      }
      prepareFrame(t0);
      g_frameReady = true;
      if (millis() - t0 > g_maxPrepMs) g_maxPrepMs = millis() - t0;
    }
    const uint32_t te = micros() - g_teUs;
    const bool teOk = g_teUs && te < 100000;
    if (g_fadeDir < 0 ? g_fadeLv == 0 : (!teOk || te < 2000)) {
      const uint32_t t0 = millis();
      pushFrame();
      g_shownSt = g_drawSt;
      if (g_fadeDir < 0) g_fadeDir = 1;
      g_framePending = g_frameReady = false;
      lastRing = millis();
      stripSinceRing = false;
      if (millis() - t0 > g_maxPushMs) g_maxPushMs = millis() - t0;
    }
    if (millis() - loopStart > g_maxLoopMs) g_maxLoopMs = millis() - loopStart;
    delay(1);
    return;
  }
#else
  showBright();
#endif
  bool periodic = now - lastFull >= (animating() ? 1000u : 5000u);
  if (periodic && !g_saver && g_page == 0) {   // 狀態頁：倒數、日期到分鐘才變
    static uint32_t lastView = 0;
    const uint32_t k = viewKey();
    periodic = k != lastView;
    if (periodic) lastView = k;
    else lastFull = now;
  }
#if BOARD_AMOLED
  if (g_saver && periodic) {                // 螢幕保護：畫面內容變了才整張重畫（時間、倒數都到分鐘）
    static uint32_t lastKey = 0;
    const uint32_t k = saverKey();
    periodic = k != lastKey;
    if (periodic) lastKey = k;
    else lastFull = now;
  }
  if (g_saver && g_fadeDir == 0) clawdTick(now);   // 螢幕保護的 Clawd：重畫中也照常走
#endif
  if (strip < 0 && (dirty || periodic)) {
    strip = 0; stripMs = now; lastFull = now; dirty = false; g_anyKeyed = false;
    g_fullN++;
    g_drawSt = g_frame.st;
  }
  // 淡出中不畫外環：新畫面一條接一條畫完，早點送出
  const int ringState = g_fadeDir < 0 ? 0 : ringFrameState(now, lastRing, strip >= 0 && !stripSinceRing);
  if (ringState == 1) {
    lcd.startWrite();
    statusRingOn(lcd, 0, now);
    lcd.endWrite();
    lastRing = now;
    stripSinceRing = false;
    if (millis() - now > g_maxRingMs) g_maxRingMs = millis() - now;
  }
  // 沒在重畫、轉場時才動 Clawd、Generating 的光、經過時間。外環在呼吸時（有 TE）幾乎每一刻不是畫外環就是在等 TE（ringState 2），
  // 只看 0 的話這些永遠排不到（光停住不動）→ 等 TE 的空檔也做（各自限制頻率，一次只重畫一小塊）
  else if ((ringState == 0 || ringState == 2) && strip < 0 && g_fadeDir >= 0) {   // 淡入中（新畫面已經送出，只是還在變亮）也照常走
#if BOARD_AMOLED
    clawdTick(now);
#endif
    genTick(now, false);
    gaugeTick(now);
    othersTick(now);
  }
  // 整張重畫中、等 TE 畫外環的空檔：光照常走（不然只在每條之後才有機會，外環 + 一條要 30-60 ms，光一頓一頓的）
  else if (ringState == 2 && strip >= 0 && g_fadeDir == 0) genTick(now, false);
  else if (ringState == 0 && strip >= 0) {
    OY = strip * BAND_H;
    const uint32_t t0 = millis();
    renderStrip(stripMs);
    stripSinceRing = true;
    if (g_fadeDir == 0) { genTick(millis(), false); gaugeTick(millis()); }   // 重畫中光、額度也照常走（不然完成 / 待命時整張重畫，動畫會停一下）
    if (millis() - t0 > g_maxStripMs) g_maxStripMs = millis() - t0;
    OY = 0;
    if (++strip * BAND_H >= H) {
      strip = -1;
#if BOARD_AMOLED
      if (g_fb) { g_framePending = true; roamFrameDone(); }
#endif
      if (!g_fb) g_shownSt = g_drawSt;
      if (g_anyKeyed && !ringAnimating()) { lcd.startWrite(); statusRingOn(lcd, 0, millis()); lcd.endWrite(); }   // 靜態外環（完成、待命）
    }
  }
  if (millis() - loopStart > g_maxLoopMs) g_maxLoopMs = millis() - loopStart;
  delay(1);
}
