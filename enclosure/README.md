# 3D 列印外殼（SolidWorks，設計中）

兩種螢幕的板子尺寸不同，各有一個資料夾，各畫各的：

| 資料夾 | 螢幕 | 板子 |
|---|---|---|
| [`lcd128/`](lcd128/) | 1.28 吋 LCD | ESP32-2424S012N |
| [`amoled175/`](amoled175/) | 1.75 吋 AMOLED | Waveshare ESP32-S3-Touch-AMOLED-1.75 |

## 每個資料夾的結構

| 位置 | 內容 | 公開 |
|---|---|---|
| `*.SLDPRT` / `*.SLDASM` | 原始檔（零件、組合件） | ✓ |
| `export/` | 從原始檔匯出的列印檔（3MF / STL）與 STEP，每次改完重新匯出覆蓋 | ✓ |
| `reference/` | 板子 3D 模型 | 官方模型（amoled175 的 Waveshare STEP）✓；來源不明的網路模型（lcd128）✗，`.gitignore` 排除 |

- 檔名用英文小寫，一個零件一個檔，例如 `case.SLDPRT`、`bezel.SLDPRT`；匯出檔跟原始檔同名（`export/case.3MF`）。
- 建議自己依實物量測畫一個 `board.SLDPRT`（簡化外型就好），用組合件 `assembly.SLDASM` 把板子跟外殼放在一起檢查干涉。
  自己畫的板子模型可以公開；組合件會把用到的板子模型一起存進去，所以只用可以公開的模型組合。
- SolidWorks 開檔時的鎖定檔 `~$*` 和備份檔已經排除，不會 commit。
- SolidWorks 檔是二進位檔，git 無法合併或比較差異；改了什麼寫在 commit 訊息裡。
- 搬動或改名檔案前先在 SolidWorks 關掉它們，組合件會依相對位置找零件，整個資料夾一起搬就不會斷。

## 設計備忘

- 列印公差：孔、卡槽單邊留 0.15-0.3 mm（看印表機）。
- 透光：白色或透明 PETG，壁厚 1.2-2.0 mm；越薄越亮，LED 前面貼描圖紙光比較均勻。
- 外接 LED 的接線見主 [README](../README.md) 的「選配：外接狀態 LED」。
