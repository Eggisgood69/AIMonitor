# 在 Windows 上重新產生 README 的預覽圖片（preview.png、preview-pages.png、preview-saver.png、preview.gif）
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
$out = Join-Path $work 'out'
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

$exe = Join-Path $work 'preview.exe'
$objs = Get-ChildItem "$obj/*.o" | ForEach-Object FullName
& $zig c++ @flags "$PSScriptRoot/preview.cpp" "$PSScriptRoot/win/stub.cpp" @objs -o $exe
if ($LASTEXITCODE) { throw "preview.cpp 編譯失敗" }

Remove-Item "$out/*.ppm" -ErrorAction SilentlyContinue
Push-Location $out
try { & $exe; if ($LASTEXITCODE) { throw "preview.exe 執行失敗" } } finally { Pop-Location }

go run "$PSScriptRoot/compose/main.go" $out $root
if ($LASTEXITCODE) { throw "合成圖片失敗" }
Write-Host "已更新：preview.png、preview-pages.png、preview-saver.png、preview.gif"
