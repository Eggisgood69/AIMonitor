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
# 排除：Claude App 背景任務的「停止此任務」（不是生成中）、分享 / 錄音 / 朗讀等
$STOP_NOT_RE = '(分享|共用|錄|播放|朗讀|語音|任務|share|record|play|read aloud|voice|sharing|task)'
$REVIEW_RE = '^(allow|allow once|allow always|always allow|allow for this chat|approve|允許|允許一次|一律允許|永遠允許|允許這次|核准)$'
# Claude App 側邊欄：背景在跑的對話旁有「執行中」狀態（橘點）；上方模式切換也會變成「Code，執行中」
$BUSY_RE = '^(.*[，,]\s*)?(執行中|running)$'

function Now-Ms { [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() }

$UIA = [System.Windows.Automation.AutomationElement]
$btnCond = New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::Button)
$editCond = New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::Edit)
$sideCond = New-Object System.Windows.Automation.OrCondition(
  (New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::StatusBar)),
  (New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::RadioButton)))
$walker = [System.Windows.Automation.TreeWalker]::RawViewWalker

$items = @{}        # key -> 狀態
$bg = @{}           # Claude App 背景對話：標題 -> 狀態（畫面上看不到停止鈕，只能看側邊欄）
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

# 畫面上是哪個對話：Claude App 標題列的「<對話名稱>，重新命名工作階段」按鈕（瀏覽器用網址）
function Get-View($names) {
  return [string]($names | Where-Object { $_ -match '重新命名|[Rr]ename' } | Select-Object -First 1)
}

function Update-Item([string]$key, [string]$ai, [bool]$focused, $names, [long]$now, [string]$view = '') {
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
    $s.genView = $view
  } elseif ($s.gen) {
    if (-not $s.genOffAt) { $s.genOffAt = $now }
    if ($now - $s.genOffAt -gt 1500) {
      $s.gen = $false
      # 畫面已經換成別的對話：是你切走了（那個對話還在跑），不算完成
      if (-not ($s.genView -and $view -and $view -ne $s.genView)) { $s.finishedAt = $now }
    }
  }
  if ($r -and -not $s.review) { $s.reviewSince = $now }
  $s.review = $r
}

function Is-Selected($e) {
  try { return [bool]$e.GetCurrentPattern([System.Windows.Automation.SelectionItemPattern]::Pattern).Current.IsSelected } catch {}
  try { return $e.GetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern).Current.ToggleState -eq 'On' } catch {}
  return $false
}

# Claude App 的背景對話（一般聊天）：切到別的對話後畫面上就沒有停止鈕，改看側邊欄的「執行中」。
# 每個背景對話一個項目 app:claude:bg:<標題>；畫面上正在看的那個由 app:claude 負責。
# Code 模式時側邊欄列的是 Claude Code 對話（hooks 已經有），只看「Claude」模式按鈕有沒有「執行中」
function Update-ClaudeBg($wins, [bool]$focused, [string]$view, [long]$now) {
  $mode = ''
  $chatBusy = $false
  $running = @{}
  foreach ($p in $wins) {
    try {
      $els = $UIA::FromHandle($p.MainWindowHandle).FindAll([System.Windows.Automation.TreeScope]::Descendants, $sideCond)
      foreach ($e in $els) {
        $c = $e.Current
        $n = [string]$c.Name
        if ($c.ControlType -eq [System.Windows.Automation.ControlType]::RadioButton) {
          if ($n -match '^Claude') {
            if (Is-Selected $e) { $mode = 'chat' }
            if ($n -match $BUSY_RE) { $chatBusy = $true }
          } elseif ($n -match '^Code' -and (Is-Selected $e)) { $mode = 'code' }
        } elseif ($n -match $BUSY_RE) {
          $b = $walker.GetPreviousSibling($e)
          if ($b -and $b.Current.ControlType -eq [System.Windows.Automation.ControlType]::Button -and $b.Current.Name) { $running[$b.Current.Name.Trim()] = $true }
        }
      }
    } catch {}
  }
  if ($mode -eq 'chat') {
    foreach ($t in $running.Keys) {
      if ($t -eq $view) { continue }
      if (-not $bg.ContainsKey($t)) { $bg[$t] = @{ gen = $false; genSince = 0; genOffAt = 0; finishedAt = 0; viewedAt = 0 } }
      $s = $bg[$t]
      if (-not $s.gen) { $s.gen = $true; $s.genSince = $now; $s.viewedAt = 0 }
      $s.genOffAt = 0
    }
  }
  foreach ($t in @($bg.Keys)) {
    $s = $bg[$t]
    if ($mode -eq 'chat' -and $t -eq $view) {
      # 你點開了這個對話：還在跑就交給 app:claude（畫面上有停止鈕）；已完成就算看過，亮 8 秒後移除
      if ($s.gen) { $bg.Remove($t); continue }
      if ($focused -and -not $s.viewedAt) { $s.viewedAt = $now }
    }
    if ($s.viewedAt -and $now - $s.viewedAt -gt 10000) { $bg.Remove($t); continue }
    $still = if ($mode -eq 'chat') { $running.ContainsKey($t) } elseif ($mode -eq 'code') { $chatBusy } else { $s.gen }
    if ($s.gen -and -not $still) {
      # 跟停止鈕一樣，要連續 1.5 秒沒有「執行中」才算完成
      if (-not $s.genOffAt) { $s.genOffAt = $now }
      if ($now - $s.genOffAt -gt 1500) { $s.gen = $false; $s.finishedAt = $now }
    }
    if (-not $s.gen -and $s.finishedAt -and $now - $s.finishedAt -gt 12 * 3600000) { $bg.Remove($t); continue }
    $items['app:claude:bg:' + $t] = @{ key = 'app:claude:bg:' + $t; ai = 'claude'; focused = [bool]($s.viewedAt -and $focused); gen = $s.gen; genSince = $s.genSince;
                                       review = $false; reviewSince = 0; finishedAt = $s.finishedAt; seen = $now; buttons = @($t) }
  }
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
    if ($wins.Count -eq 0) { if ($a.ai -eq 'claude') { $bg.Clear() }; continue }
    $names = New-Object System.Collections.Generic.List[string]
    foreach ($p in $wins) { $names.AddRange((Get-ButtonNames $p.MainWindowHandle)) }
    $focused = [bool]($procs | Where-Object { $_.Id -eq $fgPid })
    $view = Get-View $names
    Update-Item ('app:' + $a.ai) $a.ai $focused $names $now $view
    if ($a.ai -eq 'claude') { Update-ClaudeBg $wins $focused ($view -replace '\s*[，,]\s*(重新命名工作階段|rename.*)$', '') $now }
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
    if (-not $agg.ContainsKey($key)) { $agg[$key] = @{ ai = $ai; focused = $false; view = $url; names = (New-Object System.Collections.Generic.List[string]) } }
    $agg[$key].names.AddRange((Get-ButtonNames $h))
    if ($h -eq $fg) { $agg[$key].focused = $true }
  }
  foreach ($key in $agg.Keys) { Update-Item $key $agg[$key].ai $agg[$key].focused $agg[$key].names $now $agg[$key].view }

  # 這次沒看到的項目移除（App 關掉、分頁換走）
  foreach ($key in @($items.Keys)) { if ($items[$key].seen -ne $now) { $items.Remove($key) } }
  $script:diagText = ($diag -join ' ｜ ')
  $script:fgName = $fgName
}
$script:diagText = ''
$script:fgName = ''


function Get-StateJson {
  $list = @()
  foreach ($s in $items.Values) {
    $list += [ordered]@{ key = $s.key; ai = $s.ai; focused = $s.focused; gen = $s.gen; genSince = $s.genSince;
                         review = $s.review; reviewSince = $s.reviewSince; finishedAt = $s.finishedAt; buttons = @($s.buttons) }
  }
  return (ConvertTo-Json -InputObject ([ordered]@{ t = (Now-Ms); items = @($list); diag = $script:diagText; fg = $script:fgName; uia = [bool]$UIA }) -Depth 4 -Compress)
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
