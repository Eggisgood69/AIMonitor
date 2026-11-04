# AMOLED 在麥克風模式（TinyUSB，USB PID 303A:81A1）時，ESP32 的燒錄協定進不去。
# 燒錄前先用這個：送 {"mic":0} 讓螢幕重開回一般 USB（hwcdc，PID 1001），輸出它的新 COM 埠。
# 不是麥克風模式的埠就原樣輸出。先結束 AIMonitor（它會照設定再把麥克風打開，也會占用序列埠）。
#   $port = ./firmware/tools/mic-off.ps1 COM9
param([Parameter(Mandatory = $true)][string]$Port)
$ErrorActionPreference = 'Stop'

function Get-Ports {
  Get-CimInstance Win32_PnPEntity | Where-Object { $_.Name -match '\((COM\d+)\)' } |
    ForEach-Object { [pscustomobject]@{ Name = ([regex]::Match($_.Name, '\((COM\d+)\)')).Groups[1].Value; Id = $_.PNPDeviceID } }
}

$p = Get-Ports | Where-Object { $_.Name -eq $Port } | Select-Object -First 1
if (-not $p -or $p.Id -notmatch 'VID_303A&PID_81A1') { return $Port }

Write-Host "$Port 是麥克風模式，先關掉麥克風（螢幕會重新開機）…" -ForegroundColor Yellow
$before = @(Get-Ports | Where-Object { $_.Id -match 'VID_303A&PID_1001' } | ForEach-Object { $_.Name })
$sp = New-Object System.IO.Ports.SerialPort $Port, 115200
$sp.Open()
$sp.Write("{`"mic`":0}`n")
Start-Sleep -Milliseconds 300
try { $sp.Close() } catch {}   # 螢幕已經重開，關埠可能失敗

for ($i = 0; $i -lt 40; $i++) {
  Start-Sleep -Milliseconds 500
  $new = Get-Ports | Where-Object { $_.Id -match 'VID_303A&PID_1001' -and $before -notcontains $_.Name } | Select-Object -First 1
  if ($new) { Start-Sleep -Seconds 1; Write-Host "螢幕回到一般 USB：$($new.Name)" -ForegroundColor Green; return $new.Name }
}
throw "螢幕沒有從麥克風模式回來（$Port），請拔掉 USB 再插上"
