package main

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// 擴充功能的檔案打包在 exe 裡，啟動時解開到 %LOCALAPPDATA%\AIMonitor\extension（使用者從這裡「載入未封裝項目」）
//
//go:embed extension
var extFiles embed.FS

var extension = NewExtension()

func extDir() string { return filepath.Join(appDir, "extension") }

// 解開擴充功能的檔案（內容一樣就不寫，Chrome 才不會以為改過了）
func writeExtension() error {
	return fs.WalkDir(extFiles, "extension", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := extFiles.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(appDir, filepath.FromSlash(p))
		if old, e := os.ReadFile(dst); e == nil && bytes.Equal(old, b) {
			return nil
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		return os.WriteFile(dst, b, 0o644)
	})
}

func extEnabled() bool {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return !settings.NoExtension
}

func setExtEnabled(on bool) {
	settingsMu.Lock()
	settings.NoExtension = !on
	settingsMu.Unlock()
	saveSettings()
	go rescan()
}

// 在檔案總管打開擴充功能資料夾（安裝時要選這個資料夾）
func openExtFolder() {
	if err := writeExtension(); err != nil {
		logf("extension files: %v", err)
	}
	exec.Command("explorer.exe", extDir()).Start()
}

func extHandler(w http.ResponseWriter, r *http.Request) {
	// Chrome 送 JSON 之前會先問一次（CORS / 區域網路存取的預檢）：只回應擴充功能，一般網頁還是進不來
	if origin := r.Header.Get("Origin"); strings.HasPrefix(origin, "chrome-extension://") {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "POST")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	if r.Method != http.MethodPost {
		logf("extension request rejected: method=%s origin=%q", r.Method, r.Header.Get("Origin"))
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !extRequestAllowed(r.Header.Get("Origin"), r.Header.Get("Content-Type")) {
		logf("extension request rejected: origin=%q content-type=%q", r.Header.Get("Origin"), r.Header.Get("Content-Type"))
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte("{}"))
	connected, _, _ := extension.Status()
	if err := extension.Handle(body); err != nil {
		logf("extension: %v", err)
		return
	}
	if !connected {
		logf("extension connected")
	}
	go rescan()
}

func extensionStatus() map[string]interface{} {
	connected, tabs, last := extension.Status()
	m := map[string]interface{}{"enabled": extEnabled(), "connected": connected, "tabs": tabs, "folder": extDir()}
	if !last.IsZero() {
		m["lastAgo"] = int(time.Since(last).Seconds())
	}
	return m
}
