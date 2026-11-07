# 在 Windows 上重新產生 README 的預覽圖片（docs/images/states-v2.png、ais.png、pages-v3.png、demo-v2.gif）
# 用與韌體相同的 ui.h 渲染。需要：zig（winget install zig.zig）、Go、Arduino 程式庫 LovyanGFX
# 用法（專案根目錄）：./firmware/preview/build-preview.ps1
$ErrorActionPreference = 'Stop'
$root = Resolve-Path "$PSScriptRoot/../.."
$lib = "$env:USERPROFILE/Documents/Arduino/libraries/LovyanGFX/src"
if (-not (Test-Path $lib)) { throw "找不到 LovyanGFX：$lib" }
$zig = (Get-Command zig -ErrorAction SilentlyContinue).Source
if (-not $zig) {
  $zig = Get-ChildItem "$env:LOCALAPPDATA/Microsoft/WinGet/Packages/zig.zig_*/*/zig.exe" -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty FullName
}
if (-not $zig) { throw "找不到 zig，請先執行 winget install zig.zig" }

$work = Join-Path $env:TEMP 'aimon-preview'
$obj = Join-Path $work 'obj'
$out = Join-Path $work 'out'   # out/240、out/466
New-Item -ItemType Directory -Force $obj, $out | Out-Null
$flags = @('-std=c++17', '-O2', '-DLGFX_SDL', "-I$PSScriptRoot/win/fake", "-I$lib", '-include', "$PSScriptRoot/win/stub.hpp")

# LovyanGFX 只編一次（觸控、面板驅動也要編進去，平台函式由 win/stub.cpp 提供空的實作）
$cpp = Get-ChildItem "$lib/lgfx/v1/*.cpp"
$c = @(Get-ChildItem "$lib/lgfx/utility/*.c") + @(Get-ChildItem "$lib/lgfx/Fonts/IPA/*.c", "$lib/lgfx/Fonts/efont/*.c")
foreach ($f in $cpp) {
  $o = Join-Path $obj "$($f.Name).o"
  if (-not (Test-Path $o)) { & $zig c++ @flags -c $f.FullName -o $o; if ($LASTEXITCODE) { throw "編譯失敗：$($f.Name)" } }
}
foreach ($f in $c) {
  $o = Join-Path $obj "$($f.Name).o"
  if (-not (Test-Path $o)) { & $zig cc -O1 "-I$lib" -c $f.FullName -o $o; if ($LASTEXITCODE) { throw "編譯失敗：$($f.Name)" } }
}

$objs = Get-ChildItem "$obj/*.o" | ForEach-Object FullName
# 兩種螢幕各編一次：240 = 1.28 吋 LCD、466 = 1.75 吋 AMOLED
foreach ($size in 240, 466) {
  $exe = Join-Path $work "preview$size.exe"
  & $zig c++ @flags "-DLCD_W=$size" "$PSScriptRoot/preview.cpp" "$PSScriptRoot/win/stub.cpp" @objs -o $exe
  if ($LASTEXITCODE) { throw "preview.cpp 編譯失敗（$size）" }
  $dir = Join-Path $out $size
  New-Item -ItemType Directory -Force $dir | Out-Null
  Remove-Item "$dir/*.ppm" -ErrorAction SilentlyContinue
  Push-Location $dir
  try { & $exe; if ($LASTEXITCODE) { throw "preview$size.exe 執行失敗" } } finally { Pop-Location }
}

$img = Join-Path $root 'docs/images'
New-Item -ItemType Directory -Force $img | Out-Null
go run "$PSScriptRoot/compose/main.go" $out $img
if ($LASTEXITCODE) { throw "合成圖片失敗" }
Write-Host "已更新：docs/images/states-v2.png、ais.png、pages-v3.png、demo-v2.gif"
