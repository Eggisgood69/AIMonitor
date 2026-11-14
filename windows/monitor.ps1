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
# 回答完、還沒點開的對話：側邊欄狀態「未讀回應」（2026-11 實測；英文介面沒實測過）
$UNREAD_RE = '^(未讀回應|未讀|unread( response)?)$'
# 新對話還沒有標題時的名稱（2026-11-14 實測「未命名」；英文沒實測過）
$PLACEHOLDER_RE = '^(未命名|untitled|new chat|新對話)$'
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
        if (-not $n) { continue }
        $n = $n.Trim()
        if ($n -match $RETRY_RE) {
          # ChatGPT 圖片回覆下面那排動作圖示的 ↻ 也叫「再試一次」（class 有 button-icon）：每生成一張圖就多一個，不是出錯
          if ([string]$e.Current.ClassName -match '\bbutton-icon\b') { continue }
          # 捲到畫面外的「再試一次」也要算（前面加 ~，不會被當成別的按鈕）：只算畫面上的話，捲動或縮小視窗時數量會變，誤判成出錯
          if ($e.Current.IsOffscreen) { $names.Add('~' + $n); continue }
        }
        if (-not $e.Current.IsOffscreen) { $names.Add($n) }
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
        # 「再試一次 / Try again」變多 = 出錯（ChatGPT、Gemini）。Claude 不用：對話模式每則回答下面本來就有「重試」，回答完才出現，會被誤判成出錯
        # （1.13.39 曾改成只給 ChatGPT 用，Gemini 桌面版出錯時的「Try again」就不亮紅燈了）
        if ($ai -ne 'claude' -and $retries -gt $s.retries) { $s.failed = $true; $s.msg = 'FAILED' }
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
# Code 模式時側邊欄列的是 Claude Code 對話（hooks 已經有），只看「Claude」模式按鈕的「執行中」「正在等待您的輸入」（App 改版後沒有了，見下面的猜法）
# 側邊欄對話列的標題：狀態（StatusBar）旁邊 / 外面那顆按鈕，名稱是「<標題> <狀態>」（2026-11 App 改版後名稱帶狀態），去掉狀態才對得上畫面上的對話
function Get-RowTitle($e, [string]$status) {
  $b = $walker.GetPreviousSibling($e)
  for ($i = 0; $i -lt 3 -and $b -and $b.Current.ControlType -ne [System.Windows.Automation.ControlType]::Button; $i++) {
    $b = if ($i -eq 0) { $walker.GetParent($e) } else { $walker.GetParent($b) }
  }
  if (-not $b -or $b.Current.ControlType -ne [System.Windows.Automation.ControlType]::Button -or -not $b.Current.Name) { return '' }
  # 狀態後面還可能接別的（實測「台灣夜市美食介紹 未讀回應 包含成品」），從狀態那裡整段去掉
  return ($b.Current.Name.Trim() -replace ('\s+' + [regex]::Escape($status.Trim()) + '(\s.*)?$'), '').Trim()
}

# Code 模式時看不到聊天的狀態（2026-11 App 改版後「Claude」模式按鈕不再寫「執行中」，畫面上也沒有任何聊天的元素，實測完全一樣）。
# 不猜（使用者不要）：切過去時還在跑的聊天先熄燈、記著（$s.away）；切回聊天模式再看側邊欄：
# 還「執行中」就亮回生成中、「未讀回應」= 你不在的時候回答完了（亮完成）、都沒有就繼續記著、不亮。
# 聊天模式也一樣：背景對話用工具時側邊欄不標執行中，「執行中」不見了只是不知道，只有「未讀回應」才算完成。
# 模式按鈕如果又寫「執行中」（$script:modeBusySeen），就照舊相信它
# 記著、不亮的（away）也要照常送出（gen = false = 待命）：沒送的項目 AIMonitor 會忘掉，
# 之後再出現時已經是完成，會被當成「程式剛啟動時看到的舊完成」不亮（state.go，2026-11-14 實測回答完了卻沒亮）
# $s.key：「未命名」改名後換了標題，送出的 key 沿用原本的（換 key 的話 AIMonitor 也會當成新出現的舊完成）
function Emit-ClaudeBg([string]$t, $s, [bool]$focused, [long]$now) {
  $k = if ($s.key) { $s.key } else { 'app:claude:bg:' + $t }
  $items[$k] = @{ key = $k; ai = 'claude'; focused = $focused; gen = [bool]$s.gen; genSince = $s.genSince;
                                     review = [bool]($s.review -and $s.gen); reviewSince = $s.reviewSince; finishedAt = $s.finishedAt; seen = $now; buttons = @($t) }
}

function Update-ClaudeBg($wins, [bool]$focused, [string]$view, [long]$now, [bool]$viewStop = $false) {
  $mode = ''
  $chatBusy = $false
  $chatWait = $false
  $running = @{}
  $waiting = @{}
  $unread = @{}
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
          $t = Get-RowTitle $e $n
          if ($t) {
            $running[$t] = $true
            if ($n -match $WAIT_RE) { $waiting[$t] = $true }
          }
        } elseif ($n -match $UNREAD_RE) {
          $t = Get-RowTitle $e $n
          if ($t) { $unread[$t] = $true }
        }
      }
    } catch {}
  }
  if ($chatBusy -or $chatWait) { $script:modeBusySeen = $true }
  if ($diag) { $diag.Add(("Claude 模式 {0}：側邊欄執行中 {1}、未讀 {2}" -f $mode, $running.Count, $unread.Count)) }
  if ($mode -ne $script:claudeMode) { $script:claudeMode = $mode; $script:claudeModeSince = $now }
  # 剛切回聊天模式時側邊欄還沒畫好（實測第一次讀只有 51 個元素）：讀到任何一列的狀態（執行中 / 未讀回應）就算畫好了，都沒有的話等 3 秒
  # （實測側邊欄 1 秒內就畫好；只等 3 秒的話，切回來看一眼就走時判斷不到）
  $sideReady = $mode -eq 'chat' -and ($running.Count + $waiting.Count + $unread.Count -gt 0 -or $now - $script:claudeModeSince -gt 3000)
  # 新對話的標題列叫「New chat」、側邊欄叫「未命名」（2026-11-14 實測）：當成同一個，不然畫面上的對話會被多算一個背景對話
  if ($view -match $PLACEHOLDER_RE) {
    $m = @(@($running.Keys) + @($waiting.Keys) + @($unread.Keys) | Where-Object { $_ -match $PLACEHOLDER_RE } | Select-Object -First 1)
    if ($m.Count) { $view = [string]$m[0] }
  }
  # 畫面上的停止鈕會短暫消失（跑工具時），3 秒內看過都算（同一個對話）
  if ($view -ne $script:viewStopView) { $script:viewStopView = $view; $script:viewStopAt = 0 }
  if ($mode -eq 'chat' -and $viewStop) { $script:viewStopAt = $now }
  $viewStop = $mode -eq 'chat' -and $script:viewStopAt -and $now - $script:viewStopAt -lt 3000
  # 這次在跑（或在等你允許）的背景對話
  $add = @()
  $addAway = @()
  if ($mode -eq 'chat') {
    # 跟畫面上同名、畫面上卻沒有停止鈕 = 同名的另一個對話在跑
    $add = @($running.Keys | Where-Object { $_ -ne $view -or -not $viewStop })
    # 你切到別的對話時它還在跑、側邊欄卻沒標執行中（用工具時就會這樣，2026-11-14 實測 Claude Docs）：先記著、不亮，等側邊欄說執行中 / 未讀回應
    # （「未命名」換成自動標題也會讓畫面上的標題變掉：畫面上還有停止鈕 = 同一個對話改名，不算）
    if ($script:chatViewBusy -and $script:chatView -and $view -ne $script:chatView -and ($script:chatView -notmatch $PLACEHOLDER_RE -or -not $viewStop) -and -not $running.ContainsKey($script:chatView)) { $addAway = @($script:chatView) }
    # 記下畫面上的聊天對話：切到 Code 模式時它還在跑，就轉成背景對話（不然沒人追蹤它）
    # 在跑 = 側邊欄說執行中，或畫面上有停止鈕（側邊欄對畫面上這個對話常常晚好幾秒才標，2026-11-14 實測）；停止鈕會短暫消失，2 秒內看過都算
    if ($view -ne $script:chatView) { $script:chatViewBusyAt = 0 }
    $script:chatView = $view
    if ($view -and ($running.ContainsKey($view) -or $viewStop)) { $script:chatViewBusyAt = $now }
    $script:chatViewBusy = [bool]($view -and $script:chatViewBusyAt -and $now - $script:chatViewBusyAt -lt 2000)
    $script:chatViewWait = [bool]($view -and $waiting.ContainsKey($view))
  } elseif ($mode -eq 'code' -and $script:chatViewBusy -and ($chatBusy -or $chatWait -or -not $script:modeBusySeen)) {
    $add = @($script:chatView)
    $script:chatViewBusy = $false
  }
  # 這次才開始追蹤的執行中對話（「未命名」改名後的新標題也會在這裡出現）
  if (@($add | Where-Object { -not $bg.ContainsKey($_) -and $_ -notmatch $PLACEHOLDER_RE }).Count) { $script:lastNewRunAt = $now }
  foreach ($t in $add) {
    if (-not $bg.ContainsKey($t)) {
      $bg[$t] = @{ gen = $false; genSince = 0; genOffAt = 0; finishedAt = 0; viewedAt = 0; review = $false; reviewSince = 0; away = $false }
      if ($mode -eq 'code') { $bg[$t].review = $script:chatViewWait }   # 剛從聊天模式轉過來：沿用當時的狀態
    }
    $s = $bg[$t]
    if (-not $s.gen) { $s.gen = $true; $s.genSince = $now; $s.viewedAt = 0 }
    $s.away = $false
    $s.genOffAt = 0
  }
  foreach ($t in $addAway) {
    if (-not $bg.ContainsKey($t)) {
      $bg[$t] = @{ gen = $false; genSince = 0; genOffAt = 0; finishedAt = 0; viewedAt = 0; review = $false; reviewSince = 0; away = $true; awaySince = $now }
    }
  }
  $anyReview = [bool]($bg.Values | Where-Object { $_.gen -and $_.review })
  foreach ($t in @($bg.Keys)) {
    $s = $bg[$t]
    # Code 模式時看不到它：熄燈、記著（away）
    if ($mode -eq 'code' -and -not $script:modeBusySeen -and $s.gen) { $s.gen = $false; $s.review = $false; $s.away = $true; $s.awaySince = $now }
    # 記著、不亮的：側邊欄說執行中就亮回生成中、說「未讀回應」= 你不在的時候回答完了（亮完成），都沒有就繼續記著
    if ($s.away -and $sideReady) {
      if ($running.ContainsKey($t)) { $s.away = $false; $s.gen = $true; $s.genSince = $now; $s.genOffAt = 0 }
      elseif ($unread.ContainsKey($t)) { $s.away = $false; $s.finishedAt = $now }
      elseif ($t -eq $view) { $bg.Remove($t); continue }   # 你正看著它：交給 app:claude（停止鈕）
      elseif ($t -match $PLACEHOLDER_RE) {
        # 「未命名」在你不在的時候改成了自動標題（2026-11-14 實測：送出後馬上切走就會這樣）：
        # 找離開前不是未讀、現在是「未讀回應」、也沒在追蹤的對話，剛好一個就是它（兩個以上不猜）；改名後還在跑的會被當成新的執行中對話追蹤
        $cand = @($unread.Keys | Where-Object { $script:unreadPrev -and -not $script:unreadPrev.ContainsKey($_) -and -not $bg.ContainsKey($_) -and $_ -notmatch $PLACEHOLDER_RE })
        if ($cand.Count -eq 1) {
          $bg.Remove($t)
          $s.away = $false; $s.finishedAt = $now; $s.key = 'app:claude:bg:' + $t
          $bg[$cand[0]] = $s
          Emit-ClaudeBg $cand[0] $s $false $now
          continue
        }
        # 改名後還在跑：新標題這次被當成新的執行中對話追蹤了，這個不用了
        # （聊天模式下改名時，舊名那一列不見、新名出現執行中，前後差 1.5 秒左右）
        if ($script:lastNewRunAt -ge $s.awaySince - 5000 -or $now - $s.awaySince -gt 30 * 60000) { $bg.Remove($t); continue }
      }
      elseif ($now - $s.awaySince -gt 6 * 3600000) { $bg.Remove($t); continue }
    }
    if ($s.away) { Emit-ClaudeBg $t $s $false $now; continue }
    # 畫面上的對話跟它同名：可能就是它，也可能是另一個同名的對話（實測兩個「台灣夜市美食介紹」，看著舊的那個時新的被當成畫面上的、丟掉了）
    if ($mode -eq 'chat' -and $t -eq $view) {
      # 畫面上有停止鈕 = 就是它在跑：交給 app:claude（切回來時側邊欄對畫面上這個對話晚好幾秒才標「執行中」，2026-11-14 實測 7 秒，停止鈕比較準）
      if ($s.gen -and $viewStop) { $bg.Remove($t); continue }
      # 沒有停止鈕、側邊欄也沒說執行中 / 未讀：就是你正看著的這個，交給 app:claude
      # （側邊欄說執行中 = 同名的另一個還在跑；說未讀 = 同名的另一個回答完了：照背景對話處理）
      if ($s.gen -and -not $running.ContainsKey($t) -and -not $unread.ContainsKey($t)) { $bg.Remove($t); continue }
      if (-not $s.gen -and $focused -and -not $s.viewedAt -and -not $unread.ContainsKey($t)) { $s.viewedAt = $now }   # 已完成：算看過，亮 8 秒後移除
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
      if ($now - $s.genOffAt -gt 1500) {
        $s.gen = $false; $s.review = $false
        if ($mode -eq 'chat' -and -not $unread.ContainsKey($t)) {
          # 「執行中」不見了不一定是完成：背景對話用工具時側邊欄也不標（2026-11-14 實測），回答完一定會變「未讀回應」
          # 新對話一開始叫「未命名」、中途改成自動標題時那一列也會不見（改名）：一樣先記著，新標題出現執行中 / 未讀回應時再對（上面）
          $s.away = $true; $s.awaySince = $now
          Emit-ClaudeBg $t $s $false $now
          continue
        }
        $s.finishedAt = $now
      }
    } elseif ($still) { $s.genOffAt = 0 }
    if (-not $s.gen -and $s.finishedAt -and $now - $s.finishedAt -gt 12 * 3600000) { $bg.Remove($t); continue }
    Emit-ClaudeBg $t $s ([bool]($s.viewedAt -and $focused)) $now
  }
  if ($sideReady) { $script:unreadPrev = $unread.Clone() }   # 記下這次的未讀回應：之後新出現的才算（「未命名」改名時用）
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
    if ($a.ai -eq 'claude') {
      $viewStop = [bool]($names | Where-Object { $_ -match $STOP_RE -and $_ -notmatch $STOP_NOT_RE } | Select-Object -First 1)
      $side = Update-ClaudeBg $wins $focused $view $now $viewStop
    }
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
$script:chatViewBusyAt = 0
$script:unreadPrev = $null
$script:lastNewRunAt = 0
$script:viewStopView = ''
$script:viewStopAt = 0
$script:modeBusySeen = $false  # 看過「Claude，執行中」模式按鈕：App 又會寫了，Code 模式不用猜
$script:claudeMode = ''
$script:claudeModeSince = 0


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
