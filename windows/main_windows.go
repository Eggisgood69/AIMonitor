package main

// AIMonitor.exe — AI 狀態指示燈 Windows 程式
//
//   第一次雙擊：自動安裝到 %LOCALAPPDATA%\AIMonitor、設定開機自動執行、打開設定視窗（登入 Claude）。
//   之後：開機自動在背景執行；插上螢幕就會自動連線。再雙擊一次 = 打開設定視窗。

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const version = "1.9.8"
const releaseYear = "2026"
const author = "Jeff Lee"
const appTitle = "AI 狀態指示燈"

//go:embed monitor.ps1
var monitorPS1 []byte

//go:embed ui.js
var uiJS string

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	procShowWindow    = user32.NewProc("ShowWindow")
	procSetForeground = user32.NewProc("SetForegroundWindow")
	procSetWindowLong = user32.NewProc("SetWindowLongPtrW")
	procCallWindowPrc = user32.NewProc("CallWindowProcW")
	procIsVisible     = user32.NewProc("IsWindowVisible")
)

const (
	swHide    = 0
	swShow    = 5
	swRestore = 9
	wmClose   = 0x0010
)

// ---------------- 設定（%LOCALAPPDATA%\AIMonitor\settings.json）----------------
type Settings struct {
	SaverMin   int  `json:"saverMin"`   // 螢幕保護：待命幾分鐘後進入，0 = 關閉
	Brightness int  `json:"brightness"` // 背光亮度 5-255
	RemindAt   *int `json:"remindAt"`
	Rotation   int  `json:"rotation"` // 螢幕方向 0-3（×90°）   // 額度重置提醒：用量達到幾 % 才提醒，0 = 關閉（預設 90）
	Led        int  `json:"led"`      // 外接 WS2812 亮度 0-100，0 = 關閉
}

var (
	settingsMu sync.Mutex
	settings   = Settings{SaverMin: 5, Brightness: 160}
)

func loadSettings() {
	b, err := os.ReadFile(filepath.Join(appDir, "settings.json"))
	if err != nil {
		return
	}
	settingsMu.Lock()
	json.Unmarshal(b, &settings)
	settingsMu.Unlock()
}

func saveSettings() {
	settingsMu.Lock()
	b, _ := json.MarshalIndent(settings, "", "  ")
	settingsMu.Unlock()
	os.WriteFile(filepath.Join(appDir, "settings.json"), b, 0o644)
}

func brightness() int {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if settings.Brightness < 5 {
		return 160
	}
	return settings.Brightness
}

func saverMin() int {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return settings.SaverMin
}

var (
	diagMu      sync.Mutex
	monitorDiag string
	usageErr    string
)

var (
	stats  *Stats
	appDir string
	state  = NewState()
	logMu  sync.Mutex
)

func init() { runtime.LockOSThread() } // WebView2 要在主執行緒

func logf(format string, a ...interface{}) {
	logMu.Lock()
	defer logMu.Unlock()
	p := filepath.Join(appDir, "aimonitor.log")
	if st, err := os.Stat(p); err == nil && st.Size() > 512*1024 {
		os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, a...))
}

func msgBox(text string) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(appTitle)
	windows.MessageBox(0, t, c, 0x40)
}

// ---------------- 安裝 / 單一執行個體 ----------------

// 回傳 welcome = 要打開設定視窗；updated = 取代了執行中的舊版（安靜換掉，不開視窗）
func installAndMaybeExit() (welcome, updated bool) {
	appDir = filepath.Join(os.Getenv("LOCALAPPDATA"), "AIMonitor")
	os.MkdirAll(appDir, 0o755)
	target := filepath.Join(appDir, "AIMonitor.exe")
	self, _ := os.Executable()

	for _, a := range os.Args[1:] {
		switch a {
		case "--welcome":
			welcome = true
		case "--updated":
			updated = true
		}
	}

	replacing := false // 有舊版正在執行，這次是用新版取代它
	name, _ := windows.UTF16PtrFromString(`Local\AIMonitorSingleton`)
	h, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		if strings.EqualFold(filepath.Clean(self), filepath.Clean(target)) {
			// 同一份已經在執行：叫它打開視窗，自己結束
			signalShow()
			os.Exit(0)
		}
		// 從別的地方執行（例如下載的新版）：請舊的結束，再用新版取代
		replacing = true
		windows.CloseHandle(h)
		signalNamed(quitEventName)
		for i := 0; i < 50; i++ {
			time.Sleep(200 * time.Millisecond)
			h, err = windows.CreateMutex(nil, false, name)
			if err != windows.ERROR_ALREADY_EXISTS {
				break
			}
			windows.CloseHandle(h)
		}
		time.Sleep(500 * time.Millisecond) // 等舊程式完全釋放檔案
	}
	_ = h // 保持持有直到程式結束

	if !strings.EqualFold(filepath.Clean(self), filepath.Clean(target)) {
		// 從隨身碟 / 下載資料夾執行：複製到固定位置、設定開機自動執行，再從那裡啟動
		if err := copyFile(self, target); err != nil {
			logf("copy failed: %v", err)
			target = self // 複製失敗就直接用目前這份
		}
		setAutostart(target)
		windows.CloseHandle(h)
		// 取代執行中的舊版（例如更新）：安靜換掉，不打開設定視窗；第一次安裝或舊版沒在跑：打開視窗
		arg := "--welcome"
		if replacing {
			arg = "--updated"
		}
		if !strings.EqualFold(filepath.Clean(self), filepath.Clean(target)) {
			cmd := exec.Command(target, arg)
			if err := cmd.Start(); err == nil {
				os.Exit(0)
			}
		}
		h, _ = windows.CreateMutex(nil, false, name)
		welcome, updated = !replacing, replacing
	} else {
		setAutostart(target)
	}
	return welcome, updated
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	out.Close()
	os.Remove(dst)
	return os.Rename(tmp, dst)
}

func setAutostart(path string) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	k.SetStringValue("AIMonitor", `"`+path+`"`)
}

func removeAutostart() {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	k.DeleteValue("AIMonitor")
}

func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("AIMonitor")
	return err == nil
}

const showEventName = `Local\AIMonitorShow`
const quitEventName = `Local\AIMonitorQuit`

func signalShow() { signalNamed(showEventName) }

func signalNamed(n string) {
	name, _ := windows.UTF16PtrFromString(n)
	if ev, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name); err == nil {
		windows.SetEvent(ev)
		windows.CloseHandle(ev)
	}
}

// ---------------- 偵測程式（monitor.ps1）----------------

func runMonitor(onScan func([]Item)) {
	ps1 := filepath.Join(appDir, "monitor.ps1")
	for {
		os.WriteFile(ps1, monitorPS1, 0o644)
		cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
			"-File", ps1, "-ParentPid", strconv.Itoa(os.Getpid()))
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			logf("monitor start failed: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
			if !strings.HasPrefix(line, "{") {
				continue
			}
			var d struct {
				Items []Item `json:"items"`
				Diag  string `json:"diag"`
				FG    string `json:"fg"` // 前景視窗的行程名稱
				UIA   bool   `json:"uia"`
			}
			if json.Unmarshal([]byte(line), &d) == nil {
				diagMu.Lock()
				monitorDiag = d.Diag
				if !d.UIA {
					monitorDiag = "UI Automation 載入失敗｜" + monitorDiag
				}
				diagMu.Unlock()
				claudeCode.SetForeground(d.FG)
				onScan(d.Items)
			}
		}
		werr := cmd.Wait()
		logf("monitor exited (%v), restarting", werr)
		diagMu.Lock()
		monitorDiag = fmt.Sprintf("偵測程式結束（%v），重新啟動中", werr)
		diagMu.Unlock()
		time.Sleep(3 * time.Second)
	}
}

// ---------------- USB 螢幕 ----------------

type serialStatus struct {
	Connected bool   `json:"connected"`
	Port      string `json:"port"`
	Verified  bool   `json:"verified"`
	FW        string `json:"fw"` // 螢幕回報的韌體版本（舊版不會回報）
	RX        int    `json:"rx"` // 從螢幕收到的位元組數（除錯用）
}

var (
	serMu     sync.Mutex
	serStat   serialStatus
	frameCh   = make(chan []byte, 16)
	demoUntil time.Time
	demoMu    sync.Mutex
)

var (
	skipPort     = map[string]time.Time{}
	openErr      = map[string]string{} // 每個埠上次開啟失敗的原因（一樣的錯誤只記一次）
	lastGoodPort string                // 上次成功連上螢幕的埠：優先嘗試
	stuckCount   int                   // 連續幾次「一連上就寫入逾時」
	lastAnswer   time.Time             // 螢幕最後一次回應 hello 的時間（只在 serialLoop 裡讀寫）
	lastRecover  time.Time             // 上次因為「螢幕沒回應」自動重開的時間
)

// 螢幕收得到資料、卻沒在解析（畫面顯示 Disconnected）時，寫入不會失敗，只能靠定時打招呼發現
const (
	pingEvery     = 15 * time.Second // 連線中每 15 秒打一次招呼
	noAnswerAfter = 40 * time.Second // 這麼久都沒回應 = 螢幕沒在解析資料
	recoverGap    = 2 * time.Minute  // 自動重開螢幕最多每 2 分鐘一次，避免一直重開
)

func serialLoop() {
	for {
		if pauseSerial.Load() { // 韌體更新中：讓出序列埠
			time.Sleep(300 * time.Millisecond)
			continue
		}
		ports := findDisplayPorts()
		// 不回應的埠（其他 USB 裝置）暫時跳過；上次連上的埠排第一個
		var cand []string
		for _, p := range ports {
			if until, bad := skipPort[p]; !bad || time.Now().After(until) {
				if p == lastGoodPort {
					cand = append([]string{p}, cand...)
				} else {
					cand = append(cand, p)
				}
			}
		}
		multi := len(ports) > 1
		var sp *serialPort
		for _, p := range cand {
			if s, err := openSerial(p); err == nil {
				sp = s
				delete(openErr, p)
				break
			} else if openErr[p] != err.Error() {
				// 例如另一個 USB 裝置被別的程式占用：每 2 秒都會失敗，只在原因改變時記一次
				openErr[p] = err.Error()
				logf("open %s: %v", p, err)
			}
		}
		if sp == nil {
			serMu.Lock()
			serStat = serialStatus{}
			serMu.Unlock()
			time.Sleep(2 * time.Second)
			continue
		}
		logf("connected %s", sp.name)
		serMu.Lock()
		serStat = serialStatus{Connected: true, Port: sp.name}
		serMu.Unlock()
		sp.Write([]byte("{\"hello\":1}\n"))
		sendFrameNow()
		lastAnswer = time.Now()
		sent := 0 // 上次回應之後送了幾個畫面（沒回應時記在紀錄裡）

		buf := make([]byte, 512)
		line := []byte{}
		ok := true
		why := "" // 斷線原因（記在紀錄裡）
		lastHello := time.Now()
		openedAt := time.Now()
		for ok {
			if pauseSerial.Load() {
				why = "paused for firmware update"
				break
			}
			// 還沒收到螢幕回覆：每 5 秒再打一次招呼
			serMu.Lock()
			ver := serStat.Verified
			serMu.Unlock()
			// 有好幾個序列埠、這個 12 秒都沒回應：可能不是螢幕，換下一個
			if !ver && multi && time.Since(openedAt) > 12*time.Second {
				skipPort[sp.name] = time.Now().Add(2 * time.Minute)
				why = "did not answer, trying other ports"
				break
			}
			if !ver && time.Since(lastHello) > 5*time.Second {
				sp.Write([]byte("{\"hello\":1}\n"))
				lastHello = time.Now()
			}
			if ver && time.Since(lastHello) > pingEvery {
				sp.Write([]byte("{\"hello\":1}\n"))
				lastHello = time.Now()
			}
			if ver && time.Since(lastAnswer) > noAnswerAfter {
				why = fmt.Sprintf("screen not answering for %s, %d frames sent meanwhile", time.Since(lastAnswer).Round(time.Second), sent)
				break
			}
			// 送出所有待送的畫面
		drain:
			for {
				select {
				case b := <-frameCh:
					if err := sp.Write(b); err != nil {
						ok = false
						why = "write: " + err.Error()
						break drain
					}
					sent++
				default:
					break drain
				}
			}
			if !ok {
				break
			}
			n, err := sp.Read(buf)
			if err != nil {
				why = "read: " + err.Error()
				break
			}
			if n > 0 {
				serMu.Lock()
				serStat.RX += n
				serMu.Unlock()
			}
			for _, c := range buf[:n] {
				if c == '\n' || c == '\r' {
					if handleDeviceLine(string(line)) {
						lastAnswer, sent = time.Now(), 0
					}
					line = line[:0]
				} else if len(line) < 1024 {
					line = append(line, c)
				}
			}
		}
		sp.Close()
		logf("disconnected %s after %s (%s)", sp.name, time.Since(openedAt).Round(time.Second), why)
		// 一連上就寫入逾時 = 螢幕上沒有程式在讀（卡在燒錄模式或當機，畫面全黑）：連續 3 次就自動重置
		if strings.HasPrefix(why, "write:") && time.Since(openedAt) < 5*time.Second {
			stuckCount++
		} else {
			stuckCount = 0
		}
		if stuckCount >= 3 {
			stuckCount = 0
			logf("%s is not reading, resetting the screen", sp.name)
			resetScreen(sp.name)
		}
		// 螢幕活著但沒在解析資料（畫面 Disconnected）：重開螢幕
		if strings.HasPrefix(why, "screen not answering") {
			if time.Since(lastRecover) > recoverGap {
				lastRecover = time.Now()
				logf("%s: resetting the screen (not answering)", sp.name)
				resetScreen(sp.name)
			} else {
				logf("%s: still not answering, already reset %s ago; reconnecting only", sp.name, time.Since(lastRecover).Round(time.Second))
			}
		}
		serMu.Lock()
		serStat = serialStatus{}
		serMu.Unlock()
		time.Sleep(time.Second)
	}
}

// 螢幕回報的負荷統計（韌體 1.7.2 起隨 hello 回應帶上；只在 serialLoop 裡讀寫）
var scr struct {
	have              bool
	up, bad, long     float64   // 上一次的開機秒數、累計解析失敗行數、累計過長丟掉的行數
	winLoop, winStrip float64   // 這 10 分鐘內最長的 loop / 一條畫面重畫（毫秒）
	winHeap           float64   // 這 10 分鐘內最少的剩餘記憶體
	lastSummary       time.Time // 上次寫摘要的時間
}

// 只在有意義時寫紀錄：螢幕重開機、有資料被丟掉、每 10 分鐘一行摘要
func noteScreenStats(port string, m map[string]interface{}) {
	num := func(k string) float64 { v, _ := m[k].(float64); return v }
	up, loop, strip, bad, long, heap := num("up"), num("loop"), num("strip"), num("bad"), num("long"), num("heap")
	if scr.have && up+5 < scr.up {
		logf("%s: screen rebooted (was up %s, now %s)", port, time.Duration(scr.up)*time.Second, time.Duration(up)*time.Second)
		scr.bad, scr.long = 0, 0 // 重開機後累計歸零
	}
	if scr.have && (bad > scr.bad || long > scr.long) {
		logf("%s: screen dropped data: %.0f bad line(s), %.0f too long (since boot: %.0f / %.0f)", port, bad-scr.bad, long-scr.long, bad, long)
	}
	scr.up, scr.bad, scr.long = up, bad, long
	if loop > scr.winLoop {
		scr.winLoop = loop
	}
	if strip > scr.winStrip {
		scr.winStrip = strip
	}
	if heap > 0 && (scr.winHeap == 0 || heap < scr.winHeap) {
		scr.winHeap = heap
	}
	if !scr.have || time.Since(scr.lastSummary) >= 10*time.Minute {
		logf("%s: screen load: max loop %.0fms, max strip %.0fms, min free heap %.0fKB, up %s", port, scr.winLoop, scr.winStrip, scr.winHeap/1024, time.Duration(up)*time.Second)
		scr.winLoop, scr.winStrip, scr.winHeap, scr.lastSummary = 0, 0, 0, time.Now()
	}
	scr.have = true
}

// 讓螢幕重新開機（先進燒錄模式再 RTS 重置，見 flasher.go rebootToApp），等它開機
func resetScreen(port string) {
	if s, err := openSerial(port); err == nil {
		usbJTAG := true
		for _, p := range findDisplayPortInfos() {
			if p.Name == port {
				usbJTAG = p.VID == "VID_303A"
			}
		}
		rebootToApp(s, usbJTAG)
		s.Close()
	}
	time.Sleep(3 * time.Second) // 等螢幕開機
}

// 處理螢幕送來的一行；回傳 true = 這是 hello 的回應（螢幕還在正常解析資料）
func handleDeviceLine(l string) (answered bool) {
	l = strings.TrimSpace(l)
	if !strings.HasPrefix(l, "{") {
		return false
	}
	var m map[string]interface{}
	if json.Unmarshal([]byte(l), &m) != nil {
		return false
	}
	if _, ok := m["aimon"]; ok {
		answered = true
		serMu.Lock()
		if !serStat.Verified {
			logf("%s: screen answered (fw %v)", serStat.Port, m["fw"])
		}
		serStat.Verified = true
		if fw, ok := m["fw"].(string); ok {
			serStat.FW = fw
		}
		lastGoodPort = serStat.Port // 只在 serialLoop 裡讀寫
		port := serStat.Port
		serMu.Unlock()
		if _, ok := m["up"]; ok { // 韌體 1.7.2 起才有
			noteScreenStats(port, m)
		}
	}
	if m["ev"] == "btn" {
		answered = true           // 按鍵事件也代表螢幕活著
		claudeCode.AckQuestions() // 「等你回覆」的黃燈也標為已讀
		state.Acknowledge()
		rescan()
	}
	return answered
}

func queueFrame(b []byte) {
	select {
	case frameCh <- b:
	default: // 佇列滿了（沒接螢幕）：丟掉最舊的
		select {
		case <-frameCh:
		default:
		}
		frameCh <- b
	}
}

var (
	lastFrameMu   sync.Mutex
	lastFrameJSON string
	lastFrameSent time.Time
	lastFrame     Frame
)

func sendFrameNow() { pushFrame(true) }

func pushFrame(force bool) {
	demoMu.Lock()
	inDemo := time.Now().Before(demoUntil)
	demoMu.Unlock()
	if inDemo {
		return
	}
	f := state.BuildFrame()
	f.Saver = saverMin()
	f.Bri = brightness()
	settingsMu.Lock()
	f.Rot = settings.Rotation & 3
	f.Led = settings.Led
	settingsMu.Unlock()
	f.Note, f.NK = currentNote(f.AI)
	if stats != nil {
		f.D = stats.Today()
	}
	b, _ := json.Marshal(f)
	// 比較時忽略 now/age（每秒都會變）
	cmp := f
	cmp.Now, cmp.Age = 0, 0
	cb, _ := json.Marshal(cmp)
	lastFrameMu.Lock()
	changed := string(cb) != lastFrameJSON
	if changed || force || time.Since(lastFrameSent) > 3*time.Second {
		if f.AI != lastFrame.AI || f.St != lastFrame.St { // 主燈變化記下來（追查燈號用）
			logf("frame: %s %s o=%v", f.AI, f.St, f.O)
		}
		lastFrameJSON = string(cb)
		lastFrameSent = time.Now()
		lastFrame = f
		lastFrameMu.Unlock()
		queueFrame(append(b, '\n'))
		return
	}
	lastFrameMu.Unlock()
}

func playDemo() {
	demoMu.Lock()
	demoUntil = time.Now().Add(13 * time.Second)
	demoMu.Unlock()
	now := time.Now().Unix()
	f := func(s string) { queueFrame([]byte(s + "\n")) }
	go func() {
		f(fmt.Sprintf(`{"ai":"claude","st":"working","p5":45,"pw":63,"r5":%d,"age":12,"now":%d,"o":[["claude","review"],["chatgpt","error"],["gemini","done"],["claude","working"],["claude","working"]]}`, now+7800, now))
		time.Sleep(3 * time.Second)
		f(fmt.Sprintf(`{"ai":"claude","st":"review","p5":45,"pw":63,"r5":%d,"age":3,"now":%d,"o":[["claude","done"],["gemini","working"]]}`, now+7800, now))
		time.Sleep(3 * time.Second)
		f(fmt.Sprintf(`{"ai":"chatgpt","st":"done","age":5,"now":%d,"o":[["gemini","working"]]}`, now))
		time.Sleep(3 * time.Second)
		f(fmt.Sprintf(`{"ai":"claude","st":"error","msg":"Usage limit","p5":97,"pw":80,"r5":%d,"age":1,"now":%d,"o":[]}`, now+1500, now))
		time.Sleep(3 * time.Second)
		demoMu.Lock()
		demoUntil = time.Time{}
		demoMu.Unlock()
		sendFrameNow()
	}()
}

// ---------------- 視窗（WebView2，停在 claude.ai 上以便查額度）----------------

const restURL = "https://claude.ai/api/organizations" // 很輕量的頁面；狀態頁會畫在它上面
const loginURL = "https://claude.ai/login"

var (
	wv          webview2.WebView
	hwnd        uintptr
	oldWndProc  uintptr
	loginState  = "unknown" // unknown / in / out
	loginMu     sync.Mutex
	loginMode   bool
	lastUsageOK time.Time
)

func showWindow() {
	procShowWindow.Call(hwnd, swRestore)
	procShowWindow.Call(hwnd, swShow)
	procSetForeground.Call(hwnd)
}

func hideWindow() { procShowWindow.Call(hwnd, swHide) }

func windowVisible() bool {
	r, _, _ := procIsVisible.Call(hwnd)
	return r != 0
}

// 關閉視窗 = 隱藏（程式繼續在背景執行）
func subclassClose() {
	cb := syscall.NewCallback(func(h, msg, wp, lp uintptr) uintptr {
		if msg == wmClose {
			procShowWindow.Call(h, swHide)
			return 0
		}
		r, _, _ := procCallWindowPrc.Call(oldWndProc, h, msg, wp, lp)
		return r
	})
	gwlpWndProc := ^uintptr(3) // -4
	oldWndProc, _, _ = procSetWindowLong.Call(hwnd, gwlpWndProc, cb)
}

func pollUsage() {
	if wv == nil {
		return
	}
	wv.Dispatch(func() { wv.Eval("window.__aimonPoll && window.__aimonPoll()") })
}

func onUsage(raw string) {
	var d struct {
		Login  *bool           `json:"login"`
		Usage  json.RawMessage `json:"usage"`
		Status int             `json:"status"`
		Error  string          `json:"error"`
		Diag   json.RawMessage `json:"diag"`
	}
	if json.Unmarshal([]byte(raw), &d) != nil {
		return
	}
	if d.Error != "" {
		logf("usage error: %s", d.Error)
		diagMu.Lock()
		usageErr = "JS 錯誤：" + d.Error
		diagMu.Unlock()
		return
	}
	if d.Login == nil {
		return
	}
	loginMu.Lock()
	wasLoginMode := loginMode
	if *d.Login {
		loginState = "in"
		loginMode = false
	} else {
		loginState = "out"
	}
	loginMu.Unlock()
	if *d.Login {
		q, ok := Quota{}, false
		if len(d.Usage) > 0 && string(d.Usage) != "null" {
			q, ok = parseClaudeUsage(d.Usage)
		}
		diagMu.Lock()
		if ok {
			usageErr = ""
		} else {
			raw := string(d.Diag)
			if raw == "" {
				raw = string(d.Usage)
			}
			if len(raw) > 900 {
				raw = raw[:900]
			}
			usageErr = fmt.Sprintf("HTTP %d：%s", d.Status, raw)
			logf("usage failed: %s", usageErr)
		}
		diagMu.Unlock()
		if ok {
			logQuotaChange("claude", q, "")
			state.SetQuota("claude", q)
			quotaUpdated("claude", q)
			lastUsageOK = time.Now()
			pushFrame(false)
		}
	}
	if *d.Login && wasLoginMode {
		// 剛登入完成：回到狀態頁
		wv.Dispatch(func() { wv.Navigate(restURL) })
	}
}

func onCmd(cmd string) string {
	switch cmd {
	case "demo":
		playDemo()
	case "poll":
		pollUsage()
	case "fwupdate":
		startFirmwareUpdate()
	case "login:chatgpt":
		showProvider("chatgpt")
	case "login:gemini":
		showProvider("gemini")
	case "poll:all":
		pollUsage()
		pollProviders("")
	default:
		if strings.HasPrefix(cmd, "bri:") {
			if n, err := strconv.Atoi(strings.TrimPrefix(cmd, "bri:")); err == nil && n >= 5 && n <= 255 {
				settingsMu.Lock()
				settings.Brightness = n
				settingsMu.Unlock()
				saveSettings()
				sendFrameNow()
			}
		}
		if strings.HasPrefix(cmd, "remind:") {
			if n, err := strconv.Atoi(strings.TrimPrefix(cmd, "remind:")); err == nil && n >= 0 && n <= 100 {
				settingsMu.Lock()
				settings.RemindAt = &n
				settingsMu.Unlock()
				saveSettings()
			}
		}
		if strings.HasPrefix(cmd, "rot:") {
			if n, err := strconv.Atoi(strings.TrimPrefix(cmd, "rot:")); err == nil && n >= 0 && n <= 3 {
				settingsMu.Lock()
				settings.Rotation = n
				settingsMu.Unlock()
				saveSettings()
				sendFrameNow()
			}
		}
		if strings.HasPrefix(cmd, "led:") {
			if n, err := strconv.Atoi(strings.TrimPrefix(cmd, "led:")); err == nil && n >= 0 && n <= 100 {
				settingsMu.Lock()
				settings.Led = n
				settingsMu.Unlock()
				saveSettings()
				sendFrameNow()
			}
		}
		if cmd == "cc-on" || cmd == "cc-off" {
			ccCmd(cmd == "cc-on")
		}
		if strings.HasPrefix(cmd, "toast-test") {
			showToast("額度重置提醒測試", "通知功能正常。額度重置前 5 分鐘和重置時會像這樣提醒你。")
		}
		if strings.HasPrefix(cmd, "saver:") {
			if n, err := strconv.Atoi(strings.TrimPrefix(cmd, "saver:")); err == nil && n >= 0 && n <= 240 {
				settingsMu.Lock()
				settings.SaverMin = n
				settingsMu.Unlock()
				saveSettings()
				sendFrameNow()
			}
		}
	case "hide":
		wv.Dispatch(hideWindow)
	case "login":
		loginMu.Lock()
		loginMode = true
		loginMu.Unlock()
		wv.Dispatch(func() { wv.Navigate(loginURL) })
	case "logout-status":
		wv.Dispatch(func() { wv.Navigate(restURL) })
	case "autostart-on":
		self, _ := os.Executable()
		setAutostart(self)
	case "autostart-off":
		removeAutostart()
	case "quit":
		removeIfAsked := false
		_ = removeIfAsked
		wv.Dispatch(func() { trayRemove(); wv.Terminate() })
	}
	return "ok"
}

func uiStatusJSON() string {
	serMu.Lock()
	ss := serStat
	serMu.Unlock()
	loginMu.Lock()
	ls := loginState
	loginMu.Unlock()
	lastFrameMu.Lock()
	lf := lastFrame
	lastFrameMu.Unlock()
	q := state.GetQuota("claude")
	var qj interface{}
	if q != nil {
		qj = map[string]interface{}{"p5": q.P5, "pw": q.PW, "r5": q.R5, "age": int(time.Since(q.T).Seconds())}
	}
	diagMu.Lock()
	md, ue := monitorDiag, usageErr
	diagMu.Unlock()
	b, _ := json.Marshal(map[string]interface{}{
		"version": version, "releaseYear": releaseYear, "author": author, "serial": ss, "login": ls, "quota": qj,
		"items": state.Snapshot(), "frame": lf, "autostart": autostartEnabled(),
		"monitor": md, "usageErr": ue, "saverMin": saverMin(), "brightness": brightness(), "remindAt": int(remindThreshold()), "today": statsToday(), "week": statsWeek(), "claudeCode": claudeCodeStatus(), "rotation": rotation(), "led": ledPct(),
		"providers": providersStatus(), "firmware": firmwareStatus(),
	})
	return string(b)
}

func main() {
	welcome, updated := installAndMaybeExit()
	loadSettings()
	logf("start v%s welcome=%v updated=%v", version, welcome, updated)

	evName, _ := windows.UTF16PtrFromString(showEventName)
	showEv, _ := windows.CreateEvent(nil, 0, 0, evName)
	qName, _ := windows.UTF16PtrFromString(quitEventName)
	quitEv, _ := windows.CreateEvent(nil, 0, 0, qName)
	go func() { // 新版要取代：結束自己
		windows.WaitForSingleObject(quitEv, windows.INFINITE)
		logf("quit requested by new version")
		os.Exit(0)
	}()

	stats = NewStats(filepath.Join(appDir, "stats.json"))
	startClaudeCodeServer()
	go serialLoop()
	go runMonitor(func(items []Item) {
		afterScan(state.ApplyScan(mergedScan(items)), false)
	})
	go func() { // 每秒重算（經過時間、綠燈過期），變化才送；至少每 3 秒送一次當心跳
		for range time.Tick(time.Second) {
			pushFrame(false)
		}
	}()

	wv = webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:      filepath.Join(appDir, "WebView2"),
		AutoFocus:     true,
		WindowOptions: webview2.WindowOptions{Title: appTitle, Width: 460, Height: 700, Center: true},
	})
	if wv == nil {
		logf("WebView2 not available")
		msgBox("找不到 Microsoft Edge WebView2 執行環境，無法登入 Claude 查詢額度。\n\n燈號功能仍會正常運作。\n若需要額度，請安裝 WebView2 Runtime（Windows 11 已內建）。")
		select {} // 沒有視窗也繼續跑燈號
	}
	hwnd = uintptr(wv.Window())
	if !welcome {
		hideWindow()
	}
	subclassClose()

	wv.Bind("aimonUsage", func(raw string) string { onUsage(raw); return "ok" })
	wv.Bind("aimonCmd", onCmd)
	wv.Bind("aimonStatus", func() string { return uiStatusJSON() })
	wv.Init(uiJS)
	wv.Navigate(restURL)

	// ChatGPT / Gemini 的背景視窗（登入後讀取用量）
	createProviders()
	go providerLoop()
	trayInit()
	registerToastApp()
	go reminderLoop()

	// 第二次雙擊 → 打開視窗
	go func() {
		for {
			windows.WaitForSingleObject(showEv, windows.INFINITE)
			wv.Dispatch(func() {
				wv.Navigate(restURL)
				showWindow()
			})
		}
	}()
	// 額度：每 2 分鐘；等待登入時每 5 秒檢查一次
	go func() {
		t := 0
		for range time.Tick(5 * time.Second) {
			t += 5
			loginMu.Lock()
			lm := loginMode
			loginMu.Unlock()
			if lm || t%120 == 0 {
				pollUsage()
			}
		}
	}()

	_ = unsafe.Sizeof(0)
	wv.Run()
	trayRemove()
	logf("exit")
}

func statsToday() interface{} {
	if stats == nil {
		return nil
	}
	return stats.Today()
}

func statsWeek() interface{} {
	if stats == nil {
		return nil
	}
	return stats.Week()
}

func rotation() int {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return settings.Rotation & 3
}

func ledPct() int {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return settings.Led
}
