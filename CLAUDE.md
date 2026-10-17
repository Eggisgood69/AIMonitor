# AI 狀態指示燈（AIMonitor）— 給 Claude Code 的專案說明

使用者用繁體中文溝通，回覆、程式註解、UI 文字都用繁體中文（螢幕上的字型只有英文，螢幕字串用英文大寫）。
平台：Windows。使用者會把螢幕插在 USB 上實測，改完可以直接燒錄、執行驗證。

## 這是什麼

ESP32-C3 1.28 吋圓形螢幕（ESP32-2424S012N，GC9A01 240×240，無觸控）透過 USB 序列埠，
顯示 Claude / ChatGPT / Gemini 的生成狀態（外圈燈號）與額度用量。電腦端是單一執行檔 `AIMonitor.exe`。

- 外圈狀態燈：生成中綠色慢呼吸、完成綠燈恆亮（不要白閃）、需審核黃色快呼吸、錯誤紅色雙閃、待命暗灰
- 額度環：5 小時額度，下方留 56° 開口，從開口左側順時針增加；每週額度只顯示文字
- 頁面（BOOT 短按）：狀態 → 額度詳細 → 今日統計 → 時鐘；長按 0.7 秒 = 全部標為已讀
- 風格：Apple 深色、純黑底、Inter 字型。**使用者試過液態玻璃風格，覺得太花，已改回，不要再加花俏背景**

## 目錄

```
firmware/aimon/aimon.ino   韌體主程式（序列埠協定、BOOT 鍵、分條重畫 + 60fps 外環）
firmware/aimon/ui.h        所有畫面（韌體與 preview 共用）
firmware/aimon/fonts.h     由 firmware/tools/fontgen.py 產生（Inter，SIL OFL）
firmware/aimon/config.h    腳位、亮度
firmware/preview/          在電腦上把 ui.h 渲染成圖片（Linux + SDL2 版 LovyanGFX，開發用，可略過）
windows/                   Go 程式（Windows only，WebView2）
  main_windows.go          進入點、設定、序列埠連線、指令（onCmd）、狀態 JSON（uiStatusJSON）
  state.go                 偵測結果 → 送給螢幕的 Frame（可跨平台測試）
  monitor.ps1              UI Automation 偵測桌面 App / 瀏覽器分頁的「停止」「允許」按鈕
  providers.js / providers_windows.go   ChatGPT（wham/usage）與 Gemini（gemini.google.com/usage）用量
  ui.js                    設定視窗（注入到 claude.ai 頁面上畫出來，因為要用 claude.ai 的登入 cookie 讀 Claude 用量）
  claudecode*.go           Claude Code hooks（http hook → 127.0.0.1:47322/cc）
  reminder.go / notify_windows.go   額度重置提醒 + Windows Toast
  stats.go                 今日統計（stats.json，保留 7 天）
  tray_windows.go          系統匣圖示
  flasher.go / fwupdate_windows.go  一鍵更新韌體（ESP32 ROM 燒錄協定，只寫 0x10000 的 app）
  fw/aimon-app.bin, fw/version.txt  內建到 exe 的韌體（build.ps1 會自動更新）
extension/                 舊的 Chrome 擴充功能（Mac 選用，已不維護）
```

## 建置（Windows，PowerShell）

一次性安裝：
- Go 1.22+：`winget install GoLang.Go`
- arduino-cli：`winget install ArduinoSA.CLI`，然後
  ```
  arduino-cli core update-index --additional-urls https://espressif.github.io/arduino-esp32/package_esp32_index.json
  arduino-cli core install esp32:esp32@2.0.9 --additional-urls https://espressif.github.io/arduino-esp32/package_esp32_index.json
  arduino-cli lib install LovyanGFX@1.2.33 ArduinoJson@7.4.2
  ```

日常：
- `./build.ps1`：編譯韌體 → 更新 `windows/fw/` → 測試並編譯 `windows/AIMonitor.exe`
- `./build.ps1 -Flash COM5`：另外直接把韌體燒進螢幕（先在 AIMonitor 系統匣按「結束程式」，否則序列埠被占用）
- 只改電腦端：`cd windows; go test ./...; go build -ldflags "-H windowsgui -s -w" -o AIMonitor.exe .`
- 雙擊新的 AIMonitor.exe 會自動取代執行中的舊版並安裝到 `%LOCALAPPDATA%\AIMonitor`

## 版本規則（改了就要升版）

- 韌體：`firmware/aimon/aimon.ino` 的 `FW_VERSION`。電腦端會比對 `fw/version.txt`，不同就顯示「可更新」
- 電腦端：`windows/main_windows.go` 的 `const version`
- 目前：AIMonitor 1.8.1、韌體 1.6.0

## 序列埠協定（115200，每行一個 JSON）

電腦 → 螢幕：`{ai, st, msg, p5, pw, r5, rw, age, now, tz, saver, bri, rot, h:[...], o:[[ai,st]], d:[[ai,次數,秒]], note, nk}`
螢幕 → 電腦：`{"aimon":2,"fw":"x.y.z"}`（回應 `{"hello":1}`）、`{"ev":"btn"}`（長按 BOOT）

## 重要實作細節（踩過的坑）

- 螢幕記憶體不夠整張畫面：sprite 240×40 分 6 條畫，每次 loop 畫一條，中間穿插外環。
  外環用開機時算好的 scanline LUT 直接畫到 LCD（`ringDraw`），sprite 裡外環區域填 `RING_KEY` 透明色。
  **不要讓外環跟著 sprite 一起推**，否則會出現上下半圈亮度不同、呼吸燈卡頓
- ESP32-C3 沒有 FPU：每像素運算要用整數
- 螢幕方向存在 NVS（Preferences "aimon"/"rot"）
- Gemini 用量頁是獨立網址 `/usage`；背景 WebView 禁止開新視窗（曾經一直點到「瞭解詳情」跳窗）
- ChatGPT 的用量其實是 Codex 的額度（wham/usage），ChatGPT 一般聊天沒有公開的用量數字
- 多個序列埠時（例如另一個 USB 裝置在 COM3），韌體更新只用目前已連線並回應 hello 的那個埠
- go.mod 用 `replace golang.org/x/sys => github.com/golang/sys`（原開發環境擋 golang.org，在你的電腦上也能正常用）

## 待處理

- Gemini 生成時燈號不亮：monitor.ps1 的 `$STOP_RE` 已放寬，仍需在 Gemini 生成時看「偵測狀態（除錯用）」確認停止按鈕名稱
- Claude Code 連接（hooks）尚未實測，需要較新版 Claude Code（http hooks）
- Cursor Agent 支援：Cursor 有 `~/.cursor/hooks.json`（beforeSubmitPrompt / stop），但沒有「等待允許」事件
- 其他提案：生成完成 Windows 通知、額度快用完警告、夜間自動調暗、生成太久提醒
