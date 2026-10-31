#pragma once
// USB 版不需要 Wi-Fi 設定，一般不用改這個檔。
// 支援兩種板子，依編譯目標自動選（build.ps1 會兩種都編）：
//   ESP32-C3 → ESP32-2424S012（GC9A01 1.28" 240×240 LCD）
//   ESP32-S3 → Waveshare ESP32-S3-Touch-AMOLED-1.75（CO5300 1.75" 466×466 AMOLED）

// 背光亮度 0-255（長按 BOOT 鍵可循環切換）
#define BRIGHTNESS     160

// 螢幕保護：待命多久後進入（分鐘），以及進入後的背光亮度
#define SAVER_MIN      5

#define LED_SAVER_PCT  30   // 螢幕保護時 LED 亮度再乘上這個 %

#if CONFIG_IDF_TARGET_ESP32S3
// ---------------- Waveshare ESP32-S3-Touch-AMOLED-1.75 ----------------
#define BOARD_ID       "s3a175"   // hello 回報給電腦端，用來選對的韌體
#define BOARD_AMOLED   1          // 沒有背光：亮度用面板指令；黑色像素不發光（防烙印）
#define LCD_W          466
#define LCD_EVEN       1          // CO5300：寫入範圍的 x、y、寬、高都要是偶數，不對齊畫面會亂掉
#define LISTEN_UART0   0          // 只有內建 USB，UART0 不接任何東西
#define SAVER_BRIGHT   16

// QSPI 時脈。畫面有雜訊就改成 40000000
#define LCD_SPI_HZ     40000000

#define PIN_LCD_CS     12
#define PIN_LCD_SCLK   38
#define PIN_LCD_D0     4
#define PIN_LCD_D1     5
#define PIN_LCD_D2     6
#define PIN_LCD_D3     7
#define PIN_LCD_RST    39
#define PIN_LCD_TE     13   // 面板開始掃描新的一張時送出脈衝（外環跟著它寫，才不會撕裂）
#define PIN_BTN        0    // 板上的 BOOT 鍵

// 外接 WS2812（單顆，選用）：DIN 接排座的 GPIO16，電源接 3.3V / GND
#define PIN_LED        16

#else
// ---------------- ESP32-2424S012（ESP32-C3）----------------
#define BOARD_ID       "c3"
#define BOARD_AMOLED   0
#define LCD_W          240
#define LISTEN_UART0   1          // 有的批次經 CH340 轉 UART0，USB 與 UART0 兩條都收
#define SAVER_BRIGHT   28

// SPI 時脈。畫面有雜訊就改成 40000000
#define LCD_SPI_HZ     80000000

#define PIN_LCD_SCLK   6
#define PIN_LCD_MOSI   7
#define PIN_LCD_DC     2
#define PIN_LCD_CS     10
#define PIN_LCD_BL     3
#define PIN_BTN        9    // 板上的 BOOT 鍵

// 外接 WS2812（單顆，選用）：DIN 接背面 4-pin 排座的 RX（GPIO20），電源接 3.3V / GND。
// 由電腦端設定亮度（0 = 關閉）；只在走內建 USB 連線時才會接管 RX 腳。
#define PIN_LED        20
#endif
