# AI 狀態指示燈 — 畫面偵測（由 AIMonitor.exe 在背景啟動，不需要自己執行）
#
# 用 Windows 內建的 UI Automation 讀取畫面上的按鈕：
#   - Claude / ChatGPT 桌面 App
#   - Chrome / Edge / Brave / Firefox 等瀏覽器裡的 claude.ai、chatgpt.com、gemini.google.com 分頁
# 「停止」按鈕 = 生成中，「允許」按鈕 = 需審核，停止按鈕消失 = 完成。
# 每秒輸出一行 JSON 到 stdout 給 AIMonitor.exe。
#
# 手動除錯：powershell -ExecutionPolicy Bypass -File monitor.ps1 -Diag

param([switch]$Diag, [int]$ParentPid = 0)

$ErrorActionPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
public static class AimonWin {
  [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint pid);
  [DllImport("user32.dll")] public static extern bool IsWindow(IntPtr hWnd);
}
"@

# 桌面 App（行程名稱，不含 .exe）
$APPS = @(
  @{ ai = 'claude';  procs = @('claude') },
  @{ ai = 'chatgpt'; procs = @('ChatGPT') }
)
# 瀏覽器
$BROWSERS = @('chrome', 'msedge', 'brave', 'opera', 'vivaldi', 'firefox', 'arc')
$HOSTS = [ordered]@{ 'claude.ai' = 'claude'; 'chatgpt.com' = 'chatgpt'; 'chat.openai.com' = 'chatgpt'; 'gemini.google.com' = 'gemini' }
$HOST_RE = '^(https?://)?(www\.)?(claude\.ai|chatgpt\.com|chat\.openai\.com|gemini\.google\.com)(/|$|\?)'

# 「停止生成」按鈕 → 生成中；「允許 / 核准」按鈕 → 需審核
# Gemini 的停止鈕名稱各語言版本不同（例如「停止回覆」「停止產生回覆」），所以「停止 / Stop 開頭的短名稱」都算
$STOP_RE   = '^(stop( (response|responding|generating|streaming|generation))?|停止.{0,6}|中止.{0,4}|停止 .{0,8})$'
$STOP_NOT_RE = '(分享|共用|錄|播放|朗讀|語音|share|record|play|read aloud|voice|sharing)'
$REVIEW_RE = '^(allow|allow once|allow always|always allow|allow for this chat|approve|允許|允許一次|一律允許|永遠允許|允許這次|核准)$'

function Now-Ms { [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() }

$UIA = [System.Windows.Automation.AutomationElement]
$btnCond = New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::Button)
$editCond = New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::Edit)

$items = @{}        # key -> 狀態
$tracked = @{}      # 瀏覽器視窗 hwnd -> $true（前景過的視窗，切走後仍持續追蹤，才能偵測到「背景完成」）
$omni = @{}         # 瀏覽器視窗 hwnd -> 網址列元素（快取，避免每次搜尋整個畫面）

function Get-ButtonNames([IntPtr]$hwnd) {
  $names = New-Object System.Collections.Generic.List[string]
  try {
    $root = $UIA::FromHandle($hwnd)
    $els = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, $btnCond)
    foreach ($e in $els) {
      try {
        $n = $e.Current.Name
        if ($n -and -not $e.Current.IsOffscreen) { $names.Add($n.Trim()) }
      } catch {}
    }
  } catch {}
  return ,$names
}

function Get-Url([IntPtr]$hwnd) {
  $k = [string]$hwnd
  if ($omni.ContainsKey($k)) {
    try {
      $vp = $omni[$k].GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
      return [string]$vp.Current.Value
    } catch { $omni.Remove($k) }
  }
  try {
    $root = $UIA::FromHandle($hwnd)
    $edits = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, $editCond)
    $fallback = $null
    foreach ($e in $edits) {
      try {
        $vp = $e.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
        $v = [string]$vp.Current.Value
        if ($v -match $HOST_RE) { $omni[$k] = $e; return $v }
        # 看起來像網址的輸入框（網址列）
        if (-not $fallback -and $v -match '^\S+\.\S+$') { $fallback = $e }
      } catch {}
    }
    if ($fallback) { $omni[$k] = $fallback; return [string]$fallback.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern).Current.Value }
  } catch {}
  return ''
}

function Url-To-AI([string]$url) {
  if ($url -notmatch $HOST_RE) { return $null }
  $h = $Matches[3]
  return $HOSTS[$h]
}

function Update-Item([string]$key, [string]$ai, [bool]$focused, $names, [long]$now) {
  if (-not $items.ContainsKey($key)) {
    $items[$key] = @{ key = $key; ai = $ai; gen = $false; genSince = 0; genOffAt = 0; review = $false; reviewSince = 0; finishedAt = 0 }
  }
  $s = $items[$key]
  $s.ai = $ai
  $s.focused = $focused
  $s.seen = $now
  $stops = @($names | Where-Object { $_ -match $STOP_RE -and $_ -notmatch $STOP_NOT_RE })
  $reviews = @($names | Where-Object { $_ -match $REVIEW_RE })
  # 除錯畫面只列 30 個：判斷依據（停止 / 允許）排最前面，才不會被一般按鈕擠掉
  $s.buttons = @(@($stops) + @($reviews) + @($names) | Select-Object -Unique | Select-Object -First 30)
  $g = $stops.Count -gt 0
  $r = $reviews.Count -gt 0
  # 停止按鈕會短暫消失，要連續消失 1.5 秒才算真的結束
  if ($g) {
    if (-not $s.gen) { $s.gen = $true; $s.genSince = $now }
    $s.genOffAt = 0
  } elseif ($s.gen) {
    if (-not $s.genOffAt) { $s.genOffAt = $now }
    if ($now - $s.genOffAt -gt 1500) { $s.gen = $false; $s.finishedAt = $now }
  }
  if ($r -and -not $s.review) { $s.reviewSince = $now }
  $s.review = $r
}

function Scan {
  $now = Now-Ms
  $fg = [AimonWin]::GetForegroundWindow()
  [uint32]$fgPid = 0
  [void][AimonWin]::GetWindowThreadProcessId($fg, [ref]$fgPid)
  $fgProc = Get-Process -Id $fgPid
  $fgName = if ($fgProc) { $fgProc.ProcessName.ToLower() } else { '' }
  $diag = New-Object System.Collections.Generic.List[string]
  $diag.Add("前景：$fgName")

  # ---- 桌面 App ----
  foreach ($a in $APPS) {
    $procs = @(Get-Process -Name $a.procs)
    $wins = @($procs | Where-Object { $_.MainWindowHandle -ne [IntPtr]::Zero })
    $diag.Add(("{0} App：行程 {1} 個、視窗 {2} 個" -f $a.ai, $procs.Count, $wins.Count))
    if ($wins.Count -eq 0) { continue }
    $names = New-Object System.Collections.Generic.List[string]
    foreach ($p in $wins) { $names.AddRange((Get-ButtonNames $p.MainWindowHandle)) }
    $focused = [bool]($procs | Where-Object { $_.Id -eq $fgPid })
    Update-Item ('app:' + $a.ai) $a.ai $focused $names $now
  }

  # ---- 瀏覽器分頁 ----
  if ($BROWSERS -contains $fgName) { $tracked[[string]$fg] = $true }
  $agg = @{}
  foreach ($k in @($tracked.Keys)) {
    $h = [IntPtr][long]$k
    if (-not [AimonWin]::IsWindow($h)) { $tracked.Remove($k); $omni.Remove($k); continue }
    $url = Get-Url $h
    $ai = Url-To-AI $url
    $diag.Add(("瀏覽器視窗 {0}：{1}" -f $k, ($url -replace '^https?://', '').Substring(0, [Math]::Min(40, ($url -replace '^https?://', '').Length))))
    if (-not $ai) {
      if ($h -ne $fg) { $tracked.Remove($k) }   # 背景視窗已經不是 AI 網頁：不再追蹤
      continue
    }
    $key = 'web:' + $ai
    if (-not $agg.ContainsKey($key)) { $agg[$key] = @{ ai = $ai; focused = $false; names = (New-Object System.Collections.Generic.List[string]) } }
    $agg[$key].names.AddRange((Get-ButtonNames $h))
    if ($h -eq $fg) { $agg[$key].focused = $true }
  }
  foreach ($key in $agg.Keys) { Update-Item $key $agg[$key].ai $agg[$key].focused $agg[$key].names $now }

  # 這次沒看到的項目移除（App 關掉、分頁換走）
  foreach ($key in @($items.Keys)) { if ($items[$key].seen -ne $now) { $items.Remove($key) } }
  $script:diagText = ($diag -join ' ｜ ')
}
$script:diagText = ''


function Get-StateJson {
  $list = @()
  foreach ($s in $items.Values) {
    $list += [ordered]@{ key = $s.key; ai = $s.ai; focused = $s.focused; gen = $s.gen; genSince = $s.genSince;
                         review = $s.review; reviewSince = $s.reviewSince; finishedAt = $s.finishedAt; buttons = @($s.buttons) }
  }
  return (ConvertTo-Json -InputObject ([ordered]@{ t = (Now-Ms); items = @($list); diag = $script:diagText; uia = [bool]$UIA }) -Depth 4 -Compress)
}

# ---- 主迴圈 ----
if ($Diag) { Write-Host "AI 狀態指示燈 - 畫面偵測（診斷模式），Ctrl+C 結束`n" -ForegroundColor Cyan }
while ($true) {
  $t0 = Now-Ms
  Scan
  if ($Diag) {
    if ($items.Count -eq 0) { Write-Host ("[{0}] 沒有偵測到 AI App 或 AI 網頁（瀏覽器請先切到 AI 分頁）" -f (Get-Date -Format 'HH:mm:ss')) -ForegroundColor DarkGray }
    foreach ($s in $items.Values) {
      $st = if ($s.review) { '需審核' } elseif ($s.gen) { '生成中' } elseif ($s.finishedAt) { '已完成' } else { '待命' }
      Write-Host ("[{0}] {1,-12} 狀態={2}  前景={3}" -f (Get-Date -Format 'HH:mm:ss'), $s.key, $st, $s.focused) -ForegroundColor Green
      Write-Host ("             按鈕：" + ($s.buttons -join ' | ')) -ForegroundColor DarkGray
    }
  } else {
    [Console]::Out.WriteLine((Get-StateJson))
    [Console]::Out.Flush()
  }
  if ($ParentPid -and -not (Get-Process -Id $ParentPid)) { exit }   # AIMonitor.exe 結束就跟著結束
  $wait = 1000 - ((Now-Ms) - $t0)
  if ($wait -gt 50) { Start-Sleep -Milliseconds $wait }
}
