package main

// ChatGPT / Gemini：各用一個隱藏的內建瀏覽器視窗保持登入，定時讀取用量。

import (
	_ "embed"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

//go:embed providers.js
var providersJS string

type provView struct {
	ai, name, url string
	w             webview2.WebView
	hwnd          uintptr
}

type provStatus struct {
	Login     string `json:"login"` // unknown / in / out
	Err       string `json:"err"`
	Diag      string `json:"diag"`
	Note      string `json:"note"`
	WLabel    string `json:"wlabel"`
	SLabel    string `json:"slabel"`
	LoginMode bool   `json:"loginMode"`
}

var (
	provs   = map[string]*provView{}
	provMu  sync.Mutex
	provSt  = map[string]*provStatus{"chatgpt": {Login: "unknown"}, "gemini": {Login: "unknown"}}
	oldProc = map[uintptr]uintptr{}
	hideCB  uintptr
)

// 讓任何視窗按「關閉」時只是隱藏
func subclassHide(h uintptr) {
	if hideCB == 0 {
		hideCB = syscall.NewCallback(func(hw, msg, wp, lp uintptr) uintptr {
			if msg == wmClose {
				procShowWindow.Call(hw, swHide)
				return 0
			}
			r, _, _ := procCallWindowPrc.Call(oldProc[hw], hw, msg, wp, lp)
			return r
		})
	}
	old, _, _ := procSetWindowLong.Call(h, ^uintptr(3), hideCB)
	oldProc[h] = old
}

// 在主執行緒（wv.Run 之前）呼叫
func createProviders() {
	for _, p := range []*provView{
		{ai: "chatgpt", name: "ChatGPT", url: "https://chatgpt.com/"},
		{ai: "gemini", name: "Gemini", url: "https://gemini.google.com/usage"},
	} {
		w := webview2.NewWithOptions(webview2.WebViewOptions{
			DataPath:      filepath.Join(appDir, "WebView2"),
			AutoFocus:     true,
			WindowOptions: webview2.WindowOptions{Title: p.name + " 登入 — " + appTitle, Width: 520, Height: 760, Center: true},
		})
		if w == nil {
			logf("provider %s: webview failed", p.ai)
			continue
		}
		p.w = w
		p.hwnd = uintptr(w.Window())
		procShowWindow.Call(p.hwnd, swHide)
		subclassHide(p.hwnd)
		ai := p.ai
		w.Bind("aimonUsage", func(raw string) string { onProviderUsage(ai, raw); return "ok" })
		w.Init(providersJS)
		w.Navigate(p.url)
		provs[p.ai] = p
	}
}

// 所有 WebView 操作都排到主視窗的執行緒
func pollProviders(only string) {
	if wv == nil {
		return
	}
	wv.Dispatch(func() {
		for ai, p := range provs {
			if only == "" || only == ai {
				p.w.Eval("window.__aimonPoll && window.__aimonPoll()")
			}
		}
	})
}

func reloadProviders() {
	wv.Dispatch(func() {
		for _, p := range provs {
			p.w.Navigate(p.url)
		}
	})
}

func showProvider(ai string) {
	p := provs[ai]
	if p == nil {
		return
	}
	provMu.Lock()
	provSt[ai].LoginMode = true
	provMu.Unlock()
	wv.Dispatch(func() {
		p.w.Navigate(p.url)
		procShowWindow.Call(p.hwnd, swRestore)
		procShowWindow.Call(p.hwnd, swShow)
		procSetForeground.Call(p.hwnd)
	})
}

// providers.js 回報的用量（Gemini 在頁面上解析好；ChatGPT 改傳原始回應，在 Go 端解析）
type providerQuota struct {
	P5     float64 `json:"p5"`
	PW     float64 `json:"pw"`
	R5     float64 `json:"r5"`
	RW     float64 `json:"rw"`
	Plan   string  `json:"plan"`
	Len5   float64 `json:"len5"`
	LenW   float64 `json:"lenw"`
	SLabel string  `json:"slabel"`
}

func onProviderUsage(ai, raw string) {
	var d struct {
		Login *bool          `json:"login"`
		Error string         `json:"error"`
		Diag  string         `json:"diag"`
		Quota *providerQuota `json:"quota"`
		Wham  string         `json:"wham"` // ChatGPT：wham/usage 的原始回應，在 Go 端解析（chatgpt.go）
	}
	if json.Unmarshal([]byte(raw), &d) != nil {
		return
	}
	if d.Wham != "" {
		d.Quota = nil
		if u, ok := parseChatGPTUsage([]byte(d.Wham), time.Now()); ok {
			d.Quota = &providerQuota{P5: u.P5, PW: u.PW, R5: float64(u.R5), RW: float64(u.RW), Plan: u.Plan, Len5: float64(u.Len5), LenW: float64(u.LenW)}
		}
	}
	provMu.Lock()
	st := provSt[ai]
	if st == nil {
		provMu.Unlock()
		return
	}
	if d.Diag != "" {
		st.Diag = d.Diag
		if len(st.Diag) > 1500 {
			st.Diag = st.Diag[:1500]
		}
	}
	if d.Error != "" {
		st.Err = d.Error
		provMu.Unlock()
		logf("%s usage error: %s", ai, d.Error)
		return
	}
	hideNow := false
	if d.Login != nil {
		if *d.Login {
			st.Login = "in"
			if st.LoginMode {
				st.LoginMode = false
				hideNow = true
			}
		} else {
			st.Login = "out"
		}
	}
	if d.Quota != nil && (d.Quota.P5 >= 0 || d.Quota.PW >= 0) {
		st.Err = ""
		st.Note = d.Quota.Plan
		st.WLabel = "本週"
		st.SLabel = d.Quota.SLabel
		if d.Quota.LenW >= 20*86400 {
			st.WLabel = "本月（30 天）"
		}
		provMu.Unlock()
		nq := Quota{P5: d.Quota.P5, PW: d.Quota.PW, R5: int64(d.Quota.R5), RW: int64(d.Quota.RW)}
		diag := d.Diag
		if ai == "chatgpt" {
			diag = redactIDs(diag) // ChatGPT 的 diag 是 API 原始回應：遮掉 email、帳號 ID 才寫進紀錄
		}
		logQuotaChange(ai, nq, diag)
		state.SetQuota(ai, nq)
		quotaUpdated(ai, nq)
		pushFrame(false)
	} else {
		if d.Login != nil && *d.Login {
			st.Err = "已登入，但讀不到用量"
		}
		provMu.Unlock()
	}
	if hideNow {
		if p := provs[ai]; p != nil {
			go func() {
				time.Sleep(1500 * time.Millisecond)
				wv.Dispatch(func() { procShowWindow.Call(p.hwnd, swHide) })
			}()
		}
	}
}

func providersStatus() map[string]interface{} {
	provMu.Lock()
	defer provMu.Unlock()
	out := map[string]interface{}{}
	for ai, st := range provSt {
		m := map[string]interface{}{"login": st.Login, "err": st.Err, "diag": st.Diag, "note": st.Note, "wlabel": st.WLabel, "slabel": st.SLabel, "available": provs[ai] != nil}
		if q := state.GetQuota(ai); q != nil {
			m["quota"] = map[string]interface{}{"p5": q.P5, "pw": q.PW, "r5": q.R5, "rw": q.RW, "age": int(time.Since(q.T).Seconds())}
		}
		out[ai] = m
	}
	return out
}

// 等待登入時每 10 秒檢查；平常每 3 分鐘；每 30 分鐘重新整理頁面
func providerLoop() {
	t := 0
	for range time.Tick(10 * time.Second) {
		t += 10
		provMu.Lock()
		var waiting []string
		for ai, st := range provSt {
			if st.LoginMode || st.Login == "unknown" {
				waiting = append(waiting, ai)
			}
		}
		provMu.Unlock()
		for _, ai := range waiting {
			pollProviders(ai)
		}
		if t%1800 == 0 {
			reloadProviders()
		} else if t%180 == 0 {
			pollProviders("")
		}
	}
}

var (
	reIDField = regexp.MustCompile(`"([A-Za-z_]*(?:id|email|name)[A-Za-z_]*)"\s*:\s*"[^"]*"`)
	reEmail   = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
)

// 遮掉 JSON 裡的 *_id / email / name 欄位值與 email（紀錄檔可能被截圖分享）
func redactIDs(s string) string {
	s = reIDField.ReplaceAllString(s, `"$1":"…"`)
	return reEmail.ReplaceAllString(s, "…@…")
}

// 額度數字有變才記（追查用；diag 是頁面原文開頭，看得出是哪裡解析錯）。要在 SetQuota 之前呼叫
func logQuotaChange(ai string, nq Quota, diag string) {
	if old := state.GetQuota(ai); old != nil && old.P5 == nq.P5 && old.PW == nq.PW && old.R5 == nq.R5 && old.RW == nq.RW {
		return
	}
	ts := func(u int64) string {
		if u == 0 {
			return "-"
		}
		return time.Unix(u, 0).Format("01-02 15:04")
	}
	diag = strings.Join(strings.Fields(diag), " ")
	if r := []rune(diag); len(r) > 700 {
		diag = string(r[:700])
	}
	logf("%s quota: now %.0f%% reset %s, week %.0f%% reset %s | %s", ai, nq.P5, ts(nq.R5), nq.PW, ts(nq.RW), diag)
}
