# AI 狀態指示燈 — 畫面偵測（由 AIMonitor.exe 在背景啟動，不需要自己執行）
#
# 用 Windows 內建的 UI Automation 讀取畫面上的按鈕：
#   - Claude / ChatGPT / Gemini 桌面 App
#   - Google 試算表裡的 Claude for Sheets 側欄（算成 Claude）
#   claude.ai、chatgpt.com、gemini.google.com 網頁改由瀏覽器擴充功能偵測（windows/extension/）：
#   這裡只讀得到每個視窗正在顯示的分頁，切到別的分頁就不見了，有時亮有時不亮反而讓人以為壞掉
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
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);
}
"@

# 桌面 App（行程名稱，不含 .exe）
$APPS = @(
  @{ ai = 'claude';  procs = @('claude') },
  @{ ai = 'chatgpt'; procs = @('ChatGPT') },
  @{ ai = 'gemini';  procs = @('Gemini') }   # Google 的 Gemini 桌面程式（%LOCALAPPDATA%\Google\Gemini），停止鈕「停止回覆」跟網頁一樣
)
# 瀏覽器
$BROWSERS = @('chrome', 'msedge', 'brave', 'opera', 'vivaldi', 'firefox', 'arc')
$HOST_RE = '^(https?://)?(www\.)?(claude\.ai|chatgpt\.com|chat\.openai\.com|gemini\.google\.com)(/|$|\?)'
# Google 試算表：Claude for Sheets 側欄（生成時「Stop」、等允許時「Allow once」）。試算表也有 Gemini 側欄，
# 所以只在看得到 Claude 側欄自己的按鈕時才算；除錯清單只留判斷用的按鈕（工具列很多、帳戶按鈕有 email）
$SHEETS_RE = '^(https?://)?docs\.google\.com/spreadsheets/'
$SHEETS_CLAUDE = @('Model and response options', 'Queue message', 'Ask before edits', 'Chat history')

# 「停止生成」按鈕 → 生成中；「允許 / 核准」按鈕 → 需審核
# Gemini 的停止鈕名稱各語言版本不同（例如「停止回覆」「停止產生回覆」），所以「停止 / Stop 開頭的短名稱」都算
$STOP_RE   = '^(stop( (response|responding|generating|streaming|generation))?|停止.{0,6}|中止.{0,4}|停止 .{0,8})$'
# 排除：Claude App 背景任務的「停止此任務」、預覽伺服器的「停止此伺服器」（都不是生成中）、分享 / 錄音 / 朗讀等
$STOP_NOT_RE = '(分享|共用|錄|播放|朗讀|語音|任務|伺服器|share|record|play|read aloud|voice|sharing|task|server)'
# 允許按鈕的說法常變（允許一次、允許這次、允許本次工作階段、Allow for this chat…）：「允許 / allow」開頭的短名稱都算
$REVIEW_RE = '^(allow( .{1,30})?|always allow|approve|允許.{0,8}|一律允許|永遠允許|核准)$'
# Claude App 側邊欄：背景在跑的對話旁有「執行中」狀態（橘點）；上方模式切換也會變成「Code，執行中」
$BUSY_RE = '^(.*[，,]\s*)?(執行中|running)$'
# 背景聊天在等你允許：側邊欄是「等待輸入」，Code 模式時模式按鈕是「Claude，正在等待您的輸入」
$WAIT_RE = '^(.*[，,]\s*)?(等待輸入|正在等待您的輸入|waiting for (your )?input|needs? (your )?input)$'
# ChatGPT App（ChatGPT / Codex 合一，2026-11 實測）側邊欄：對話列（ListItem > Group > Button）裡有「處理中」= 在跑；
# 等你核准時對話列名稱後面多「待核准」，完成後沒點開前面多「未讀」。英文介面的說法沒實測過
$GPT_BUSY_RE = '^(處理中|processing)$'
$GPT_WAIT_RE = '^(未讀|unread)?(.+?)(待核准|awaiting approval|needs approval)$'
# 出錯：ChatGPT 聊天模式生成失敗時停止鈕消失、出現「再試一次」（2026-11 實測）。舊訊息上的也會一直留著，
# 所以要比生成開始時多一個才算這次出錯。Codex 模式斷網只會「正在重新連線 n/5」，網路回來自己接著做完，不算錯誤
$RETRY_RE = '^(再試一次|重試|重新嘗試|try again|retry)$'

function Now-Ms { [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() }

$UIA = [System.Windows.Automation.AutomationElement]
$btnCond = New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::Button)
$editCond = New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::Edit)
$sideCond = New-Object System.Windows.Automation.OrCondition(
  (New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::StatusBar)),
  (New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::RadioButton)))
$gptCond = New-Object System.Windows.Automation.OrCondition(
  (New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::StatusBar)),
  (New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::ListItem)),
  (New-Object System.Windows.Automation.PropertyCondition($UIA::ControlTypeProperty, [System.Windows.Automation.ControlType]::Document)))
$walker = [System.Windows.Automation.TreeWalker]::RawViewWalker

$items = @{}        # key -> 狀態
$bg = @{}           # Claude App 背景對話：標題 -> 狀態（畫面上看不到停止鈕，只能看側邊欄）
$bgGpt = @{}        # ChatGPT App 背景對話：標題 -> 狀態
$appWins = @{}      # 桌面 App -> 看過的主視窗 handle（視窗隱藏後還能繼續讀）
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
        # 捲到畫面外的「再試一次」也要算（前面加 ~，不會被當成別的按鈕）：只算畫面上的話，捲動或縮小視窗時數量會變，誤判成出錯
        elseif ($n -and $n.Trim() -match $RETRY_RE) { $names.Add('~' + $n.Trim()) }
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
        if ($v -match $HOST_RE -or $v -match $SHEETS_RE) { $omni[$k] = $e; return $v }
        # 看起來像網址的輸入框（網址列）
        if (-not $fallback -and $v -match '^\S+\.\S+$') { $fallback = $e }
      } catch {}
    }
    if ($fallback) { $omni[$k] = $fallback; return [string]$fallback.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern).Current.Value }
  } catch {}
  return ''
}

function Url-To-AI([string]$url) {
  if ($url -match $SHEETS_RE) { return 'claude' }
  return $null   # AI 網站本身交給擴充功能（見檔案開頭）
}

# 畫面上是哪個對話：Claude App 標題列的「<對話名稱>，重新命名工作階段」按鈕（瀏覽器用網址）
# 只取對話名稱：按鈕是「<名稱>，重新命名工作階段」，載入中還會多「，對話載入後即可重新命名」
function Get-View($names) {
  $n = [string]($names | Where-Object { $_ -match '重新命名|[Rr]ename' } | Select-Object -First 1)
  return ($n -replace '\s*[，,]\s*(重新命名|[Rr]ename).*$', '')
}

function Update-Item([string]$key, [string]$ai, [bool]$focused, $names, [long]$now, [string]$view = '', [bool]$busy = $false, [bool]$wait = $false) {
  if (-not $items.ContainsKey($key)) {
    $items[$key] = @{ key = $key; ai = $ai; gen = $false; genSince = 0; genOffAt = 0; review = $false; reviewSince = 0; finishedAt = 0; failed = $false; msg = ''; retries = 0 }
  }
  $s = $items[$key]
  $s.ai = $ai
  $s.focused = $focused
  $s.seen = $now
  $s.view = $view
  $stops = @($names | Where-Object { $_ -match $STOP_RE -and $_ -notmatch $STOP_NOT_RE })
  $reviews = @($names | Where-Object { $_ -match $REVIEW_RE })
  $retries = @($names | Where-Object { ($_ -replace '^~', '') -match $RETRY_RE }).Count
  # 除錯畫面只列 30 個：判斷依據（停止 / 允許）排最前面，才不會被一般按鈕擠掉
  $s.buttons = @(@($stops) + @($reviews) + @($names | Where-Object { $_ -notlike '~*' }) | Select-Object -Unique | Select-Object -First 30)
  # $busy：Claude App 側邊欄說畫面上這個對話「執行中」（停止鈕在跑工具等時候會暫時不見）
  $g = $stops.Count -gt 0 -or $busy
  $r = $reviews.Count -gt 0 -or $wait
  # 停止按鈕會短暫消失，要連續消失 1.5 秒才算真的結束
  if ($g) {
    if (-not $s.gen) { $s.gen = $true; $s.genSince = $now; $s.failed = $false; $s.msg = '' }
    $s.genOffAt = 0
    if ($s.genView -ne $view) { $s.retries = $retries }   # 開始生成 / 換了對話：記下畫面上原本有幾個「再試一次」
    $s.genView = $view
  } elseif ($s.gen) {
    if (-not $s.genOffAt) { $s.genOffAt = $now }
    if ($now - $s.genOffAt -gt 1500) {
      $s.gen = $false
      # 畫面已經換成別的對話：是你切走了（那個對話還在跑），不算完成
      if (-not ($s.genView -and $view -and $view -ne $s.genView)) {
        $s.finishedAt = $now
        # 只有 ChatGPT 用「再試一次」判斷出錯：Claude 對話模式每則回答下面本來就有「重試」，回答完才出現，會被誤判成出錯
        if ($ai -eq 'chatgpt' -and $retries -gt $s.retries) { $s.failed = $true; $s.msg = 'FAILED' }
      }
    }
  }
  if (-not $s.gen) { $s.genView = '' }
  if ($r -and -not $s.review) { $s.reviewSince = $now }
  $s.review = $r
}

function Is-Selected($e) {
  try { return [bool]$e.GetCurrentPattern([System.Windows.Automation.SelectionItemPattern]::Pattern).Current.IsSelected } catch {}
  try { return $e.GetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern).Current.ToggleState -eq 'On' } catch {}
  return $false
}

# Claude App 的背景對話（一般聊天）：切到別的對話後畫面上就沒有停止鈕，改看側邊欄對話旁的狀態：
# 「執行中」= 生成中、「等待輸入」= 在等你允許（亮黃燈）。出錯時狀態只會消失，跟完成分不出來（只能當完成）。
# 每個背景對話一個項目 app:claude:bg:<標題>；畫面上正在看的那個由 app:claude 負責。
# Code 模式時側邊欄列的是 Claude Code 對話（hooks 已經有），只看「Claude」模式按鈕的「執行中」「正在等待您的輸入」
function Update-ClaudeBg($wins, [bool]$focused, [string]$view, [long]$now) {
  $mode = ''
  $chatBusy = $false
  $chatWait = $false
  $running = @{}
  $waiting = @{}
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
            if ($n -match $WAIT_RE) { $chatWait = $true }
          } elseif ($n -match '^Code' -and (Is-Selected $e)) { $mode = 'code' }
        } elseif ($n -match $BUSY_RE -or $n -match $WAIT_RE) {
          $b = $walker.GetPreviousSibling($e)
          if ($b -and $b.Current.ControlType -eq [System.Windows.Automation.ControlType]::Button -and $b.Current.Name) {
            $t = $b.Current.Name.Trim()
            $running[$t] = $true
            if ($n -match $WAIT_RE) { $waiting[$t] = $true }
          }
        }
      }
    } catch {}
  }
  # 這次在跑（或在等你允許）的背景對話
  $add = @()
  if ($mode -eq 'chat') {
    $add = @($running.Keys | Where-Object { $_ -ne $view })
    # 記下畫面上的聊天對話：切到 Code 模式時它還在跑，就轉成背景對話（不然沒人追蹤它）
    $script:chatView = $view
    $script:chatViewBusy = [bool]($view -and $running.ContainsKey($view))
    $script:chatViewWait = [bool]($view -and $waiting.ContainsKey($view))
  } elseif ($mode -eq 'code' -and ($chatBusy -or $chatWait) -and $script:chatViewBusy) {
    $add = @($script:chatView)
    $script:chatViewBusy = $false
  }
  foreach ($t in $add) {
    if (-not $bg.ContainsKey($t)) {
      $bg[$t] = @{ gen = $false; genSince = 0; genOffAt = 0; finishedAt = 0; viewedAt = 0; review = $false; reviewSince = 0 }
      if ($mode -eq 'code') { $bg[$t].review = $script:chatViewWait }   # 剛從聊天模式轉過來：沿用當時的狀態
    }
    $s = $bg[$t]
    if (-not $s.gen) { $s.gen = $true; $s.genSince = $now; $s.viewedAt = 0 }
    $s.genOffAt = 0
  }
  $anyReview = [bool]($bg.Values | Where-Object { $_.gen -and $_.review })
  foreach ($t in @($bg.Keys)) {
    $s = $bg[$t]
    if ($mode -eq 'chat' -and $t -eq $view) {
      # 你點開了這個對話：還在跑就交給 app:claude（畫面上有停止鈕）；已完成就算看過，亮 8 秒後移除
      if ($s.gen) { $bg.Remove($t); continue }
      if ($focused -and -not $s.viewedAt) { $s.viewedAt = $now }
    }
    if ($s.viewedAt -and $now - $s.viewedAt -gt 10000) { $bg.Remove($t); continue }
    $still = if ($mode -eq 'chat') { $running.ContainsKey($t) } elseif ($mode -eq 'code') { $chatBusy -or $chatWait } else { $s.gen }
    # 在等你允許：聊天模式看這一列；Code 模式分不出是哪個對話，模式按鈕說在等就沿用（沒有人標著的話全部標上）
    $wait = if ($mode -eq 'chat') { $waiting.ContainsKey($t) } elseif ($mode -eq 'code') { $chatWait -and ($s.review -or -not $anyReview) } else { $s.review }
    if ($wait -and -not $s.review) { $s.reviewSince = $now }
    $s.review = [bool]($wait -and $s.gen)
    if ($s.gen -and -not $still) {
      # 跟停止鈕一樣，要連續 1.5 秒沒有狀態才算完成
      if (-not $s.genOffAt) { $s.genOffAt = $now }
      if ($now - $s.genOffAt -gt 1500) { $s.gen = $false; $s.review = $false; $s.finishedAt = $now }
    }
    if (-not $s.gen -and $s.finishedAt -and $now - $s.finishedAt -gt 12 * 3600000) { $bg.Remove($t); continue }
    $items['app:claude:bg:' + $t] = @{ key = 'app:claude:bg:' + $t; ai = 'claude'; focused = [bool]($s.viewedAt -and $focused); gen = $s.gen; genSince = $s.genSince;
                                       review = $s.review; reviewSince = $s.reviewSince; finishedAt = $s.finishedAt; seen = $now; buttons = @($t) }
  }
  # 畫面上的對話：側邊欄說它執行中 / 在等你允許（停止鈕、允許鈕暫時讀不到時也不會中斷）
  $onView = $mode -eq 'chat' -and $view
  return @{ busy = [bool]($onView -and $running.ContainsKey($view)); wait = [bool]($onView -and $waiting.ContainsKey($view)) }
}

# ChatGPT App 的背景對話：跟 Claude App 一樣看側邊欄，每個背景對話一個項目 app:chatgpt:bg:<標題>。
# ChatGPT 與 Codex 模式的對話都列在側邊欄，不用分模式。畫面上的對話 = Document 的名稱（就是對話標題）。
# 等你核准時「處理中」和停止鈕都會消失，要靠「待核准」撐住，不然會先算一次完成
function Update-ChatGPTSide($wins, [bool]$focused, [long]$now, [bool]$hidden) {
  $view = ''
  $running = @{}
  $waiting = @{}
  foreach ($p in $wins) {
    try {
      $els = $UIA::FromHandle($p.MainWindowHandle).FindAll([System.Windows.Automation.TreeScope]::Descendants, $gptCond)
      foreach ($e in $els) {
        $c = $e.Current
        $n = [string]$c.Name
        if ($c.ControlType -eq [System.Windows.Automation.ControlType]::Document) {
          if (-not $view -and $n) { $view = $n.Trim() }
        } elseif ($c.ControlType -eq [System.Windows.Automation.ControlType]::StatusBar) {
          if ($n -match $GPT_BUSY_RE) {
            # StatusBar 的上兩層是對話按鈕（名稱就是標題）
            $b = $walker.GetParent($walker.GetParent($e))
            if ($b -and $b.Current.ControlType -eq [System.Windows.Automation.ControlType]::Button -and $b.Current.Name) { $running[$b.Current.Name.Trim()] = $true }
          }
        } elseif ($n -match $GPT_WAIT_RE) {
          # 專案那一列的名稱是底下所有對話接起來，最後一個對話待核准時也會符合：要對得上這一列自己的對話按鈕
          $t = $Matches[2].Trim()
          $g = $walker.GetFirstChild($e)
          $b = if ($g) { $walker.GetFirstChild($g) }
          if ($b -and $b.Current.ControlType -eq [System.Windows.Automation.ControlType]::Button -and $b.Current.Name.Trim() -eq $t) { $waiting[$t] = $true }
        }
      }
    } catch {}
  }
  foreach ($t in @(@($running.Keys) + @($waiting.Keys) | Where-Object { $_ -ne $view })) {
    if (-not $bgGpt.ContainsKey($t)) { $bgGpt[$t] = @{ gen = $false; genSince = 0; genOffAt = 0; finishedAt = 0; viewedAt = 0; review = $false; reviewSince = 0 } }
    $s = $bgGpt[$t]
    if (-not $s.gen) { $s.gen = $true; $s.genSince = $now; $s.viewedAt = 0 }
    $s.genOffAt = 0
  }
  foreach ($t in @($bgGpt.Keys)) {
    $s = $bgGpt[$t]
    if ($t -eq $view) {
      # 點開了：還在跑就交給 app:chatgpt；已完成就算看過，亮 8 秒後移除
      if ($s.gen) { $bgGpt.Remove($t); continue }
      if ($focused -and -not $s.viewedAt) { $s.viewedAt = $now }
    }
    if ($s.viewedAt -and $now - $s.viewedAt -gt 10000) { $bgGpt.Remove($t); continue }
    $wait = $waiting.ContainsKey($t)
    if ($wait -and -not $s.review) { $s.reviewSince = $now }
    $s.review = [bool]($wait -and $s.gen)
    if ($s.gen -and -not ($running.ContainsKey($t) -or $wait)) {
      if (-not $s.genOffAt) { $s.genOffAt = $now }
      if ($now - $s.genOffAt -gt 1500) { $s.gen = $false; $s.review = $false; $s.finishedAt = $now }
    }
    if (-not $s.gen -and $s.finishedAt -and $now - $s.finishedAt -gt 12 * 3600000) { $bgGpt.Remove($t); continue }
    $items['app:chatgpt:bg:' + $t] = @{ key = 'app:chatgpt:bg:' + $t; ai = 'chatgpt'; focused = [bool]($s.viewedAt -and $focused); gen = $s.gen; genSince = $s.genSince;
                                        review = $s.review; reviewSince = $s.reviewSince; finishedAt = $s.finishedAt; seen = $now; buttons = @($t); hidden = $hidden }
  }
  return @{ view = $view; busy = [bool]($view -and ($running.ContainsKey($view) -or $waiting.ContainsKey($view))); wait = [bool]($view -and $waiting.ContainsKey($view)) }
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
    # 視窗隱藏（例如按 X 縮到系統匣、App 還在背景跑）時 MainWindowHandle 會變 0，但用原本的 handle 還讀得到整個畫面：
    # 記住看過的視窗，還在、還是這個 App 的就繼續讀（縮小到工作列不受影響）
    $ids = @($procs | ForEach-Object { $_.Id })
    $hs = @($procs | Where-Object { $_.MainWindowHandle -ne [IntPtr]::Zero } | ForEach-Object { $_.MainWindowHandle })
    foreach ($h in @($appWins[$a.ai])) {
      if ($hs -contains $h -or -not [AimonWin]::IsWindow($h)) { continue }
      [uint32]$wp = 0
      [void][AimonWin]::GetWindowThreadProcessId($h, [ref]$wp)
      if ($ids -contains [int]$wp) { $hs += $h }
    }
    $appWins[$a.ai] = $hs
    $wins = @($hs | ForEach-Object { [pscustomobject]@{ MainWindowHandle = $_ } })
    # 全部視窗都隱藏了：讀到的畫面停在隱藏那一刻（Chromium 不再更新），讓 AIMonitor 知道（Codex 改看紀錄）
    $hidden = -not ($hs | Where-Object { [AimonWin]::IsWindowVisible($_) })
    $diag.Add(("{0} App：行程 {1} 個、視窗 {2} 個" -f $a.ai, $procs.Count, $wins.Count))
    if ($wins.Count -eq 0) { if ($a.ai -eq 'claude') { $bg.Clear() } elseif ($a.ai -eq 'chatgpt') { $bgGpt.Clear() }; continue }
    $names = New-Object System.Collections.Generic.List[string]
    foreach ($p in $wins) { $names.AddRange((Get-ButtonNames $p.MainWindowHandle)) }
    $focused = [bool]($procs | Where-Object { $_.Id -eq $fgPid })
    $view = Get-View $names
    $side = @{ busy = $false; wait = $false }
    if ($a.ai -eq 'claude') { $side = Update-ClaudeBg $wins $focused $view $now }
    elseif ($a.ai -eq 'chatgpt') { $side = Update-ChatGPTSide $wins $focused $now $hidden; $view = $side.view }
    Update-Item ('app:' + $a.ai) $a.ai $focused $names $now $view $side.busy $side.wait
    $items['app:' + $a.ai].hidden = $hidden
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
    $view = $url
    $names = Get-ButtonNames $h
    if ($url -match $SHEETS_RE) {
      $key = 'web:claude:sheets'
      $view = $url -replace '[#?].*$', ''   # 換工作表、選儲存格會改 #gid= / range=，不算換對話
      $claude = @($names | Where-Object { $SHEETS_CLAUDE -contains $_ }).Count -gt 0
      $names = [System.Collections.Generic.List[string]]@($names | Where-Object { $claude -and ($_ -match $STOP_RE -or $_ -match $REVIEW_RE) })
    }
    if (-not $agg.ContainsKey($key)) { $agg[$key] = @{ ai = $ai; focused = $false; view = $view; names = (New-Object System.Collections.Generic.List[string]) } }
    if ($names.Count) { $agg[$key].names.AddRange($names) }
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
$script:chatView = ''
$script:chatViewBusy = $false
$script:chatViewWait = $false


function Get-StateJson {
  $list = @()
  foreach ($s in $items.Values) {
    $list += [ordered]@{ key = $s.key; ai = $s.ai; focused = $s.focused; gen = $s.gen; genSince = $s.genSince;
                         review = $s.review; reviewSince = $s.reviewSince; finishedAt = $s.finishedAt; failed = [bool]$s.failed; msg = [string]$s.msg; view = [string]$s.view; hidden = [bool]$s.hidden; buttons = @($s.buttons) }
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
