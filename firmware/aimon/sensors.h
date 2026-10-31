#pragma once
// AMOLED 板上的觸控（CST9217）與陀螺儀（QMI8658），共用 I2C（config.h 的 PIN_I2C_*）
// 協定參考 SensorLib（lewisxhe，MIT）的 TouchDrvCST92xx / SensorQMI8658
#include <Wire.h>

static bool i2cRead(uint8_t addr, const uint8_t* reg, int regLen, uint8_t* buf, int n) {
  Wire.beginTransmission(addr);
  Wire.write(reg, regLen);
  if (Wire.endTransmission(false) != 0) return false;
  if (Wire.requestFrom((int)addr, n) != n) return false;
  for (int i = 0; i < n; i++) buf[i] = Wire.read();
  return true;
}
static bool i2cWrite(uint8_t addr, const uint8_t* d, int n) {
  Wire.beginTransmission(addr);
  Wire.write(d, n);
  return Wire.endTransmission() == 0;
}
static bool i2cPresent(uint8_t addr) {
  Wire.beginTransmission(addr);
  return Wire.endTransmission() == 0;
}

static bool g_hasTouch = false, g_hasImu = false;
static volatile uint32_t g_tpIntMs = 0;         // 觸控最後一次送出 INT 脈衝的時間
static void IRAM_ATTR onTouchInt() { g_tpIntMs = millis(); }

static void sensorsInit() {
  pinMode(PIN_TP_RST, OUTPUT);
  digitalWrite(PIN_TP_RST, LOW); delay(10); digitalWrite(PIN_TP_RST, HIGH);
  Wire.begin(PIN_I2C_SDA, PIN_I2C_SCL, 400000);
  Wire.setTimeOut(20);
  delay(50);                                    // 觸控放開重置後要一點時間才回應
  g_hasTouch = i2cPresent(I2C_TOUCH);
  // 沒碰的時候一直讀，偶爾會卡住 1 秒（I2C 等不到回應）→ 只在 INT 脈衝之後讀（手指按著時會一直送脈衝）
  pinMode(PIN_TP_INT, INPUT_PULLUP);
  attachInterrupt(PIN_TP_INT, onTouchInt, FALLING);
  uint8_t r = 0x00, id = 0;                     // WHO_AM_I = 0x05
  if (i2cRead(I2C_IMU, &r, 1, &id, 1) && id == 0x05) {
    const uint8_t c1[] = {0x02, 0x40};          // CTRL1：位址自動遞增、資料 little-endian
    const uint8_t c2[] = {0x03, 0x16};          // CTRL2：加速度 ±4 g（8192 = 1 g）
    const uint8_t c7[] = {0x08, 0x01};          // CTRL7：只開加速度
    g_hasImu = i2cWrite(I2C_IMU, c1, 2) && i2cWrite(I2C_IMU, c2, 2) && i2cWrite(I2C_IMU, c7, 2);
  }
}

// 觸控：有手指按著就回 true，x / y 是面板座標（0° 時跟畫面一樣，0-465）。沒按的時候控制器回 FF（不是錯誤）
// 控制器的原始座標兩軸都跟面板相反（Waveshare 範例一樣 setMirrorXY(true, true)）
static bool touchRead(int& x, int& y) {
  static const uint8_t reg[2] = {0xD0, 0x00};
  static const uint8_t ack[3] = {0xD0, 0x00, 0xAB};
  uint8_t b[15];
  if (millis() - g_tpIntMs > 60) return false;  // 60 ms 沒有脈衝：沒有按著
  if (!i2cRead(I2C_TOUCH, reg, 2, b, sizeof b)) return false;
  i2cWrite(I2C_TOUCH, ack, 3);                  // 讀完回 ACK，下一筆才會更新
  if (b[6] != 0xAB || b[0] == 0xAB || b[0] == 0x00) return false;
  int n = b[5] & 0x7F;
  if (n < 1 || n > 2 || (b[0] & 0x0F) != 0x06) return false;   // 0x06 = 按著
  x = TP_MIRROR_X ? LCD_W - 1 - ((b[1] << 4) | (b[3] >> 4)) : (b[1] << 4) | (b[3] >> 4);
  y = TP_MIRROR_Y ? LCD_W - 1 - ((b[2] << 4) | (b[3] & 0x0F)) : (b[2] << 4) | (b[3] & 0x0F);
  return true;
}

// 陀螺儀：加速度（8192 = 1 g）
static bool imuAccel(int16_t& x, int16_t& y, int16_t& z) {
  const uint8_t reg = 0x35;                     // AX_L
  uint8_t b[6];
  if (!i2cRead(I2C_IMU, &reg, 1, b, 6)) return false;
  x = (int16_t)(b[0] | b[1] << 8); y = (int16_t)(b[2] | b[3] << 8); z = (int16_t)(b[4] | b[5] << 8);
  return true;
}

// 重力方向 → 螢幕方向（實測：USB 朝左 = x 正 = 0°、朝下 = y 正、朝右 = x 負、朝上 = y 負；平放 z ≈ -1 g）
// 回傳 0-3，螢幕平放、斜到分不出來時回 -1（維持原本的方向）
static int imuOrientation(int x, int y) {
  const int ax = abs(x), ay = abs(y);
  if (ax * ax + ay * ay < 3500 * 3500) return -1;   // 傾斜不到約 25°：當作平放
  if (ax > ay * 2) return x > 0 ? 0 : 2;
  if (ay > ax * 2) return y > 0 ? 1 : 3;
  return -1;                                       // 斜 45° 左右：不換
}
