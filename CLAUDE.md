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
firmware/aimon/fonts.h     由 firmware/tools/fontgen.py 產生（Inter，SIL OFL；授權全文 firmware/tools/fonts/OFL.txt，散布字型要一起附上）
firmware/aimon/config.h    腳位、亮度
firmware/preview/          把 ui.h 渲染成 README 的預覽圖片：build-preview.ps1（zig 編譯，win/ 是不需要 SDL 的平台替身，compose/ 合成圖片）
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
- 只改電腦端：`cd windows; go test ./...; go build -trimpath -ldflags "-H windowsgui -s -w" -o AIMonitor.exe .`
- 雙擊新的 AIMonitor.exe 會自動取代執行中的舊版並安裝到 `%LOCALAPPDATA%\AIMonitor`

## 版本規則（改了就要升版）

- 韌體：`firmware/aimon/aimon.ino` 的 `FW_VERSION`。電腦端會比對 `fw/version.txt`，不同就顯示「可更新」
- 電腦端：`windows/main_windows.go` 的 `const version`
- 目前：AIMonitor 1.9.15、韌體 1.8.2

## 序列埠協定（115200，每行一個 JSON）

電腦 → 螢幕：`{ai, st, msg, p5, pw, r5, rw, age, now, tz, saver, bri, rot, led, o:[[ai,st]], d:[[ai,次數,秒]], note, nk}`（`led` = 外接 WS2812 亮度 0-100）
螢幕 → 電腦：`{"aimon":2,"fw":"x.y.z","up":開機秒數,"loop":最長迴圈ms,"strip":最長一條重畫ms,"bad":解析失敗行數,"long":過長丟掉行數,"heap":剩餘記憶體}`（回應 `{"hello":1}`，最長值回報後歸零）、`{"ev":"btn"}`（長按 BOOT）

## 重要實作細節（踩過的坑）

- 螢幕記憶體不夠整張畫面：sprite 240×40 分 6 條畫，每次 loop 畫一條，中間穿插外環。
  外環用開機時算好的 scanline LUT 直接畫到 LCD（`ringDraw`），sprite 裡外環區域填 `RING_KEY` 透明色。
  **不要讓外環跟著 sprite 一起推**，否則會出現上下半圈亮度不同、呼吸燈卡頓
- ESP32-C3 沒有 FPU：每像素運算要用整數。LovyanGFX 的 `drawWideLine` / 抗鋸齒弧線是逐像素浮點運算，
  額度環用它拼 76 段時，一條畫面要 160-220 ms（外環呼吸會頓）。現在額度環跟外環一樣查表（ui.h `gaugeLut` / `progressRing`）：
  開機時 `gaugeInit` 算好兩圈每個像素的角度與距離（只算四分之一再鏡射；第一次用到才算的話會卡 0.7 秒），
  之後只做整數混色，最重的一條 7-15 ms。表約 50 KB（剩餘記憶體 255 → 198 KB）。
  以像素中心算距離才會跟舊畫法對齊（`drawWideLine` 只吃整數座標），邊緣多 0.2 px（`GAUGE_AA`）補回舊畫法重疊造成的粗細
- 螢幕方向存在 NVS（Preferences "aimon"/"rot"）
- 外接 WS2812 接背面 4-pin 排座（GND / 3.3V / TX / RX）的 RX（GPIO20，不用旁邊的 IO8 焊點：它是 strapping pin，被拉低會讓一鍵更新進不了燒錄模式）。
  韌體同時聽 USB 與 UART0（CH340 板），所以只有從內建 USB 收到 `led>0` 才 `Serial0.end()` 接管 RX；
  顏色亮度照 `ringParams()`（gamma 2），但綠 / 紅換成純色 0x00FF00 / 0xFF0000（系統色在 LED 上偏藍、偏粉紅），待命 / 沒連線熄燈，用 `neopixelWrite()`（RMT）
- Gemini 用量頁是獨立網址 `/usage`；背景 WebView 禁止開新視窗（曾經一直點到「瞭解詳情」跳窗）。
  Gemini 網頁生成時的停止鈕叫「停止回覆」（2026-10 實測燈號正常）
- 背景 WebView 省記憶體（providers_windows.go 開頭）：完整網頁一個 300-500 MB，平常停在 idle 頁——ChatGPT `robots.txt`
  （同網站的小頁面，providers.js 一樣會注入、帶 cookie 打 API），讀不到才開一次完整頁面（30 分鐘最多一次）；Gemini 讀的時候才開 `/usage`，讀完換 `about:blank`。
  完整頁面 90 秒沒讀到就回 idle 頁。實測私有記憶體 1113 → 460 MB。Claude 本來就停在 `/api/organizations`（JSON 頁）
- ChatGPT 的用量其實是 Codex 的額度（wham/usage），ChatGPT 聊天也有上限，但 OpenAI 不公開剩餘量
- 多個序列埠時（例如另一個 USB 裝置在 COM3），韌體更新只用目前已連線並回應 hello 的那個埠
- 燒錄（flasher.go）：**不要送 FLASH_END(1)**，它會讓 ROM 直接跳進韌體、螢幕卡在全黑；
  寫完用 `rebootToApp`（先進燒錄模式再 RTS 重置）。Windows usbser.sys 只改 RTS 不會送出，
  `SetRTS` 之後要再設一次 DTR（esptool 同樣做法）。卡住的韌體單純 RTS 重置救不回來
- Claude Code hooks：按下允許時沒有任何事件；`permission_prompt` 通知在提示等了約 6 秒才送（只送一次）
- Claude 問你問題不是權限提示（只有 Stop）：看 PreToolUse 的工具判斷（`ccAskKind`）——AskUserQuestion / ExitPlanMode 題目出現就亮黃燈，
  Claude App「下一步」方塊是 wrap_up 工具帶 `ask`，Stop 時亮黃燈、送出下一則訊息才熄。不要靠按鈕文字（每次都不一樣）
- 主燈規則（state.go `rank`）：「你沒看到的事」優先——背景需審核 > 正在看的需審核 > 背景錯誤 > 背景完成 >
  背景生成中 > 正在看的生成中 > 背景完成（1 分鐘後降到這裡）> 看過的錯誤 / 完成（8 秒）> 待機。**不跳 Windows 通知**（使用者不要，已移除）。
  正在看的是另一個 AI 時，主燈跟著你看的 AI（需審核、錯誤除外）
- Claude Code hooks 不知道你在看哪個對話：Stop 與 Claude App 停止鈕消失的時間差 4 秒內 = 同一個對話（`matchClaudeApp`），
  只亮一個綠燈、切回 App 一起熄。Claude App 背景任務的「停止此任務」不算生成中（monitor.ps1 `$STOP_NOT_RE`）
- 在 Claude App / 瀏覽器裡切換對話時停止鈕也會消失：monitor.ps1 記下生成時畫面上的對話
  （App 標題列「…，重新命名工作階段」按鈕、瀏覽器網址），換了對話就不算完成。
  今日統計：App 的完成等 4 秒，跟 Claude Code 同時完成的只算一次（state.go `flushPending`）
- Claude App 一般聊天切走後：側邊欄對話列旁有 StatusBar「執行中」（橘點），monitor.ps1 `Update-ClaudeBg` 每個背景對話回報一項
  `app:claude:bg:<標題>`（正在看的那個由 app:claude 負責；點開已完成的亮 8 秒後移除）。只在「Claude」模式（RadioButton 選取）看側邊欄；
  Code 模式的側邊欄是 Claude Code 對話（hooks 已有），改看「Claude，執行中」模式按鈕；
  切到 Code 模式時，剛才畫面上還在跑的聊天對話要轉成背景對話（`$script:chatView`），否則沒人追蹤、主燈會被別的完成蓋過。
  正在看的對話停止鈕會間歇消失（實測一次 25 秒），側邊欄說它執行中就也算生成中（`Update-Item` 的 `$busy`），否則主燈會被別的完成蓋過。
  在等你允許時側邊欄是「等待輸入」、Code 模式的模式按鈕是「Claude，正在等待您的輸入」（`$WAIT_RE`）→ 亮黃燈（`$wait`）；
  允許按鈕說法常變（「允許本次工作階段」等），`$REVIEW_RE` 認「允許 / allow」開頭的短名稱。
  背景聊天出錯（例如斷網）時側邊欄狀態只會消失，跟完成分不出來，只能當完成（對話內才有「重試」）
- 正看著 Claude App 裡的 Claude Code 對話時出錯：StopFailure 跟 App 停止鈕消失配對成同一個對話，
  App 那項不要亮完成蓋掉紅燈（state.go `matchClaudeApp`），紅燈亮 8 秒
- Claude for Sheets（Google 試算表側欄，2026-10 實測）：生成時按鈕「Stop」、等允許時「Allow once Enter」「Always allow Ctrl Enter」「Deny Esc」，
  monitor.ps1 `$SHEETS_RE` 的分頁算成 `web:claude:sheets`。試算表也有 Gemini 側欄，所以要看得到 Claude 側欄自己的按鈕（`$SHEETS_CLAUDE`）才算；
  網址去掉 `#gid=` / `range=` 才當對話（換工作表、選儲存格會改）；按鈕清單只留停止 / 允許（帳戶按鈕名稱含 email）
- hook 伺服器（127.0.0.1:47322）只收不帶 Origin、Content-Type 是 JSON 的請求，網頁送不進來（`ccRequestAllowed`）
- Claude Code 對話狀態存在 `%LOCALAPPDATA%\AIMonitor\cc-sessions.json`，重啟後讀回（等你回覆 / 允許時對話不會再送事件，不存的話黃燈會消失）；
  讀回來的「生成中」10 分鐘沒新事件就當作重啟期間已結束（`ccRestoredStale`）
- USB 接收緩衝要設大（`Serial.setRxBufferSize(2048)`，在 begin 之前）：一行畫面資料 400-600 位元組，預設 256；
  畫一條畫面時沒人讀 USB（韌體 1.8.2 前最重 177-220 ms，現在約 15 ms），緩衝小就會截斷那一行（舊韌體 Disconnected 的主因）。
  負荷數字寫在 AIMonitor 紀錄：`screen load`（每 10 分鐘）、`screen dropped`、`screen rebooted`
- 螢幕顯示 Disconnected = 有收到資料但 20 秒沒解析到畫面（寫入不會失敗）：serialLoop 每 15 秒送 hello，
  40 秒沒回應就記錄「screen not answering」並 `resetScreen`（最多每 2 分鐘一次）
- 更新：從專案資料夾啟動新版、取代執行中的舊版時加 `--updated`，安靜換掉不開設定視窗；第一次安裝或舊版沒在跑才 `--welcome` 開視窗
- 公開專案：exe 一定要用 `-trimpath` 編譯（否則會內嵌 `C:/Users/<使用者>/go/...` 路徑）；紀錄 / 額度原文不要寫入 email、帳號 ID。
  韌體同理：Arduino 核心的 `__FILE__` 會留下 `C:\Users\<使用者>\AppData\...`，build.ps1 用 `-fmacro-prefix-map` 換成 `~`，
  編完檢查韌體檔裡還有路徑就 `--clean` 重編（舊的編譯快取不會因為參數改了就重編）
- go.mod 用 `replace golang.org/x/sys => github.com/golang/sys`（原開發環境擋 golang.org，在你的電腦上也能正常用）

## 待處理

- Cursor Agent 支援：Cursor 有 `~/.cursor/hooks.json`（beforeSubmitPrompt / stop），但沒有「等待允許」事件
- 其他提案：額度快用完警告、夜間自動調暗、生成太久提醒
