# AI 狀態指示燈（AIMonitor）— 給 Claude Code 的專案說明

使用者用繁體中文溝通，回覆、程式註解、UI 文字都用繁體中文（螢幕上的字型只有英文，螢幕字串用英文大寫）。
平台：Windows。使用者會把螢幕插在 USB 上實測，改完可以直接燒錄、執行驗證。

## 這是什麼

圓形螢幕透過 USB 序列埠，顯示 Claude / ChatGPT / Gemini 的生成狀態（外圈燈號）與額度用量。電腦端是單一執行檔 `AIMonitor.exe`。
支援兩種螢幕（同一份韌體原始碼，依編譯目標自動選，`config.h`），可以同時接好幾塊，內容一樣：
- `c3`：ESP32-2424S012N（ESP32-C3、GC9A01 1.28 吋 240×240 LCD，無觸控）
- `s3a175`：Waveshare ESP32-S3-Touch-AMOLED-1.75（ESP32-S3R8、CO5300 1.75 吋 466×466 AMOLED，QSPI；有用觸控 CST9217、陀螺儀 QMI8658；電池 / 音訊沒用到）

- 外圈狀態燈：生成中綠色慢呼吸、完成綠燈恆亮（不要白閃）、需審核黃色快呼吸、錯誤紅色雙閃、待命暗灰
- 額度環：5 小時額度，下方留 56° 開口，從開口左側順時針增加；每週額度只顯示文字
- 頁面（BOOT 短按；AMOLED 左右滑，使用者的 AMOLED 裝在殼裡、按不到 BOOT 鍵）：狀態 → 額度詳細 → 今日統計 → 時鐘；長按 0.7 秒（或按住螢幕不動）= 全部標為已讀
- 風格：Apple 深色、純黑底、Inter 字型。**使用者試過液態玻璃風格，覺得太花，已改回，不要再加花俏背景**

## 目錄

```
firmware/aimon/aimon.ino   韌體主程式（序列埠協定、BOOT 鍵、分條重畫 + 60fps 外環）
firmware/aimon/ui.h        所有畫面（韌體與 preview 共用）；照 240 設計，S() / SI() 等比例放大到 LCD_W
firmware/aimon/fonts.h     由 firmware/tools/fontgen.py 產生（Inter，SIL OFL；授權全文 firmware/tools/fonts/OFL.txt，散布字型要一起附上）
firmware/aimon/fonts466.h  AMOLED 用的放大字型（`fontgen.py 466`）
firmware/aimon/config.h    兩種板子的腳位、亮度（`#if CONFIG_IDF_TARGET_ESP32S3`）
firmware/aimon/sensors.h   AMOLED 的觸控、陀螺儀（I2C）
firmware/aimon/logos.h     三家官方 LOGO 的小圖（今日統計頁），由 firmware/tools/logogen.py 從 windows/ui.js 的 LOGOS 產生（需要 pip install resvg-py pillow）；使用者要官方 LOGO，不要自己畫
firmware/preview/          把 ui.h 渲染成 README 的預覽圖片：build-preview.ps1（zig 編譯，win/ 是不需要 SDL 的平台替身，compose/ 合成圖片）
windows/                   Go 程式（Windows only，WebView2）
  main_windows.go          進入點、設定、序列埠連線（每個埠一條，runScreen）、指令（onCmd）、狀態 JSON（uiStatusJSON）
  state.go                 偵測結果 → 送給螢幕的 Frame（可跨平台測試）
  monitor.ps1              UI Automation 偵測桌面 App / 瀏覽器分頁的「停止」「允許」按鈕
  providers.js / providers_windows.go   ChatGPT（wham/usage）與 Gemini（gemini.google.com/usage）用量
  ui.js                    設定視窗（注入到 claude.ai 頁面上畫出來，因為要用 claude.ai 的登入 cookie 讀 Claude 用量）
  claudecode*.go           Claude Code hooks（http hook → 127.0.0.1:47322/cc）
  reminder.go / notify_windows.go   額度重置提醒 + Windows Toast
  stats.go                 今日統計（stats.json，保留 7 天）
  tray_windows.go          系統匣圖示
  flasher.go / fwupdate_windows.go  一鍵更新韌體（ESP32 ROM 燒錄協定，只寫 0x10000 的 app；先確認晶片跟韌體相符）
  fw/aimon-<hw>.bin, fw/version.txt  內建到 exe 的兩種韌體（c3 / s3a175，build.ps1 會自動更新）
enclosure/                 3D 列印外殼（使用者自己用 SolidWorks 畫，設計中），每種螢幕一個資料夾：
  lcd128/ amoled175/       原始檔、export/ 匯出檔、reference/ 板子參考模型（amoled175 是 Waveshare 官方 STEP，公開；lcd128 的來源不明，.gitignore 排除）
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
- `./build.ps1`：編譯兩種韌體（`build/firmware/<hw>/`）→ 更新 `windows/fw/` 與 `firmware/aimon-usb-*.bin` → 測試並編譯 `windows/AIMonitor.exe`
- `./build.ps1 -Flash COM5`：另外直接把韌體燒進螢幕，用 esptool chip_id 自動判斷種類（或 `-Board s3a175`）。先在 AIMonitor 系統匣按「結束程式」，否則序列埠被占用
- S3 的 FQBN：`esp32:esp32:esp32s3:CDCOnBoot=cdc,USBMode=hwcdc,PSRAM=opi,FlashSize=16M,PartitionScheme=app3M_fat9M_16MB`（核心一樣 2.0.9）
- 只改電腦端：`cd windows; go test ./...; go build -trimpath -ldflags "-H windowsgui -s -w" -o AIMonitor.exe .`
- 雙擊新的 AIMonitor.exe 會自動取代執行中的舊版並安裝到 `%LOCALAPPDATA%\AIMonitor`

## 版本規則（改了就要升版）

- 韌體：`firmware/aimon/aimon.ino` 的 `FW_VERSION`。電腦端會比對 `fw/version.txt`，不同就顯示「可更新」
- 電腦端：`windows/main_windows.go` 的 `const version`
- 目前：AIMonitor 1.11.15、韌體 1.10.14（兩種螢幕共用版本號）

## 序列埠協定（115200，每行一個 JSON）

電腦 → 螢幕：`{ai, st, msg, p5, pw, r5, rw, age, now, tz, saver, bri, rot, arot, led, o:[[ai,st]], d:[[ai,次數,秒]], note, nk}`（`led` = 外接 WS2812 亮度 0-100；`arot` = 1 時有陀螺儀的螢幕自動轉向、不理 `rot`）
螢幕 → 電腦：`{"aimon":2,"fw":"x.y.z","hw":"c3|s3a175","id":"晶片 MAC","up":開機秒數,"loop":最長迴圈ms,"strip":最長一條重畫ms,"bad":解析失敗行數,"long":過長丟掉行數,"heap":剩餘記憶體}`（回應 `{"hello":1}`，最長值回報後歸零；AMOLED 另有 `"te"` TE 脈衝數、`"ring"` 最長一格外環 ms、`"push"` 最長一次整張送出 ms（只算 SPI）、`"prep"` 送出前準備 ms（外環、轉向）、`"fb"` 整張緩衝、`"tp"` / `"imu"` 有沒有找到觸控 / 陀螺儀、`"arot"`、`"rot"` 目前方向、`"acc"` 加速度（8192 = 1 g）、`"roam"` 螢幕保護的 Clawd 兩步之間最長 ms（正常 50）；韌體 1.9.0 前沒有 `hw` = c3；`id` 韌體 1.10.6 起才有）、`{"ev":"btn"}`（長按 BOOT）

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
- AMOLED（CO5300，LovyanGFX 1.2.33 的 `Panel_CO5300` + `Bus_SPI` 四線）：
  - 直接寫到螢幕的範圍 x、y、寬、高都要是**偶數**（`LCD_EVEN`）：x / 寬奇數 LovyanGFX 會整段丟掉，y / 高奇數面板會亂掉（全白）。
    外環兩列一組推（`RING_ROWS`，兩列取聯集、每個像素照實際距離查色），sprite 每條 40 列；所以外環也畫進 sprite（`g_ringDirect = false`），不用透明色挖空
  - QSPI 下 LovyanGFX 的 DMA 路徑、以及 **64 位元組以下**的寫入（改塞 SPI 暫存器那條路）都會畫壞：bus `dma_channel = 0`，外環每段加寬到 > 64 位元組（右側 3 點鐘曾有一條暗線）
  - 面板記憶體 472 欄、從第 6 欄開始（`offset_x = 6`）；放開重置後要多等 150 ms 再送初始化（不然偶爾開機全白，`Panel_CO5300_466`）
  - 整張畫面放 PSRAM（`g_fb`，466×480×2）：分條畫照舊（`band.setBuffer` 指到 g_fb 裡那一條），全部畫好後 `g_framePending`，
    先 `prepareFrame`（外環換成現在的亮度、轉向時整張轉到 `g_fbRot`，16×16 一塊一塊搬，6-46 ms），再在 TE 之後 `pushFrame` 只做 SPI 寫入。
    QSPI 80 MHz 整張約 20 ms（40 MHz 時 31 ms；以前轉向邊送邊轉 46 ms）。理論上從 TE 開始、兩格（33 ms）內寫完就不會撕裂，
    但實測 20 ms 換頁時還是看得到撕裂（面板掃描方向 / TE 時間點跟假設不同，沒查清楚）→ 改用轉場
  - 轉場（`fadeTick` / `showBright`）：換頁、轉向、進出螢幕保護時亮度 120 ms 淡到 0，全暗時才送新畫面（不用等 TE），再 160 ms 淡入；
    淡出時不畫外環、重畫中的那張作廢重來。亮度只在 `showBright` 設（每次 loop），淡出用轉場前的亮度（`g_fadeOutBright`），離開螢幕保護不會先亮一下。
    每秒更新數字不轉場（小範圍，看不出撕裂）。使用者覺得轉場比硬換好
  - Clawd（ui.h `drawClawd` / `clawdPose` / `clawdBox`，只有 LCD_W > 240）：12×7 格方塊畫，主燈是 Claude 時取代主畫面的名稱、
    主畫面的範圍不碰到額度環、大數字。動畫不整張重畫：沒在重畫 / 轉場時每 50 ms
    `clawdTick` 只重畫那一塊進 g_fb、轉向後 `pushFbRect` 直接送（範圍要偶數、不超過 g_rotBuf）。使用者喜歡，1.28 吋不做。
    螢幕保護時在整個圓裡亂跑（使用者要的，擋到時鐘沒關係；`roamTick`）：整張畫好時存一份沒有 Clawd 的底圖 `g_fbBg`（`roamFrameDone`），
    每一步在小暫存區（`g_roamTmp`）組好「底圖 + 新位置的 Clawd」、只送舊 ∪ 新那一塊，不碰 g_fb（整張重畫中、等送出時也照常走）；
    中心離圓心 ≤ 100 設計像素 - 7 格，整隻在 107.5 的額度弧裡面。
    曾經每隔幾秒卡一下：電腦每次送資料都整張重畫，送出前後約 100 ms 牠停住 → 螢幕保護時電腦送的資料不設 dirty，
    loop 看 `saverKey`（分鐘、額度、重置倒數、連線、防烙印位置）變了才重畫
  - 時鐘頁的大字型：LovyanGFX 的 VLW 字型畫一個字會 `alloca(字寬 × 字高)`，466 的時鐘數字超過預設 8 KB 堆疊（Stack canary 當機重開）→ `SET_LOOP_TASK_STACK_SIZE(16 KB)`
  - 觸控 CST9217（I2C 0x5A，16 位元暫存器 0xD000 讀 15 bytes、讀完寫 `D0 00 AB`；[6] = 0xAB、[0] 低 4 位元 0x06 = 按著）：
    一直輪詢的話偶爾整個 I2C 卡 1 秒 → 只在 INT（GPIO11）下降緣之後 60 ms 內讀（手指按著時會一直送脈衝；脈衝很短，讀電位看不到）。
    `pollTouch`：左右滑換頁（看到的畫面的左右，`touchToView` 依 g_rot 換算；水平移動 > 30 設計像素、比垂直多 1.5 倍、1 秒內）、
    按住不動 0.7 秒 = 標為已讀、點一下只叫醒螢幕保護（使用者不要點一下就換頁）。觸控原始座標兩軸跟面板相反（`TP_MIRROR_X/Y`）
  - 換頁滑動（`startSlide` / `slideTick`，BOOT 鍵換頁也是）：舊畫面複製到 `g_fbOld`，新頁整張畫好（約 150 ms），
    每格在面板座標組合（轉向時沿面板的另一軸，不用每格轉向）、外環重畫在上面（固定不動）、等 TE 送出，280 ms ease-out。
    轉向、螢幕保護、自動回首頁還是淡出淡入（`g_slideDir = 0`）。PSRAM 多兩張 434 KB
  - 陀螺儀 QMI8658（0x6B）：USB 朝左 = x 正 = 0°、朝下 = y 正 = 90°、朝右 = 180°、朝上 = 270°（`imuOrientation`）；傾斜不到約 25° 當平放、維持原方向；
    同一方向 0.8 秒才轉。自動轉向開著時不理電腦送的 rot、也不存 NVS（開機時先讀一次）
  - `#include <Wire.h>` 要放在 aimon.ino 最前面：放在 LovyanGFX 之後或只在 sensors.h 裡，arduino-cli 偵測不到 Wire 函式庫（`'Wire' was not declared`）
  - 螢幕方向**不用面板的硬體旋轉**（MADCTL 交換 / 鏡像後的定址跟 LovyanGFX 對不上，試過偏移 2/4/6/8 都會在外環或小圓點附近錯位）：
    面板固定 0°，`pushBand` 把每一條轉向後送到對應位置（`applyRotation`、`g_rotBuf`）；外環是正圓、只跟半徑有關，不用轉
  - 外環跟著 TE 腳（GPIO13，60 Hz）寫，避免撕裂；一格外環約 12 ms，重畫中每格外環之後先畫一條（`stripSinceRing`），否則字永遠不更新。
    sprite 裡的外環用螢幕上最後一格的亮度（`g_ringShownMs`），不會一條一條閃
  - 防烙印：文字與小圓點每 2 分鐘在 9 個位置間挪動（`g_shX/g_shY`，最多 2 個設計像素）
    `text()` 會自動加上挪動，跟字搭在一起的圖形（狀態點、今日統計的品牌圓點與長條）要自己加 `g_shX/g_shY`，不然字會偏到圖形外（曾經「C」偏到圓點左下）
  - 外環整圈畫進畫面緩衝（`ringDraw`，涵蓋半徑 110.5 以外），其他東西畫在那裡會被每一格外環蓋掉：螢幕保護的額度弧放在 107.5（`drawClock`），
    只畫已用的部分、不畫軌道（使用者要的；LCD 維持原樣），用原色（調暗時很暗的顏色會變成一點一點）
  - 額度環查表的距離在 466 用 1/8 px（`GAUGE_DQ`，1/16 會超過 uint8）
  - 待機 / 未連線的暗灰外環在 AMOLED 上看起來偏綠；未連線本來就會灰色慢呼吸（不是錯誤）
- 外接 WS2812 接背面 4-pin 排座（GND / 3.3V / TX / RX）的 RX（GPIO20，不用旁邊的 IO8 焊點：它是 strapping pin，被拉低會讓一鍵更新進不了燒錄模式）。
  韌體同時聽 USB 與 UART0（CH340 板），所以只有從內建 USB 收到 `led>0` 才 `Serial0.end()` 接管 RX；
  顏色亮度照 `ringParams()`（gamma 2），但綠 / 紅換成純色 0x00FF00 / 0xFF0000（系統色在 LED 上偏藍、偏粉紅），待命 / 沒連線熄燈，用 `neopixelWrite()`（RMT）
- Gemini 用量頁是獨立網址 `/usage`；背景 WebView 禁止開新視窗（曾經一直點到「瞭解詳情」跳窗）。
  Gemini 網頁生成時的停止鈕叫「停止回覆」（2026-10 實測燈號正常）
- 背景 WebView 省記憶體（providers_windows.go 開頭）：完整網頁一個 300-500 MB，平常停在 idle 頁——ChatGPT `robots.txt`
  （同網站的小頁面，providers.js 一樣會注入、帶 cookie 打 API），讀不到才開一次完整頁面（30 分鐘最多一次）；Gemini 讀的時候才開 `/usage`，讀完換 `about:blank`。
  完整頁面 90 秒沒讀到就回 idle 頁。實測私有記憶體 1113 → 460 MB。Claude 本來就停在 `/api/organizations`（JSON 頁）
- ChatGPT 的用量其實是 Codex 的額度（wham/usage），ChatGPT 聊天也有上限，但 OpenAI 不公開剩餘量
- 好幾塊螢幕：serialLoop 每 2 秒看插著的埠，每個埠一個 `runScreen` 連線，`queueFrameEach` 把同一張畫面送給每塊螢幕；設定視窗每塊一行（方向 / 亮度 / LED 用「調整哪塊螢幕」選單切換，只接一塊時不顯示選單）。
  方向 / 亮度 / 外接 LED / 自動轉向每塊分開（settings.json 的 `screens`，key = hello 的 `id`，舊韌體用埠名稱；screenset.go）：
  frame 裡的 rot / bri / led / arot 依螢幕換掉；沒設過的項目用共用值（`rotation` / `brightness` / `led` / `noAutoRot`）。設定視窗指令 `scr:<key>:<bri|rot|led>:<值>`（rot 可以是 `auto`）
  12 秒沒回應的埠（例如 COM3 的 CH340 別的裝置）記成 `quietPort`：已經有螢幕連上時就不再去開它，直到拔掉重插
- 韌體更新只用已連線並回應 hello 的埠（`pickFirmwareTarget`），依 `hw` 選韌體，只暫停那個埠（`fwPort`）；
  `FlashApp` 先讀 ROM magic 暫存器（0x40001000）比對映像檔標頭的晶片 ID（C3 = 5、S3 = 9），不符就不擦除
- 燒錄（flasher.go）：**不要送 FLASH_END(1)**，它會讓 ROM 直接跳進韌體、螢幕卡在全黑；
  寫完用 `rebootToApp`（先進燒錄模式再 RTS 重置）。Windows usbser.sys 只改 RTS 不會送出，
  `SetRTS` 之後要再設一次 DTR（esptool 同樣做法）。卡住的韌體單純 RTS 重置救不回來
- Claude Code 被中斷（Esc / 停止鈕）時不送 Stop，綠燈會一直亮：hook 事件帶的 `transcript_path`（對話紀錄 .jsonl）
  在生成中 / 需審核時每 2 秒看結尾，最後一則對話是「[Request interrupted by user…」就熄（不算完成，claudecode.go `ccInterrupted`）
- Claude Code hooks：按下允許時沒有任何事件；`permission_prompt` 通知在提示等了約 6 秒才送（只送一次）
- Claude 問你問題不是權限提示（只有 Stop）：看 PreToolUse 的工具判斷（`ccAskKind`）——AskUserQuestion / ExitPlanMode 題目出現就亮黃燈，
  Claude App「下一步」方塊是 wrap_up 工具帶 `ask`，Stop 時亮黃燈、送出下一則訊息才熄。不要靠按鈕文字（每次都不一樣）
- 主燈規則（state.go `rank`）：「你沒看到的事」優先——背景需審核 > 正在看的需審核 > 背景錯誤 > 背景完成 >
  背景生成中 > 正在看的生成中 > 背景完成（1 分鐘後降到這裡）> 看過的錯誤 / 完成（8 秒）> 待機。**不跳 Windows 通知**（使用者不要，已移除）。
  正在看的是另一個 AI 時，主燈跟著你看的 AI（需審核、錯誤除外）
- Claude Code hooks 不知道你在看哪個對話：Stop 與 Claude App 停止鈕消失的時間差 4 秒內 = 同一個對話（`matchClaudeApp`），
  只亮一個綠燈、切回 App 一起熄。Claude App 背景任務的「停止此任務」、預覽伺服器的「停止此伺服器」不算生成中（monitor.ps1 `$STOP_NOT_RE`；
  沒排除的話 App 那項一直是生成中，跟 Claude Code 的完成對不上，多出一顆小圓點）。
  主燈選到 `app:claude` 時，有狀態相同的 Claude Code 對話就改用它（看著它完成時 App 晚 1-2 秒完成、排在前面，不換的話 Claude Code 那項變成多餘的點）
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
- 追查燈號看紀錄：`frame:` 只記主燈顏色，`lights:`（state.go `Lights`）記每個沒在待命的項目「key=狀態」，`*` = 正在看；
  背景聊天的 key 含對話標題（只寫在本機紀錄）
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
