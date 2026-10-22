#pragma once
// 畫面繪製（韌體與電腦預覽程式共用）
// 使用前需先定義：LGFX_Sprite band; String; millis()
//
// Apple Watch 活動圓環風格：
//   外環 = 狀態燈（生成中呼吸、完成恆亮、需審核快呼吸、錯誤閃爍）
//   內環 = 5 小時額度（有資料時）
//   中央 = AI 名稱、大數字、狀態

#include "fonts.h"
#ifndef SAVER_MIN
#define SAVER_MIN 5
#endif

static const int W = 240, H = 240, CX = 120, CY = 120, BAND_H = 40;   // 分 6 條畫（每條很快，中間穿插呼吸燈）
static int OY = 0;                 // 240x40 緩衝分 6 條畫，目前在畫哪一條

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
static uint32_t g_lastFrameMs = 0;
static bool g_everConnected = false;
static int32_t g_epochOffset = 0;
static const uint32_t LINK_TIMEOUT_MS = 20000;
static uint32_t g_stateSinceMs = 0;
static uint32_t g_stateSinceBase = 0;
static uint32_t g_stChangedMs = 0;
static int32_t g_tz = 8 * 3600;        // 時區（秒），由電腦端提供；預設台灣
static uint32_t g_lastActiveMs = 0;    // 最後一次「有事」的時間（螢幕保護用）
static bool g_saver = false;
static int g_saverMin = SAVER_MIN;     // 0 = 關閉；可由電腦端設定
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

static void text(const char* s, int x, int y, uint32_t col, const lgfx::IFont* font, textdatum_t datum = middle_center) {
  band.setFont(font);
  band.setTextDatum(datum);
  band.setTextColor(col);
  band.drawString(s, x, y - OY);
}
static int textW(const char* s, const lgfx::IFont* font) { band.setFont(font); return band.textWidth(s); }

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
static const int RS_OUT = 119, RS_IN = 113;           // 環本體
static const float R_USAGE  = 104.5f, HW_USAGE = 6.0f; // 內環：5 小時額度（98.5 ~ 110.5，緊貼外環）

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
static const float RING_R0 = 111.0f, RING_R1 = 121.0f;   // 外環區域（環內緣 ~ 螢幕邊）
static const int RING_LV = 40;                              // 距離等級數（(RING_R1-RING_R0)*4）
struct RingSeg { int16_t x0, n; uint16_t off; };
static RingSeg g_rseg[H][2];
static uint8_t g_rsegN[H];
static uint8_t* g_rlv = nullptr;                            // 每個外環像素的距離等級
static bool g_ringDirect = false;                          // 韌體：外環直接畫到螢幕（sprite 這區留空）
static bool g_ringKeyed = false;                           // 這次畫面有沒有把外環區域留空
static const uint16_t RING_KEY = 0x0821;                   // 「透明」用的顏色（幾乎黑）

static void ringInit() {
  if (g_rlv) return;
  // 先數像素
  int total = 0;
  for (int y = 0; y < H; y++) {
    float dy = y + 0.5f - CY;
    if (fabsf(dy) >= RING_R1) continue;
    float xo = sqrtf(RING_R1 * RING_R1 - dy * dy);
    if (fabsf(dy) < RING_R0) { float xi = sqrtf(RING_R0 * RING_R0 - dy * dy); total += 2 * ((int)ceilf(xo - xi) + 2); }
    else total += (int)(2 * xo) + 4;
  }
  g_rlv = (uint8_t*)malloc(total);
  if (!g_rlv) return;
  int off = 0;
  for (int y = 0; y < H; y++) {
    g_rsegN[y] = 0;
    float dy = y + 0.5f - CY;
    if (fabsf(dy) >= RING_R1) continue;
    float xo = sqrtf(RING_R1 * RING_R1 - dy * dy);
    int L = (int)floorf(CX - xo), R = (int)ceilf(CX + xo) - 1;
    if (L < 0) L = 0;
    if (R > W - 1) R = W - 1;
    auto addSeg = [&](int x0, int x1) {
      if (x1 < x0) return;
      RingSeg& sg = g_rseg[y][g_rsegN[y]++];
      sg.x0 = x0; sg.n = x1 - x0 + 1; sg.off = off;
      for (int x = x0; x <= x1; x++) {
        float dx = x + 0.5f - CX, d = sqrtf(dx * dx + dy * dy);
        int lv = (int)((d - RING_R0) * 4);
        g_rlv[off++] = lv < 0 ? 0 : (lv >= RING_LV ? RING_LV - 1 : lv);
      }
    };
    if (fabsf(dy) < RING_R0) {
      float xi = sqrtf(RING_R0 * RING_R0 - dy * dy);
      int a = (int)floorf(CX - xi + 0.5f), b = (int)ceilf(CX + xi - 0.5f) - 1;   // 內圈以內不碰
      addSeg(L, a - 1);
      addSeg(b + 1, R);
    } else addSeg(L, R);
  }
}

// 依目前狀態算 64 色調色盤（距離 → 顏色）
static void ringPalette(uint32_t ms, uint16_t* pal) {
  uint32_t col; float k;
  ringParams(ms, col, k);
  uint32_t track = mix(C_BG, col, (linkAlive() && g_frame.st != S_IDLE) ? 0.16f : 0.22f);
  uint32_t body = k <= 0.01f ? track : mix(track, col, k);
  const float E1 = RS_IN - 0.5f, E2 = RS_OUT + 0.5f;          // 環本體 112.5 ~ 119.5
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
  static uint16_t line[W];
  ringPalette(ms, pal);
  for (int y = yoff; y < yoff + rows && y < H; y++) {
    for (int s = 0; s < g_rsegN[y]; s++) {
      const RingSeg& sg = g_rseg[y][s];
      const uint8_t* lv = g_rlv + sg.off;
      for (int i = 0; i < sg.n; i++) line[i] = pal[lv[i]];
      g.pushImage(sg.x0, y - yoff, sg.n, 1, (const lgfx::rgb565_t*)line);
    }
  }
}

// 畫面（sprite）裡的外環：韌體模式下只留空（之後直接畫到螢幕），預覽程式則直接畫進去
static void statusRingBand(uint32_t ms) {
  if (!g_ringDirect) { ringDraw(band, OY, BAND_H, ms); return; }
  ringInit();
  if (!g_rlv) return;
  for (int y = OY; y < OY + BAND_H; y++)
    for (int s = 0; s < g_rsegN[y]; s++)
      band.drawFastHLine(g_rseg[y][s].x0, y - OY, g_rseg[y][s].n, RING_KEY);
  g_ringKeyed = true;
}

// 整圈直接畫到螢幕（呼吸燈每一格）
static void statusRingOn(lgfx::LovyanGFX& g, int yoff, uint32_t ms) { ringDraw(g, yoff, H, ms); }

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
static void progressRingSlow(float r, float hw, float pct, uint32_t col) {
  uint32_t track = mix(C_BG, col, 0.20f);
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
struct GaugeLut {
  float r0 = 0;              // 涵蓋 r0 ~ r0 + 2 * (hw + 1)（環寬 + 抗鋸齒）
  RingSeg seg[H][2];
  uint8_t segN[H];
  uint16_t* ang = nullptr;   // 角度，1/16 度（0 = 12 點鐘，順時針）
  uint8_t* dist = nullptr;   // 距離 - r0，1/16 px
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
        int dv = (int)((d - L.r0) * 16 + 0.5f);
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
  gaugeLut(R_USAGE - 14, HW_USAGE);
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
  const uint32_t track = mix(C_BG, col, 0.20f), c0 = mix(col, C_BG, 0.25f);
  const float p = pct > 100 ? 100 : pct;
  const int rr = (int)((r - L->r0) * 16 + 0.5f);          // 環的中心線
  const int edge = (int)((hw + GAUGE_AA) * 16 + 0.5f);     // 距中心線這麼遠以外完全透明
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
        int aR = (edge - dd) * 16;                          // 環的內外緣抗鋸齒（256 = 不透明）
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
  int uw = percent ? textW("%", &fUnit.font) + 2 : 0;
  int x = CX - (w + uw) / 2;
  text(num, x, y, C_LABEL, &fNum.font, middle_left);
  if (percent) text("%", x + w + 2, y + 8, C_LABEL2, &fUnit.font, middle_left);
}

static void statusLine(St st, int y, uint32_t ms) {
  const char* s = ST_TITLE[st];
  uint32_t col = st == S_IDLE ? C_LABEL2 : stColor(st);
  int tw = textW(s, &fLabel.font);
  int x = CX - (tw + 12) / 2;
  band.fillSmoothCircle(x + 4, y - OY, 3.5f, col);
  text(s, x + 12, y + 1, col, &fLabel.font, middle_left);
}

static void othersRow(const Frame& f) {
  // 其他對話：排在額度環下方的缺口裡（同一個半徑、跟環一樣粗），只用顏色（綠 = 完成、黃 = 需審核、紅 = 錯誤），空心 = 生成中
  // 順序由電腦端排好（要你處理的在前），從左邊開始排
  const float r = HW_USAGE, step = 16.0f / R_USAGE * RAD_TO_DEG;
  for (int i = 0; i < f.nOthers; i++) {
    float deg = 90 + ((f.nOthers - 1) / 2.0f - i) * step;
    float x = px(R_USAGE, deg), y = py(R_USAGE, deg) - OY;
    band.fillSmoothCircle(x, y, r, stColor(f.others[i].st));
    if (f.others[i].st == S_WORKING) band.fillSmoothCircle(x, y, r - 2.2f, C_BG);
  }
}

// ---------------- 螢幕保護（Apple Watch 風格時鐘）----------------
#ifndef SAVER_MIN
#define SAVER_MIN 5
#endif
static bool saverWanted() {
  if (g_saverMin <= 0) return false;
  if (millis() - g_lastActiveMs < (uint32_t)g_saverMin * 60000UL) return false;
  if (!linkAlive()) return true;
  return g_frame.st == S_IDLE;            // 生成中 / 需審核 / 錯誤 / 完成 都不進入
}

static void drawClock(uint32_t ms, bool pageMode) {
  band.fillScreen(C_BG);
  if (pageMode) statusRingBand(ms);
  uint32_t now = nowEpoch();
  if (!now) {                             // 還不知道時間：只畫一個慢慢呼吸的小點
    band.fillSmoothCircle(CX, CY - OY, 4, mix(C_BG, C_GRAY, 0.2f + 0.4f * breath(ms, 5000)));
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
  // 整組（時間、日期、額度、重置）垂直置中；沒有額度資料時只有時間和日期
  const int dx = 0, dy = (linkAlive() && g_frame.p5 >= 0) ? -26 : -14;

  // 細細的 Claude 額度弧（螢幕保護時）
  if (!pageMode && linkAlive() && g_frame.p5 >= 0) {
    const Brand& b = BRANDS[g_frame.brand];
    float p = g_frame.p5 > 100 ? 100 : g_frame.p5;
    uint32_t col = mix(C_BG, p >= 90 ? C_RED : b.color, 0.55f);
    ringArc(112, 1.5f, GAUGE_START, GAUGE_START + GAUGE_SPAN, mix(C_BG, col, 0.25f), mix(C_BG, col, 0.25f));
    if (p > 0) ringArc(112, 1.5f, GAUGE_START, GAUGE_START + GAUGE_SPAN * p / 100, col, col);
  }

  char buf[16];
  snprintf(buf, sizeof buf, "%02d:%02d", hh, mm);
  text(buf, CX + dx, CY - 6 + dy, pageMode ? C_LABEL : 0xE6E6EA, &fClock.font);
  char db[24];
  snprintf(db, sizeof db, "%u/%u  %s", (unsigned)mon, (unsigned)dd, WD[days % 7]);
  text(db, CX + dx, CY + 36 + dy, pageMode ? C_LABEL2 : 0xA8A8B0, &fLabel.font);
  if (linkAlive() && g_frame.p5 >= 0) {
    snprintf(buf, sizeof buf, "%s %d%%", BRANDS[g_frame.brand].name, (int)(g_frame.p5 + 0.5f));
    text(buf, CX + dx, CY + 58 + dy, C_LABEL2, &fTiny.font);
    String left = fmtLeft(g_frame.r5);   // 5 小時額度多久後重置
    if (left.length()) text((String("RESETS ") + left).c_str(), CX + dx, CY + 74 + dy, C_LABEL2, &fTiny.font);
  }
}

static void pageDots(int y) {
  int gap = 10, x0 = CX - (N_PAGES - 1) * gap / 2;
  for (int i = 0; i < N_PAGES; i++)
    band.fillSmoothCircle(x0 + i * gap, y - OY, i == g_page ? 3 : 2.2f, i == g_page ? C_LABEL : C_LABEL3);
}

// 額度詳細頁：5 小時（外環）＋本週（內環）＋今天的 5 小時用量折線
static void drawUsagePage(uint32_t ms) {
  band.fillScreen(C_BG);
  statusRingBand(ms);
  const Frame& f = g_frame;
  const Brand& b = BRANDS[f.brand];
  if (f.p5 < 0) {
    text(f.brand == N_BRANDS - 1 ? "Usage" : b.name, CX, 92, f.brand == N_BRANDS - 1 ? C_LABEL2 : b.color, &fName.font);
    text("NO USAGE DATA", CX, 124, C_LABEL2, &fLabel.font);
    text("ONLY CLAUDE PROVIDES IT", CX, 146, C_LABEL3, &fTiny.font);
    pageDots(200);
    return;
  }
  uint32_t c5 = f.p5 >= 90 ? C_RED : b.color;
  uint32_t cw = f.pw >= 90 ? C_RED : mix(b.color, 0xFFFFFF, 0.5f);
  progressRing(R_USAGE, HW_USAGE, f.p5, c5);
  if (f.pw >= 0) progressRing(R_USAGE - 14, HW_USAGE, f.pw, cw);   // 半徑要跟 gaugeInit 一致

  // 由上往下：名稱、5 小時大數字、5 小時重置、本週＋重置；沒有本週時整組往下移，保持在環內置中
  bool hasWeek = f.pw >= 0;
  int dy = 14 + (hasWeek ? 0 : 16);
  int yWeek = 134 + dy;

  char buf[40];
  text(b.name, CX, 50 + dy, b.color, &fLabel.font);
  snprintf(buf, sizeof buf, "%d", (int)(f.p5 + 0.5f));
  bigValue(buf, true, 84 + dy);
  String l5 = fmtLeft(f.r5);   // 倒數一律寫 RESETS，避免「5H · 16M」被看成 5 小時 16 分
  snprintf(buf, sizeof buf, "5-HOUR%s%s", l5.length() ? " · RESETS " : "", l5.c_str());
  text(buf, CX, 116 + dy, C_LABEL2, &fTiny.font);

  // 本週分兩行：一行寫不下（內環裡的寬度不夠），也跟上面的 5 小時分得開
  if (hasWeek) {
    snprintf(buf, sizeof buf, "WEEK %d%%", (int)(f.pw + 0.5f));
    text(buf, CX, yWeek, cw, &fTiny.font);
    String lw = fmtLeft(f.rw);
    if (lw.length()) text((String("RESETS ") + lw).c_str(), CX, yWeek + 15, C_LABEL2, &fTiny.font);
  }
  pageDots(196);
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
  text("TODAY", CX, 52, C_LABEL2, &fLabel.font);
  if (f.nToday == 0) {
    text("No Activity", CX, 112, C_LABEL, &fName.font);
    text("NOTHING GENERATED YET", CX, 146, C_LABEL3, &fTiny.font);
    pageDots(200);
    return;
  }
  uint32_t maxS = 1, totS = 0; int totC = 0;
  for (int i = 0; i < f.nToday; i++) { if (f.today[i].secs > maxS) maxS = f.today[i].secs; totS += f.today[i].secs; totC += f.today[i].count; }
  const int rowH = 36, y0 = 122 - (f.nToday - 1) * rowH / 2 - 4;
  const int bx = 64, bw = 84;
  for (int i = 0; i < f.nToday; i++) {
    const Frame::Today& t = f.today[i];
    const Brand& b = BRANDS[t.brand];
    int y = y0 + i * rowH;
    // 品牌圓點
    band.fillSmoothCircle(48, y - OY, 9, b.color);
    text(b.letter, 48, y + 1, 0x000000, &fTiny.font);
    // 生成時間長條
    band.fillSmoothRoundRect(bx, y - 4 - OY, bw, 8, 4, mix(C_BG, b.color, 0.22f));
    int w = (int)((float)bw * t.secs / maxS + 0.5f);
    if (w < 8) w = 8;
    band.fillSmoothRoundRect(bx, y - 4 - OY, w, 8, 4, b.color);
    text(fmtDur(t.secs).c_str(), bx + bw + 8, y + 1, C_LABEL, &fLabel.font, middle_left);
    char c[20]; snprintf(c, sizeof c, "%d %s", t.count, t.count == 1 ? "REPLY" : "REPLIES");
    text(c, bx, y + 15, C_LABEL2, &fTiny.font, middle_left);
  }
  char tot[32];
  snprintf(tot, sizeof tot, "TOTAL %d  ·  %s", totC, fmtDur(totS).c_str());
  text(tot, CX, y0 + (f.nToday - 1) * rowH + 38, C_LABEL2, &fTiny.font);
  pageDots(210);
}

static void drawScene(uint32_t ms) {
  if (g_saver) { drawClock(ms, false); return; }
  if (g_page == 1 && linkAlive()) { drawUsagePage(ms); return; }
  if (g_page == 2 && linkAlive()) { drawTodayPage(ms); return; }
  if (g_page == 3 || g_page == 2) { drawClock(ms, true); pageDots(208); return; }
  band.fillScreen(C_BG);

  if (!linkAlive()) {
    statusRingBand(ms);
    text("AI Monitor", CX, 76, C_LABEL2, &fName.font);
    bigValue("--", false, 113);
    text(g_everConnected ? "Disconnected" : "Not Connected", CX, 150, C_LABEL2, &fLabel.font);
    text("PLUG IN USB", CX, 172, C_LABEL3, &fTiny.font);
    return;
  }

  const Frame& f = g_frame;
  const Brand& b = BRANDS[f.brand];
  bool hasAI = f.brand != N_BRANDS - 1;
  bool hasUsage = f.p5 >= 0;

  statusRingBand(ms);

  char num[12];
  if (hasUsage) {
    uint32_t uc = f.p5 >= 90 ? C_RED : b.color;
    progressRing(R_USAGE, HW_USAGE, f.p5, uc);

    text(b.name, CX, 60, b.color, &fName.font);
    snprintf(num, sizeof num, "%d", (int)(f.p5 + 0.5f));
    bigValue(num, true, 106);
    statusLine(f.st, 145, ms);

    // 每週額度：只顯示文字
    if (f.pw >= 0) {
      char w[24];
      snprintf(w, sizeof w, "WEEK %d%%", (int)(f.pw + 0.5f));
      text(w, CX, 167, f.pw >= 90 ? C_RED : C_LABEL2, &fTiny.font);
    }

    String cap;
    if (f.note.length()) cap = f.note;
    else if (f.msg.length()) cap = f.msg;
    else {
      String left = fmtLeft(f.r5);
      cap = left.length() ? String("RESETS ") + left : String("");
    }
    uint32_t capCol = f.note.length() ? (f.noteKind == 2 ? C_GREEN : C_YELLOW) : f.msg.length() ? stColor(f.st) : C_LABEL3;
    if (cap.length()) text(fitText(cap, 150, &fTiny.font).c_str(), CX, 185, capCol, &fTiny.font);
    if (f.nOthers > 0) othersRow(f);
  } else {
    text(hasAI ? b.name : "No AI Tab", CX, 56, hasAI ? b.color : C_LABEL2, &fName.font);
    if (hasAI) {
      uint32_t el = (millis() - g_stateSinceMs) / 1000 + g_stateSinceBase;
      if (el >= 3600) snprintf(num, sizeof num, "%u:%02u", (unsigned)(el / 3600), (unsigned)((el % 3600) / 60));
      else snprintf(num, sizeof num, "%u:%02u", (unsigned)(el / 60), (unsigned)(el % 60));
    } else {
      strcpy(num, "--");
    }
    bigValue(num, false, 106);
    statusLine(f.st, 148, ms);
    String cap = f.note.length() ? f.note : f.msg.length() ? f.msg : String(hasAI ? (f.st == S_IDLE ? "SINCE LAST REPLY" : "ELAPSED") : "OPEN CHATGPT, CLAUDE OR GEMINI");
    uint32_t capCol = f.note.length() ? (f.noteKind == 2 ? C_GREEN : C_YELLOW) : f.msg.length() ? stColor(f.st) : C_LABEL2;
    text(fitText(cap, 170, &fTiny.font).c_str(), CX, 173, capCol, &fTiny.font);
    if (f.nOthers > 0) othersRow(f);
  }
}

// 整個畫面是否需要每秒重畫（經過時間、倒數）
static bool animating() {
  if (g_saver) return false;              // 螢幕保護每 5 秒更新就夠（時鐘到分鐘）
  if (g_page != 0) return true;
  return linkAlive() && g_frame.p5 < 0 && g_frame.brand != N_BRANDS - 1;
}
