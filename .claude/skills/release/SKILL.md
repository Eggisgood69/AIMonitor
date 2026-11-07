---
name: release
description: AIMonitor 的升版、建置、燒錄流程。只要使用者說「升版」「發版」「出新版」「build」「編譯」「燒錄」「燒進螢幕」「更新韌體」「打包 exe」，或改完程式要實測（螢幕插在 USB 上）、要 commit 一個新版本時，就用這個 skill，即使沒提到「release」這個字。
---

# AIMonitor 升版＋建置＋燒錄

使用者改完會直接把螢幕插 USB 實測，所以這個流程的目標是：版本號一致、exe 內建的韌體是最新的、螢幕上跑的就是剛編好的版本。

## 1. 判斷要升哪些版本

先看自上一個版本 commit 以來改了什麼：

```bash
git log --oneline -5
git diff --stat HEAD   # 再加上尚未 commit 的改動
```

- `firmware/` 有改 → 韌體要升版：`firmware/aimon/aimon.ino` 的 `#define FW_VERSION "x.y.z"`
- 任何會進 exe 的改動（`windows/` 或韌體）→ 電腦端要升版：`windows/main_windows.go` 的 `const version = "x.y.z"`
  （韌體改了也要升電腦端，因為新韌體是內建在 exe 裡發出去的）
- 只改 README / LICENSE / CLAUDE.md 之類的文件 → 不用升版，也不用建置

預設升修訂號（最後一碼 +1）。新功能或協定改變時可以升次版號，但先問使用者。

為什麼要確實升韌體版本：電腦端是比對 `windows/fw/version.txt` 和螢幕回報的 `fw` 來決定要不要顯示「可更新」。韌體改了卻沒升版，使用者的螢幕永遠不會提示更新。

升版後同步更新 `CLAUDE.md` 的「目前：AIMonitor x.y.z、韌體 x.y.z」那一行。

`windows/fw/version.txt` 和 `windows/fw/aimon-<hw>.bin` 不要手動改，`build.ps1` 會從 `FW_VERSION` 自動產生（兩種螢幕共用版本號）。

## 2. 建置

在專案根目錄用 PowerShell：

| 情況 | 指令 |
|---|---|
| 有改韌體 | `./build.ps1` |
| 只改電腦端 | `./build.ps1 -AppOnly` |
| 有改韌體且要直接燒進螢幕 | `./build.ps1 -Flash COMx` |

韌體編譯兩種螢幕大約要 2～5 分鐘，用背景執行或給足 timeout。`build.ps1` 會依序：編兩種韌體（`build/firmware/c3`、`build/firmware/s3a175`）→ 複製到 `windows/fw/` → 產生 `firmware/aimon-usb-esp32c3.bin`、`firmware/aimon-usb-esp32s3-amoled175.bin` → `go test` → 編出 `windows/AIMonitor.exe`。
（`go build` 偶爾會說 AIMonitor.exe「is not an object file」：剛產生的 exe 被防毒掃描鎖住，等幾秒再編一次就好。）任何一步失敗都會丟例外，把錯誤原文貼給使用者，不要略過測試。

## 3. 安裝與燒錄（建置成功後直接做，不用問）

使用者要求「以後都你幫我更新」：建置成功後自己把新版裝好、韌體燒進螢幕，不要再請使用者雙擊 exe 或按更新。

1. 找螢幕的 COM 埠：看 USB VID，兩種螢幕都是 `VID_303A`；`VID_1A86` 那個是別的裝置（平常在 COM3），不要燒它。
   分辨是哪種螢幕：AIMonitor 紀錄的 `screen answered (fw …, hw c3|s3a175)`，或 `esptool --port COMx chip_id`（ESP32-C3 / ESP32-S3）：
   ```powershell
   Get-CimInstance Win32_PnPEntity | Where-Object { $_.Name -match '\(COM\d+\)' } | Select-Object Name, PNPDeviceID
   ```
   AMOLED 開著麥克風時是 `VID_303A&PID_81A1`（TinyUSB，燒錄協定進不去）：結束 AIMonitor 後先 `$port = ./firmware/tools/mic-off.ps1 COMx`，
   它會讓螢幕重開回一般 USB（`PID_1001`）並輸出新的埠，用那個埠燒錄。燒完啟動 AIMonitor 會照設定再打開麥克風。
2. 請執行中的 AIMonitor 正常結束（跟系統匣「結束程式」同一個事件，會存好狀態；不要直接砍程序），否則序列埠被占用：
   ```powershell
   $ev = $null
   if ([System.Threading.EventWaitHandle]::TryOpenExisting('Local\AIMonitorQuit', [ref]$ev)) { [void]$ev.Set(); $ev.Dispose() }
   for ($i = 0; $i -lt 50 -and (Get-Process AIMonitor -ErrorAction SilentlyContinue); $i++) { Start-Sleep -Milliseconds 200 }
   ```
3. 韌體有改才燒，用 `build.ps1` 剛編好的檔案，不必重編，**每塊螢幕都要燒**：
   - 1.28 吋（c3）：`arduino-cli upload -p COMx --fqbn 'esp32:esp32:esp32c3:CDCOnBoot=cdc' --input-dir build\firmware\c3 firmware\aimon`
   - AMOLED（s3a175）：`arduino-cli upload -p COMx --fqbn 'esp32:esp32:esp32s3:CDCOnBoot=cdc,USBMode=hwcdc,PSRAM=opi,FlashSize=16M,PartitionScheme=app3M_fat9M_16MB' --input-dir build\firmware\s3a175 firmware\aimon`
4. 安裝並啟動：把 `windows\AIMonitor.exe` 複製到 `%LOCALAPPDATA%\AIMonitor\AIMonitor.exe`，再 `Start-Process` 那一份
   （從安裝位置、不帶參數啟動 → 不會跳出設定視窗）
5. 確認：`%LOCALAPPDATA%\AIMonitor\aimonitor.log` 最後幾行要有 `start v<新版>`，以及每塊螢幕的 `connected COMx`、`screen answered (fw <新韌體>, hw …)`

只改電腦端時跳過第 3 步。燒錄失敗或螢幕沒回應就把錯誤原文給使用者；螢幕全黑參考 CLAUDE.md「燒錄」那段的已知問題。
改到 `flasher.go`（一鍵更新）時，這條路不會測到它，要另外請使用者在設定視窗按「更新」驗證。

## 4. Commit

內建韌體要進版控（`windows/fw/*`、`firmware/aimon-usb-*.bin`）。`windows/AIMonitor.exe` **不進版控**（.gitignore）：別人從 GitHub Release 下載，`./build.ps1 -Release` 會從 commit 好的程式碼重新編。

訊息格式沿用既有慣例，用繁體中文：

```
AIMonitor 1.8.8：一句話說明這版改了什麼
```

韌體有升版時寫在說明裡，例如 `AIMonitor 1.8.8（韌體 1.6.1）：...`。

使用者沒有要求就不要 commit 或 push，先回報建置結果、等使用者實測。

## 5. Push 前更新文件

push 到 GitHub 之前，先把這次（以及上次 push 之後所有還沒 push 的 commit）使用者看得到的改動寫進文件，
跟程式一起 push，不要讓 GitHub 上的說明落後於程式：

- `README.md`：螢幕畫面、燈號、頁面、設定視窗、按鈕操作、安裝步驟有變就要改
- `CLAUDE.md`：版本號那一行、新踩到的坑、主燈規則、待處理清單
- 用 `git log origin/main..HEAD` 和 `git diff` 找出要寫的東西；畫面（`firmware/aimon/ui.h`）有改就重新產生預覽圖片，檢查圖片跟現在的畫面一致：
  `./firmware/preview/build-preview.ps1`（需要 zig：`winget install zig.zig`）。
  會更新 `docs/images/` 的 `states-v2.png`、`ais.png`、`pages-v4.png`、`demo-v2.gif`（GitHub 會快取圖片，內容改了就換新檔名，例如 -v3，並改 compose/main.go 與 README）（畫面用 AMOLED 466 版；狀態圖用 Claude，ais.png 是三家 AI 各一張）；新增畫面或狀態時順便改 `firmware/preview/preview.cpp` 的場景

## 6. 發佈 GitHub Release（使用者同意發版 / push 之後才做）

AIMonitor 1.12.0 起會檢查 GitHub 最新的 Release 並提示使用者更新，所以 push 了新版電腦端之後要發 Release，別人才收得到：

```powershell
./build.ps1 -Release                      # Release 說明預設用最後一個 commit 的訊息
./build.ps1 -Release -Notes "這版改了什麼"
```

**有看得到的改動（螢幕畫面、設定視窗）就在說明裡附圖**（使用者要求）：用 `docs/images/` 的預覽圖，網址固定在這個 tag，之後換圖也不會變：
`![說明](https://raw.githubusercontent.com/Eggisgood69/AIMonitor/v<版本>/docs/images/<檔名>)`。
README 的圖沒有涵蓋這次的改動時，先另外產生一張只放改動頁面的圖、commit 進 `docs/images/`。純邏輯改動不用附。發完打開 Release 頁面確認圖片顯示得出來。

它會確認工作目錄乾淨、commit 已 push，從這份程式碼重新 `go test` + 編 exe，再 `gh release create v<版本>` 上傳 `AIMonitor.exe`、`AIMonitor.exe.sha256` 與兩個完整韌體 `aimon-usb-esp32c3.bin`、`aimon-usb-esp32s3-amoled175.bin`（新板子第一次燒錄用，Release 說明會自動附上燒錄說明）。
這是對外公開的動作：每次都要先問使用者，**不要在每次 build 時自動發**。只改文件、沒升版的 push 不用發。發完到 Release 頁面確認四個檔案都在。

## 回報

最後簡短告訴使用者：
- 新版本號（電腦端／韌體）
- 測試與建置是否通過、已安裝並燒錄（log 裡螢幕回報的韌體版本）
- 請使用者看螢幕確認改的地方
