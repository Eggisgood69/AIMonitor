// 電腦端畫面預覽：用與韌體相同的 ui.h 渲染成 PPM（build-preview.ps1 編譯；-DLCD_W=466 = AMOLED 版）
#define LGFX_USE_V1
#include <LovyanGFX.hpp>
#include <string>
#include <cstdio>
#include <cmath>
#include <cstring>
struct String : public std::string {
  String() {}
  String(const char* s) : std::string(s ? s : "") {}
  String(const std::string& s) : std::string(s) {}
  void remove(size_t i) { erase(i); }
};
static uint32_t g_ms = 100000;
static uint32_t millis() { return g_ms; }
#define PI 3.14159265358979f
#define DEG_TO_RAD 0.017453292519943295f
#define RAD_TO_DEG 57.29577951308232f
// Arduino 的 max / min 可以混用型別
template <class A, class B> static auto max(A a, B b) -> decltype(a + b) { return a > b ? a : b; }
template <class A, class B> static auto min(A a, B b) -> decltype(a + b) { return a < b ? a : b; }
static LGFX_Sprite band;
#include "../aimon/ui.h"

// 分條畫完整張畫面，存成 PPM
static void render(const char* fn) {
  static uint8_t img[W * H * 3];
  for (OY = 0; OY < H; OY += BAND_H) {
    drawScene(g_ms);
    for (int y = 0; y < BAND_H && y + OY < H; y++) for (int x = 0; x < W; x++) {
      auto c = band.readPixelRGB(x, y);
      uint8_t* p = &img[((y + OY) * W + x) * 3]; p[0] = c.R8(); p[1] = c.G8(); p[2] = c.B8();
    }
  }
  FILE* fp = fopen(fn, "wb"); fprintf(fp, "P6 %d %d 255\n", W, H); fwrite(img, 1, sizeof img, fp); fclose(fp);
}

int main() {
  band.setColorDepth(16);
  band.createSprite(W, BAND_H);
  loadFonts();
  struct Sc { const char* file; const char* ai; St st; float p5, pw; const char* msg; int age; int no; bool link; uint32_t ms; };
  // 各種狀態用 Claude 展示；ai_*.ppm 是三家 AI 各一張（官方 LOGO）
  Sc sc[] = {
    {"ai_claude.ppm",  "claude",  S_WORKING, 38, 52, "", 42, 0, true, 101300},
    {"ai_chatgpt.ppm", "chatgpt", S_REVIEW,  40,  6, "", 18, 0, true, 100700},
    {"ai_gemini.ppm",  "gemini",  S_DONE,     3,  6, "", 95, 0, true, 100000},
    {"review.ppm",  "claude",  S_REVIEW,  45, 63, "", 12, 0, true, 100700},
    {"working.ppm", "claude",  S_WORKING, 38, 52, "", 42, 4, true, 101300},
    {"done.ppm",    "claude",  S_DONE,     4, 30, "", 95, 1, true, 100000},
    {"error.ppm",   "claude",  S_ERROR,   97, 80, "Usage limit", 3, 0, true, 100700},
    {"idle.ppm",    "claude",  S_IDLE,    23, 41, "", 300, 0, true, 100000},
    {"nolink.ppm",  "none",    S_IDLE,    -1, -1, "", 0, 0, false, 100000},
  };
  g_epochOffset = 1792218000 - 100;
  for (auto& s : sc) {
    Frame f; f.brand = brandIndex(s.ai); f.st = s.st; f.p5 = s.p5; f.pw = s.pw; f.msg = s.msg;
    f.r5 = 1792218000 + 2 * 3600 + 600;
    f.nOthers = s.no;
    if (!strcmp(s.file, "idle.ppm")) { f.note = "5H RESETS IN 4M"; f.noteKind = 1; }
    if (!strcmp(s.file, "done.ppm")) { f.note = "5H QUOTA RESET"; f.noteKind = 2; }
    // 小圓點：要你處理的在左邊（需審核 → 錯誤 → 完成 → 生成中）
    if (s.no == 1) { f.others[0].brand = 0; f.others[0].st = S_DONE; }
    if (s.no == 4) {
      f.others[0] = {0, S_REVIEW}; f.others[1] = {0, S_ERROR};
      f.others[2] = {0, S_DONE};   f.others[3] = {0, S_WORKING};
    }
    g_frame = f; g_ms = s.ms; g_stChangedMs = 0;
    g_lastFrameMs = s.link ? g_ms - 1000 : 0;
    g_stateSinceMs = g_ms; g_stateSinceBase = s.age;
    render(s.file);
  }

  // 額度詳細頁、時鐘頁
  for (int pg = 1; pg <= 3; pg++) {
    Frame f; f.brand = brandIndex("claude"); f.st = S_WORKING; f.p5 = 68; f.pw = 12;
    f.r5 = 1792226400 + 2 * 3600 + 600; f.rw = 1792226400 + 3 * 86400 + 4 * 3600;
    // 今日統計：三家都有（照秒數排，最多的在上面）
    f.today[0] = {brandIndex("claude"), 12, 1083}; f.today[1] = {brandIndex("chatgpt"), 7, 642};
    f.today[2] = {brandIndex("gemini"), 3, 158}; f.nToday = 3;
    g_frame = f; g_ms = 900000; g_lastFrameMs = g_ms - 500; g_page = pg;
    g_epochOffset = 1792226400 - g_ms / 1000;
    char fn[32]; snprintf(fn, sizeof fn, "page%d.ppm", pg);
    render(fn);
    g_page = 0;
  }
  // 螢幕保護
  {
    Frame f; f.brand = brandIndex("claude"); f.st = S_IDLE; f.p5 = 68; f.pw = 12;
    g_frame = f; g_ms = 900000; g_lastFrameMs = g_ms - 500; g_saver = true;
    g_epochOffset = 1792226400 - g_ms / 1000;   // 2026-10-17 16:40 (UTC+8)
    render("saver.ppm");
    g_saver = false;
  }
  // 動畫：生成中(慢呼吸) → 需審核(快呼吸) → 完成(恆亮) → 錯誤(雙閃)
  {
    struct Seg { const char* ai; St st; float p5; int frames; const char* msg; };
    Seg segs[] = { {"claude", S_WORKING, 45, 60, ""}, {"claude", S_REVIEW, 45, 33, ""}, {"claude", S_DONE, 46, 30, ""}, {"claude", S_ERROR, 46, 40, "Network error"} };
    int n = 0; uint32_t t = 200000;
    g_epochOffset = 1792218000 - t / 1000;   // 跟上面的狀態畫面同一個時間（重置倒數 2H 10M）
    for (auto& sg : segs) {
      Frame f; f.brand = brandIndex(sg.ai); f.st = sg.st; f.p5 = sg.p5; f.pw = sg.p5 >= 0 ? 63 : -1; f.msg = sg.msg;
      f.r5 = 1792218000 + 2 * 3600 + 600;
      g_frame = f; g_stChangedMs = t; g_stateSinceMs = t; g_stateSinceBase = 40;
      for (int i = 0; i < sg.frames; i++, n++) {
        g_ms = t; g_lastFrameMs = t - 100;
        char fn[32]; snprintf(fn, sizeof fn, "anim_%03d.ppm", n);
        render(fn);
        t += 80;
      }
    }
  }
}
