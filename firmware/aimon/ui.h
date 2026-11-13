#pragma once
// 畫面繪製（韌體與電腦預覽程式共用）
// 使用前需先定義：LGFX_Sprite band; String; millis()
//
// Apple Watch 活動圓環風格：
//   外環 = 狀態燈（生成中呼吸、完成恆亮、需審核快呼吸、錯誤閃爍）
//   內環 = 5 小時額度（有資料時）
//   中央 = AI 名稱、大數字、狀態

#ifndef LCD_W
#define LCD_W 240                  // 螢幕寬高（圓形，正方形畫面）
#endif
#ifndef LCD_EVEN
#define LCD_EVEN 0                 // 1 = 直接寫到螢幕的範圍 x、y、寬、高都要是偶數（CO5300：不對齊會亂掉）
#endif
#if LCD_W == 466
#include "fonts466.h"
#else
#include "fonts.h"
#endif
#include "logos.h"                 // 三家官方 LOGO 的小圖（firmware/tools/logogen.py 產生）
#ifndef SAVER_MIN
#define SAVER_MIN 5
#endif

static const int W = LCD_W, H = LCD_W, CX = W / 2, CY = H / 2, BAND_H = 40;   // 分條畫（每條很快，中間穿插呼吸燈）
static int OY = 0;                 // W×BAND_H 緩衝一條一條畫，目前在畫哪一條

// 版面照 240×240 設計，其他尺寸等比例放大：S() / SI() = 設計座標 → 像素
static constexpr float UI_K = LCD_W / 240.0f;
static constexpr float S(float v) { return v * UI_K; }
static constexpr int SI(float v) { return (int)(v * UI_K + (v < 0 ? -0.5f : 0.5f)); }

// ---------------- Apple 深色模式系統色 ----------------
static const uint32_t C_BG      = 0x000000;
static const uint32_t C_LABEL   = 0xFFFFFF;
static const uint32_t C_LABEL2  = 0x98989F;   // secondary label
static const uint32_t C_LABEL3  = 0x5A5A5F;   // tertiary label
static const uint32_t C_GREEN   = 0x30D158;
static const uint32_t C_YELLOW  = 0xFFD60A;
static const uint32_t C_RED     = 0xFF453A;
static const uint32_t C_BLUE    = 0x0A84FF;
static const uint32_t C_GRAY    = 0x8E8E93;

static uint32_t mix(uint32_t a, uint32_t b, float t) {      // a→b，t=0..1
  if (t <= 0) return a;
  if (t >= 1) return b;
  int ar = (a >> 16) & 0xFF, ag = (a >> 8) & 0xFF, ab = a & 0xFF;
  int br = (b >> 16) & 0xFF, bg = (b >> 8) & 0xFF, bb = b & 0xFF;
  return ((uint32_t)(ar + (br - ar) * t) << 16) | ((uint32_t)(ag + (bg - ag) * t) << 8) | (uint32_t)(ab + (bb - ab) * t);
}

// ---------------- 抗鋸齒字型（Inter）----------------
struct SmoothFont {
  lgfx::PointerWrapper data;
  lgfx::VLWfont font;
  bool ok = false;
  void load(const uint8_t* p) { data.set(p); ok = font.loadFont(&data); }
};
static SmoothFont fNum, fClock, fUnit, fName, fLabel, fSub, fTiny;
static void loadFonts() {
  fNum.load(F_NUM); fClock.load(F_CLOCK); fUnit.load(F_UNIT); fName.load(F_NAME);
  fLabel.load(F_LABEL); fSub.load(F_SUB); fTiny.load(F_TINY);
}

// ---------------- State（由電腦端每次送完整畫面）----------------
enum St : uint8_t { S_IDLE = 0, S_DONE = 1, S_WORKING = 2, S_REVIEW = 3, S_ERROR = 4 };
static const char* ST_KEY[]   = {"idle", "done", "working", "review", "error"};
static const char* ST_TITLE[] = {"Idle", "Done", "Generating", "Needs Review", "Error"};

struct Brand { const char* key; const char* name; const char* letter; uint32_t color; };
static const Brand BRANDS[] = {
  {"claude",  "Claude",  "C", 0xF0855A},
  {"chatgpt", "ChatGPT", "O", 0xE5E5EA},
  {"gemini",  "Gemini",  "G", 0x64A8FF},
  {"none",    "",        "",  0x8E8E93},
};
static const int N_BRANDS = 4;

static int brandIndex(const char* key) {
  for (int i = 0; i < N_BRANDS; i++) if (key && !strcmp(key, BRANDS[i].key)) return i;
  return N_BRANDS - 1;
}
static St parseSt(const char* s) {
  for (int i = 0; i < 5; i++) if (s && !strcmp(s, ST_KEY[i])) return (St)i;
  return S_IDLE;
}

struct Other { int brand; St st; };
struct Frame {
  int brand = N_BRANDS - 1;
  St st = S_IDLE;
  String msg;
  String note; uint8_t noteKind = 0;      // 額度重置提醒：1 = 即將重置（黃）、2 = 已重置（綠）
  float p5 = -1, pw = -1;
  uint32_t r5 = 0, rw = 0;
  Other others[5]; int nOthers = 0;
  struct Today { int brand; int count; uint32_t secs; } today[3]; int nToday = 0;   // 今日統計
};

static Frame g_frame;
static float g_redAt = 90;             // 額度幾 % 以上變紅：跟著電腦端「額度重置提醒」的門檻（frame 的 red，提醒關閉時電腦送 0 = 不變紅）
static uint32_t g_lastFrameMs = 0;
static bool g_everConnected = false;
static const char* g_netHint = nullptr;   // 沒連上電腦時最下面那行（AMOLED 的 WiFi 狀態，wifi.h）；nullptr = PLUG IN USB
static int g_otaPct = -1;                 // 經 WiFi 更新韌體中（wifi.h wifiOta）：畫面只顯示進度；-1 = 沒在更新
static int32_t g_epochOffset = 0;
static const uint32_t LINK_TIMEOUT_MS = 20000;
static uint32_t g_stateSinceMs = 0;
static uint32_t g_stateSinceBase = 0;
static uint32_t g_stChangedMs = 0;
static int32_t g_tz = 8 * 3600;        // 時區（秒），由電腦端提供；預設台灣
static uint32_t g_lastActiveMs = 0;    // 最後一次「有事」的時間（螢幕保護用）
static bool g_saver = false;
static int g_saverMin = SAVER_MIN;     // 0 = 關閉；可由電腦端設定
static int g_saverSec = 0;             // > 0：用秒數（電腦端的 30 秒選項），不看分鐘數
static int g_page = 0;                 // 0 狀態、1 額度詳細、2 今日統計、3 時鐘（短按 BOOT 切換）
static const int N_PAGES = 4;

static bool linkAlive() { return g_lastFrameMs != 0 && millis() - g_lastFrameMs < LINK_TIMEOUT_MS; }
static uint32_t nowEpoch() { return g_epochOffset ? (uint32_t)(g_epochOffset + millis() / 1000) : 0; }

static String fmtLeft(uint32_t resetEpoch) {
  uint32_t now = nowEpoch();
  if (!resetEpoch || !now) return "";
  long s = (long)resetEpoch - (long)now;
  if (s <= 0) return "";
  char buf[16];
  if (s >= 86400) snprintf(buf, sizeof buf, "%ldD %ldH", s / 86400, (s % 86400) / 3600);
  else if (s >= 3600) snprintf(buf, sizeof buf, "%ldH %02ldM", s / 3600, (s % 3600) / 60);
  else snprintf(buf, sizeof buf, "%ldM", (s + 59) / 60);
  return String(buf);
}

// ---------------- 繪圖工具（y 自動扣掉目前半畫面位移）----------------
static inline float px(float r, float deg) { return CX + r * cosf(deg * DEG_TO_RAD); }
static inline float py(float r, float deg) { return CY + r * sinf(deg * DEG_TO_RAD); }

// 抗鋸齒圓頭弧（角度 0 = 12 點鐘方向，順時針），可從 c0 漸層到 c1
static void ringArc(float r, float halfW, float a0, float a1, uint32_t c0, uint32_t c1) {
  a0 -= 90; a1 -= 90;
  if (a1 - a0 < 0.5f) {
    band.fillSmoothCircle(px(r, a0), py(r, a0) - OY, halfW, c0);
    return;
  }
  const float step = 4.0f;
  float a = a0, span = a1 - a0;
  while (a < a1 - 0.01f) {
    float b = a + step > a1 ? a1 : a + step;
    uint32_t c = mix(c0, c1, (b - a0) / span);
    band.drawWideLine(px(r, a), py(r, a) - OY, px(r, b), py(r, b) - OY, halfW, c);
    a = b;
  }
}
static void ringFull(float r, float halfW, uint32_t c) { ringArc(r, halfW, 0, 360, c, c); }

// 防烙印（AMOLED）：文字與小圓點整體挪幾個像素，由韌體定時換位置；外環、額度環不動
static int g_shX = 0, g_shY = 0;

static void text(const char* s, int x, int y, uint32_t col, const lgfx::IFont* font, textdatum_t datum = middle_center) {
  x += g_shX; y += g_shY;
  band.setFont(font);
  band.setTextDatum(datum);
  band.setTextColor(col);
  band.drawString(s, x, y - OY);
}
static int textW(const char* s, const lgfx::IFont* font) { band.setFont(font); return band.textWidth(s); }

// 以 y 為中心、上下各 h 的一行字，整行都在半徑 r 的圓裡時最寬能多寬
static int circleW(int y, int h, float r) {
  const float dy = fabsf((float)(y - CY)) + h;
  return dy >= r ? 0 : (int)(2 * sqrtf(r * r - dy * dy));
}

static String fitText(const String& s, int maxW, const lgfx::IFont* font) {
  if (textW(s.c_str(), font) <= maxW) return s;
  String out = s;
  while (out.length() > 0) {
    out.remove(out.length() - 1);
    if (textW((out + "...").c_str(), font) <= maxW) return out + "...";
  }
  return "";
}

static float breath(uint32_t ms, uint32_t period) {      // 0..1 平滑呼吸
  float ph = (ms % period) / (float)period;
  return 0.5f - 0.5f * cosf(ph * 2 * PI);
}

// ---------------- 狀態燈 ----------------
static uint32_t stColor(St st) {
  switch (st) {
    case S_DONE: return C_GREEN;
    case S_REVIEW: return C_YELLOW;
    case S_ERROR: return C_RED;
    case S_WORKING: return C_GREEN;      // 生成中：綠色呼吸；完成：綠色恆亮
    default: return C_GRAY;
  }
}

// 外環亮度 0..1
static float ringLevel(St st, uint32_t ms) {
  switch (st) {
    case S_WORKING: return 0.06f + 0.94f * breath(ms, 2600);          // 慢呼吸（接近熄滅 ↔ 全亮）
    case S_REVIEW:  return 0.10f + 0.90f * breath(ms, 1200);          // 快呼吸，提醒你
    case S_ERROR: {                                                     // 雙閃
      uint32_t p = ms % 1600;
      return (p < 170 || (p > 340 && p < 510)) ? 1.0f : 0.18f;
    }
    case S_DONE: return 1.0f;                                           // 恆亮
    default: return 0.0f;
  }
}

// 外環（狀態燈）幾何：用整數半徑的實心環，可直接畫到螢幕上，整圈同時變化
static const int RS_OUT = 119, RS_IN = 113;           // 環本體（設計座標）
static constexpr float R_USAGE = S(104.5f), HW_USAGE = S(6.0f); // 內環：5 小時額度（98.5 ~ 110.5，緊貼外環）

// 依目前狀態算外環顏色與亮度
static void ringParams(uint32_t ms, uint32_t& col, float& k) {
  if (!linkAlive()) { col = C_GRAY; k = 0.10f + 0.25f * breath(ms, 4000); return; }
  col = stColor(g_frame.st);
  k = ringLevel(g_frame.st, ms);
}

// ---- 外環的快速繪製 ----
// 開機時算好外環每一列的像素範圍與「到圓心的距離等級」（1/4 px 一級），
// 每一格只要算 64 色的調色盤，再一列一列整段送到螢幕：
//   - 一次畫完約 3 ms，可以跑到 60 fps，呼吸燈很順
//   - 整圈用同一個亮度，不會有上下半圈不同步
static constexpr float RING_R0 = S(111.0f), RING_R1 = S(121.0f);   // 外環區域（環內緣 ~ 螢幕邊）
static constexpr int RING_LV = (int)((RING_R1 - RING_R0) * 4 + 0.5f); // 距離等級數（1/4 px 一級）
struct RingSeg { int16_t x0, n; uint16_t off; };
static constexpr int RING_ROWS = LCD_EVEN ? 2 : 1;        // 每段幾列（CO5300：y 與高度也要偶數，兩列一起送）
static RingSeg g_rseg[H][2];                                // 每 RING_ROWS 列記在第一列，其餘列沒有段
static uint8_t g_rsegN[H];
static uint8_t* g_rlv = nullptr;                            // 每個外環像素的距離等級（每段 n × RING_ROWS 個）
static bool g_ringDirect = false;                          // 韌體：外環直接畫到螢幕（sprite 這區留空）
static bool g_ringKeyed = false;                           // 這次畫面有沒有把外環區域留空
static const uint16_t RING_KEY = 0x0821;                   // 「透明」用的顏色（幾乎黑）

// 第 y 列在外環區域的範圍 [L, R]；hole = 中間有環內不碰的範圍 [a, b]
static bool ringRow(int y, int& L, int& R, bool& hole, int& a, int& b) {
  const float dy = y + 0.5f - CY;
  if (fabsf(dy) >= RING_R1) return false;
  const float xo = sqrtf(RING_R1 * RING_R1 - dy * dy);
  L = (int)floorf(CX - xo); R = (int)ceilf(CX + xo) - 1;
  if (L < 0) L = 0;
  if (R > W - 1) R = W - 1;
  hole = fabsf(dy) < RING_R0;
  if (hole) {
    const float xi = sqrtf(RING_R0 * RING_R0 - dy * dy);
    a = (int)floorf(CX - xi + 0.5f); b = (int)ceilf(CX + xi - 0.5f) - 1;
  }
  return true;
}

static void ringInit() {
  if (g_rlv) return;
  // 先排好每一段（RING_ROWS 列取聯集），再算每個像素的距離
  int total = 0;
  for (int y = 0; y < H; y++) {
    g_rsegN[y] = 0;
    if (y % RING_ROWS) continue;
    int L = W, R = -1, a = -1, b = W;
    bool any = false, hole = true;
    for (int r = y; r < y + RING_ROWS && r < H; r++) {
      int l1, r1, a1, b1; bool h1;
      if (!ringRow(r, l1, r1, h1, a1, b1)) continue;
      any = true;
      if (l1 < L) L = l1;
      if (r1 > R) R = r1;
      if (!h1) hole = false;
      else { if (a1 > a) a = a1; if (b1 < b) b = b1; }    // 環內：幾列都在環內的部分才不碰
    }
    if (!any) continue;
    // outer0 / outer1：這一端是環的外側（螢幕邊，本來就是黑的）還是內側
    auto addSeg = [&](int x0, int x1, bool outer0, bool outer1) {
      if (LCD_EVEN) {                   // 對齊偶數：往外側多畫一格黑的，內側少畫一格（那格在環內緣以內，也是黑的）
        if (x0 & 1) x0 += outer0 ? -1 : 1;
        if (!(x1 & 1)) x1 += outer1 ? 1 : -1;
        // 64 位元組以下的寫入，LovyanGFX 改塞 SPI 暫存器送出，走 QSPI 會畫壞（外環右側一條暗線）：
        // 太短就加寬，先往外側（螢幕邊），到邊了才往內側（環內緣以內，也是黑的）
        while (x1 >= x0 && (x1 - x0 + 1) * RING_ROWS * 2 <= 64) {
          if (outer0 && x0 >= 2) x0 -= 2;
          else if (outer1 && x1 <= W - 3) x1 += 2;
          else if (!outer0) x0 -= 2;
          else x1 += 2;
        }
      }
      if (x1 < x0) return;
      RingSeg& sg = g_rseg[y][g_rsegN[y]++];
      sg.x0 = x0; sg.n = x1 - x0 + 1; sg.off = total;
      total += sg.n * RING_ROWS;
    };
    if (hole) {
      addSeg(L, a - 1, true, false);
      addSeg(b + 1, R, false, true);
    } else addSeg(L, R, true, true);
  }
  g_rlv = (uint8_t*)malloc(total);
  if (!g_rlv) return;
  for (int y = 0; y < H; y++)
    for (int s = 0; s < g_rsegN[y]; s++) {
      const RingSeg& sg = g_rseg[y][s];
      uint8_t* lv = g_rlv + sg.off;
      for (int r = 0; r < RING_ROWS; r++) {
        const float dy = y + r + 0.5f - CY;
        for (int x = sg.x0; x < sg.x0 + sg.n; x++) {
          const float dx = x + 0.5f - CX, d = sqrtf(dx * dx + dy * dy);
          const int v = (int)((d - RING_R0) * 4);              // 環內緣以內 → 0（黑）
          *lv++ = v < 0 ? 0 : (v >= RING_LV ? RING_LV - 1 : v);
        }
      }
    }
}

// 依目前狀態算 64 色調色盤（距離 → 顏色）
static void ringPalette(uint32_t ms, uint16_t* pal) {
  uint32_t col; float k;
  ringParams(ms, col, k);
  uint32_t track = mix(C_BG, col, (linkAlive() && g_frame.st != S_IDLE) ? 0.16f : 0.22f);
  uint32_t body = k <= 0.01f ? track : mix(track, col, k);
  const float E1 = S(RS_IN - 0.5f), E2 = S(RS_OUT + 0.5f);    // 環本體 112.5 ~ 119.5（設計座標）
  for (int i = 0; i < RING_LV; i++) {
    float d = RING_R0 + (i + 0.5f) / 4.0f;
    uint32_t glow = C_BG;                                       // 不再往內發光，讓額度環緊貼外環
    uint32_t c;
    if (d < E1 - 0.5f) c = glow;
    else if (d < E1 + 0.5f) c = mix(glow, body, d - (E1 - 0.5f));
    else if (d < E2 - 0.5f) c = body;
    else if (d < E2 + 0.5f) c = mix(body, C_BG, d - (E2 - 0.5f));
    else c = C_BG;
    pal[i] = lgfx::color565((c >> 16) & 0xFF, (c >> 8) & 0xFF, c & 0xFF);
  }
}

// 把外環畫到 g 上（g 的第 0 列 = 螢幕第 yoff 列，共 rows 列）
static void ringDraw(lgfx::LovyanGFX& g, int yoff, int rows, uint32_t ms) {
  ringInit();
  if (!g_rlv) return;
  static uint16_t pal[RING_LV];
  static uint16_t line[W * RING_ROWS];
  ringPalette(ms, pal);
  for (int y = yoff; y < yoff + rows && y < H; y++) {
    for (int s = 0; s < g_rsegN[y]; s++) {
      const RingSeg& sg = g_rseg[y][s];
      const uint8_t* lv = g_rlv + sg.off;
      for (int i = 0; i < sg.n * RING_ROWS; i++) line[i] = pal[lv[i]];
      g.pushImage(sg.x0, y - yoff, sg.n, RING_ROWS, (const lgfx::rgb565_t*)line);
    }
  }
}

// 畫面（sprite）裡的外環：韌體模式下只留空（之後直接畫到螢幕），預覽程式則直接畫進去
// g_ringLive：韌體把外環也畫進 sprite 時（AMOLED），用螢幕上最後一次畫的亮度，推上去才不會一條一條閃
static bool g_ringLive = false;
static uint32_t g_ringShownMs = 0;
static void statusRingBand(uint32_t ms) {
  if (!g_ringDirect) { ringDraw(band, OY, BAND_H, g_ringLive && g_ringShownMs ? g_ringShownMs : ms); return; }
  ringInit();
  if (!g_rlv) return;
  for (int y = OY; y < OY + BAND_H && y < H; y++)
    for (int s = 0; s < g_rsegN[y]; s++)
      band.fillRect(g_rseg[y][s].x0, y - OY, g_rseg[y][s].n, RING_ROWS, RING_KEY);
  g_ringKeyed = true;
}

// 整圈直接畫到螢幕（呼吸燈每一格）
static void statusRingOn(lgfx::LovyanGFX& g, int yoff, uint32_t ms) { g_ringShownMs = ms; ringDraw(g, yoff, H, ms); }

// 外環是否需要動畫（呼吸、閃爍、剛完成的亮光）
static bool ringAnimating() {
  if (g_saver) return false;
  if (!linkAlive()) return true;
  St st = g_frame.st;
  return st == S_WORKING || st == S_REVIEW || st == S_ERROR;
}

// 帶漸層與端點的進度環（Apple Activity 風格）
// 額度環：下方留一個開口（像儀表），從開口左側開始順時針增加
static const float GAUGE_GAP = 56.0f;                 // 開口角度
static const float GAUGE_START = 180.0f + GAUGE_GAP / 2, GAUGE_SPAN = 360.0f - GAUGE_GAP;

// 原本的畫法（drawWideLine 拼 76 段）：每個像素都要算浮點距離，ESP32-C3 沒有 FPU，一條畫面要 130-170 ms，
// 這段時間外環沒辦法更新（呼吸會頓）。只在查表用的記憶體配置失敗時才用
static float g_trackK = 0.20f;   // 額度環沒用到的部分（底色）有多亮；額度詳細頁兩圈時調暗（白色的 ChatGPT 底色太亮，看起來兩圈都快滿了）
static void progressRingSlow(float r, float hw, float pct, uint32_t col) {
  uint32_t track = mix(C_BG, col, g_trackK);
  float e = GAUGE_START + GAUGE_SPAN;
  ringArc(r, hw, GAUGE_START, e, track, track);
  band.fillSmoothCircle(px(r, GAUGE_START - 90), py(r, GAUGE_START - 90) - OY, hw, track);   // 圓頭
  band.fillSmoothCircle(px(r, e - 90), py(r, e - 90) - OY, hw, track);
  if (pct <= 0) return;
  float p = pct > 100 ? 100 : pct;
  float a1 = GAUGE_START + GAUGE_SPAN * p / 100.0f;
  ringArc(r, hw, GAUGE_START, a1, mix(col, C_BG, 0.25f), col);
  band.fillSmoothCircle(px(r, GAUGE_START - 90), py(r, GAUGE_START - 90) - OY, hw, mix(col, C_BG, 0.25f));
  band.fillSmoothCircle(px(r, a1 - 90), py(r, a1 - 90) - OY, hw, col);
}

// ---- 額度環的快速繪製（跟外環一樣先算好、之後只查表）----
// 開機時（gaugeInit）算好環上每個像素的角度與到圓心的距離，之後每個像素只做整數運算與混色；
// 只有圓頭附近幾百個像素要開根號。距離以像素中心（x + 0.5）計算，跟外環一樣；
// 原本的 drawWideLine 只吃整數座標，端點被捨去小數，結果也等於以像素中心為準，所以畫出來跟原本一樣
// 舊畫法多段線重疊、邊緣混色好幾次，環實際上粗約 0.4 px：邊緣各放寬一點，看起來一樣粗
static const float GAUGE_AA = 0.7f;
static constexpr int GAUGE_DQ = 2 * (HW_USAGE + 1) * 16 < 256 ? 16 : 8;   // 距離每 px 幾級（環寬要放得進 uint8）
struct GaugeLut {
  float r0 = 0;              // 涵蓋 r0 ~ r0 + 2 * (hw + 1)（環寬 + 抗鋸齒）
  RingSeg seg[H][2];
  uint8_t segN[H];
  uint16_t* ang = nullptr;   // 角度，1/16 度（0 = 12 點鐘，順時針）
  uint8_t* dist = nullptr;   // 距離 - r0，1/GAUGE_DQ px
  bool failed = false;       // 記憶體不夠：改用舊畫法，不要每次都重試
};
static GaugeLut g_gauge[2];  // [0] 5 小時（R_USAGE），[1] 每週（額度詳細頁的內圈）

static GaugeLut* gaugeLut(float r, float hw) {
  GaugeLut& L = g_gauge[r < R_USAGE - 1 ? 1 : 0];
  if (L.ang || L.failed) return L.ang ? &L : nullptr;
  L.r0 = r - hw - 1;
  const float r1 = r + hw + 1, a2 = L.r0 * L.r0, b2 = r1 * r1;
  int total = 0;
  for (int y = 0; y < H; y++) {                    // 每一列在環上的像素範圍（最多左右兩段）
    L.segN[y] = 0;
    const float dy = y + 0.5f - CY;
    for (int x = 0, run = -1; x <= W; x++) {
      const float dx = x + 0.5f - CX, d2 = dx * dx + dy * dy;
      const bool in = x < W && d2 >= a2 && d2 < b2;
      if (in && run < 0) run = x;
      if (in || run < 0) continue;
      if (L.segN[y] < 2) {
        RingSeg& sg = L.seg[y][L.segN[y]++];
        sg.x0 = run; sg.n = x - run; sg.off = total;
        total += x - run;
      }
      run = -1;
    }
  }
  L.ang = (uint16_t*)malloc(total * sizeof(uint16_t));
  L.dist = (uint8_t*)malloc(total);
  if (!L.ang || !L.dist) { free(L.ang); free(L.dist); L.ang = nullptr; L.dist = nullptr; L.failed = true; return nullptr; }
  // 以像素中心算，上下左右對稱（x ↔ 239 - x、y ↔ 239 - y）：只算右下象限再鏡射到另外三個，
  // 浮點運算少 4 倍（沒有 FPU，全部自己算一圈要 0.7 秒）
  auto at = [&](int x, int y) {
    for (int s = 0; s < L.segN[y]; s++) {
      const RingSeg& sg = L.seg[y][s];
      if (x >= sg.x0 && x < sg.x0 + sg.n) return sg.off + x - sg.x0;
    }
    return -1;
  };
  for (int y = CY; y < H; y++) {
    const float dy = y + 0.5f - CY;
    for (int s = 0; s < L.segN[y]; s++) {
      const RingSeg& sg = L.seg[y][s];
      for (int x = sg.x0 > CX ? sg.x0 : CX; x < sg.x0 + sg.n; x++) {
        const float dx = x + 0.5f - CX, d = sqrtf(dx * dx + dy * dy);
        const int a = (int)((atan2f(dy, dx) * RAD_TO_DEG + 90) * 16 + 0.5f);   // 右下象限：90 ~ 180 度
        int dv = (int)((d - L.r0) * GAUGE_DQ + 0.5f);
        if (dv > 255) dv = 255;
        const int mx = W - 1 - x, my = H - 1 - y;
        const int q[4][3] = {{x, y, a}, {mx, y, 5760 - a}, {x, my, 2880 - a}, {mx, my, a + 2880}};
        for (const auto& m : q) {
          const int i = at(m[0], m[1]);
          if (i < 0) continue;
          L.ang[i] = (uint16_t)(((m[2] % 5760) + 5760) % 5760);
          L.dist[i] = (uint8_t)dv;
        }
      }
    }
  }
  return &L;
}

// 開機時先建好兩圈的表（第一次翻到額度詳細頁才建的話，那一下會卡 0.7 秒）
static void gaugeInit() {
  gaugeLut(R_USAGE, HW_USAGE);
  gaugeLut(R_USAGE - S(14), HW_USAGE);
}

// 圓頭：到端點 (cx, cy) 的距離 → 不透明度 0..256
static inline int capAlpha(int x, int y, float cx, float cy, float hw) {
  const float dx = x + 0.5f - cx, dy = y + 0.5f - cy;
  const int a = (int)((hw + GAUGE_AA - sqrtf(dx * dx + dy * dy)) * 256);
  return a < 0 ? 0 : (a > 256 ? 256 : a);
}
static inline uint32_t blend888(uint32_t u, uint32_t c, int a) {   // a = 0..256
  const int b = 256 - a;
  return ((((u >> 16) & 0xFF) * b + ((c >> 16) & 0xFF) * a) >> 8) << 16 |
         ((((u >> 8) & 0xFF) * b + ((c >> 8) & 0xFF) * a) >> 8) << 8 |
         (((u & 0xFF) * b + (c & 0xFF) * a) >> 8);
}

static void progressRing(float r, float hw, float pct, uint32_t col) {
  GaugeLut* L = band.getColorDepth() == 16 ? gaugeLut(r, hw) : nullptr;
  if (!L) { progressRingSlow(r, hw, pct, col); return; }
  const uint32_t track = mix(C_BG, col, g_trackK), c0 = mix(col, C_BG, 0.25f);
  const float p = pct > 100 ? 100 : pct;
  const int rr = (int)((r - L->r0) * GAUGE_DQ + 0.5f);    // 環的中心線
  const int edge = (int)((hw + GAUGE_AA) * GAUGE_DQ + 0.5f);   // 距中心線這麼遠以外完全透明
  const int start = (int)(GAUGE_START * 16 + 0.5f) % 5760, span = (int)(GAUGE_SPAN * 16 + 0.5f);
  const int end = pct > 0 ? (int)(span * p / 100 + 0.5f) : -1;   // 進度的終點（-1 = 沒有進度）
  const int CAP = 6 * 16;                                   // 圓頭延伸到端點外約 3.6°，6° 以外不用算
  const float sx = px(r, GAUGE_START - 90), sy = py(r, GAUGE_START - 90);
  const float tx = px(r, GAUGE_START + GAUGE_SPAN - 90), ty = py(r, GAUGE_START + GAUGE_SPAN - 90);
  const float ex = px(r, GAUGE_START + GAUGE_SPAN * p / 100 - 90), ey = py(r, GAUGE_START + GAUGE_SPAN * p / 100 - 90);
  uint16_t* buf = (uint16_t*)band.getBuffer();              // 16 位元 sprite：RGB565 高低位元組對調
  for (int y = OY; y < OY + BAND_H && y < H; y++) {
    uint16_t* row = buf + (y - OY) * W;
    for (int s = 0; s < L->segN[y]; s++) {
      const RingSeg& sg = L->seg[y][s];
      for (int i = 0; i < sg.n; i++) {
        const int x = sg.x0 + i;
        int dd = L->dist[sg.off + i] - rr;
        if (dd < 0) dd = -dd;
        int aR = (edge - dd) * (256 / GAUGE_DQ);            // 環的內外緣抗鋸齒（256 = 不透明）
        if (aR <= 0) continue;
        if (aR > 256) aR = 256;
        int rel = L->ang[sg.off + i] - start;               // 從起點順時針的角度
        if (rel < 0) rel += 5760;
        const bool nearStart = rel >= 5760 - CAP;
        // 底色軌道（整個 304°，兩端圓頭）
        int a1;
        if (rel <= span) a1 = aR;
        else if (rel <= span + CAP) a1 = capAlpha(x, y, tx, ty, hw);
        else if (nearStart) a1 = capAlpha(x, y, sx, sy, hw);
        else continue;                                      // 開口
        // 進度（起點顏色較暗、漸層到終點，兩端圓頭）
        int a2 = 0;
        uint32_t g = col;
        if (end >= 0) {
          if (rel <= end) {
            a2 = aR;
            const int t = end > 0 ? rel * 256 / end : 256;
            g = blend888(c0, col, t);
          } else if (rel <= end + CAP && !nearStart) a2 = capAlpha(x, y, ex, ey, hw);
          if (nearStart) { a2 = capAlpha(x, y, sx, sy, hw); g = c0; }
        }
        if (!a1 && !a2) continue;
        const uint16_t v = (uint16_t)((row[x] >> 8) | (row[x] << 8));
        uint32_t u = ((v >> 11) & 0x1F) << 19 | ((v >> 13) & 0x07) << 16 |
                     ((v >> 5) & 0x3F) << 10 | ((v >> 9) & 0x03) << 8 |
                     (v & 0x1F) << 3 | ((v >> 2) & 0x07);
        if (a1) u = blend888(u, track, a1);
        if (a2) u = blend888(u, g, a2);
        const uint16_t o = lgfx::color565((u >> 16) & 0xFF, (u >> 8) & 0xFF, u & 0xFF);
        row[x] = (uint16_t)((o >> 8) | (o << 8));
      }
    }
  }
}

// ---------------- Scene ----------------
static void bigValue(const char* num, bool percent, int y) {
  int w = textW(num, &fNum.font);
  int x = CX - w / 2;                     // 數字本身置中，「%」掛在右邊（不算進置中）
  text(num, x, y, C_LABEL, &fNum.font, middle_left);
  if (percent) text("%", x + w + SI(2), y + SI(8), C_LABEL2, &fUnit.font, middle_left);
}

static const uint32_t GEN_SWEEP_MS = 1800;   // 「Generating」的光掃一次多久

static void statusLine(St st, int y, uint32_t ms) {
  const char* s = ST_TITLE[st];
  uint32_t col = st == S_IDLE ? C_LABEL2 : stColor(st);
  int tw = textW(s, &fLabel.font);
  int x = CX - (tw + SI(12)) / 2;
  band.fillSmoothCircle(x + S(4) + g_shX, y + g_shY - OY, S(3.5f), col);
  if (st != S_WORKING) { text(s, x + SI(12), y + SI(1), col, &fLabel.font, middle_left); return; }
  // 生成中：暗綠的字，一道亮光由左掃到右（GEN_SWEEP_MS 一次）。亮帶分成幾條直條、各用 clip 重畫一次字，越靠中間越亮
  const int tx = x + SI(12);
  const uint32_t dim = mix(col, C_BG, 0.45f), peak = 0xB9FFCF;
  text(s, tx, y + SI(1), dim, &fLabel.font, middle_left);
  const float p = -0.3f + 1.6f * (ms % GEN_SWEEP_MS) / GEN_SWEEP_MS;
  const int mid = tx + g_shX + (int)(p * tw), half = tw * 18 / 100, N = 4;
  for (int i = -N; i < N; i++) {
    const int a = mid + i * half / N, b = mid + (i + 1) * half / N;
    if (b <= tx + g_shX || a >= tx + g_shX + tw) continue;          // 在字外面
    band.setClipRect(a, 0, b - a, band.height());
    text(s, tx, y + SI(1), mix(dim, peak, 1 - fabsf((i + 0.5f) / N)), &fLabel.font, middle_left);
  }
  band.clearClipRect();
}

// 畫面內容的指紋：電腦每 1-3 秒送一次資料，內容沒變就不要整張重畫（整張送出時動畫會頓一下）
static uint32_t hashStr(uint32_t k, const String& s) {
  for (size_t i = 0; i < s.length(); i++) k = k * 31 + (uint8_t)s.c_str()[i];
  return k * 31 + s.length();
}
static uint32_t frameKey(const Frame& f) {
  uint32_t k = f.brand;
  k = k * 31 + f.st;
  k = hashStr(k, f.msg);
  k = hashStr(k, f.note) * 31 + f.noteKind;
  k = k * 31 + (f.p5 >= 0 ? 1 : 0) + (f.p5 >= g_redAt ? 2 : 0);   // 5 小時額度的數字由 gaugeTick 慢慢跑過去，不整張重畫
  k = k * 31 + (uint32_t)(int)(f.pw * 2 + 0.5f);
  k = k * 31 + f.r5;
  k = k * 31 + f.rw;
  for (int i = 0; i < f.nToday; i++) k = (k * 31 + f.today[i].brand * 1000 + f.today[i].count) * 31 + f.today[i].secs;
  return k * 31 + f.nToday;
}
// 其他對話的小圓點（othersRow）：只有這個變了時不整張重畫，韌體只重畫小圓點那一塊（othersBox）。
// 狀態切換時電腦常在幾秒內連送好幾張只差在小圓點的畫面，每次整張重畫（AMOLED 一次 300-600 ms）Generating 的光就頓一下
static uint32_t othersKey(const Frame& f) {
  uint32_t k = f.nOthers;
  for (int i = 0; i < f.nOthers; i++) k = k * 31 + f.others[i].brand * 8 + f.others[i].st;
  return k;
}
// 跟時間有關、到分鐘才會變的東西（重置倒數、日期）與防烙印位置：變了才整張重畫
static uint32_t viewKey() {
  const uint32_t now = nowEpoch();
  uint32_t k = now / 60;
  if (g_frame.r5 > now) k = (k * 31 + (g_frame.r5 - now) / 60) * 31 + (g_frame.r5 - now + 59) / 60;
  if (g_frame.rw > now) k = k * 31 + (g_frame.rw - now) / 60;
  k = k * 31 + (linkAlive() ? 1 : 0);
  return k * 31 + (uint32_t)((g_shX + 16) * 64 + g_shY + 16);
}

// 額度環的漸進動畫：畫面上顯示的 5 小時額度（g_p5Shown）每 33 ms 往電腦送來的值靠近（韌體 gaugeTick），
// 每一步只重畫環上變化的那一小段（gaugeArcBox）與數字（pctBox）
static float g_p5Shown = -1;
static float p5View() { return g_p5Shown >= 0 && g_frame.p5 >= 0 ? g_p5Shown : g_frame.p5; }
// 大百分比數字：里程表那樣捲動。顯示值的小數部分 = 往下一個數字捲了多少；個位每次都捲，
// 十位、百位只有右邊全是 9 時才跟著捲（9 → 10、19 → 20…）。停住時（整數）就是一般的數字
static void pctDraw() {
  const float t = g_frame.p5;
  float disp = p5View() + (floorf(t + 0.5f) - t);          // 目標本身有小數（例如 44.6）時，停在四捨五入的整數上
  if (disp < 0) disp = 0;
  const int n = (int)floorf(disp);
  float fr = disp - n;
  if (fr < 0.02f) fr = 0;
  char cur[8];
  snprintf(cur, sizeof cur, "%d", n);
  const int len = strlen(cur), w = textW(cur, &fNum.font), y = SI(106), roll = SI(48);
  const int x = CX - w / 2;                                 // 跟 bigValue 一樣：數字置中，「%」掛在右邊
  bool allNine = true;
  for (int i = 0; i < len; i++) allNine = allNine && cur[i] == '9';
  if (fr > 0) band.setClipRect(x - SI(30) + g_shX, y - SI(25) + g_shY - OY, w + SI(34), SI(50));   // 只露出一個數字的高度
  const int off = (int)(fr * roll);
  int cx = x;
  for (int i = 0; i < len; i++) {
    char c[2] = {cur[i], 0};
    const int cw = textW(c, &fNum.font);
    bool rolls = fr > 0;
    for (int j = i + 1; j < len && rolls; j++) rolls = cur[j] == '9';
    if (!rolls) text(c, cx, y, C_LABEL, &fNum.font, middle_left);
    else {
      char nc[2] = {(char)(cur[i] == '9' ? '0' : cur[i] + 1), 0};
      text(c, cx, y - off, C_LABEL, &fNum.font, middle_left);                                  // 舊數字往上捲出去
      text(nc, cx + (cw - textW(nc, &fNum.font)) / 2, y - off + roll, C_LABEL, &fNum.font, middle_left);   // 新數字從下面捲上來
    }
    cx += cw;
  }
  if (allNine && fr > 0) text("1", x - textW("1", &fNum.font), y - off + roll, C_LABEL, &fNum.font, middle_left);   // 9 → 10 多出來的那一位
  if (fr > 0) band.clearClipRect();
  text("%", x + w + SI(2), y + SI(8), C_LABEL2, &fUnit.font, middle_left);
}
// 狀態頁大百分比數字的範圍（含「%」，偶數對齊、含防烙印挪動）：上面是 LOGO（到 70）、下面是狀態列（從 133）
static void pctBox(int& x, int& y, int& w, int& h) {
  const int nw = textW("100", &fNum.font);
  x = (CX - nw / 2 - SI(6) + g_shX) & ~1; w = (nw + SI(34) + 2) & ~1;
  y = (SI(80) + g_shY) & ~1; h = SI(52) & ~1;
}
// 額度環上 a% 到 b% 那一段（含圓頭、抗鋸齒）的範圍，偶數對齊
static void gaugeArcBox(float a, float b, int& x, int& y, int& w, int& h) {
  if (a > b) { const float t = a; a = b; b = t; }
  a = a < 0 ? 0 : (a > 100 ? 100 : a); b = b < 0 ? 0 : (b > 100 ? 100 : b);
  float x0 = 1e9f, y0 = 1e9f, x1 = -1e9f, y1 = -1e9f;
  const int n = 2 + (int)((b - a) * GAUGE_SPAN / 100 / 4);   // 每 4° 取一點
  for (int i = 0; i <= n; i++) {
    const float d = GAUGE_START + GAUGE_SPAN * (a + (b - a) * i / n) / 100 - 90;
    const float qx = px(R_USAGE, d), qy = py(R_USAGE, d);
    x0 = qx < x0 ? qx : x0; x1 = qx > x1 ? qx : x1; y0 = qy < y0 ? qy : y0; y1 = qy > y1 ? qy : y1;
  }
  const float pad = HW_USAGE + S(3);
  x = ((int)(x0 - pad) & ~1); y = ((int)(y0 - pad) & ~1);
  if (x < 0) x = 0;
  if (y < 0) y = 0;
  w = ((int)(x1 + pad) - x + 2) & ~1; h = ((int)(y1 + pad) - y + 2) & ~1;
  if (x + w > W) w = (W - x) & ~1;
  if (y + h > H) h = (H - y) & ~1;
}

// 主畫面「Generating」那一列的範圍（偶數對齊，含防烙印挪動）與它的 y；不是生成中就不用動畫
static bool genBox(int& x, int& y, int& w, int& h, int& ly) {
  if (g_saver || g_page != 0 || !linkAlive() || g_frame.st != S_WORKING) return false;
  ly = g_frame.brand != N_BRANDS - 1 ? SI(145) : SI(148);   // 跟 drawScene 一樣
  const int tw = textW(ST_TITLE[S_WORKING], &fLabel.font) + SI(12);
  x = (CX - tw / 2 - SI(4) + g_shX) & ~1; w = (tw + SI(8) + 2) & ~1;
  y = (ly - SI(12) + g_shY) & ~1; h = (SI(24) + 2) & ~1;
  return true;
}

// 小圓點那一塊（最多 5 顆：中心左右各 2 格，偶數對齊）。在額度環下方的缺口裡，碰不到額度環的圓頭與文字；
// 最下面碰到外環內緣一點點（重畫時外環照樣畫進去）。看不到（其他頁、螢幕保護）就不用畫
static bool othersBox(int& x, int& y, int& w, int& h) {
  if (g_saver || g_page != 0 || !linkAlive()) return false;
  const float a = 2 * S(16) / R_USAGE;                     // 最外面那顆離正下方的角度（弧度）
  const float dx = R_USAGE * sinf(a) + HW_USAGE + S(2);
  const float top = R_USAGE * cosf(a) - HW_USAGE - S(2), bot = R_USAGE + HW_USAGE + S(1.5f);
  x = (int)(CX - dx) & ~1; w = ((int)(2 * dx) + 4) & ~1;
  y = (int)(CY + top) & ~1; h = ((int)(bot - top) + 4) & ~1;
  if (y + h > H) h = (H - y) & ~1;
  return true;
}

static void othersRow(const Frame& f) {
  // 其他對話：排在額度環下方的缺口裡（同一個半徑、跟環一樣粗），只用顏色（綠 = 完成、黃 = 需審核、紅 = 錯誤），空心 = 生成中
  // 順序由電腦端排好（要你處理的在前），從左邊開始排
  const float r = HW_USAGE, step = S(16) / R_USAGE * RAD_TO_DEG;
  for (int i = 0; i < f.nOthers; i++) {
    float deg = 90 + ((f.nOthers - 1) / 2.0f - i) * step;
    float x = px(R_USAGE, deg), y = py(R_USAGE, deg) - OY;
    band.fillSmoothCircle(x, y, r, stColor(f.others[i].st));
    if (f.others[i].st == S_WORKING) band.fillSmoothCircle(x, y, r - S(2.2f), C_BG);
  }
}

// ---------------- Clawd（Claude Code 的像素小螃蟹，只畫在 AMOLED）----------------
// 主畫面的主燈是 Claude 時取代上方的名稱，動作跟著狀態；螢幕保護時在整個圓裡亂跑（韌體的 roamTick，不在這裡畫）。
// 12×7 格：身體 2-9 欄 0-4 列、兩隻手 0-1 / 10-11 欄 2-3 列、眼睛 3 / 8 欄第 1 列、四隻腳 2 / 4 / 7 / 9 欄 5-6 列。
// 韌體每 50 ms 只重畫它所在的那一塊（clawdBox）直接送到螢幕，不用整張重畫
#if LCD_W > 240
static const uint32_t C_CLAWD = 0xD97757;
struct ClawdPose { int dx = 0, dy = 0; uint8_t legsUp = 0; bool armUp = false, blink = false; };   // legsUp：第 i 隻腳抬起（短一格）

static void drawClawd(int x, int y, int u, const ClawdPose& p) {
  x += p.dx + g_shX; y += p.dy + g_shY - OY;
  auto R = [&](int c, int r, int w, int h, uint32_t col) { band.fillRect(x + c * u, y + r * u, w * u, h * u, col); };
  R(2, 0, 8, 5, C_CLAWD);                                   // 身體
  R(0, 2, 2, 2, C_CLAWD);                                   // 左手
  R(10, p.armUp ? 0 : 2, 2, 2, C_CLAWD);                    // 右手（揮手時舉到頭頂旁邊）
  static const int LEG[4] = {2, 4, 7, 9};
  for (int i = 0; i < 4; i++) R(LEG[i], 5, 1, (p.legsUp >> i) & 1 ? 1 : 2, C_CLAWD);
  for (int c : {3, 8}) {                                    // 眼睛；眨眼時只剩一條線
    if (p.blink) band.fillRect(x + c * u, y + u + u * 2 / 5, u, max(2, u / 4), C_BG);
    else R(c, 1, 1, 1, C_BG);
  }
}

static ClawdPose clawdPose(St st, uint32_t ms, int u) {
  ClawdPose p;
  const auto tri = [](uint32_t t, uint32_t period, int amp) {   // 三角波 -amp..amp
    int ph = (int)(t % period) * 4 * amp / (int)period;
    return ph < 2 * amp ? ph - amp : 3 * amp - ph;
  };
  switch (st) {
    case S_WORKING: {                                       // 踏步、左右慢慢晃
      const bool f = (ms / 180) % 2;
      p.legsUp = f ? 0b0101 : 0b1010;
      p.dy = f ? -max(1, u / 5) : 0;
      p.dx = tri(ms, 2400, u / 2);
      break;
    }
    case S_REVIEW: p.armUp = (ms / 300) % 2; break;         // 揮手：在等你
    case S_DONE: {                                          // 每 1.4 秒跳一下
      const int ph = ms % 1400;
      if (ph < 300) p.dy = -(u * (150 - abs(ph - 150)) / 150);
      break;
    }
    case S_ERROR:                                           // 抖一抖
      if (ms % 1000 < 400) p.dx = ((ms / 50) % 2 ? 1 : -1) * max(2, u / 3);
      break;
    default: break;
  }
  if (st != S_ERROR && ms % 4000 < 130) p.blink = true;
  return p;
}

static const int CLAWD_U = SI(5);                           // 主畫面：一格 5 個設計像素
static const int CLAWD_SU = SI(3);                          // 螢幕保護：小一點

// 自訂動畫（anim.h，使用者上傳的影片）：放在 Clawd 的位置，主燈是 Claude 時取代 Clawd
// 畫格的底邊跟 clawdBox 一樣（下面就是大數字），往上伸進圓頂；超出額度環內緣（ANIM_RC）的部分不畫，所以四個角碰不到環
static const int ANIM_W = 140, ANIM_H = 114;                // 畫格大小（偶數）
static const int ANIM_Y = ((SI(31) & ~1) + ((10 * CLAWD_U) & ~1) - ANIM_H) & ~1;   // 畫格上緣（沒挪動時）
static const int ANIM_RC = SI(96);                          // 只畫在這個半徑以內（額度環內緣 98.5，留一點空）
static const uint16_t* g_animBuf = nullptr;                 // 全部畫格（PSRAM，RGB565）
static uint16_t g_animN = 0, g_animMs = 66;                 // 幾格、每格幾 ms
static uint32_t g_animCrc = 0;                              // 螢幕上這份動畫的 CRC32（0 = 沒有）
static uint32_t g_animWant = 0;                             // 電腦要顯示哪一份（畫面的 "av"，0 = Clawd）
static bool animOn() { return g_animBuf && g_animN && g_animWant && g_animWant == g_animCrc; }

// 畫一格自訂動畫（一列一列畫，每列只畫圓以內的那段）
static void animDraw(uint32_t ms) {
  const uint16_t* fr = g_animBuf + ((ms / g_animMs) % g_animN) * (ANIM_W * ANIM_H);
  const int x0 = CX - ANIM_W / 2 + g_shX, y0 = ANIM_Y + g_shY, r2 = ANIM_RC * ANIM_RC;
  for (int r = 0; r < ANIM_H; r++) {
    const int y = y0 + r, dy = y - CY, rem = r2 - dy * dy;
    if (rem <= 0) continue;
    const int hx = (int)sqrtf((float)rem), xa = max(x0, CX - hx), xb = min(x0 + ANIM_W, CX + hx);
    if (xb > xa) band.pushImage(xa, y - OY, xb - xa, 1, (const lgfx::rgb565_t*)(fr + r * ANIM_W + (xa - x0)));
  }
}
#endif

// 現在要不要畫 Clawd、它可能占到的範圍（偶數對齊，含晃動、跳、防烙印挪動）
static bool clawdBox(int& x, int& y, int& w, int& h) {
#if LCD_W > 240
  if (g_saver || g_page != 0 || !linkAlive() || g_frame.brand != 0) return false;   // 0 = claude
  x = (CX - 7 * CLAWD_U) & ~1; w = (14 * CLAWD_U + 2) & ~1;
  y = SI(31) & ~1; h = (10 * CLAWD_U) & ~1;
  return true;
#else
  (void)x; (void)y; (void)w; (void)h;
  return false;
#endif
}

// 主畫面上方的 AI：官方 LOGO 取代名稱文字（跟字一樣跟著防烙印挪動）
static void brandMark(int brand, int y) {
  if (brand < 0 || brand >= 3) return;
  band.pushImage(CX + g_shX - LOGOB_N / 2, y + g_shY - LOGOB_N / 2 - OY, LOGOB_N, LOGOB_N, (const lgfx::rgb565_t*)LOGOS_BIG[brand]);
}

// 畫主畫面的 Clawd
static void clawdDraw(uint32_t ms) {
#if LCD_W > 240
  int bx, by, bw, bh;
  if (!clawdBox(bx, by, bw, bh)) return;
  if (animOn()) { animDraw(ms); return; }
  const int u = CLAWD_U;
  drawClawd(CX - 6 * u, by + (bh - 7 * u) / 2 + u / 2, u, clawdPose(g_frame.st, ms, u));
#else
  (void)ms;
#endif
}

// ---------------- 螢幕保護（Apple Watch 風格時鐘）----------------
#ifndef SAVER_MIN
#define SAVER_MIN 5
#endif
static bool saverWanted() {
  if (g_saverMin <= 0) return false;
  if (millis() - g_lastActiveMs < (g_saverSec > 0 ? (uint32_t)g_saverSec * 1000UL : (uint32_t)g_saverMin * 60000UL)) return false;
  if (!linkAlive()) return true;
  return g_frame.st == S_IDLE;            // 生成中 / 需審核 / 錯誤 / 完成 都不進入
}

static void drawClock(uint32_t ms, bool pageMode) {
  band.fillScreen(C_BG);
  if (pageMode) statusRingBand(ms);
  uint32_t now = nowEpoch();
  if (!now) {                             // 還不知道時間：只畫一個慢慢呼吸的小點
    band.fillSmoothCircle(CX, CY - OY, S(4), mix(C_BG, C_GRAY, 0.2f + 0.4f * breath(ms, 5000)));
    return;
  }
  uint32_t t = now + g_tz;
  int hh = (t / 3600) % 24, mm = (t / 60) % 60;
  uint32_t days = t / 86400;              // 1970-01-01 是星期四
  static const char* WD[] = {"THU", "FRI", "SAT", "SUN", "MON", "TUE", "WED"};
  // 日期（civil from days）
  int32_t z = days + 719468, era = z / 146097;
  uint32_t doe = z - era * 146097, yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
  uint32_t doy = doe - (365 * yoe + yoe / 4 - yoe / 100), mp = (5 * doy + 2) / 153;
  uint32_t dd = doy - (153 * mp + 2) / 5 + 1, mon = mp < 10 ? mp + 3 : mp - 9;
  // 細細的 Claude 額度弧（螢幕保護時）
  if (!pageMode && linkAlive() && g_frame.p5 >= 0) {
    const Brand& b = BRANDS[g_frame.brand];
    float p = g_frame.p5 > 100 ? 100 : g_frame.p5;
#if LCD_W > 240
    // AMOLED：外環整圈畫進畫面緩衝（RING_R0 以外，含加寬的段約 110.5），弧放在 112 會被蓋掉、斷斷續續 → 往內移到外環區域外。
    // 調暗時很暗的顏色也會變成一點一點：用原色、線粗一點；只畫已用的部分，不畫軌道（使用者要的）
    const float r = S(107.5f), lit = 1.0f, trk = 0.0f, hw = S(2.0f);
#else
    const float r = S(112), lit = 0.55f, trk = 0.25f, hw = S(1.5f);
#endif
    uint32_t col = mix(C_BG, p >= g_redAt ? C_RED : b.color, lit);
    if (trk > 0) ringArc(r, hw, GAUGE_START, GAUGE_START + GAUGE_SPAN, mix(C_BG, col, trk), mix(C_BG, col, trk));
    if (p > 0) ringArc(r, hw, GAUGE_START, GAUGE_START + GAUGE_SPAN * p / 100, col, col);
  }

  const bool hasQ = linkAlive() && g_frame.p5 >= 0;
  const uint32_t c2 = pageMode ? C_LABEL2 : 0xA8A8B0;
  // 整組垂直置中：時間、日期、（額度一行）。日期與額度同一個字級、名字換成官方小 LOGO：
  // 以前「Claude 68%」「RESETS 2H 10M」用比日期小的字、擠在一起，使用者覺得字大小不一、太擠（2026-11）
  const int yT = CY + (hasQ ? SI(-24) : SI(-14));
  char buf[24];
  snprintf(buf, sizeof buf, "%02d:%02d", hh, mm);
  text(buf, CX, yT, pageMode ? C_LABEL : 0xE6E6EA, &fClock.font);
  snprintf(buf, sizeof buf, "%u/%u  %s", (unsigned)mon, (unsigned)dd, WD[days % 7]);
  const int yD = yT + SI(44);
  text(buf, CX, yD, c2, &fLabel.font);
  if (hasQ) {
    const String left = fmtLeft(g_frame.r5);
    auto logoLine = [&](const char* s, int y) {   // 官方小 LOGO + 字，整組置中
      const int tw = textW(s, &fLabel.font), g = SI(6), x0 = CX - (LOGO_N + g + tw) / 2;
      if (g_frame.brand < 3) band.pushImage(x0 + g_shX, y + g_shY - LOGO_N / 2 - OY, LOGO_N, LOGO_N, (const lgfx::rgb565_t*)LOGOS[g_frame.brand]);
      text(s, x0 + LOGO_N + g, y, c2, &fLabel.font, middle_left);
    };
    if (left.length()) snprintf(buf, sizeof buf, "%d%%  ·  %s", (int)(g_frame.p5 + 0.5f), left.c_str());
    else snprintf(buf, sizeof buf, "%d%%", (int)(g_frame.p5 + 0.5f));
    logoLine(buf, yD + SI(30));
  }
}

static void pageDots(int y) {
  int gap = SI(10), x0 = CX - (N_PAGES - 1) * gap / 2;
  for (int i = 0; i < N_PAGES; i++)
    band.fillSmoothCircle(x0 + i * gap + g_shX, y + g_shY - OY, i == g_page ? S(3) : S(2.2f), i == g_page ? C_LABEL : C_LABEL3);
}

// 額度詳細頁：5 小時（外環）＋本週（內環）。由上往下：官方 LOGO、5 小時大數字、左右兩欄（5-HOUR | WEEK）各一行重置倒數。
// 以前是名稱文字 + 「5-HOUR · RESETS 4H 18M」一整行 + 本週兩行，使用者覺得太擠、字快碰到內環 → 改成兩欄往中線對齊（2026-11）
static void drawUsagePage(uint32_t ms) {
  band.fillScreen(C_BG);
  statusRingBand(ms);
  const Frame& f = g_frame;
  const Brand& b = BRANDS[f.brand];
  if (f.p5 < 0) {
    if (f.brand == N_BRANDS - 1) text("Usage", CX, SI(92), C_LABEL2, &fName.font);
    else brandMark(f.brand, SI(88));
    text("NO USAGE DATA", CX, SI(124), C_LABEL2, &fLabel.font);
    text("ONLY CLAUDE PROVIDES IT", CX, SI(146), C_LABEL3, &fTiny.font);
    pageDots(SI(200));
    return;
  }
  const bool hasWeek = f.pw >= 0;
  const bool red5 = f.p5 >= g_redAt, redW = f.pw >= g_redAt;
  // 外圈 5 小時用品牌色、內圈每週用暗一半的同色，下面的標籤字也用各自環的顏色，一眼分得出哪圈是哪個。
  // 以前每週是品牌色混白：ChatGPT 本來就是白色，兩圈一樣、分不出來
  const uint32_t col5 = red5 ? C_RED : b.color;
  const uint32_t colW = redW ? C_RED : mix(C_BG, b.color, 0.5f);
  g_trackK = 0.11f;
  progressRing(R_USAGE, HW_USAGE, f.p5, col5);
  if (hasWeek) progressRing(R_USAGE - S(14), HW_USAGE, f.pw, colW);   // 半徑要跟 gaugeInit 一致
  g_trackK = 0.20f;

  char buf[40];
  // 位置是量過的：最長的字（WEEK 100%、6D 23H）離內圈至少留 5 設計像素，LOGO 離內圈約 9（以前 1-3，快碰到）
  brandMark(f.brand, SI(60));                               // 離內環的空隙跟下面兩欄的字差不多（53 時貼著內環）
  snprintf(buf, sizeof buf, "%d", (int)(f.p5 + 0.5f));
  bigValue(buf, true, SI(104));

  // 兩欄：上面標籤（跟各自的環同色，超過門檻紅）、下面重置倒數（白）。倒數一律在「5-HOUR」下面，避免「5H · 16M」被看成 5 小時 16 分
  const int y1 = SI(143), y2 = SI(158);
  auto column = [&](int x, textdatum_t dt, const char* label, uint32_t lc, const String& left) {
    text(label, x, y1, lc, &fTiny.font, dt);
    if (left.length()) text(left.c_str(), x, y2, C_LABEL, &fTiny.font, dt);
  };
  const String l5 = fmtLeft(f.r5), lw = fmtLeft(f.rw);
  if (!hasWeek) column(CX, middle_center, "5-HOUR", col5, l5);
  else {
    snprintf(buf, sizeof buf, "WEEK %d%%", (int)(f.pw + 0.5f));
    // 兩欄整組置中：右欄比較寬（WEEK 100%）時中線往左移，兩邊離內環一樣遠（固定在中間時 WEEK 100% 會碰到內環）
    const int wl = max(textW("5-HOUR", &fTiny.font), textW(l5.c_str(), &fTiny.font));
    const int wr = max(textW(buf, &fTiny.font), textW(lw.c_str(), &fTiny.font));
    const int dx = (wl - wr) / 2, g = SI(6);
    band.fillRect(CX + dx + g_shX, y1 - SI(6) + g_shY - OY, S(1) < 1 ? 1 : (int)S(1), SI(32), C_LABEL3);   // 中間的分隔線
    column(CX + dx - g, middle_right, "5-HOUR", col5, l5);
    column(CX + dx + g, middle_left, buf, colW, lw);
  }
  pageDots(SI(196));
}

// 今日統計頁
static String fmtDur(uint32_t s) {
  char b[16];
  if (s < 60) snprintf(b, sizeof b, "%uS", (unsigned)s);
  else if (s < 3600) snprintf(b, sizeof b, "%uM", (unsigned)((s + 30) / 60));
  else snprintf(b, sizeof b, "%uH %02uM", (unsigned)(s / 3600), (unsigned)((s % 3600) / 60));
  return String(b);
}

static void drawTodayPage(uint32_t ms) {
  band.fillScreen(C_BG);
  statusRingBand(ms);
  const Frame& f = g_frame;
  text("TODAY", CX, SI(52), C_LABEL2, &fLabel.font);
  if (f.nToday == 0) {
    text("No Activity", CX, SI(112), C_LABEL, &fName.font);
    text("NOTHING GENERATED YET", CX, SI(146), C_LABEL3, &fTiny.font);
    pageDots(SI(200));
    return;
  }
  uint32_t maxS = 1, totS = 0; int totC = 0;
  for (int i = 0; i < f.nToday; i++) { if (f.today[i].secs > maxS) maxS = f.today[i].secs; totS += f.today[i].secs; totC += f.today[i].count; }
  const int rowH = SI(36), y0 = SI(122) - (f.nToday - 1) * rowH / 2 - SI(4);
  // 版面依實際字寬算：時間靠右對齊在圓裡（離外環留空），長條從 LOGO 右邊延伸到最寬的那個時間前面。
  // 以前長條固定 84、時間接在後面，「4H 44M」這種長的會切到外環
  int maxTW = 0;
  for (int i = 0; i < f.nToday; i++) maxTW = max(maxTW, textW(fmtDur(f.today[i].secs).c_str(), &fLabel.font));
  const float RS = S(98);                                   // 字和圖示不超出這個半徑（外環從 110.5 開始）
  float half = RS;                                          // 每一列都放得下的最小半寬
  for (int i = 0; i < f.nToday; i++) {
    const float dy = (y0 + i * rowH) - CY;
    const float dy2 = fabsf(dy) + SI(10);                   // 字有高度：用字的上 / 下緣算
    half = min(half, sqrtf(max(0.0f, RS * RS - dy2 * dy2)));
  }
  const int txr = CX + (int)half;                           // 時間的右緣
  const int lx = CX - (int)half + LOGO_N / 2;               // LOGO 中心
  const int bx = lx + LOGO_N / 2 + SI(7), bw = txr - maxTW - SI(8) - bx;   // 長條：LOGO 右邊到最寬的時間前面
  for (int i = 0; i < f.nToday; i++) {
    const Frame::Today& t = f.today[i];
    const Brand& b = BRANDS[t.brand];
    int y = y0 + i * rowH;
    // 品牌 LOGO（官方圖示；跟字一樣跟著防烙印挪動）
    if (t.brand < 3) band.pushImage(lx + g_shX - LOGO_N / 2, y + g_shY - LOGO_N / 2 - OY, LOGO_N, LOGO_N, (const lgfx::rgb565_t*)LOGOS[t.brand]);
    else band.fillSmoothCircle(lx + g_shX, y + g_shY - OY, S(9), b.color);
    // 生成時間長條
    band.fillSmoothRoundRect(bx + g_shX, y - SI(4) + g_shY - OY, bw, SI(8), SI(4), mix(C_BG, b.color, 0.12f));   // 底色暗一點（白色的 ChatGPT 0.22 時看起來像整條滿的）
    int w = (int)((float)bw * t.secs / maxS + 0.5f);
    if (w < SI(8)) w = SI(8);
    band.fillSmoothRoundRect(bx + g_shX, y - SI(4) + g_shY - OY, w, SI(8), SI(4), b.color);
    text(fmtDur(t.secs).c_str(), txr, y + SI(1), C_LABEL, &fLabel.font, middle_right);
    char c[20]; snprintf(c, sizeof c, "%d %s", t.count, t.count == 1 ? "REPLY" : "REPLIES");
    text(c, bx, y + SI(15), C_LABEL2, &fTiny.font, middle_left);
  }
  char tot[32];
  snprintf(tot, sizeof tot, "TOTAL %d  ·  %s", totC, fmtDur(totS).c_str());
  text(tot, CX, y0 + (f.nToday - 1) * rowH + SI(38), C_LABEL2, &fTiny.font);
  pageDots(SI(210));
}

static void drawScene(uint32_t ms) {
  if (g_otaPct >= 0) {                                      // 經 WiFi 更新韌體：藍色進度環 + 百分比
    band.fillScreen(C_BG);
    progressRing(R_USAGE, HW_USAGE, g_otaPct, C_BLUE);
    char b[8];
    snprintf(b, sizeof b, "%d", g_otaPct);
    bigValue(b, true, SI(106));
    text("UPDATING", CX, SI(145), C_LABEL2, &fLabel.font);
    text("KEEP POWER ON", CX, SI(172), C_LABEL3, &fTiny.font);
    return;
  }
  if (g_saver) { drawClock(ms, false); return; }
  if (g_page == 1 && linkAlive()) { drawUsagePage(ms); return; }
  if (g_page == 2 && linkAlive()) { drawTodayPage(ms); return; }
  if (g_page == 3 || g_page == 2) { drawClock(ms, true); pageDots(SI(208)); return; }
  band.fillScreen(C_BG);

  if (!linkAlive()) {
    statusRingBand(ms);
    text("AI Monitor", CX, SI(76), C_LABEL2, &fName.font);
    bigValue("--", false, SI(113));
    text(g_everConnected ? "Disconnected" : "Not Connected", CX, SI(150), C_LABEL2, &fLabel.font);
    text(g_netHint ? g_netHint : "PLUG IN USB", CX, SI(172), C_LABEL3, &fTiny.font);
    return;
  }

  const Frame& f = g_frame;
  const Brand& b = BRANDS[f.brand];
  bool hasAI = f.brand != N_BRANDS - 1;
  bool hasUsage = f.p5 >= 0;
  int bx, by, bw, bh;                                       // Clawd 的範圍（只用來判斷要不要畫）

  statusRingBand(ms);

  // 有 AI 時一律用額度的版面。還沒讀到額度（例如 AIMonitor 剛重開、ChatGPT 額度晚一分鐘才讀到）時只畫軌道、數字寫「--」，
  // 讀到後 gaugeTick 從 0 捲上去。以前沒有額度時改顯示經過時間（0:15 ELAPSED），使用者不要、也看不懂為什麼跳成那樣
  if (hasAI) {
    const float p5 = hasUsage ? p5View() : 0;               // 額度變了會慢慢跑過去（韌體 gaugeTick）
    uint32_t uc = f.p5 >= g_redAt ? C_RED : b.color;        // 顏色看目標值、不看捲動中的值：捲過門檻時整圈突然變色很醜（切換 AI 時最明顯）
    progressRing(R_USAGE, HW_USAGE, p5, uc);

    if (!clawdBox(bx, by, bw, bh)) brandMark(f.brand, SI(54));   // 官方 LOGO 取代名稱（比名稱文字高一點，跟數字留空間）
    else clawdDraw(ms);                                     // AMOLED：Claude 換成會動的 Clawd
    if (hasUsage) pctDraw();
    else bigValue("--", false, SI(106));
    statusLine(f.st, SI(145), ms);

    String cap;
    if (f.note.length()) cap = f.note;
    else if (f.msg.length()) cap = f.msg;
    else {
      String left = hasUsage ? fmtLeft(f.r5) : String("");
      cap = left.length() ? String("RESETS ") + left : String("");
    }
    uint32_t capCol = f.note.length() ? (f.noteKind == 2 ? C_GREEN : C_YELLOW) : f.msg.length() ? stColor(f.st) : C_LABEL3;
    char wk[24] = "";
    if (f.pw >= 0) snprintf(wk, sizeof wk, "WEEK %d%%", (int)(f.pw + 0.5f));   // 每週額度：只顯示文字
    const uint32_t wkCol = f.pw >= g_redAt ? C_RED : C_LABEL2;
    // 下面兩行（WEEK、提醒 / 錯誤 / 重置倒數）的寬度依圓算：字的上下緣離額度環內緣（98.5）留 5。
    // 最下面那行最窄：放不下就跟 WEEK 交換（上面那行比較寬）；還放不下就只顯示放得下的完整單字（不加「…」、不拆兩行）。
    // 以前固定寬 150，長的訊息會碰到額度環；截成「USAGE LIMIT RE...」或拆兩行時第二行被截一半，使用者不要看到半句話
    const int yA = SI(167), yB = SI(185), wA = circleW(yA, SI(7), S(93.5f)), wB = circleW(yB, SI(7), S(93.5f));
    const int cw = textW(cap.c_str(), &fTiny.font);
    if (!cap.length() || cw <= wB) {
      if (wk[0]) text(wk, CX, yA, wkCol, &fTiny.font);
      if (cap.length()) text(cap.c_str(), CX, yB, capCol, &fTiny.font);
    } else {
      String line = cap;
      if (cw > wA) {   // 太長：從後面找第一個讓整行放得下的空白，去掉結尾的標點
        line = "";
        for (int i = cap.length() - 1; i > 0; i--)
          if (cap[i] == ' ' && textW(cap.substring(0, i).c_str(), &fTiny.font) <= wA) { line = cap.substring(0, i); break; }
        while (line.length() && strchr(",;:-", line[line.length() - 1])) line = line.substring(0, line.length() - 1);
      }
      if (line.length()) text(line.c_str(), CX, yA, capCol, &fTiny.font);
      if (wk[0]) text(wk, CX, yB, wkCol, &fTiny.font);
    }
    if (f.nOthers > 0) othersRow(f);
  } else {
    text("No AI Tab", CX, SI(56), C_LABEL2, &fName.font);   // 沒有任何 AI 開著
    bigValue("--", false, SI(106));
    statusLine(f.st, SI(148), ms);
    String cap = f.note.length() ? f.note : f.msg.length() ? f.msg : String("OPEN CHATGPT, CLAUDE OR GEMINI");
    uint32_t capCol = f.note.length() ? (f.noteKind == 2 ? C_GREEN : C_YELLOW) : f.msg.length() ? stColor(f.st) : C_LABEL2;
    text(fitText(cap, SI(170), &fTiny.font).c_str(), CX, SI(173), capCol, &fTiny.font);
    if (f.nOthers > 0) othersRow(f);
  }
}

// 整個畫面是否需要每秒重畫（其他頁的倒數）；狀態頁由 viewKey 決定
static bool animating() {
  if (g_saver) return false;              // 螢幕保護每 5 秒更新就夠（時鐘到分鐘）
  return g_page != 0;
}
