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

`windows/fw/version.txt` 和 `windows/fw/aimon-app.bin` 不要手動改，`build.ps1` 會從 `FW_VERSION` 自動產生。

## 2. 建置

在專案根目錄用 PowerShell：

| 情況 | 指令 |
|---|---|
| 有改韌體 | `./build.ps1` |
| 只改電腦端 | `./build.ps1 -AppOnly` |
| 有改韌體且要直接燒進螢幕 | `./build.ps1 -Flash COMx` |

韌體編譯大約要 1～3 分鐘，用背景執行或給足 timeout。`build.ps1` 會依序：編韌體 → 複製到 `windows/fw/` → 產生 `firmware/aimon-usb-esp32c3.bin` → `go test` → 編出 `windows/AIMonitor.exe`。任何一步失敗都會丟例外，把錯誤原文貼給使用者，不要略過測試。

## 3. 安裝與燒錄（建置成功後直接做，不用問）

使用者要求「以後都你幫我更新」：建置成功後自己把新版裝好、韌體燒進螢幕，不要再請使用者雙擊 exe 或按更新。

1. 找螢幕的 COM 埠：看 USB VID，螢幕（ESP32-C3）是 `VID_303A`；`VID_1A86` 那個是別的裝置（平常在 COM3），不要燒它：
   ```powershell
   Get-CimInstance Win32_PnPEntity | Where-Object { $_.Name -match '\(COM\d+\)' } | Select-Object Name, PNPDeviceID
   ```
2. 請執行中的 AIMonitor 正常結束（跟系統匣「結束程式」同一個事件，會存好狀態；不要直接砍程序），否則序列埠被占用：
   ```powershell
   $ev = $null
   if ([System.Threading.EventWaitHandle]::TryOpenExisting('Local\AIMonitorQuit', [ref]$ev)) { [void]$ev.Set(); $ev.Dispose() }
   for ($i = 0; $i -lt 50 -and (Get-Process AIMonitor -ErrorAction SilentlyContinue); $i++) { Start-Sleep -Milliseconds 200 }
   ```
3. 韌體有改才燒，用 `build.ps1` 剛編好的檔案，不必重編：
   `arduino-cli upload -p COMx --fqbn 'esp32:esp32:esp32c3:CDCOnBoot=cdc' --input-dir build\firmware firmware\aimon`
4. 安裝並啟動：把 `windows\AIMonitor.exe` 複製到 `%LOCALAPPDATA%\AIMonitor\AIMonitor.exe`，再 `Start-Process` 那一份
   （從安裝位置、不帶參數啟動 → 不會跳出設定視窗）
5. 確認：`%LOCALAPPDATA%\AIMonitor\aimonitor.log` 最後幾行要有 `start v<新版>`、`connected COMx`、`screen answered (fw <新韌體>)`

只改電腦端時跳過第 3 步。燒錄失敗或螢幕沒回應就把錯誤原文給使用者；螢幕全黑參考 CLAUDE.md「燒錄」那段的已知問題。
改到 `flasher.go`（一鍵更新）時，這條路不會測到它，要另外請使用者在設定視窗按「更新」驗證。

## 4. Commit

exe 和內建韌體都要進版控（`windows/AIMonitor.exe`、`windows/fw/*`、`firmware/aimon-usb-esp32c3.bin`），使用者是直接從 repo 拿 exe 的。

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
  會更新 `preview.png`、`preview-pages.png`、`preview-saver.png`、`preview.gif`；新增畫面或狀態時順便改 `firmware/preview/preview.cpp` 的場景

## 回報

最後簡短告訴使用者：
- 新版本號（電腦端／韌體）
- 測試與建置是否通過、已安裝並燒錄（log 裡螢幕回報的韌體版本）
- 請使用者看螢幕確認改的地方
