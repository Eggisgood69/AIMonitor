// 桌面版 LovyanGFX 只用 sprite 時需要的平台函式（取代 SDL 版）
#include <LovyanGFX.hpp>
#include <chrono>
namespace lgfx { inline namespace v1 {
  static auto t0 = std::chrono::steady_clock::now();
  unsigned long millis(void) { return std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - t0).count(); }
  unsigned long micros(void) { return std::chrono::duration_cast<std::chrono::microseconds>(std::chrono::steady_clock::now() - t0).count(); }
  void delay(unsigned long) {}
  void delayMicroseconds(unsigned int) {}
  void gpio_hi(uint32_t) {}
  void gpio_lo(uint32_t) {}
  bool gpio_in(uint32_t) { return false; }
  void pinMode(int_fast16_t, pin_mode_t) {}
  void lgfxPinMode(int_fast16_t, pin_mode_t) {}
  namespace spi {
    cpp::result<void, error_t> init(int, int, int, int, int) { return {}; }
    void beginTransaction(int) {}
    cpp::result<void, error_t> init(int, int, int, int) { return {}; }
    void beginTransaction(int, uint32_t, int) {}
    void endTransaction(int) {}
    void writeBytes(int, const uint8_t*, size_t) {}
    void readBytes(int, uint8_t*, size_t) {}
  }
  namespace i2c {
    using R = cpp::result<void, error_t>;
    R init(int, int, int) { return {}; }
    R release(int) { return {}; }
    R restart(int, int, uint32_t, bool) { return {}; }
    R beginTransaction(int, int, uint32_t, bool) { return {}; }
    R endTransaction(int) { return {}; }
    R writeBytes(int, const uint8_t*, size_t) { return {}; }
    R readBytes(int, uint8_t*, size_t, bool) { return {}; }
    R transactionWrite(int, int, const uint8_t*, uint8_t, uint32_t) { return {}; }
    R transactionRead(int, int, uint8_t*, uint8_t, uint32_t) { return {}; }
    R transactionWriteRead(int, int, const uint8_t*, uint8_t, uint8_t*, size_t, uint32_t) { return {}; }
    cpp::result<uint8_t, error_t> readRegister8(int, int, uint8_t, uint32_t) { return 0; }
    R writeRegister8(int, int, uint8_t, uint8_t, uint8_t, uint32_t) { return {}; }
  }
}}
