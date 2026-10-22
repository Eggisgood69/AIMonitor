# 一鍵建置：韌體 → 內建到 exe → AIMonitor.exe
#   ./build.ps1              建置全部
#   ./build.ps1 -Flash COM5  建置後直接燒錄韌體到螢幕（先結束 AIMonitor，避免序列埠被占用）
#   ./build.ps1 -AppOnly     只編譯電腦端程式
param([string]$Flash = "", [switch]$AppOnly)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$fqbn = 'esp32:esp32:esp32c3:CDCOnBoot=cdc'

if (-not $AppOnly) {
  Write-Host '== 編譯韌體 ==' -ForegroundColor Cyan
  $out = Join-Path $root 'build\firmware'
  # 公開的韌體檔不要內嵌本機路徑：Arduino 核心用 __FILE__ 記下原始檔完整路徑（C:\Users\<使用者>\AppData\...），換成 ~
  $noPath = "-fmacro-prefix-map=$env:USERPROFILE=~"
  $cliArgs = @('compile', '--fqbn', $fqbn, '--output-dir', $out,
    '--build-property', "compiler.c.extra_flags=$noPath", '--build-property', "compiler.cpp.extra_flags=$noPath",
    (Join-Path $root 'firmware\aimon'))
  arduino-cli @cliArgs
  if ($LASTEXITCODE) { throw '韌體編譯失敗' }
  # 編譯快取裡的舊 .o 不會因為參數改了就重編：還看得到本機路徑就整個重編一次
  $hasPath = { [Text.Encoding]::GetEncoding(28591).GetString([IO.File]::ReadAllBytes((Join-Path $out 'aimon.ino.bin'))).IndexOf($env:USERPROFILE, [StringComparison]::OrdinalIgnoreCase) -ge 0 }
  if (& $hasPath) {
    Write-Host '韌體裡還有本機路徑（舊的編譯快取），整個重新編譯' -ForegroundColor Yellow
    arduino-cli @cliArgs --clean
    if ($LASTEXITCODE) { throw '韌體編譯失敗' }
    if (& $hasPath) { throw "韌體裡還有本機路徑（$env:USERPROFILE）" }
  }

  $ver = (Select-String -Path (Join-Path $root 'firmware\aimon\aimon.ino') -Pattern '#define FW_VERSION "([^"]+)"').Matches[0].Groups[1].Value
  Copy-Item (Join-Path $out 'aimon.ino.bin') (Join-Path $root 'windows\fw\aimon-app.bin') -Force
  [IO.File]::WriteAllText((Join-Path $root 'windows\fw\version.txt'), $ver)
  Write-Host "韌體 $ver 已內建到 windows\fw\" -ForegroundColor Green

  # 完整映像（給網頁燒錄器用，從 0x0 燒）
  $esptool = Get-ChildItem "$env:LOCALAPPDATA\Arduino15\packages\esp32\tools\esptool_py" -Recurse -Filter 'esptool.exe' -ErrorAction SilentlyContinue | Select-Object -First 1
  $bootApp = Get-ChildItem "$env:LOCALAPPDATA\Arduino15\packages\esp32\hardware\esp32" -Recurse -Filter 'boot_app0.bin' -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($esptool -and $bootApp) {
    & $esptool.FullName --chip esp32c3 merge_bin -o (Join-Path $root 'firmware\aimon-usb-esp32c3.bin') --flash_mode dio --flash_freq 80m --flash_size 4MB `
      0x0 (Join-Path $out 'aimon.ino.bootloader.bin') 0x8000 (Join-Path $out 'aimon.ino.partitions.bin') 0xe000 $bootApp.FullName 0x10000 (Join-Path $out 'aimon.ino.bin') | Out-Null
  }

  if ($Flash) {
    Write-Host "== 燒錄到 $Flash ==" -ForegroundColor Cyan
    arduino-cli upload -p $Flash --fqbn $fqbn --input-dir $out (Join-Path $root 'firmware\aimon')
    if ($LASTEXITCODE) { throw '燒錄失敗（AIMonitor 是否還在執行？可在系統匣按「結束程式」）' }
  }
}

Write-Host '== 測試並編譯 AIMonitor.exe ==' -ForegroundColor Cyan
Push-Location (Join-Path $root 'windows')
try {
  go test ./...
  if ($LASTEXITCODE) { throw '測試失敗' }
  go build -trimpath -ldflags '-H windowsgui -s -w' -o AIMonitor.exe .
  if ($LASTEXITCODE) { throw '編譯失敗' }
} finally { Pop-Location }
Write-Host '完成：windows\AIMonitor.exe（雙擊即可取代執行中的舊版）' -ForegroundColor Green
