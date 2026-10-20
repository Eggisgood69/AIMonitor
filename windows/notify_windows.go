package main

// Windows 通知（Toast）＋ 額度重置提醒的接線

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"

	_ "embed"

	"golang.org/x/sys/windows/registry"
)

//go:embed winres/icon.png
var iconPNG []byte

const toastAppID = "AIMonitor.StatusLight"

var resetWatch = NewResetWatcher()

// 讓通知顯示「AI 狀態指示燈」和我們的圖示（免安裝程式也能註冊）
func registerToastApp() {
	ico := filepath.Join(appDir, "icon.png")
	os.WriteFile(ico, iconPNG, 0o644)
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\AppUserModelId\`+toastAppID, registry.SET_VALUE)
	if err != nil {
		logf("toast register: %v", err)
		return
	}
	k.SetStringValue("DisplayName", appTitle)
	k.SetStringValue("IconUri", ico)
	k.Close()
}

func xmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

func showToast(title, body string) {
	xml := `<toast><visual><binding template="ToastGeneric"><text>` + xmlEsc(title) + `</text><text>` + xmlEsc(body) + `</text></binding></visual><audio src="ms-winsoundevent:Notification.Default"/></toast>`
	ps := `$ErrorActionPreference='Stop'
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$x = New-Object Windows.Data.Xml.Dom.XmlDocument
$x.LoadXml('` + strings.ReplaceAll(xml, "'", "''") + `')
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('` + toastAppID + `').Show([Windows.UI.Notifications.ToastNotification]::new($x))`
	u := utf16.Encode([]rune(ps))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	go func() {
		if out, err := cmd.CombinedOutput(); err != nil {
			logf("toast failed: %v %s", err, out)
		}
	}()
}

// 螢幕上的提醒字（顯示一段時間）
var (
	noteMu    sync.Mutex
	noteEv    ResetEvent
	noteUntil time.Time
)

// 螢幕顯示用：mainAI 是目前螢幕上的 AI，不同的話前面加上名字
func currentNote(mainAI string) (text, kind string) {
	noteMu.Lock()
	defer noteMu.Unlock()
	now := time.Now()
	if now.After(noteUntil) {
		return "", ""
	}
	t := noteEv.Short(now)
	if noteEv.AI != mainAI {
		t = strings.ToUpper(aiNames[noteEv.AI]) + " " + t
	}
	return t, noteEv.Kind
}

func remindThreshold() float64 {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if settings.RemindAt == nil {
		return 90
	}
	return float64(*settings.RemindAt)
}

// 每次讀到新的用量就呼叫
func quotaUpdated(ai string, q Quota) {
	resetWatch.Observe(ai, q, remindThreshold(), time.Now())
}

func reminderLoop() {
	for range time.Tick(15 * time.Second) {
		now := time.Now()
		for _, e := range resetWatch.Tick(now) {
			if remindThreshold() <= 0 {
				continue
			}
			title, body := e.Text()
			logf("reminder: %s", body)
			showToast(title, body)
			noteMu.Lock()
			noteEv = e
			if e.Kind == "soon" {
				noteUntil = time.Unix(e.At, 0) // 一直顯示到重置
			} else {
				noteUntil = now.Add(3 * time.Minute)
			}
			noteMu.Unlock()
			pushFrame(true)
			if e.Kind == "reset" { // 重置後馬上重新讀取用量
				if e.AI == "claude" {
					go func() { time.Sleep(20 * time.Second); pollUsage() }()
				} else {
					a := e.AI
					go func() { time.Sleep(20 * time.Second); pollProviders(a) }()
				}
			}
		}
	}
}
