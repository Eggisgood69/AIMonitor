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

## 3. 燒錄（只有韌體改了才需要）

有兩條路，先問使用者要哪一種：

- **透過 AIMonitor 一鍵更新（建議）**：雙擊新的 `windows/AIMonitor.exe`，它會取代執行中的舊版，設定視窗會顯示「可更新」，按下去即可。這也順便驗證了 flasher.go 的更新流程。
- **直接用 arduino-cli 燒**：`./build.ps1 -Flash COMx`。
  - 先請使用者在系統匣按「結束程式」，否則序列埠被占用，會出現「燒錄失敗」。
  - 不知道是哪個 COM 埠時，用 `arduino-cli board list` 找；可能還有別的 USB 裝置（例如 COM3），看清楚是 ESP32-C3 那個。

燒完請使用者看螢幕確認：開機正常、不是全黑、外環呼吸燈順暢。螢幕全黑的話，參考 CLAUDE.md「燒錄」那段的已知問題。

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
- 用 `git log origin/main..HEAD` 和 `git diff` 找出要寫的東西；`preview.png` / `preview.gif` 過時的話告訴使用者（要在 Linux 用 `firmware/preview/` 重新產生）

## 回報

最後簡短告訴使用者：
- 新版本號（電腦端／韌體）
- 測試與建置是否通過
- 下一步要使用者做什麼（雙擊 exe、按更新、看螢幕）
