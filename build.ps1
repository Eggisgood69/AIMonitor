# 一鍵建置：韌體（兩種螢幕）→ 內建到 exe → AIMonitor.exe
#   ./build.ps1              建置全部
#   ./build.ps1 -Flash COM5  建置後直接燒錄韌體到螢幕（自動判斷是哪種螢幕；先結束 AIMonitor，避免序列埠被占用）
#   ./build.ps1 -Flash COM8 -Board s3a175   指定螢幕種類（c3 / s3a175）
#   ./build.ps1 -AppOnly     只編譯電腦端程式
param([string]$Flash = "", [string]$Board = "", [switch]$AppOnly)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot

# 支援的螢幕：Id 要跟韌體 config.h 的 BOARD_ID 一致（螢幕 hello 回報的 hw，電腦端依它選韌體）
$boards = @(
  @{ Id = 'c3'; Chip = 'esp32c3'; Fqbn = 'esp32:esp32:esp32c3:CDCOnBoot=cdc'
     Full = 'aimon-usb-esp32c3.bin'; MergeArgs = @('--flash_mode', 'dio', '--flash_freq', '80m', '--flash_size', '4MB') },
  @{ Id = 's3a175'; Chip = 'esp32s3'; Fqbn = 'esp32:esp32:esp32s3:CDCOnBoot=cdc,USBMode=hwcdc,PSRAM=opi,FlashSize=16M,PartitionScheme=app3M_fat9M_16MB'
     Full = 'aimon-usb-esp32s3-amoled175.bin'; MergeArgs = @('--flash_mode', 'keep', '--flash_freq', 'keep', '--flash_size', '16MB') }
)

if (-not $AppOnly) {
  $esptool = Get-ChildItem "$env:LOCALAPPDATA\Arduino15\packages\esp32\tools\esptool_py" -Recurse -Filter 'esptool.exe' -ErrorAction SilentlyContinue | Select-Object -First 1
  $bootApp = Get-ChildItem "$env:LOCALAPPDATA\Arduino15\packages\esp32\hardware\esp32" -Recurse -Filter 'boot_app0.bin' -ErrorAction SilentlyContinue | Select-Object -First 1
  # 公開的韌體檔不要內嵌本機路徑：Arduino 核心用 __FILE__ 記下原始檔完整路徑（C:\Users\<使用者>\AppData\...），換成 ~
  $noPath = "-fmacro-prefix-map=$env:USERPROFILE=~"
  $ver = (Select-String -Path (Join-Path $root 'firmware\aimon\aimon.ino') -Pattern '#define FW_VERSION "([^"]+)"').Matches[0].Groups[1].Value

  foreach ($b in $boards) {
    Write-Host "== 編譯韌體（$($b.Id)）==" -ForegroundColor Cyan
    $out = Join-Path $root "build\firmware\$($b.Id)"
    $cliArgs = @('compile', '--fqbn', $b.Fqbn, '--output-dir', $out,
      '--build-property', "compiler.c.extra_flags=$noPath", '--build-property', "compiler.cpp.extra_flags=$noPath",
      (Join-Path $root 'firmware\aimon'))
    arduino-cli @cliArgs
    if ($LASTEXITCODE) { throw "韌體編譯失敗（$($b.Id)）" }
    # 編譯快取裡的舊 .o 不會因為參數改了就重編：還看得到本機路徑就整個重編一次
    $hasPath = { [Text.Encoding]::GetEncoding(28591).GetString([IO.File]::ReadAllBytes((Join-Path $out 'aimon.ino.bin'))).IndexOf($env:USERPROFILE, [StringComparison]::OrdinalIgnoreCase) -ge 0 }
    if (& $hasPath) {
      Write-Host '韌體裡還有本機路徑（舊的編譯快取），整個重新編譯' -ForegroundColor Yellow
      arduino-cli @cliArgs --clean
      if ($LASTEXITCODE) { throw "韌體編譯失敗（$($b.Id)）" }
      if (& $hasPath) { throw "韌體裡還有本機路徑（$env:USERPROFILE）" }
    }
    Copy-Item (Join-Path $out 'aimon.ino.bin') (Join-Path $root "windows\fw\aimon-$($b.Id).bin") -Force

    # 完整映像（給網頁燒錄器用，從 0x0 燒）
    if ($esptool -and $bootApp) {
      $mergeArgs = @('--chip', $b.Chip, 'merge_bin', '-o', (Join-Path $root "firmware\$($b.Full)")) + $b.MergeArgs + @(
        '0x0', (Join-Path $out 'aimon.ino.bootloader.bin'), '0x8000', (Join-Path $out 'aimon.ino.partitions.bin'),
        '0xe000', $bootApp.FullName, '0x10000', (Join-Path $out 'aimon.ino.bin'))
      & $esptool.FullName @mergeArgs | Out-Null
      if ($LASTEXITCODE) { throw "產生完整映像失敗（$($b.Id)）" }
    }
  }
  [IO.File]::WriteAllText((Join-Path $root 'windows\fw\version.txt'), $ver)
  Write-Host "韌體 $ver 已內建到 windows\fw\" -ForegroundColor Green

  if ($Flash) {
    # AMOLED 開著麥克風時是另一個 COM 埠（TinyUSB），燒錄不了：先讓它重開回一般 USB
    $Flash = & (Join-Path $root 'firmware\tools\mic-off.ps1') $Flash
    if (-not $Board) {
      # 讀晶片型號，選對的韌體（燒錯晶片的韌體 esptool 也會拒絕，但先判斷比較清楚）
      if (-not $esptool) { throw '找不到 esptool，請用 -Board 指定螢幕種類（c3 / s3a175）' }
      $id = (& $esptool.FullName --port $Flash chip_id 2>&1) -join "`n"
      $chip = if ($id -match 'ESP32-S3') { 'esp32s3' } elseif ($id -match 'ESP32-C3') { 'esp32c3' } else { '' }
      if (-not $chip) { throw "讀不到 $Flash 的晶片型號（AIMonitor 是否還在執行？）：`n$id" }
      $Board = ($boards | Where-Object { $_.Chip -eq $chip } | Select-Object -First 1).Id
    }
    $b = $boards | Where-Object { $_.Id -eq $Board } | Select-Object -First 1
    if (-not $b) { throw "不認得的螢幕種類：$Board（c3 / s3a175）" }
    Write-Host "== 燒錄到 $Flash（$($b.Id)）==" -ForegroundColor Cyan
    arduino-cli upload -p $Flash --fqbn $b.Fqbn --input-dir (Join-Path $root "build\firmware\$($b.Id)") (Join-Path $root 'firmware\aimon')
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
