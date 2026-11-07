#pragma once
// AMOLED 的 USB 麥克風（電腦端設定視窗的「麥克風」開關）
//
// 平常開機一定是 hwcdc（USB-Serial-JTAG，硬體 USB：韌體當掉也還在，一鍵更新救得回來）。
// 電腦送 mic:1 才在執行中切成 TinyUSB：CDC（新的 COM 埠，序列埠照常）+ UAC1 麥克風（板上兩顆，ES7210）。
// mic:0 = 重新開機回 hwcdc。開關不存 NVS；當機重開也回 hwcdc，而且這次開機不再進麥克風模式（避免一直當機重開）。
//
// 踩過的坑（2026-11 實測，核心 2.0.9 / TinyUSB 0.15）：
//  - ES7210 的 I2C 寫入要 ≤ 50 kHz（100 / 400 kHz 時每個位元要前一位元也是 1 才寫得進去，讀沒問題）
//  - ES7210 要 4 槽 TDM 讀：槽 0 = MIC1、槽 2 = MIC2（槽 1 是喇叭回授、槽 3 空），標準 I2S 兩聲道讀不到
//  - TinyUSB CDC 預設把 DTR/RTS 切換（關開 COM 埠）當成「重開進燒錄模式」，之後一直停在燒錄模式 → enableReboot(false)
//  - 軟體重開後 RTC_CNTL_USB_CONF（PHY 選擇）、USB_SERIAL_JTAG_CONF0（上拉）還是 TinyUSB 時的設定（只有斷電會清）：
//    開機看到標記（RTC_NOINIT）就把 USB 交回 jtag，並把 D-/D+ 拉低一段時間逼電腦重新列舉；
//    只清 PHY 的話電腦以為還是舊裝置，沒恢復上拉的話電腦完全看不到板子（都要拔線才救得回來）
//  - TinyUSB 0.15 的 dcd 不幫 ISO IN 設奇偶 frame，也不處理錯過的同步封包（DWC2 把它一直掛著，串流整個停住）。
//    沒有畫面時剛好每 2 ms 送一包；正式韌體推 QSPI 畫面時常錯過 → 自己設奇偶、看門狗收拾錯過的（uacQueue / micUsbWatch）
#include "USB.h"
#include "USBCDC.h"
#include "esp32-hal-tinyusb.h"
#include "device/usbd_pvt.h"
#include "device/dcd.h"
#include "soc/usb_struct.h"
#include "soc/usb_reg.h"
#include "soc/rtc_cntl_reg.h"
#include "soc/usb_serial_jtag_reg.h"
#include "driver/periph_ctrl.h"
#include "driver/i2s.h"

#define MIC_RATE      48000
#define MIC_GAIN      10          // ES7210 類比增益預設值（電腦端的 mg 可以改）：0-10 每階 3 dB（10 = 30 dB），11 = 33、12 = 34.5、13 = 36、14 = 37.5 dB
#define MIC_PID       0x81A1      // 麥克風模式的 USB PID（跟 hwcdc 的 0x1001 不同，Windows 才不會用錯快取的描述元）
#define MIC_RING      4096        // 環形緩衝（格，一格 = 左右各一個 int16），約 85 ms

static bool g_micOn = false;      // 已經切到 TinyUSB 麥克風模式
static bool g_micBlocked = false; // 上次在麥克風模式下當機重開：這次開機不再進
static USBCDC* g_micSerial = nullptr;
static uint8_t g_micGain = MIC_GAIN;

// ---------- 開機：從麥克風模式重開時把 USB 交回 hwcdc ----------
#define MIC_MAGIC 0xA1C0FFEE
RTC_NOINIT_ATTR static uint32_t g_micMagic;   // 軟體重開、當機重開都還在，斷電後是亂數

static void micBootCheck() {
  if (g_micMagic != MIC_MAGIC) return;
  g_micMagic = 0;
  g_micBlocked = esp_reset_reason() != ESP_RST_SW;   // mic:0、15 秒沒被電腦認到的重開是 SW；其他 = 當機
  periph_module_reset(PERIPH_USB_MODULE);
  periph_module_disable(PERIPH_USB_MODULE);
  CLEAR_PERI_REG_MASK(RTC_CNTL_USB_CONF_REG, RTC_CNTL_SW_HW_USB_PHY_SEL | RTC_CNTL_SW_USB_PHY_SEL | RTC_CNTL_USB_PAD_ENABLE);
  CLEAR_PERI_REG_MASK(USB_SERIAL_JTAG_CONF0_REG, USB_SERIAL_JTAG_PHY_SEL | USB_SERIAL_JTAG_USB_PAD_ENABLE);
  pinMode(19, OUTPUT_OPEN_DRAIN); pinMode(20, OUTPUT_OPEN_DRAIN);   // USB D- / D+
  digitalWrite(19, LOW); digitalWrite(20, LOW);
  delay(300);                     // 電腦看到拔除，等一下會重新列舉成 hwcdc
  // 同 hal/usb_phy_ll.h 的 usb_phy_ll_int_jtag_enable（那個標頭檔 C++ 編不過）
  CLEAR_PERI_REG_MASK(USB_SERIAL_JTAG_CONF0_REG, USB_SERIAL_JTAG_PHY_SEL | USB_SERIAL_JTAG_PAD_PULL_OVERRIDE);
  SET_PERI_REG_MASK(USB_SERIAL_JTAG_CONF0_REG, USB_SERIAL_JTAG_DP_PULLUP | USB_SERIAL_JTAG_USB_PAD_ENABLE);
  SET_PERI_REG_MASK(RTC_CNTL_USB_CONF_REG, RTC_CNTL_SW_HW_USB_PHY_SEL);
  CLEAR_PERI_REG_MASK(RTC_CNTL_USB_CONF_REG, RTC_CNTL_SW_USB_PHY_SEL);
}

// ---------- ES7210（I2C 0x40）----------
static bool esW(uint8_t r, uint8_t v) {
  Wire.beginTransmission(I2C_ES7210);
  Wire.write(r); Wire.write(v);
  return Wire.endTransmission() == 0;
}

static bool es7210Init() {
  Wire.setClock(50000);           // 寫 ES7210 要慢（觸控、陀螺儀照常 400 kHz）
  bool ok = esW(0x00, 0xFF);      // 重置
  if (ok) {
    static const uint8_t seq[][2] = {
      {0x00, 0x41}, {0x01, 0x3F},
      {0x09, 0x30}, {0x0A, 0x30},                         // 上電時序
      {0x23, 0x2A}, {0x22, 0x0A}, {0x20, 0x0A}, {0x21, 0x2A},   // 高通濾波
      {0x08, 0x10},                                       // slave（LRCK / BCLK 由 ESP32 給）
      {0x40, 0xC3}, {0x41, 0x70}, {0x42, 0x70},           // 類比電源、麥克風偏壓 2.87 V
      {0x07, 0x20}, {0x02, 0xC1}, {0x04, 0x01}, {0x05, 0x00},   // MCLK = 256 fs
      {0x11, 0x60},                                       // 16 bit、標準 I2S
      {0x12, 0x02},                                       // TDM：MIC1、MIC3、MIC2、MIC4 四槽
      {0x01, 0x00},                                       // 時脈全開
      {0x4B, 0x00}, {0x4C, 0x00},                         // MIC1-4 電源
      {0x06, 0x00}, {0x40, 0x43},
      {0x47, 0x08}, {0x48, 0x08}, {0x49, 0x08}, {0x4A, 0x08},
      {0x00, 0x71}, {0x00, 0x41},
    };
    for (auto& s : seq) ok &= esW(s[0], s[1]);
    for (int i = 0; i < 4; i++) ok &= esW(0x43 + i, 0x10 | g_micGain);   // 增益（bit 4 = 啟用）
  }
  Wire.setClock(400000);
  return ok;
}

// 電腦端調增益（mg，0-14）：已經在錄音就馬上寫進 ES7210，不用重開
static void micSetGain(int g) {
  if (g < 0 || g > 14 || g == g_micGain) return;
  g_micGain = g;
  if (!g_micOn) return;           // 還沒開麥克風：開的時候 es7210Init 會用這個值
  Wire.setClock(50000);
  for (int i = 0; i < 4; i++) esW(0x43 + i, 0x10 | g_micGain);
  Wire.setClock(400000);
}

// ---------- 錄音：I2S（TDM 4 槽）→ 環形緩衝（只留 MIC1、MIC2）----------
static int16_t* g_micRing = nullptr;
static volatile uint32_t g_micWr = 0, g_micRd = 0;

// 音量（給螢幕的說話光暈）：每次讀進來的一段（240 格 = 5 ms）兩顆平均後的均方值
static volatile uint32_t g_micMs = 0;

static void micI2sTask(void*) {
  static int16_t buf[240 * 4];
  for (;;) {
    size_t got = 0;
    i2s_read(I2S_NUM_0, buf, sizeof(buf), &got, portMAX_DELAY);
    const int frames = got / 8;
    uint64_t sq = 0;
    for (int i = 0; i < frames; i++) {
      const int32_t m = ((int32_t)buf[i * 4] + buf[i * 4 + 2]) / 2;
      sq += (uint64_t)(m * m);
    }
    if (frames) g_micMs = (uint32_t)(sq / frames);
    for (int i = 0; i < frames; i++) {
      const uint32_t w = g_micWr;
      if (w - g_micRd >= MIC_RING) break;          // 電腦沒在收：丟掉（開始收時從最新的開始）
      g_micRing[(w % MIC_RING) * 2] = buf[i * 4];
      g_micRing[(w % MIC_RING) * 2 + 1] = buf[i * 4 + 2];
      g_micWr = w + 1;
    }
  }
}

static bool micI2sInit() {
  i2s_config_t cfg = {};
  cfg.mode = (i2s_mode_t)(I2S_MODE_MASTER | I2S_MODE_RX);
  cfg.sample_rate = MIC_RATE;
  cfg.bits_per_sample = I2S_BITS_PER_SAMPLE_16BIT;
  cfg.channel_format = I2S_CHANNEL_FMT_MULTIPLE;
  cfg.chan_mask = (i2s_channel_t)(I2S_TDM_ACTIVE_CH0 | I2S_TDM_ACTIVE_CH1 | I2S_TDM_ACTIVE_CH2 | I2S_TDM_ACTIVE_CH3);
  cfg.total_chan = 4;
  cfg.communication_format = I2S_COMM_FORMAT_STAND_I2S;
  cfg.dma_buf_count = 6;
  cfg.dma_buf_len = 240;
  cfg.mclk_multiple = I2S_MCLK_MULTIPLE_256;
  cfg.bits_per_chan = I2S_BITS_PER_CHAN_16BIT;
  if (i2s_driver_install(I2S_NUM_0, &cfg, 0, NULL) != ESP_OK) return false;
  i2s_pin_config_t pins = {};
  pins.mck_io_num = PIN_I2S_MCLK;
  pins.bck_io_num = PIN_I2S_BCLK;
  pins.ws_io_num = PIN_I2S_WS;
  pins.data_out_num = I2S_PIN_NO_CHANGE;
  pins.data_in_num = PIN_I2S_DIN;
  return i2s_set_pin(I2S_NUM_0, &pins) == ESP_OK;
}

// ---------- UAC1 麥克風（TinyUSB app driver）----------
#define UAC_DESC_LEN 99
#define UAC_PKT_MAX (100 * 4)     // 一包最多 100 格（2 ms 約 96 格）
static uint8_t g_uacItf, g_uacEp, g_uacAlt = 0, g_uacRhport = 0;
static uint8_t g_uacCtrl[4];
static uint8_t g_uacEpDesc[9];
WORD_ALIGNED_ATTR static uint8_t g_uacPkt[UAC_PKT_MAX];

static uint16_t uacDescriptor(uint8_t* dst, uint8_t* itf) {
  g_uacItf = *itf;
  const uint8_t as = g_uacItf + 1;
  g_uacEp = 0x80 | tinyusb_get_free_in_endpoint();
  const uint8_t str = tinyusb_add_string_descriptor("AIMonitor Mic");
  const uint8_t d[UAC_DESC_LEN] = {
    8, 0x0B, g_uacItf, 2, 0x01, 0x01, 0x00, 0,             // IAD
    9, 0x04, g_uacItf, 0, 0, 0x01, 0x01, 0x00, str,        // AC 介面
    9, 0x24, 0x01, 0x00, 0x01, 30, 0, 1, as,               // AC header：UAC 1.0、總長 30、一個串流介面
    12, 0x24, 0x02, 1, 0x01, 0x02, 0, 2, 0x03, 0x00, 0, 0, // Input Terminal：麥克風、2 聲道（左右）
    9, 0x24, 0x03, 2, 0x01, 0x01, 0, 1, 0,                 // Output Terminal：USB streaming
    9, 0x04, as, 0, 0, 0x01, 0x02, 0x00, 0,                // AS alt 0（不錄音）
    9, 0x04, as, 1, 1, 0x01, 0x02, 0x00, 0,                // AS alt 1（錄音）
    7, 0x24, 0x01, 2, 1, 0x01, 0x00,                       // AS general：PCM
    11, 0x24, 0x02, 0x01, 2, 2, 16, 1,                     // Type I：2 聲道、16 bit、一種取樣率
    (uint8_t)(MIC_RATE & 0xFF), (uint8_t)((MIC_RATE >> 8) & 0xFF), (uint8_t)(MIC_RATE >> 16),
    9, 0x05, g_uacEp, 0x05, (uint8_t)(UAC_PKT_MAX & 0xFF), (uint8_t)(UAC_PKT_MAX >> 8), 1, 0, 0,   // ISO IN（非同步）
    7, 0x25, 0x01, 0x00, 0, 0, 0,                          // CS 端點
  };
  memcpy(g_uacEpDesc, d + UAC_DESC_LEN - 16, 9);
  memcpy(dst, d, UAC_DESC_LEN);
  *itf += 2;
  return UAC_DESC_LEN;
}

// 同步傳輸（ISO IN）：封包要在指定奇偶的 frame 送出。TinyUSB 0.15 的 dcd 不設奇偶、也不處理「錯過了」
// （DWC2 會把那包一直掛著，串流整個停住）：送之前指定下一個 frame，錯過的由 micUsbWatch 收拾。
// 封包的間隔因此不固定（1 ms、偶爾更久）→ 每包送「距離上一包經過的時間」那麼多格，再依緩衝水位微調。
// 那個 dcd 也沒有 dcd_edpt_close（usbd_edpt_close 什麼都不做），而且每次 usbd_edpt_open 都再配置一塊 TX FIFO：
// 每開一次錄音就重開端點的話，第二次錄音起串流就壞了（一半錯過、電腦收不到資料）→ 端點只開一次，停止錄音時自己停掉掛著的那包
#define MIC_TARGET 480                 // 緩衝目標 10 ms（I2S 一次進來 240 格）
static volatile uint32_t g_uacQueuedUs = 0;   // 正在等送出的那包排進去的時間（0 = 沒有）
static uint32_t g_uacLastUs = 0;
static bool g_uacOpened = false;              // ISO 端點開過了（bus reset 之前不再開）
static volatile uint32_t g_uacMiss = 0;       // 上次回報之後錯過幾包
static volatile uint32_t g_uacPkts = 0;       // 上次回報之後送了幾包（正常每秒約 1000）
static volatile uint32_t g_uacMaxGap = 0;     // 上次回報之後，兩包之間最長隔多久（ms）
static SemaphoreHandle_t g_uacLock = nullptr; // usbd 任務（排封包、切 alt）與 micUsbWatch 互斥

static void uacQueue() {
  const uint32_t now = micros();
  uint32_t el = g_uacLastUs ? now - g_uacLastUs : 1000;
  g_uacLastUs = now;
  if (el > 3000) el = 3000;
  const int avail = (int)(g_micWr - g_micRd);
  int n = (int)((el * 48 + 500) / 1000) + (avail - MIC_TARGET) / 8;
  if (n > UAC_PKT_MAX / 4) n = UAC_PKT_MAX / 4;
  if (n > avail) n = avail;
  if (n < 0) n = 0;
  int16_t* p = (int16_t*)g_uacPkt;
  for (int i = 0; i < n; i++) {
    const uint32_t r = (g_micRd + i) % MIC_RING;
    p[i * 2] = g_micRing[r * 2]; p[i * 2 + 1] = g_micRing[r * 2 + 1];
  }
  g_micRd = g_micRd + n;
  const uint32_t fn = (USB0.dsts >> 8) & 0x3FFF;
  USB0.in_ep_reg[g_uacEp & 0x7F].diepctl |= ((fn + 1) & 1) ? USB_DI_SETD1PID1 : USB_DI_SETD0PID1;
  g_uacQueuedUs = now ? now : 1;
  usbd_edpt_xfer(g_uacRhport, g_uacEp, g_uacPkt, n * 4);
}

// 停掉掛著的那包：照 DWC2 的程序停用端點、清它的 TX FIFO，再告訴 TinyUSB 這包結束了（它才會讓我們排下一包）。
// 要拿著 g_uacLock
static void uacAbort() {
  usb_in_endpoint_t& ep = USB0.in_ep_reg[g_uacEp & 0x7F];
  if (ep.diepctl & USB_D_EPENA1) {
    ep.diepctl |= USB_DI_SNAK1;
    for (int i = 0; i < 1000 && !(ep.diepint & USB_D_INEPNAKEFF1_M); i++) delayMicroseconds(1);
    ep.diepctl |= USB_D_EPDIS1 | USB_DI_SNAK1;
    for (int i = 0; i < 1000 && !(ep.diepint & USB_D_EPDISBLD1_M); i++) delayMicroseconds(1);
    ep.diepint = USB_D_EPDISBLD1_M | USB_D_INEPNAKEFF1_M;
  }
  USB0.grstctl = (((ep.diepctl >> USB_D_TXFNUM1_S) & 0xF) << USB_TXFNUM_S) | USB_TXFFLSH_M;
  for (int i = 0; i < 1000 && (USB0.grstctl & USB_TXFFLSH_M); i++) delayMicroseconds(1);
  g_uacQueuedUs = 0;
  dcd_event_xfer_complete(g_uacRhport, g_uacEp, 0, XFER_RESULT_FAILED, false);
}

// 每 1 ms 看一次：那包排進去 4 ms 還沒送出 = 錯過了
static void micUsbWatch(void*) {
  for (;;) {
    vTaskDelay(1);
    const uint32_t q = g_uacQueuedUs;
    if (!g_uacAlt || !q || micros() - q < 4000) continue;
    xSemaphoreTake(g_uacLock, portMAX_DELAY);
    if (g_uacAlt && g_uacQueuedUs == q) {   // 拿到鎖之前可能剛好送出了
      g_uacMiss++;
      uacAbort();
    }
    xSemaphoreGive(g_uacLock);
  }
}

static void uacInit() {}
static void uacReset(uint8_t) { g_uacAlt = 0; g_uacOpened = false; g_uacQueuedUs = 0; }   // bus reset：dcd 會關掉所有端點
static uint16_t uacOpen(uint8_t rhport, tusb_desc_interface_t const* d, uint16_t maxLen) {
  if (d->bInterfaceClass != 0x01 || d->bInterfaceSubClass != 0x01) return 0;
  g_uacRhport = rhport;
  return UAC_DESC_LEN - 8 <= maxLen ? UAC_DESC_LEN - 8 : 0;   // AC 介面到最後（IAD 由 usbd 處理過）
}
static bool uacControl(uint8_t rhport, uint8_t stage, tusb_control_request_t const* req) {
  if (req->bmRequestType_bit.type == TUSB_REQ_TYPE_STANDARD && req->bmRequestType_bit.recipient == TUSB_REQ_RCPT_INTERFACE) {
    const uint8_t itf = tu_u16_low(req->wIndex);
    if (req->bRequest == TUSB_REQ_SET_INTERFACE) {
      if (stage != CONTROL_STAGE_SETUP) return true;
      const uint8_t alt = tu_u16_low(req->wValue);
      if (itf == g_uacItf + 1 && alt != g_uacAlt) {
        xSemaphoreTake(g_uacLock, portMAX_DELAY);
        g_uacAlt = alt;
        if (!alt) {
          if (g_uacQueuedUs) uacAbort();        // 停止錄音：停掉掛著的那包（端點不關，下次直接用）
        } else {
          if (!g_uacOpened) g_uacOpened = usbd_edpt_open(rhport, (tusb_desc_endpoint_t const*)g_uacEpDesc);
          const uint32_t w = g_micWr;
          g_micRd = w > MIC_TARGET ? w - MIC_TARGET : 0;   // 從最新的開始，留 10 ms 緩衝
          g_uacLastUs = 0;
          if (!g_uacQueuedUs) uacQueue();
        }
        xSemaphoreGive(g_uacLock);
      }
      return tud_control_status(rhport, req);
    }
    if (req->bRequest == TUSB_REQ_GET_INTERFACE) {
      if (stage != CONTROL_STAGE_SETUP) return true;
      g_uacCtrl[0] = itf == g_uacItf + 1 ? g_uacAlt : 0;
      return tud_control_xfer(rhport, req, g_uacCtrl, 1);
    }
    return false;
  }
  if (req->bmRequestType_bit.type == TUSB_REQ_TYPE_CLASS && req->bmRequestType_bit.recipient == TUSB_REQ_RCPT_ENDPOINT) {
    if (stage != CONTROL_STAGE_SETUP) return true;    // 取樣率：只有一種
    g_uacCtrl[0] = MIC_RATE & 0xFF; g_uacCtrl[1] = (MIC_RATE >> 8) & 0xFF; g_uacCtrl[2] = MIC_RATE >> 16;
    return tud_control_xfer(rhport, req, g_uacCtrl, 3);
  }
  return false;
}
static bool uacXfer(uint8_t, uint8_t ep, xfer_result_t result, uint32_t) {
  if (ep != g_uacEp) return true;
  xSemaphoreTake(g_uacLock, portMAX_DELAY);
  // 停掉的那包（FAILED）在這之前可能已經又排了新的一包（重新開始錄音）：那就不要再排
  if (g_uacAlt && (result == XFER_RESULT_SUCCESS || !g_uacQueuedUs)) {
    static uint32_t last = 0;
    const uint32_t ms = millis();
    if (last && ms - last > g_uacMaxGap) g_uacMaxGap = ms - last;
    last = ms;
    g_uacPkts++;
    uacQueue();
  }
  xSemaphoreGive(g_uacLock);
  return true;
}
static const usbd_class_driver_t g_uacDriver = {
#if CFG_TUSB_DEBUG >= 2
  "UAC1",
#endif
  uacInit, uacReset, uacOpen, uacControl, uacXfer, NULL
};
// TinyUSB 的弱符號：自訂的 class driver（arduino-cli 的函式原型產生器會弄壞 extern "C" 回傳指標的寫法，要包在大括號裡）
extern "C" {
const usbd_class_driver_t* usbd_app_driver_get_cb(uint8_t* count) {
  *count = 1;
  return &g_uacDriver;
}
}

// ---------- 切換 ----------
// USB.begin() 在 core 0 的任務裡做：USB 中斷配置在呼叫它的 core 上，loop（core 1）推 QSPI 畫面時
// 中斷會被拖延，同步傳輸的封包大多送不出去（實測錄 10 秒只拿到 1.7 秒）
static volatile int g_usbBeginRes = -1;
static void micUsbBeginTask(void*) {
  g_usbBeginRes = USB.begin() ? 1 : 0;
  vTaskDelete(NULL);
}

// 收到 mic:1 時呼叫：成功後序列埠改走 micStream()（新的 COM 埠）
static bool micEnter() {
  if (g_micOn || g_micBlocked) return g_micOn;
  g_micRing = (int16_t*)heap_caps_malloc(MIC_RING * 4, MALLOC_CAP_INTERNAL);
  if (!g_micRing || !es7210Init() || !micI2sInit()) { g_micBlocked = true; return false; }
  g_uacLock = xSemaphoreCreateMutex();
  xTaskCreatePinnedToCore(micI2sTask, "mic", 4096, NULL, 10, NULL, 0);
  xTaskCreatePinnedToCore(micUsbWatch, "micwd", 2048, NULL, 20, NULL, 0);
  Serial.flush();
  delay(50);
  tinyusb_enable_interface(USB_INTERFACE_CUSTOM, UAC_DESC_LEN, uacDescriptor);
  g_micSerial = new USBCDC(0);
  g_micSerial->setRxBufferSize(2048);           // 跟 hwcdc 一樣：一行畫面資料 400-600 位元組
  g_micSerial->enableReboot(false);             // 關開 COM 埠不要進燒錄模式
  g_micSerial->begin();
  USB.VID(0x303A);
  USB.PID(MIC_PID);
  USB.productName("AIMonitor");
  USB.manufacturerName("AIMonitor");
  g_micMagic = MIC_MAGIC;                       // 從現在起重開（含當機）要把 USB 交回 hwcdc
  xTaskCreatePinnedToCore(micUsbBeginTask, "usbon", 4096, NULL, 5, NULL, 0);
  for (int i = 0; i < 2000 && g_usbBeginRes < 0; i++) delay(1);
  g_micOn = g_usbBeginRes != 0;   // 還沒回來（-1）也當作切過去了：沒被電腦認到的話 micTick 15 秒後重開
  return g_micOn;
}

static Stream* micStream() { return g_micSerial; }

// 有程式正在錄音（電腦把串流打開了，例如按了 Win+H）
static bool micListening() { return g_micOn && g_uacAlt; }

// 現在的音量 0-1（給說話光暈）：-56 dBFS 以下（環境噪音約 -63）= 0，-36 dBFS = 1（正常講話約 -48，大約一半）
static float micLevel() {
  if (!micListening()) return 0;
  const float ms = (float)g_micMs;
  if (ms < 1) return 0;
  const float db = 10.0f * log10f(ms / (32768.0f * 32768.0f));
  const float l = (db + 56.0f) / 20.0f;
  return l < 0 ? 0 : (l > 1 ? 1 : l);
}

// 收到 mic:0：重開回 hwcdc（開機時 micBootCheck 會讓電腦重新列舉）
static void micLeave() {
  if (!g_micOn) return;
  g_micSerial->flush();
  delay(100);
  esp_restart();
}

// 切過去 15 秒還沒被電腦認到（例如 Windows 列舉失敗）：重開回 hwcdc
static void micTick(uint32_t ms) {
  static uint32_t since = 0;
  if (!g_micOn) return;
  if (tud_mounted()) since = 0;
  else if (!since) since = ms ? ms : 1;
  else if (ms - since > 15000) esp_restart();
}
