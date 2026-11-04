package main

// 電腦端程式的新版檢查（啟動時一次、之後每天一次）與一鍵更新。
// 只有使用者在設定視窗或系統匣按了「更新」才下載；下載、驗證 SHA256 後啟動新版，
// 由 installAndMaybeExit 現有的「取代執行中的舊版」流程（--updated）安靜換掉。不跳 Windows 通知。

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const updateCheckEvery = 24 * time.Hour

var (
	updMu       sync.Mutex
	updLatest   *ReleaseInfo // 最新的 Release（比目前新才記）
	updChecked  time.Time    // 上次檢查成功的時間
	updCheckErr string       // 上次檢查失敗的原因（只在設定視窗顯示）
	updChecking bool
	updRunning  bool // 下載中
	updProgress int
	updMsg      string
	updErr      string
	updKick     = make(chan struct{}, 1) // 立即檢查
)

func init() { updateUserAgent = "AIMonitor/" + version }

func updateCheckEnabled() bool {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return !settings.NoUpdateCheck
}

func updateDir() string { return filepath.Join(appDir, "update") }

// 背景：啟動 20 秒後查一次，之後每天一次（每 10 分鐘看一下到時間沒，睡眠喚醒後也會補查）
func updateLoop() {
	// 上次更新留下的下載檔（新版啟動時舊的暫存 exe 可能還沒結束，晚一點再刪）
	go func() {
		for i := 0; i < 5; i++ {
			time.Sleep(10 * time.Second)
			if os.RemoveAll(updateDir()) == nil {
				return
			}
		}
	}()
	select {
	case <-time.After(20 * time.Second):
	case <-updKick:
	}
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		updMu.Lock()
		due := time.Since(updChecked) >= updateCheckEvery
		updMu.Unlock()
		if due && updateCheckEnabled() {
			checkForUpdate()
		}
		select {
		case <-t.C:
		case <-updKick:
			if updateCheckEnabled() {
				checkForUpdate()
			}
		}
	}
}

func kickUpdateCheck() {
	select {
	case updKick <- struct{}{}:
	default:
	}
}

func checkForUpdate() {
	updMu.Lock()
	if updChecking {
		updMu.Unlock()
		return
	}
	updChecking = true
	updMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	ri, err := fetchLatestRelease(ctx, http.DefaultClient, updateAPI)
	cancel()
	updMu.Lock()
	defer updMu.Unlock()
	updChecking = false
	if err != nil {
		if updCheckErr != err.Error() {
			logf("update check failed: %v", err)
		}
		updCheckErr = err.Error()
		updChecked = time.Now().Add(-updateCheckEvery + time.Hour) // 失敗的話一小時後再試
		return
	}
	updCheckErr = ""
	updChecked = time.Now()
	if newerVersion(ri.Version, version) {
		if updLatest == nil || updLatest.Version != ri.Version {
			logf("update available: %s (running %s)", ri.Version, version)
		}
		updLatest = &ri
	} else {
		updLatest = nil
	}
}

// 有新版就回傳版本號（系統匣用）
func updateAvailable() string {
	if !updateCheckEnabled() {
		return ""
	}
	updMu.Lock()
	defer updMu.Unlock()
	if updLatest == nil {
		return ""
	}
	return updLatest.Version
}

// 使用者按了「更新」
func startAppUpdate() {
	updMu.Lock()
	if updRunning || updLatest == nil {
		updMu.Unlock()
		return
	}
	ri := *updLatest
	updRunning, updProgress, updMsg, updErr = true, 0, "下載中…", ""
	updMu.Unlock()
	go func() {
		err := doAppUpdate(ri)
		updMu.Lock()
		updRunning = false
		if err != nil {
			updErr, updMsg = err.Error(), ""
			logf("app update to %s failed: %v", ri.Version, err)
		}
		updMu.Unlock()
	}()
}

func doAppUpdate(ri ReleaseInfo) error {
	logf("app update: downloading %s", ri.Version)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	os.RemoveAll(updateDir())
	path, err := downloadRelease(ctx, http.DefaultClient, ri, filepath.Join(updateDir(), ri.Version), func(done, total int64) {
		if total > 0 {
			updMu.Lock()
			updProgress = int(done * 95 / total)
			updMu.Unlock()
		}
	})
	if err != nil {
		return err
	}
	updMu.Lock()
	updProgress, updMsg = 97, "安裝中，程式會自動重新開啟…"
	updMu.Unlock()
	logf("app update: %s verified, starting it to replace this version", ri.Version)
	// 新版從暫存資料夾啟動 → 它會請這個舊版結束、複製到 %LOCALAPPDATA%\AIMonitor、再以 --updated 啟動
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	if err := cmd.Start(); err != nil {
		return err
	}
	cmd.Process.Release()
	go func() { // 一分鐘還沒被新版結束：新版沒起來，保留舊版
		time.Sleep(time.Minute)
		updMu.Lock()
		if updMsg != "" && updErr == "" {
			updMsg, updErr = "", "新版沒有啟動成功，已保留目前的版本"
			logf("app update: new version did not take over")
		}
		updMu.Unlock()
	}()
	return nil
}

func appUpdateStatus() map[string]interface{} {
	updMu.Lock()
	defer updMu.Unlock()
	m := map[string]interface{}{
		"enabled": updateCheckEnabled(), "checking": updChecking, "running": updRunning, "progress": updProgress,
		"msg": updMsg, "err": updErr, "checkErr": updCheckErr,
	}
	if !updChecked.IsZero() && updCheckErr == "" {
		m["checked"] = updChecked.Unix()
	}
	if updLatest != nil {
		m["latest"] = updLatest.Version
		m["url"] = updLatest.URL
	}
	return m
}

func setUpdateCheck(on bool) {
	settingsMu.Lock()
	settings.NoUpdateCheck = !on
	settingsMu.Unlock()
	saveSettings()
	if on {
		updMu.Lock()
		updChecked = time.Time{}
		updMu.Unlock()
		kickUpdateCheck()
	} else {
		updMu.Lock()
		updLatest, updCheckErr = nil, ""
		updMu.Unlock()
	}
}
