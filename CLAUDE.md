# AI 狀態指示燈（AIMonitor）— 給 Claude Code 的專案說明

使用者用繁體中文溝通，回覆、程式註解、UI 文字都用繁體中文（螢幕上的字型只有英文，螢幕字串用英文大寫）。
平台：Windows。使用者會把螢幕插在 USB 上實測，改完可以直接燒錄、執行驗證。

## 這是什麼

圓形螢幕透過 USB 序列埠，顯示 Claude / ChatGPT / Gemini 的生成狀態（外圈燈號）與額度用量。電腦端是單一執行檔 `AIMonitor.exe`。
支援兩種螢幕（同一份韌體原始碼，依編譯目標自動選，`config.h`），可以同時接好幾塊，內容一樣：
- `c3`：ESP32-2424S012N（ESP32-C3、GC9A01 1.28 吋 240×240 LCD，無觸控）
- `s3a175`：Waveshare ESP32-S3-Touch-AMOLED-1.75（ESP32-S3R8、CO5300 1.75 吋 466×466 AMOLED，QSPI；有用觸控 CST9217、陀螺儀 QMI8658、雙麥克風 ES7210（選用的 USB 麥克風）；電池 / 喇叭沒用到）

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
firmware/aimon/mic.h       AMOLED 的 USB 麥克風（ES7210 + I2S + 自訂 UAC1 class driver，執行中切 TinyUSB）
firmware/aimon/anim.h      AMOLED 的自訂動畫（取代主畫面的 Clawd；FFat 存檔、PSRAM 播放、收電腦傳來的原始畫格）
firmware/aimon/wifi.h      AMOLED 的 WiFi（掃描、連線、TCP 47323 + 配對金鑰、mDNS aimon-<id 後 6 碼>.local）；1.28 吋不做
firmware/tools/mic-off.ps1 燒錄前讓麥克風模式的 AMOLED 重開回 hwcdc，輸出新的 COM 埠
firmware/aimon/logos.h     三家官方 LOGO 的小圖（今日統計頁）與大圖（主畫面取代 AI 名稱，AMOLED 的 Claude 仍是 Clawd），由 firmware/tools/logogen.py 從 windows/ui.js 的 LOGOS 產生（需要 pip install resvg-py pillow）；使用者要官方 LOGO，不要自己畫
firmware/preview/          把 ui.h 渲染成 README 的預覽圖片：build-preview.ps1（zig 編譯，win/ 是不需要 SDL 的平台替身，compose/ 合成圖片）→ docs/images/（用 AMOLED 466 的畫面、透明背景加機身邊框；狀態圖用 Claude，ais.png 是三家 AI 各一張）
windows/                   Go 程式（Windows only，WebView2）
  main_windows.go          進入點、設定、序列埠連線（每個埠一條，runScreen）、指令（onCmd）、狀態 JSON（uiStatusJSON）
  state.go                 偵測結果 → 送給螢幕的 Frame（可跨平台測試）
  monitor.ps1              UI Automation 偵測桌面 App（Claude / ChatGPT / Gemini）與 Claude for Sheets 的「停止」「允許」按鈕
  ext.go / ext_windows.go  網頁版：收「AIMonitor 網頁偵測」擴充功能送來的分頁狀態（127.0.0.1:47322/ext）
  extension/               那個擴充功能（MV3，embed 進 exe，啟動時解開到 %LOCALAPPDATA%\AIMonitor\extension 讓使用者載入）
  providers.js / providers_windows.go   ChatGPT（wham/usage）與 Gemini（gemini.google.com/usage）用量
  ui.js                    設定視窗（注入到 claude.ai 頁面上畫出來，因為要用 claude.ai 的登入 cookie 讀 Claude 用量）
  claudecode*.go           Claude Code hooks（http hook → 127.0.0.1:47322/cc）
  anim.go / anim_windows.go   自訂動畫：動畫庫（%LOCALAPPDATA%\AIMonitor\anims，<id>.raw + <id>.json）、ffmpeg 轉檔、選檔視窗、傳給螢幕（serveScreen 裡的 animMaybeUpload）
  wifi.go / wifiscreen_windows.go   螢幕的 WiFi：設定指令（wscan / wifi / wforget）、沒插 USB 時用 TCP 連到螢幕（跟 USB 共用 serveScreen）
  codex.go                 Codex 對話紀錄（~/.codex/sessions 的 rollout-*.jsonl）→ cx:<id> 項目，跟 ChatGPT App 畫面項目配對去重
  reminder.go / notify_windows.go   額度重置提醒 + Windows Toast
  （設定視窗的顏色：額度長條、今日統計、7 天圖、額度曲線都用各家品牌色 `BRAND`（跟韌體 BRANDS 同色），每週 / 長週期同色調淡、到 `remindAt` 門檻變紅；額度卡片的提示框叫 `.rtip`，`.qtip` 是額度曲線的，撞名過）
  （設定視窗有淺色 / 深色兩套（1.15.0，右上角按鈕或設定分頁「外觀」，記在 localStorage `aimonTheme`，預設深色）：顏色全用 CSS 變數（`--card` `--fg` `--gpt` 等），**不要再寫死色碼**；
  SVG 的 stroke / fill 屬性不吃 `var()`，要寫在 style。ChatGPT 的 LOGO 在 ui.js 執行時才改成 `fill:var(--gpt)`，原始碼保持白色（logogen.py 從那裡產生螢幕的 LOGO）。版面參考使用者給的淺色元件圖：卡片外一圈淡灰（box-shadow）、膠囊分頁、綠色漸層開關；狀態分頁最上面是黑色的「螢幕正在顯示」卡片）
  quotahist.go             額度歷史（quotahist.json，7 天；數字變了才記、沒變每 10 分鐘一點），統計分頁的額度曲線（`aimonQuotaHist`，只在那頁開著時每分鐘讀一次）
  stats.go                 今日統計（stats.json，保留 7 天）；`Week()` 回傳複製的 map（設定視窗解鎖後才編 JSON，共用會並行讀寫、整個程式當掉）
  tray_windows.go          系統匣圖示
  appupdate.go / appupdate_windows.go   電腦端新版檢查（GitHub releases/latest）與一鍵更新（下載 + SHA256 驗證；可跨平台的部分有 go test）
  flasher.go / fwupdate_windows.go  一鍵更新韌體（ESP32 ROM 燒錄協定，只寫 0x10000 的 app；先確認晶片跟韌體相符；S3 寫完再清 otadata）
  ota.go                   經 WiFi 更新 AMOLED 的韌體（協定在下面；螢幕端 wifi.h `wifiOta`）
  fw/aimon-<hw>.bin, fw/version.txt  內建到 exe 的兩種韌體（c3 / s3a175，build.ps1 會自動更新）
enclosure/                 3D 列印外殼（使用者自己用 SolidWorks 畫，設計中），每種螢幕一個資料夾：
  lcd128/ amoled175/       原始檔、export/ 匯出檔、reference/ 板子參考模型（amoled175 是 Waveshare 官方 STEP，公開；lcd128 的來源不明，.gitignore 排除）
extension-mac-legacy/      舊的 Chrome 擴充功能（Mac 選用，直接連 USB，已不維護；Windows 的是 windows/extension/）。以前叫 extension/，容易跟 windows/extension/ 搞混才改名
.github/workflows/test.yml 每次 push / PR 在 GitHub Actions（Linux）跑 `go vet` + `go test`：只編得到非 *_windows.go 的檔案，新的可測邏輯放在沒有 build tag 的檔案
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
- `./build.ps1 -Flash COM5`：另外直接把韌體燒進螢幕，用 esptool chip_id 自動判斷種類（或 `-Board s3a175`）。先在 AIMonitor 系統匣按「結束程式」，否則序列埠被占用。
  AMOLED 開著麥克風時是另一個埠（PID 81A1），會先用 `mic-off.ps1` 讓它重開回 hwcdc
- S3 的 FQBN：`esp32:esp32:esp32s3:CDCOnBoot=cdc,USBMode=hwcdc,PSRAM=opi,FlashSize=16M,PartitionScheme=app3M_fat9M_16MB`（核心一樣 2.0.9）
- 並行問題用 `go test -race`：要 64 位元 gcc（`winget install BrechtSanders.WinLibs.POSIX.UCRT`；PATH 上的 `C:\MinGW` 是 32 位元，會說 `64-bit mode not compiled in`），
  跑之前設 `$env:CC` 指到 WinLibs 的 `mingw64\bin\gcc.exe`、`$env:CGO_ENABLED="1"`（`.rsrc merge failure` 警告可以不管）
- 只改電腦端：`cd windows; go test ./...; go build -trimpath -ldflags "-H windowsgui -s -w" -o AIMonitor.exe .`
- 雙擊新的 AIMonitor.exe 會自動取代執行中的舊版並安裝到 `%LOCALAPPDATA%\AIMonitor`
- 發版：使用者同意後 commit + push，再 `./build.ps1 -Release`（`gh release create v<版本>`，上傳 `AIMonitor.exe`、`AIMonitor.exe.sha256`，
  以及第一次燒錄用的完整韌體 `firmware/aimon-usb-*.bin`）。它會檢查工作目錄乾淨、commit 已 push，再從這份程式碼重新編 exe（確認 exe 裡有 `AIMonitor/<版本>` 字串）。每次 build 不會自動發佈
- **exe 不進版控**（.gitignore，1.13.0 之後）：以前每版 commit 10 MB 的 exe，repo 漲到 155 MB。exe 只放在 Release；`windows/fw/*` 與 `firmware/aimon-usb-*.bin` 照樣 commit（exe 編譯時內嵌、第一次燒錄要用）

## 版本規則（改了就要升版）

- 韌體：`firmware/aimon/aimon.ino` 的 `FW_VERSION`。電腦端會比對 `fw/version.txt`，不同就顯示「可更新」
- 電腦端：`windows/main_windows.go` 的 `const version`
- 目前：AIMonitor 1.16.2、韌體 1.12.2（兩種螢幕共用版本號）

## 序列埠協定（115200，每行一個 JSON）

電腦 → 螢幕：`{ai, st, msg, p5, pw, red, r5, rw, age, now, tz, saver, ss, bri, rot, arot, led, mic, mg, o:[[ai,st]], d:[[ai,次數,秒]], note, nk}`（`note` 只送目前主畫面那家 AI 的重置提醒（別家的不顯示：以前字前面加名字，太長被截成「CHATGPT 5H RESETS ...」，試過放小 LOGO 使用者覺得太複雜）；`saver` = 螢幕保護分鐘數，`ss` = 秒數（設定視窗的「30 秒」：送 `saver:1, ss:30`，韌體 1.10.47 起有 ss 就用它，舊韌體當 1 分鐘）；`red` = 額度幾 % 以上變紅，0 = 不變紅，沒送 = 90；`led` = 外接 WS2812 亮度 0-100；`arot` = 1 時有陀螺儀的螢幕自動轉向、不理 `rot`；`mic` 只送給 s3a175：1 = 切成 USB 麥克風、0 = 重開回 hwcdc，也可以單獨一行 `{"mic":0}`；`mg` = 麥克風增益 0-14，ES7210 的暫存器值，10 = 30 dB）
WiFi（只有 AMOLED，韌體 1.11.0 起）：`{"wscan":1}` → 螢幕回 `{"wscan":[[ssid,rssi,0 開放|1 密碼|2 企業版],...]}`（失敗加 `"err":1`）；`{"wset":{"s","p","k"}}` 存進 NVS 並連線（`s` 空白 = 忘記）；
螢幕回 `{"wst":{"st":0 沒設定|1 連線中|2 已連線|3 密碼可能不對|4 找不到,"ssid","ip","rssi","r"}}`（回 hello 後、狀態變了時送）。WiFi 的 TCP 連線第一行要是 `{"auth":"<配對金鑰>"}`（回 `{"auth":1}`），不對就斷線
自訂動畫（只有 AMOLED）：畫面的 `av` = 要顯示哪一份（CRC32，0 = Clawd），hello 回 `"av"` = 螢幕上存的那份。不一樣就傳：`{"anim":{"w":140,"h":114,"n","ms","crc","len"}}` → 螢幕回 `{"anim":"rdy"}`（第一次要格式化 FFat，可能十幾秒），
之後這條連線改收 `len` 個位元組的原始 RGB565（不是 JSON），每 16 KB 回 `{"ak":已收}`，電腦收到才送下一段（hwcdc 的接收緩衝滿了會直接丟資料，所以 AMOLED 的 `Serial.setRxBufferSize` 是 24 KB）；收完驗 CRC32 回 `{"anim":"ok","crc","saved"}` 或 `{"anim":"bad","why":"size|mem|crc"}`；5 秒沒資料就放棄、回去收 JSON
螢幕 → 電腦：`{"aimon":2,"fw":"x.y.z","hw":"c3|s3a175","id":"晶片 MAC","up":開機秒數,"loop":最長迴圈ms,"strip":最長一條重畫ms,"bad":解析失敗行數,"long":過長丟掉行數,"heap":剩餘記憶體,"gen":Generating 的光兩次更新最長間隔 ms,"full":整張重畫次數}`（回應 `{"hello":1}`，最長值回報後歸零；AMOLED 另有 `"te"` TE 脈衝數、`"ring"` 最長一格外環 ms、`"push"` 最長一次整張送出 ms（只算 SPI）、`"prep"` 送出前準備 ms（外環、轉向）、`"fb"` 整張緩衝、`"tp"` / `"imu"` 有沒有找到觸控 / 陀螺儀、`"arot"`、`"rot"` 目前方向、`"acc"` 加速度（8192 = 1 g）、`"roam"` 螢幕保護的 Clawd 兩步之間最長 ms（正常 50）、`"mic"` 麥克風模式中、`"micx"` 上次在麥克風模式下當機（這次不進）、`"mpk"` / `"mgap"` / `"mms"` 麥克風送了幾包（正常每秒約 1000）/ 兩包最長間隔 ms / 錯過幾包、`"mg"` 目前的麥克風增益、`"mfx"` 補回幾次 USB 中斷遮罩（見下面麥克風那段）、`"vgn"` / `"vgi"` / `"vgt"` 說話光暈更新幾次 / 最長間隔 ms / 最長一次 ms；韌體 1.9.0 前沒有 `hw` = c3；`id` 韌體 1.10.6 起才有）、`{"ev":"btn"}`（長按 BOOT）

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
- 只重畫一小塊的動畫（aimon.ino `redrawRect`，兩種螢幕共用）：「Generating」的光（`genTick`，每 40 ms）、
  額度環與里程表數字（`gaugeTick`，3 秒 ease-in-out，`GAUGE_ANIM_MS`）。
  一次最多畫 BAND_H 列（外環 / 額度環查表一次只畫一條高），AMOLED 畫進 g_fb 再一段一段 `pushFbRect`：
  **轉向暫存區 `g_rotBuf` 只有 W × BAND_H**，整塊一次送會寫爆它、螢幕當機重開（曾經切到 ChatGPT 就全黑；`pushRect` 現在超過就不送）。
  1.28 吋借用分條的 sprite，送出時跳過 `RING_KEY`（不蓋掉螢幕上的外環）
  - 排程：外環在呼吸時（有 TE）幾乎每一刻不是畫外環就是在等 TE（`ringFrameState` 回 2），只在 0 做的話動畫永遠排不到 → 等 TE 時也做；
    整張重畫的每一條之間也做，整張送出前（`prepareFrame` 前）再用現在的時間補畫一次，否則每次整張重畫動畫就停一下或往回跳；
    淡出時不做、淡入時照做（切換 AI 時淡入的同時開始捲額度）
  - 整張重畫只在內容變了才做：收到資料比對 `frameKey`（不含 5 小時額度的數字，那個交給 gaugeTick 慢慢捲），
    狀態頁的定時重畫改看 `viewKey`（到分鐘才變的倒數、日期、防烙印位置）。以前電腦每 1-3 秒送一次就整張重畫，動畫固定間隔頓一下
  - 其他對話的小圓點不在 `frameKey` 裡（`othersKey`）：只有它變了就 `othersTick` 只重畫那一塊（`othersBox`，額度環下方缺口）。
    狀態切換時（需審核 → 生成中）電腦幾秒內會連送好幾張只差小圓點的畫面（2026-11-07 紀錄：4 秒內 4 次），每次整張重畫 Generating 的光就停一下。
    AMOLED 每次整張送出光會斷約 70-80 ms（180° 轉向時 prep 約 48 ms + push 21 ms + 等 TE；其他時候光約每 51 ms 走一格）。
    量法：hello 的 `gen`（光兩次更新最長間隔）、`full`（整張重畫次數）。實測小圓點連變 6 次：改前 full=5、gen=81，改後 full=0、gen=52
  - Generating 的光只在螢幕上**真的是**生成中畫面時才走（`g_shownSt`，整張送出 / 畫完才換；`g_drawSt` = 正在畫的那張）：
    需審核 → 生成中時，整張重畫完成前螢幕還是「Needs Review」（比 Generating 寬），以前光先畫上去會留下舊字的頭尾。
    整張送出前補畫光 / 額度（`g_rectFbOnly`）只畫進 g_fb 不送（馬上整張送，先送那一小塊會蓋在舊畫面上）
  - 額度動畫踩過：差不到 0.05 就直接設成目標卻沒重畫 → 數字停在捲了 95% 的地方；ease-out cubic 最後 30% 時間只走 3%，看起來卡住；
    只有小數變（44.6 → 44.7）時不要重新計時；目標有小數時里程表停在四捨五入的整數（`pctDraw` 的 `disp`）
  - Arduino 的 `F()` 巨集會吃掉叫 `F` 的樣板參數，自動產生的函式原型也會弄壞樣板函式 → `redrawRect` 用函式指標
- 主畫面沒有額度（剛重開、額度還沒讀到）時跟有額度同一個版面，數字顯示 `--`、讀到後從 0 捲上去（aimon.ino 設 `g_p5Shown = 0`）。
  以前改顯示經過時間（0:15 ELAPSED），使用者不要（2026-11 拿掉 `numTick` / `numDraw`）。電腦端額度存在 `quota.json`（quotastore.go，只存數字、時間與設定視窗的週期標籤 `SLabel` / `WLabel`），啟動時讀回 15 分鐘內的。
  沒存標籤的舊快取設定視窗寫「上次用量」（不知道是 Gemini 的「今天」還是 ChatGPT 的 30 天）。
  沒有額度時 `gaugeTick` 的動畫目標 `to` 也要清掉（1.10.42）：以前 40% → 沒資料 → 40% 讀回來第一格就跳到 40，沒有從 0 捲
- 字離圓環要留空隙（使用者要的）：量法是 preview 放最長的字（100%、WEEK 100%、6D 23H、12H 48M），看非黑像素離最內圈內緣多遠；至少 5 設計像素。
  主畫面最下面的提醒 / 錯誤 / 重置倒數寬度用 `circleW` 依圓算：放不下就跟 WEEK 交換到比較寬的上一行，還放不下就只留放得下的完整單字（不加「…」、不拆兩行，使用者不要半句話）
- AMOLED 切換 AI 時淡出淡入（跟換頁同一套 `fadeTick`），以前直接整張送看得到撕裂；換 AI 時額度照樣捲（使用者喜歡從上一家捲到這一家）
- 額度變紅的門檻 = 設定視窗「額度重置提醒」的門檻（frame 的 `red`，`redThreshold()`；提醒關閉時送 0 = 不變紅，韌體 `g_redAt` 預設 90 給舊版電腦端）。
  顏色看目標值 `f.p5`、**不看捲動中的 `p5View()`**：以前捲過 90 才在 gaugeTick 設 dirty 整張重畫成紅色，切換 AI（例如 GPT 白 → 紅）時看得到跳色、撕裂
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
    每格在面板座標組合（轉向時沿面板的另一軸，不用每格轉向）、外環重畫在上面（固定不動）、送出，280 ms ease-out。
    轉向、螢幕保護、自動回首頁還是淡出淡入（`g_slideDir = 0`）。PSRAM 多三張 434 KB。
    以前一格接一格做（組合 20 ms + 外環 6.5 ms + 等 TE + 送出 20.5 ms ≈ 50 ms，一次只有 7 格，看起來卡）→ 韌體 1.12.2（PR #4，外部貢獻）：
    兩張滑動緩衝輪流用（`g_fbSlide` / `g_fbSlide2`），core 0 的 `slideWorkerTask` 組下一格、core 1 同時畫外環並送出這一格；滑動時**不等 TE**
    （送一格 21 ms 比面板掃一次 16.7 ms 久，等了也避不開撕裂），每格約 33 ms、一次約 10 格，實機看不到撕裂。外環交給 core 0 試過：兩邊搶 PSRAM 反而慢。
    滑動中再滑、或被淡出打斷時要先 `slideWorkerWait`（core 0 還在讀 `g_fbOld` / 寫緩衝）
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
- AMOLED 的 USB 麥克風（mic.h，2026-11 實測，核心 2.0.9 不用升級）：
  - 平常開機一定是 hwcdc（硬體 USB，韌體當掉也還在；使用者的 AMOLED 裝在殼裡按不到 BOOT）。電腦送 `mic:1` 才在執行中 `USB.begin()` 切 TinyUSB：
    USBCDC（新 COM 埠，PID 0x81A1）+ 自訂 UAC1 class driver（`usbd_app_driver_get_cb` 弱符號 + `tinyusb_enable_interface(USB_INTERFACE_CUSTOM)`）。
    開關存在電腦端（settings.json `screens.<id>.mic`），**不要存 NVS**；`mic:0` 用 esp_restart 回 hwcdc。電腦端用 hello 的 `id` 認回同一塊螢幕
  - ES7210（I2C 0x40）的 **I2C 寫入要 ≤ 50 kHz**：100 / 400 kHz 時每個位元要前一位元也是 1 才寫得進去（讀沒問題，初始化後也一樣）→ 寫的時候暫時 `Wire.setClock(50000)`。
    要 4 槽 TDM（REG12 = 0x02）：槽 0 = MIC1、槽 2 = MIC2（槽 1 喇叭回授、槽 3 空），標準 I2S 兩聲道讀不到。增益 30 dB：環境 -63 dBFS、講話 -45 dBFS。
    增益由設定視窗調（`mg`，每塊螢幕各自記住，`micSetGain` 錄音中直接寫 REG43-46）：喇叭播同一段語音 24 dB ≈ -55、30 dB ≈ -49、37.5 dB ≈ -41 dBFS
  - TinyUSB CDC 預設把 DTR/RTS 切換（關開 COM 埠）當成「重開進燒錄模式」→ 設了 RTC_CNTL_OPTION1 FORCE_DOWNLOAD_BOOT，之後 RTS 重置都停在 `waiting for download`、
    esptool 的 stub 也跑不起來（`--no-stub` 可以）→ 一定要 `enableReboot(false)`。救法：`esptool --no-stub write_mem 0x6000812C 0` 再 hard_reset
  - 軟體重開後 RTC_CNTL_USB_CONF（PHY 選擇）與 USB_SERIAL_JTAG_CONF0（上拉）還是 TinyUSB 時的設定（只有斷電清）：只清 PHY 電腦以為還是舊裝置、沒恢復上拉電腦看不到板子，都要拔線。
    → 切過去前在 RTC_NOINIT 記 magic，開機最前面 `micBootCheck` 看到就關 OTG、D-/D+ 拉低 300 ms 逼重新列舉、照 `usb_phy_ll_int_jtag_enable` 設回（那個標頭檔 C++ 編不過）。
    重開原因不是 SW（= 當機）就這次開機不再進麥克風（`micx`，避免一直當機重開）
  - TinyUSB 0.15 的 dcd 不幫 ISO IN 設奇偶 frame、也不處理錯過的同步封包（DWC2 一直掛著，串流整個停住）。沒畫面的測試韌體剛好每 2 ms 一包，
    正式韌體推 QSPI 時常錯過（錄 10 秒只拿到 1.7 秒、或完全卡住）→ 送之前設下一個 frame 的奇偶、`micUsbWatch` 4 ms 沒送出就停用端點 + 清 FIFO + `dcd_event_xfer_complete(FAILED)`；
    每包送「距離上一包的時間」那麼多格再依緩衝水位微調。`USB.begin()` 要在 core 0 的任務裡呼叫（中斷配置在呼叫的 core，core 1 的 loop 推畫面會拖延它）
  - 那個 dcd 也**沒有 `dcd_edpt_close`**（`usbd_edpt_close` 什麼都不做），每次 `usbd_edpt_open` 又多配置一塊 TX FIFO：
    每次開始錄音（SET_INTERFACE alt 1）就重開端點的話，第一次正常、第二次起一半封包錯過、電腦收不到任何資料，序列埠也會卡住（使用者用 Win+H 時遇到）。
    → ISO 端點只開一次（bus reset 前不再開），停止錄音（alt 0）時用 `uacAbort` 停掉掛著的那包；看門狗、切 alt、排封包共用 `g_uacLock`
  - **TinyUSB 0.15 dcd_esp32sx 的 TX FIFO 配置錯誤**（2026-11-09 找到，麥克風模式整塊不穩的根本原因）：dcd_edpt_open 給 IN 端點的 TXFNUM 是「第幾個開的」，
    大小 / 起點卻寫進 `dieptxf[端點號碼 - 1]` → 序列埠資料端點（EP4，FIFO 1）、麥克風（EP3，FIFO 2）的 FIFO 停在預設值（起點 512 / 768 字，這顆晶片 FIFO 只有 200 字，GHWCFG3）疊在一起，
    兩邊同時傳輸時序列埠送出壞資料 → Windows 序列埠驅動要求重置裝置（USB 追蹤：bulk URB 0xC0000001 → USBHUB3「Client Initiated Recovery Action」）→ 聲音斷、重新列舉失敗、裝置消失要拔插。
    通知端點（EP5）還拿到不存在的 FIFO 5。→ `usbFixFifos`：列舉完成、開麥克風端點後照實際 TXFNUM 重排（RX 0-51、EP0 52-67、序列埠 16、麥克風 100、通知 16，共 200）。
    修正後錄音 3 分鐘都不再重置
  - **同一個 dcd 的「TX FIFO 空了」中斷遮罩（DIEPEMPMSK，`USB0.dtknqr4_fifoemptymsk`）沒有鎖**（2026-11-10）：`dcd_edpt_xfer` 在呼叫它的工作裡 `|=`（序列埠是 core 1 的 loop、usbd 工作沒綁 core），
    中斷（core 0，麥克風每毫秒一次）`&= ~`。撞在一起時新設的位元被蓋掉 → 那筆資料永遠不會放進 FIFO、端點一直「傳送中」：序列埠的話螢幕 → 電腦整個方向停掉
    （螢幕還收得到、麥克風正常、關開 COM 埠沒用；光暈 + AIMonitor + 錄音時約 1-2 分鐘一次）。→ `usbMaskRepair`（micUsbWatch 每 1 ms）：端點啟用中、還有資料沒進 FIFO、遮罩卻是關的，連續兩次看到就補回去。
    實測 5 分鐘補了序列埠 1 次、麥克風 1 次，沒有再停（hello 的 `mfx` = 開機以來補了幾次）
  - Arduino 的 `(bool)USBCDC` 要 DTR **和 RTS** 都開才是 true，AIMonitor 開埠時 RTS 是關的 → 判斷「電腦開著 COM 埠」要用 `tud_cdc_n_connected(0)`（只看 DTR）。2026-11-09 加過三個靠它把關的保護（送不出去 / TX 卡住 / 收不到資料就重開），其實從沒生效，原因找到後拿掉了
  - `periph_module_enable / disable` 有計數（uint8）：`micBootCheck` 以前沒 enable 過就 disable，計數 0 → 255，之後 TinyUSB 的 enable 看到不是 0 就**不開時脈** →
    USB.begin() 一碰 USB 暫存器整顆卡死、中斷看門狗重開、麥克風被擋住（`micx`）。所以「mic:0 或自動重開之後再開麥克風」必當（關了再開、看門狗重開後 AIMonitor 再開都是）。→ 先 enable 再 reset、disable。
    這種當機沒有 core dump（硬體看門狗重置）；找法：兩個 core 各一個工作每 20 ms 把狀態寫進 RTC_NOINIT，重開後開機時搬進 NVS、hello 印出來看停在哪（查完已拿掉）。
    時脈沒開時讀 USB0 暫存器也會卡死（診斷程式自己踩過）→ 先看 `SYSTEM_PERIP_CLK_EN0_REG` bit 23
  - 軟體重開（esp_restart、當機）只重置 CPU，I2S 與 DMA 照舊在跑、繼續寫上一輪的緩衝 → 開機時 `periph_module_reset` I2S0 / GDMA，重開前先停 I2S 接收（`micRestart`）。預防性質，沒有證實出過事
  - 留下的保護（都是重開回 hwcdc，電腦端會照設定再開麥克風，`micRestart`）：15 秒沒被電腦認到、列舉完成後又收到 bus reset（這個 dcd 重新列舉會失敗）、錄音中 3 秒沒有任何一包送到。
    USB 層的查法：`logman` 開 Microsoft-Windows-USB-USBHUB3 / UCX / USBXHCI 追蹤（要系統管理員），`Get-WinEvent -Path x.etl -Oldest` 看出事那幾毫秒
  - 麥克風模式下序列埠偶爾掉一行（約每分鐘 1 行，紀錄 `screen dropped data`，原因沒查清楚），下一張畫面就補上
  - Arduino 的函式原型產生器會弄壞 `extern "C" usbd_class_driver_t const* f()`，要寫成 `extern "C" { const usbd_class_driver_t* f() … }`
  - 一鍵更新：目標在麥克風模式時先強制送 `mic:0`（`fwMicOff`），等同一個 id 從 hwcdc 的埠回應再燒（`micOffForUpdate`）
  - 切過去後 Windows 要約 10 秒才把麥克風放進 waveIn 清單；安靜時錄到 -90 dB（板上量到 -63），應該是 Windows 的麥克風增強
  - 說話光暈（aimon.ino `vg*`，使用者要「整個背景、淡淡的、看得到就好」）：有程式在錄音（`micListening` = alt 1）時背景隨音量亮起，
    -56 dBFS 以下 = 0、-36 dBFS = 最亮（品牌色 85%，`VG_MAX`；一開始 -58 ~ -30、22%，之後 45%、65% 使用者都覺得太暗），越大聲半徑越大（45% → 100%，只到額度環內側 S(104)）；強度分 128 階（`VG_STEPS`，32 階時慢慢淡掉看得到一階一階跳，使用者說太卡）；兩條包絡取大的（`vgSmooth`）：快的跟著每個字起伏（變亮 50 ms、變暗 250 ms；20 ms / 80 ms 時講話中光一直閃），慢的記住剛才的音量、停 0.6 秒後以 1.6 秒的時間常數淡掉，只用 60% 當底（講完約 4 秒消失；底 45%、停 0.2 秒 + 0.6 秒，以及停 0.4 秒 + 1 秒時使用者都覺得消失太快）。調過的：只有一條、350 ms 就暗 → 消失太快；停 0.4 秒 + 1.2 秒 → 反應慢；停 0.2 秒 + 0.6 秒 → 字與字之間不變暗，「沒有很跟嘴」。
    **不重畫畫面**（整張重畫 150 ms 以上，也會搶 CPU 讓麥克風掉封包）：g_fb 保持原樣，送出前才用加法混色加上去——整張送出時跟轉向一起搬到 g_fbRot（`composeFrame`，0° 時也用 g_fbRot，`g_fbOut` 記住送的是哪張），
    熄滅時要連調色盤快取 `g_vgPalQ` 一起作廢，否則聲音回到熄滅前同一階時範圍一直是 0、光亮不起來（GPT 審查抓到，1.13.41 也有）。小範圍更新在 `pushRect` 裡加同一張的光（不然 Clawd、Generating 的光那一塊會變黑底）。RGB565 太暗會一圈一圈 → 4×4 Bayer 抖動。
    音量變了最多每 33 ms 只重送中間光照得到的圓（`vgPushGlow`：舊範圍 ∪ 新範圍；先用 `vgComposeGlow` 加光、轉向寫進 g_fbRot（距離平方逐點遞增、黑底查表 `g_vgBlk`），每段 8 列，段太高方塊角會碰到外環）。
    **送出時機：跟在掃描線後面**（2026-11-10 使用者確認沒有切線了）：半徑 98 送 4.6 ms，大約跟面積成正比，最大的圓約 20 ms，比面板掃過那塊的 15 ms 慢。
    從 TE 開始送（搶在掃描線前面）圓大時會被追過 → 一條切線，四種送出方向都一樣。現在等掃描線過了這塊頂端（TE 後 `a0 × 一列時間` + 1.5 ms）才由上往下送；
    送得比掃描快時（圓小）再多等「掃描時間 - 預估送出時間」，免得追過掃描線；送出速度每次量了更新（`rate`）。只要在下一次掃描追上來前送完就不撕裂。
    整張換頁「20 ms 從 TE 送還是撕裂」應該是同一個道理（沒改，現在用轉場）
    以前每次整張送（外環 + 整張加光 + 送出），每秒只更新 6-12 次，改成慢慢淡掉後使用者說「很卡」（量法：hello 的 `vgn` 次數、`vgi` 最長間隔、`vgt` 最長一次 ms）。整張重畫中、轉場中不送；螢幕保護時照舊整張送（關掉光）。
    螢幕保護時不亮：Clawd 亂跑用另一張底圖，整張送 g_fb 會留下殘影。所以講話時直接離開螢幕保護（1.10.47，使用者選的）：慢的那條包絡超過 `VG_WAKE`（0.25 ≈ -51 dBFS）就當作有動靜（`g_lastActiveMs`），
    走原本的淡出淡入回主畫面、光暈照常；安靜後照設定的分鐘數再進螢幕保護
- AMOLED 的 WiFi（2026-11-11，wifi.h / wifi.go / wifiscreen_windows.go）：使用者要「不用一直插著電腦」，只做 AMOLED（C3 單核心、記憶體少）。
  設定走 USB（設定視窗 WiFi 分頁：螢幕掃描、選 SSID、輸入密碼）；密碼只存在螢幕 NVS，電腦只存配對金鑰 `wkey`、最後回報的 IP `wip`、`wssid`（settings.json screens.<id>）。
  電腦端每 3 秒看設定過 WiFi、沒插 USB 的螢幕：先連記下的 IP，不行用 mDNS 名稱：**自己送查詢**（wifi.go `mdnsLookup`，QU 單播回應）——Windows 內建解析實測找不到（螢幕有回答，Resolve-DnsName / GetAddrInfo 都說不存在）；連不上 10 → 80 秒退避。
  WiFi 不能當麥克風：USB 麥克風是 USB 音訊裝置，經 WiFi 要在 Windows 裝虛擬音訊驅動程式才會多一支麥克風（沒做）。
  同一塊螢幕插著 USB 時 WiFi 那條斷掉（`usbHasID`）。WiFi 連線的 screens key 是 `wifi:<id>`、Port「WiFi <IP>」、`Net = true`：不送 mic / mg；韌體 1.12.0 起（hello 的 `ota`）一鍵更新韌體改走 WiFi（下面），更舊的 `pickFirmwareTarget` 說要插 USB。
  韌體：`WiFi.setSleep(false)`（省電模式收資料晚 100-300 ms）、自己管重連（`setAutoReconnect(false)`，掃描時不能同時在連線）；
  USB 接的是充電器時寫 Serial 會卡住 → 狀態只在 USB 60 秒內有收到資料時才主動送（`g_usbRxMs`）。沒連上電腦時畫面最下面顯示「WIFI <IP>」等（ui.h `g_netHint`）。
  已驗證：掃描（實機 17 個 WiFi、開 WiFi 後剩餘記憶體 185 → 169 KB）；實測正常：連線、拔 USB 改接充電器（約 17 秒接上）、插回自動切 USB、WiFi 開著時 USB 麥克風（Win+H）；mDNS 182 ms 找到螢幕
- 經 WiFi 更新韌體（2026-11-12，韌體 1.12.0 / AIMonitor 1.16.0，ota.go、wifi.h `wifiOta`）：設定視窗的更新按鈕對 WiFi 連線的螢幕也能按。
  電腦先讓 WiFi 那條一般連線斷掉（`fwPort` = 「WiFi <IP>」，runWifiScreen 的 stop 看到就讓出來；`wifiLoop` 在 fwPort 清掉前不重連），另開一條 TCP：
  auth → `{"ota":{"n","sha"}}` → 螢幕 `Update.begin` 後回 `{"ota":"rdy"}` → 直接送 n 個位元組 → 螢幕比對 SHA256、`Update.end()`（映像檔驗證：晶片 ID、校驗和）→ `{"ota":"ok"}` 後重開。
  寫進另一個 app 區（app0 / app1 各 3 MB 輪流），驗證沒過不切換、舊版照常開機。收的時候螢幕整個 loop 停在 `wifiOta`（畫面只畫進度：藍色環 + 百分比，每 10% 整張重畫一次，外環不動）；15 秒沒資料就放棄。
  **USB 一鍵更新只寫 0x10000（app0）**：OTA 過一次後是從 app1 開機，所以 S3 燒完 app0 要把 otadata（0xe000、8 KB）清成 0xFF（flasher.go `writeRegion`），bootloader 才會開 app0（之後 bootloader 自己寫一筆 seq 1、狀態 VALID = app0；2026-11-12 實測：WiFi 更新到 app1 → USB 重新寫入 → 讀 0xe000 是 seq 1）。
  實測 WiFi 傳 1.3 MB 約 6 秒，重開後約 6 秒經 WiFi 接回。更新完成的訊息 15 秒後收掉（`fwDoneAt`；以前一直蓋住「重新寫入」，要重開程式）
  arduino-cli / esptool 燒錄會一起寫 boot_app0.bin 到 0xe000，不受影響。只有已驗證配對金鑰的連線能送（wifiGate）
  - **新版確認有效（1.12.1）**：bootloader 有開回退（S3 sdkconfig `CONFIG_BOOTLOADER_APP_ROLLBACK_ENABLE`），但 Arduino 核心的 initArduino 預設在 setup() 前就
    `esp_ota_mark_app_valid_cancel_rollback`（1.12.0 被外部審查抓到，新版初始化就當機的話會一直重開、不會退回）。→ aimon.ino 覆寫 `verifyRollbackLater()` 回 true，
    第一次回 hello 之後才 `otaConfirm`（WiFi 要先過配對金鑰）；確認前重開 → bootloader 開回舊版；開機 2 分鐘還沒確認 → `esp_ota_mark_app_invalid_rollback_and_reboot`。
    hello 的 `app`（從 app0 / app1 開機）、`pv`（還沒確認；回 hello 之後才確認，所以第一次回的是 1），電腦端紀錄寫成「app1 pending」。
    2026-11-12 實測（AIMonitor 關掉、用腳本走同一個協定，esptool 的 RTS 重置當作當機 / 斷電）：正常更新 → 第一次 hello pv=1、之後 0，重置後還是新版；
    更新後 2 分鐘沒電腦 → 自己退回另一區；傳到 50% 重置 → 舊版照常開機；更新完、確認前重置 → 開回舊版。真的拔電源也測過：慢慢送（40 秒），傳到約 40% 時使用者拔掉 USB、插回 → app1、1.12.1、沒有 pending
- 外接 WS2812 接背面 4-pin 排座（GND / 3.3V / TX / RX）的 RX（GPIO20，不用旁邊的 IO8 焊點：它是 strapping pin，被拉低會讓一鍵更新進不了燒錄模式）。
  韌體同時聽 USB 與 UART0（CH340 板），所以只有從內建 USB 收到 `led>0` 才 `Serial0.end()` 接管 RX；
  顏色亮度照 `ringParams()`（gamma 2），但綠 / 紅換成純色 0x00FF00 / 0xFF0000（系統色在 LED 上偏藍、偏粉紅），待命 / 沒連線熄燈，用 `neopixelWrite()`（RMT）
- Gemini 用量頁是獨立網址 `/usage`；背景 WebView 禁止開新視窗（曾經一直點到「瞭解詳情」跳窗）。
  登入視窗（設定視窗「開啟 Gemini」，`LoginMode`）踩過三個坑（2026-11-11）：
  (1) 「登入」鈕是 `window.open("gemini.google.com/signin?...")`，再轉 accounts.google.com：擋新視窗 / 外站連結時要放行這兩個（providers.js `isSignIn`，在同一個視窗開），以前按了沒反應；
  (2) 登入中 providerLoop 每 10 秒叫頁面讀用量，providers.js 會自己點選單、設定 → 畫面自己動、按不到登入 → 登入中只看登入了沒（`inLogin()`），手動關視窗也算登入結束（`providerClosed`）；
  (3) **背景視窗的頁面不能 await Go 的綁定函式**：go-webview2 的回覆經 `p.w.Dispatch` 排進那個視窗的佇列，但只有主視窗在跑訊息迴圈，回覆永遠送不到、await 卡死
  （曾經整個讀不到用量）。要傳給頁面的狀態由 Go 在 `pollProviders` 的 Eval 裡設（`window.__aimonLogin`）；`aimonUsage` 能用是因為沒人等它的回覆
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
  Claude App「下一步」方塊是 wrap_up 工具帶 `ask`，Stop 時亮黃燈、送出下一則訊息才熄。不要靠按鈕文字（每次都不一樣）。
  一輪裡最後一次 wrap_up 說了算：帶 ask 那次被 App 拒絕（PostToolUseFailure）、改成不帶 ask 再呼叫時，AskAfter 要清掉（曾經 Stop 時誤亮黃燈）。
  App 自動叫醒的一輪（別的工作階段回報、背景工作結束、工作階段之間傳訊息）也會送 UserPromptSubmit，但不是你回答了：
  `prompt` 拿掉開頭的 `<system-reminder>` 後是空的，或以 `<side-sessions-event>` / `<cross-session-message` / `<task-notification>` / `[SYSTEM NOTIFICATION` 開頭（`ccAutoPrompt`）→
  還在等你回覆的問題保留（AskAfter），那一輪結束時亮回黃燈；那一輪的 wrap_up 換掉方塊就照新的。紀錄裡這種 UserPromptSubmit 標 `(auto)`（2026-11-07 實測）
- Claude App 自己跳的權限詢問（例如內建瀏覽器「允許 Claude 在以下位置執行 JavaScript？」）**不經過 Claude Code 的權限系統**，hooks 沒有任何事件，
  畫面上的「允許一次」也只有正在看那個對話時才讀得到 → 看 App 的紀錄檔 `%LOCALAPPDATA%\Claude\logs\main.log`（applog.go，2026-11 實測 App 4.23118）：
  `Emitted tool permission request <id> for <工具> in session local_<內部 id>` 到 `Received permission response for <id>` 之間在等你；
  `Mapping internal session local_<內部 id> to CLI session <session_id>` 對到 Claude Code 對話，那個 cc 項目亮黃燈（`AppPerms.Waiting`）。
  每次掃描讀新增的部分（第一次只讀最後 2 MB；檔案變小 = App 換新檔、從頭讀），沒回答的詢問 6 小時後不算。
  側邊欄在 Code 模式沒有「等待輸入」之類的標記可看（試過）
- 主燈規則（state.go `rank`）：「你沒看到的事」優先——背景需審核 > 正在看的需審核 > 背景錯誤 > 背景完成 >
  背景生成中 > 正在看的生成中 > 背景完成（1 分鐘後降到這裡）> 看過的錯誤 / 完成（8 秒）> 待機。**不跳 Windows 通知**（使用者不要，已移除）。
  正在看的是另一個 AI 時，主燈跟著你看的 AI（需審核、錯誤除外）。
  跟著你看的 AI 時用那家 AI 裡最需要注意的一項，不是畫面上那一項：以前 Codex 剛在背景完成、你在 Claude App 看著待機的對話、另一個 Claude Code 對話在跑時，主燈變成 Claude 待機、在跑的變小圓點（呼吸燈不亮，1.14.0 修）。
  大家都待命、你切到別的程式 / 網站時，主畫面留在你最後看的 AI（`State.lastView`）：以前同分時照名稱排，`app:chatgpt` 在 `app:claude` 前面，一點別的視窗就跳成 GPT（1.13.32 修）
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
- ChatGPT App（Microsoft Store 套件 `OpenAI.Codex`，行程還是 `ChatGPT`；ChatGPT / Codex 合一，「切換模式，目前模式：Codex」，2026-11 實測）：
  生成中停止鈕「停止」，畫面上的對話 = Document 名稱（對話標題）。側邊欄對話列是 ListItem > Group > Button（標題），在跑時 Button 底下有 StatusBar「處理中」；
  等你核准時出現「允許一次」「拒絕」「核准選項」，而且**停止鈕與「處理中」都會消失**，對話列名稱變成「<標題>待核准」（沒點開的完成是「未讀<標題>」）
  → monitor.ps1 `Update-ChatGPTSide`：「處理中」或「待核准」都算在跑，否則等核准時先算一次完成；背景對話 `app:chatgpt:bg:<標題>`（跟 Claude App 同樣的規則）。
  專案那一列的 ListItem 名稱是底下所有對話接起來，要比對這一列自己的 Button 才不會誤認。同名對話會併成一個（沒處理）；英文介面、雲端 / 已排程任務都還沒實測
  - 出錯（紅燈）：聊天模式生成失敗時停止鈕消失、出現「再試一次」（有時還有「重試」、圖片「任務失敗」）。舊訊息上的會一直留著 →
    生成結束時「再試一次」**比開始時多**才算出錯（`$RETRY_RE`）；捲到畫面外的也要算（`Get-ButtonNames` 加 `~` 前綴），只算畫面上的話縮小 / 捲動時數量會變，曾經誤亮紅燈。
    **Claude 不用**（Update-Item 看 `$ai`）：Claude 對話模式每則回答下面本來就有「重試」圖示、回答完才出現，以前套到 Claude App 時正常回答完也閃一下紅燈（1.13.39 修）。1.13.39 一度改成只給 ChatGPT 用，Gemini 桌面版出錯時的「Try again」就不亮紅燈了（1.13.42 改回 Gemini 也算）。
    ChatGPT 圖片回覆下面那排動作圖示的 ↻ 也叫「再試一次」（文字回覆的叫「重新產生回覆」），每生成一張圖多一個 → class 含 `button-icon` 的不算（`Get-ButtonNames`，1.13.40 修）
    Codex 模式斷網只顯示「正在重新連線 n /5」「Reconnecting... waiting for network」，停止鈕還在，網路回來自己接著做完，不是錯誤
  - 縮小到工作列照樣讀得到；按 X 是**隱藏**視窗（縮到系統匣）：MainWindowHandle 變 0，但用原本的 handle 還讀得到（monitor.ps1 `$appWins` 記住看過的視窗，Claude App 也適用）。
    不過隱藏後 Chromium 不再更新給 UI Automation 的畫面，停在隱藏那一刻（Codex 15:21:27 就完成，畫面到 15:22:03 重新打開才變）→ Codex 改看紀錄檔（下一條）；聊天模式沒有紀錄，隱藏時一樣停住
- Codex 對話紀錄（codex.go，2026-11）：`%USERPROFILE%\.codex\sessions\YYYY\MM\DD\rollout-*.jsonl`（`CODEX_HOME` 可改），一個對話一個檔，每行一個 JSON。
  第一行 `session_meta`（id、cwd、`originator` 含 desktop 是 ChatGPT App（舊對話「Codex Desktop」、新開的「codex_work_desktop」，只認前者時新對話配不上、亮兩個燈）、`source` 含 `subagent` 是子代理 / guardian，不單獨亮燈；這行含整份系統提示，很長）；
  `event_msg` 的 `task_started` / `task_complete` / `turn_aborted`（你中斷，不算完成）。**沒有等你核准的事件**：看得到畫面時黃燈看畫面的「待核准」；
  視窗隱藏時（monitor 送 `hidden`）改看紀錄：`turn_context.approvals_reviewer` = user（「要求核准」；auto_review =「代我核准」不算）時，
  `custom_tool_call` / `function_call` 的 input 含 `tools.request_permissions(` 或 `sandbox_permissions:"require_escalated"`、1.5 秒後還沒有同 call_id 的 `*_output` = 在等你（`cxAskMarks`、`logReview`）。
  按過「一律允許」的指令不會再問、但紀錄分不出來，所以看得到畫面時不用這招。紀錄裡也沒有對話標題
  （`session_index.jsonl` 只有新對話，App 更新時搬進來的舊對話標題在 sqlite）→ 用「畫面上開始生成的 5 秒內有 Codex 對話也開始」配對（`cxPairWindow`），
  配對後畫面那項（app:chatgpt / app:chatgpt:bg:<標題>，monitor 送 `view` = 畫面上的對話標題）不送出，cx 項目借它的「正在看」「待核准」。
  新對話生成到一半標題會從第一句話換成自動產生的標題：同一次生成（key@GenSince，只在生成中）也認得（`genPairs`）。
  AIMonitor 重開時配對不見、開始時間也對不上：畫面上沒配對的生成中、紀錄裡沒配對的生成中各只有一個時直接配（兩個以上不猜）。
  續聊較早建立的對話時紀錄寫回建立那天的資料夾：每 3 秒只看最近三天的資料夾，另外每 15 秒把整個 sessions 列一次、收 10 分鐘內有寫入的（`cxOldEvery` / `cxOldRecent`）。
  首行 `session_meta` 整行寫完才算讀過（`metaLocked` 回 false = 下次再讀）：以前先標成讀過，剛好掃到空檔 / 半行的對話之後一直沒燈。
  App 更新時會把舊對話搬成新檔（同一天幾十個、好幾 MB、裡面上千個舊 task 事件）：第一次只讀最後 256 KB；生成中 30 分鐘沒寫紀錄當作已結束（`cxStale`）。
  紀錄檔被 Codex 開著，PowerShell 的 `[IO.File]::ReadLines` 會說被占用，Go 的 os.Open / Get-Content 可以讀
- 正看著 Claude App 裡的 Claude Code 對話時出錯：StopFailure 跟 App 停止鈕消失配對成同一個對話，
  App 那項不要亮完成蓋掉紅燈（state.go `matchClaudeApp`），紅燈亮 8 秒
- 網頁版（claude.ai / chatgpt.com / gemini.google.com）只靠擴充功能（2026-11）：UI Automation 只讀得到每個視窗正在顯示的分頁，切到別的分頁燈號就沒了，
  時有時無讓人以為壞掉 → monitor.ps1 的 `Url-To-AI` 不再認這三個網站（只留 Sheets），沒裝擴充功能網頁就不亮，設定視窗講清楚要裝。
  擴充功能：content.js 用跟 monitor.ps1 相同的 STOP / REVIEW 規則看按鈕（MutationObserver，背景分頁也會觸發），有變化或每 5 秒回報；
  background.js 把所有 AI 分頁 + 是否正在看（分頁 active 且視窗 focused）POST 過來，每 30 秒（alarms）沒分頁也送空清單報平安；
  停止鈕消失 1.5 秒才算完成、換網址不算完成在 ext.go 判斷；3 分鐘沒收到 = 不在了（`extStale`，背景分頁計時器會被 Chrome 放慢）。
  踩過：manifest 少 `storage` 權限時 `chrome.storage` 是 undefined（service worker 一直丟錯，什麼都沒送）；
  background.js 的分頁狀態放在記憶體（`tabs`），每次改動都同步完成、不在中間 await，`storage.session` 只當 service worker 重開時的備份（1.0.4）：
  以前「讀整份 → 改一筆 → 寫回」中間有 await，兩個分頁同時回報時後寫的蓋掉先寫的（AIMonitor 把缺席的分頁當成關掉，黃燈會掉）；`send()` 也不再寫回快照，送出前發現有新回報就整份重做。
  同一個分頁換成別家 AI（ChatGPT → Gemini）時，新網站回報前留著的是舊網站的狀態：網址的 AI 跟回報的 `ai` 不同就丟掉（1.0.5）。
  background.js 每次送出前用 `chrome.tabs.get` 的網址再確認分頁還是 AI 網站（`urlAI`；沒有 tabs 權限時別的網站讀不到網址 = 不是），哪家 AI 也照網址：
  避免分頁從 chatgpt.com 換走時舊頁面最後一次回報晚到、又被加回清單，之後一直算「正在看 ChatGPT」（1.0.3，保險用；當時跳成 GPT 的真正原因是下面主燈規則的 `lastView`）。
  POST JSON 前 Chrome 會先送 OPTIONS 預檢，要回 CORS + `Access-Control-Allow-Private-Network`（只對 `chrome-extension://` Origin），否則請求根本不會送出。
  `/ext` 只收 Origin 是 `chrome-extension://` 的 JSON（`extRequestAllowed`）；設定 `noExtension` = 不用它的資料。Key = `ext:<ai>:<分頁 id>`
- Gemini 桌面程式（Google 出的 `%LOCALAPPDATA%\Google\Gemini\app-*\Gemini.exe`，行程名稱 `Gemini`）：停止鈕「停止回覆」跟網頁一樣，切到別的程式也讀得到（2026-11 實測）
- Claude for Sheets（Google 試算表側欄，2026-10 實測）：生成時按鈕「Stop」、等允許時「Allow once Enter」「Always allow Ctrl Enter」「Deny Esc」，
  monitor.ps1 `$SHEETS_RE` 的分頁算成 `web:claude:sheets`。試算表也有 Gemini 側欄，所以要看得到 Claude 側欄自己的按鈕（`$SHEETS_CLAUDE`）才算；
  網址去掉 `#gid=` / `range=` 才當對話（換工作表、選儲存格會改）；按鈕清單只留停止 / 允許（帳戶按鈕名稱含 email）
- 螢幕 hello 的 `fw` / `hw` / `id` 會顯示在設定視窗（注入在 claude.ai 頁面上，`innerHTML` 拼字串）：任何會回 hello 的 USB 裝置都能送字串，
  所以 Go 端只收韌體會送的格式（devinfo.go：`x.y.z`、小寫英數、12 碼 hex），ui.js 再用 `H()` 跳脫（螢幕 key 在選擇器裡用 `CSS.escape`）。新增外來字串進 innerHTML 時一樣要過 `H()`
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
- 開機自動執行：關掉時記在 settings.json `noAutostart`，啟動 / 更新時看到就不再寫登錄的 Run（1.13.33 以前每次啟動都加回去，關了也沒用）。
  從舊版升上來時沒有這個欄位：已經裝過（有 settings.json，或沒有 settings.json 但已有裝好的 AIMonitor.exe——舊版沒改過設定就不會產生這個檔）、Run 裡又沒有 = 上次執行時關掉了，啟動時補記（`autostartOff`，這時 settings 還沒讀，直接看檔案）
- 設定視窗的 `fwupdate:<埠>`（指定螢幕）也要先確認（以前只比對 `fwupdate`，一按就燒），確認框寫出是哪塊螢幕
- 更新：從專案資料夾啟動新版、取代執行中的舊版時加 `--updated`，安靜換掉不開設定視窗；第一次安裝或舊版沒在跑才 `--welcome` 開視窗
- 電腦端自動更新（1.12.0 起，appupdate*.go）：啟動 20 秒後、之後每天一次讀 `api.github.com/repos/Eggisgood69/AIMonitor/releases/latest`
  （失敗一小時後再試；只送 `User-Agent: AIMonitor/<版本>`）。有新版只在設定視窗上方、系統匣 tooltip / 右鍵選單提示，**不跳通知**，按了才更新。
  下載到 `%LOCALAPPDATA%\AIMonitor\update\<版本>\`，比對 Release 的 `AIMonitor.exe.sha256`（`<hash>  AIMonitor.exe`），通過才從那裡啟動新版 →
  走上面的取代流程（新版啟動後刪掉 update 資料夾）；一分鐘內沒被新版結束就顯示「新版沒有啟動成功」。
  設定 `noUpdateCheck`（設定分頁「檢查更新」開關）= 完全不連 GitHub。只認非草稿、非預先發行、tag 是 `v<x.y.z>`、附了這兩個檔案的 Release
- 公開專案：exe 一定要用 `-trimpath` 編譯（否則會內嵌 `C:/Users/<使用者>/go/...` 路徑）；紀錄 / 額度原文不要寫入 email、帳號 ID。
  韌體同理：Arduino 核心的 `__FILE__` 會留下 `C:\Users\<使用者>\AppData\...`，build.ps1 用 `-fmacro-prefix-map` 換成 `~`，
  編完檢查韌體檔裡還有路徑就 `--clean` 重編（舊的編譯快取不會因為參數改了就重編）
- go.mod 用 `replace golang.org/x/sys => github.com/golang/sys`（原開發環境擋 golang.org，在你的電腦上也能正常用）

## 待處理

- WiFi 麥克風（使用者 2026-11-11 說先不做）：可行做法是使用者自己裝 VB-CABLE，螢幕經 WiFi 送 16 kHz 單聲道 PCM（約 32 KB/s），AIMonitor 播進「CABLE Input」、
  Windows 用「CABLE Output」當麥克風。自己寫虛擬麥克風驅動要 EV 憑證 + 微軟簽章，不實際；S3 沒有傳統藍牙，不能當藍牙耳麥。
  要處理：螢幕不知道有沒有程式在錄音（一直送、光暈只看音量）、插回 USB 時預設麥克風要切回 AIMonitor Mic
- Fable 用量（使用者 2026-11-11 說先不加）：Claude 用量回應的 `limits` 陣列裡 `kind: "weekly_scoped"`、`scope.model.display_name: "Fable"` 那一項（`percent`、`resets_at`）；
  `weekly_scoped_shares` 是它占每週額度的比例（allowance 50%、used）。`seven_day_opus` / `seven_day_sonnet` 是 null。
  另外 `seven_day_breakdown.rows` 有本週用量依來源分（claude_code / chat / cowork / other 的 %），統計分頁可以用。
  要加的話：parseClaudeUsage 多讀這一項，額度曲線多一條線、狀態分頁 Claude 額度卡片多一行
- Cursor Agent 支援：Cursor 有 `~/.cursor/hooks.json`（beforeSubmitPrompt / stop），但沒有「等待允許」事件
- 其他提案：額度跨過門檻當下主動提醒（現在只有到門檻變紅、重置前 5 分鐘 / 重置時的提醒 reminder.go；使用者 2026-11 說先不做）、夜間自動調暗、生成太久提醒
