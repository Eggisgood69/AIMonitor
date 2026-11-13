// AMOLED 自訂動畫（取代主畫面的 Clawd）：電腦把影片轉成 ANIM_W×ANIM_H 的 RGB565 畫格傳過來，
// 存在 FFat（/anim.bin，重開機還在）並整份放進 PSRAM 播放。
// 協定：電腦送 {"anim":{"w","h","n","ms","crc","len"}} → 螢幕回 {"anim":"rdy"}，之後收 len 個位元組的原始資料（不是 JSON），
//       每 16 KB 回 {"ak":已收位元組}（電腦收到才送下一段；hwcdc 的接收緩衝滿了會直接丟資料，aimon.ino 設成 24 KB），收完驗 CRC32 回 {"anim":"ok","crc":…} 或 {"anim":"bad"}。
//       畫面的 "av" = 要顯示哪一份（CRC32，0 = Clawd）；hello 回報螢幕上有的那份（"av"）。
// 檔案格式：16 位元組標頭（"AIMA"、w、h、n、ms（各 uint16）、crc（uint32））+ 畫格
#pragma once

static bool g_ffat = false;               // FFat 掛載成功
static Stream* g_upSrc = nullptr;         // 正在傳動畫過來的連線（這條連線改收原始資料）
static uint8_t* g_upBuf = nullptr;
static uint32_t g_upLen = 0, g_upGot = 0, g_upAcked = 0, g_upCrc = 0, g_upLastMs = 0;
static uint16_t g_upN = 0, g_upMs = 0;
static File g_upFile;

static const uint32_t ANIM_ACK = 16384;   // 電腦每送這麼多就等回覆（要小於 Serial 的接收緩衝）

static uint32_t animCrc32(const uint8_t* p, uint32_t n) {
  uint32_t c = 0xFFFFFFFFu;
  while (n--) {
    c ^= *p++;
    for (int k = 0; k < 8; k++) c = (c >> 1) ^ (0xEDB88320u & (0u - (c & 1)));
  }
  return ~c;
}

static void animFreeUpload() {
  if (g_upFile) g_upFile.close();
  if (g_upBuf) heap_caps_free(g_upBuf);
  g_upBuf = nullptr;
  g_upSrc = nullptr;
}

// 開機時讀回上次存的動畫
static void animLoad() {
  g_ffat = FFat.begin(false);   // 不格式化（第一次傳動畫時才格式化，要好幾秒）
  if (!g_ffat) return;
  File f = FFat.open("/anim.bin", "r");
  if (!f) return;
  uint8_t h[16];
  if (f.read(h, 16) != 16 || memcmp(h, "AIMA", 4)) { f.close(); return; }
  const uint16_t w = h[4] | h[5] << 8, hh = h[6] | h[7] << 8, n = h[8] | h[9] << 8, ms = h[10] | h[11] << 8;
  const uint32_t crc = h[12] | h[13] << 8 | h[14] << 16 | (uint32_t)h[15] << 24;
  const uint32_t len = (uint32_t)w * hh * 2 * n;
  if (w != ANIM_W || hh != ANIM_H || !n || len + 16 != f.size()) { f.close(); return; }
  uint8_t* b = (uint8_t*)heap_caps_malloc(len, MALLOC_CAP_SPIRAM);
  if (!b) { f.close(); return; }
  const bool ok = f.read(b, len) == len;
  f.close();
  if (!ok || animCrc32(b, len) != crc) { heap_caps_free(b); return; }
  g_animBuf = (const uint16_t*)b;
  g_animN = n;
  g_animMs = ms < 20 ? 20 : ms;
  g_animCrc = crc;
}

static void animBegin(Stream* src, JsonObject a) {
  animFreeUpload();
  const uint16_t w = a["w"] | 0, h = a["h"] | 0, n = a["n"] | 0, ms = a["ms"] | 0;
  const uint32_t crc = a["crc"] | 0UL, len = a["len"] | 0UL;
  if (w != ANIM_W || h != ANIM_H || !n || len != (uint32_t)w * h * 2 * n) { sendLine(src, "{\"anim\":\"bad\",\"why\":\"size\"}"); return; }
  // 舊的先放掉（兩份同時放不下），傳完之前顯示 Clawd
  if (g_animBuf) { const uint16_t* old = g_animBuf; g_animBuf = nullptr; g_animN = 0; g_animCrc = 0; heap_caps_free((void*)old); }
  if (len + 512 * 1024 > heap_caps_get_free_size(MALLOC_CAP_SPIRAM)) { sendLine(src, "{\"anim\":\"bad\",\"why\":\"mem\"}"); return; }
  g_upBuf = (uint8_t*)heap_caps_malloc(len, MALLOC_CAP_SPIRAM);
  if (!g_upBuf) { sendLine(src, "{\"anim\":\"bad\",\"why\":\"mem\"}"); return; }
  if (!g_ffat) g_ffat = FFat.begin(true);   // 第一次：格式化（幾秒，畫面會停一下）
  if (g_ffat) {
    g_upFile = FFat.open("/anim.tmp", "w");
    if (g_upFile) {
      uint8_t hd[16] = {'A', 'I', 'M', 'A', (uint8_t)w, (uint8_t)(w >> 8), (uint8_t)h, (uint8_t)(h >> 8), (uint8_t)n, (uint8_t)(n >> 8),
                        (uint8_t)ms, (uint8_t)(ms >> 8), (uint8_t)crc, (uint8_t)(crc >> 8), (uint8_t)(crc >> 16), (uint8_t)(crc >> 24)};
      g_upFile.write(hd, 16);
    }
  }
  g_upLen = len; g_upGot = 0; g_upAcked = 0; g_upCrc = crc; g_upN = n; g_upMs = ms;
  g_upLastMs = millis();
  g_upSrc = src;
  sendLine(src, "{\"anim\":\"rdy\"}");
}

static void animFinish() {
  Stream* src = g_upSrc;
  char b[64];
  if (animCrc32(g_upBuf, g_upLen) != g_upCrc) {
    animFreeUpload();
    sendLine(src, "{\"anim\":\"bad\",\"why\":\"crc\"}");
    return;
  }
  bool saved = false;
  if (g_upFile) {
    g_upFile.close();
    FFat.remove("/anim.bin");
    saved = FFat.rename("/anim.tmp", "/anim.bin");
  }
  g_animBuf = (const uint16_t*)g_upBuf;
  g_animN = g_upN;
  g_animMs = g_upMs < 20 ? 20 : g_upMs;
  g_animCrc = g_upCrc;
  g_upBuf = nullptr;
  g_upSrc = nullptr;
  snprintf(b, sizeof b, "{\"anim\":\"ok\",\"crc\":%lu,\"saved\":%d}", (unsigned long)g_animCrc, saved ? 1 : 0);
  sendLine(src, b);
}

// 收原始資料（pollReader 在這條連線正在傳動畫時呼叫）
static void animRecv(Stream* s) {
  int avail;
  while (g_upSrc == s && (avail = s->available()) > 0) {
    const uint32_t want = min((uint32_t)avail, min(g_upLen - g_upGot, (uint32_t)4096));
    const size_t n = s->readBytes(g_upBuf + g_upGot, want);
    if (!n) break;
    if (g_upFile && g_upFile.write(g_upBuf + g_upGot, n) != n) g_upFile.close();   // 存不進去：只放 PSRAM（重開機要再傳）
    g_upGot += n;
    g_upLastMs = millis();
    if (g_upGot == g_upLen) { animFinish(); return; }
    if (g_upGot - g_upAcked >= ANIM_ACK) {
      char b[32];
      snprintf(b, sizeof b, "{\"ak\":%lu}", (unsigned long)g_upGot);
      sendLine(s, b);
      g_upAcked = g_upGot;
    }
  }
}

// 傳到一半斷了：5 秒沒資料就放棄，回到收 JSON
static void animTimeout(uint32_t now) {
  if (g_upSrc && (int32_t)(now - g_upLastMs) > 5000) animFreeUpload();
}
