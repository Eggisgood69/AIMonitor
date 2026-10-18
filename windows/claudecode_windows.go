package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	claudeCode   = NewClaudeCode()
	lastScanMu   sync.Mutex
	lastScanList []Item
	ccServerErr  string
)

func ccSettingsPath() string {
	home, _ := os.UserHomeDir()
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "settings.json")
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// 本機 HTTP：只聽 127.0.0.1，接收 Claude Code hooks
func startClaudeCodeServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/cc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if !ccRequestAllowed(r.Header.Get("Origin"), r.Header.Get("Content-Type")) {
			// 瀏覽器裡的網頁也能對 127.0.0.1 送請求（會帶 Origin）：擋掉，免得網頁亂亮燈、亂跳通知
			logf("claude code hook rejected: origin=%q content-type=%q", r.Header.Get("Origin"), r.Header.Get("Content-Type"))
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}")) // 先回應，Claude Code 不用等
		if err := claudeCode.Handle(body); err != nil {
			logf("claude code hook: %v", err)
			return
		}
		// 記下收到的事件（除錯用；每個工具都會觸發的 Pre/PostToolUse 不記，避免洗版）
		var e ccEvent
		if json.Unmarshal(body, &e) == nil && e.HookEventName != "PreToolUse" && e.HookEventName != "PostToolUse" {
			logf("claude code hook: %s %s %s", e.HookEventName, shortID(e.SessionID), e.NotificationType)
		}
		go rescan()
	})
	ln, err := net.Listen("tcp", "127.0.0.1:"+ccPort)
	if err != nil {
		ccServerErr = "連接埠 " + ccPort + " 被占用：" + err.Error()
		logf("claude code server: %v", err)
		return
	}
	srv := &http.Server{Handler: mux, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	go srv.Serve(ln)
}

// 偵測程式每次掃描的結果 + Claude Code 的工作階段
func mergedScan(fromMonitor []Item) []Item {
	lastScanMu.Lock()
	if fromMonitor != nil {
		lastScanList = fromMonitor
	}
	base := append([]Item(nil), lastScanList...)
	lastScanMu.Unlock()
	return append(base, claudeCode.Items()...)
}

// hook 事件進來時立刻更新螢幕（不用等下一次掃描）
func rescan() {
	state.ApplyScan(mergedScan(nil))
	for _, f := range state.TakeFinished() {
		stats.Add(f.AI, f.Dur)
	}
	showAlerts()
	pushFrame(true)
}

func ccInstalled() bool {
	b, err := os.ReadFile(ccSettingsPath())
	return err == nil && ccInstalledIn(b)
}

func ccSetInstalled(install bool) error {
	p := ccSettingsPath()
	orig, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	out, err := ccPatchSettings(orig, install)
	if err != nil {
		return err
	}
	if len(orig) > 0 {
		bak := p + ".aimonitor-backup"
		if _, e := os.Stat(bak); os.IsNotExist(e) { // 只保留第一次的原始備份
			os.WriteFile(bak, orig, 0o644)
		}
	}
	os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

var ccMsgMu sync.Mutex
var ccMsg string

func ccCmd(install bool) {
	err := ccSetInstalled(install)
	ccMsgMu.Lock()
	defer ccMsgMu.Unlock()
	switch {
	case err != nil:
		ccMsg = "失敗：" + err.Error()
	case install:
		ccMsg = "已連接。新開的 Claude Code 工作階段就會亮燈（已開著的請重新啟動 Claude Code）"
	default:
		ccMsg = "已移除"
	}
	logf("claude code hooks install=%v err=%v", install, err)
}

func claudeCodeStatus() map[string]interface{} {
	n, last := claudeCode.Status()
	ccMsgMu.Lock()
	msg := ccMsg
	ccMsgMu.Unlock()
	m := map[string]interface{}{"installed": ccInstalled(), "sessions": n, "msg": msg, "err": ccServerErr, "path": ccSettingsPath()}
	if !last.IsZero() {
		m["lastAgo"] = fmt.Sprintf("%d", int(time.Since(last).Seconds()))
	}
	return m
}
