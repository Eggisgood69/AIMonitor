#pragma once
// USB 版不需要 Wi-Fi 設定，一般不用改這個檔。

// 背光亮度 0-255（長按 BOOT 鍵可循環切換）
#define BRIGHTNESS     160

// 螢幕保護：待命多久後進入（分鐘），以及進入後的背光亮度
#define SAVER_MIN      5
#define SAVER_BRIGHT   28

// SPI 時脈。畫面有雜訊就改成 40000000
#define LCD_SPI_HZ     80000000

// 板子腳位（ESP32-2424S012）
#define PIN_LCD_SCLK   6
#define PIN_LCD_MOSI   7
#define PIN_LCD_DC     2
#define PIN_LCD_CS     10
#define PIN_LCD_BL     3
#define PIN_BTN        9    // 板上的 BOOT 鍵

// 外接 WS2812（單顆，選用）：DIN 接背面 4-pin 排座的 RX（GPIO20），電源接 3.3V / GND。
// 由電腦端設定亮度（0 = 關閉）；只在走內建 USB 連線時才會接管 RX 腳。
#define PIN_LED        20
#define LED_SAVER_PCT  30   // 螢幕保護時 LED 亮度再乘上這個 %
