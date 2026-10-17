package main

// 額度重置提醒：用量曾經達到門檻（預設 90%）的時窗，在重置前 5 分鐘和重置當下各提醒一次。

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const remindLead = 5 * time.Minute

type ResetEvent struct {
	AI, Win string // Win: "5" = 短時窗、"w" = 每週
	Kind    string // "soon" / "reset"
	At      int64  // 重置時間
}

type watch struct {
	reset      int64
	soon, done bool
}

type ResetWatcher struct {
	mu sync.Mutex
	w  map[string]*watch // key: ai/win
}

func NewResetWatcher() *ResetWatcher { return &ResetWatcher{w: map[string]*watch{}} }

// 收到新的用量資料時呼叫
func (r *ResetWatcher) Observe(ai string, q Quota, threshold float64, now time.Time) {
	if threshold <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range []struct {
		win   string
		p     float64
		reset int64
	}{{"5", q.P5, q.R5}, {"w", q.PW, q.RW}} {
		if x.p < threshold || x.reset <= now.Unix() {
			continue
		}
		k := ai + "/" + x.win
		cur := r.w[k]
		// 重置時間有時會跳動幾秒：差 2 分鐘以內視為同一個時窗
		if cur != nil && abs64(cur.reset-x.reset) < 120 {
			cur.reset = x.reset
			continue
		}
		r.w[k] = &watch{reset: x.reset, soon: time.Unix(x.reset, 0).Sub(now) <= remindLead}
	}
}

// 每隔一段時間呼叫，回傳該提醒的事件
func (r *ResetWatcher) Tick(now time.Time) []ResetEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ev []ResetEvent
	for k, w := range r.w {
		ai, win, _ := strings.Cut(k, "/")
		at := time.Unix(w.reset, 0)
		switch {
		case !now.Before(at):
			ev = append(ev, ResetEvent{ai, win, "reset", w.reset})
			delete(r.w, k)
		case !w.soon && at.Sub(now) <= remindLead:
			w.soon = true
			ev = append(ev, ResetEvent{ai, win, "soon", w.reset})
		}
	}
	return ev
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

var aiNames = map[string]string{"claude": "Claude", "chatgpt": "ChatGPT", "gemini": "Gemini"}

// 通知文字（中文，給 Windows 通知）
func (e ResetEvent) Text() (title, body string) {
	name := aiNames[e.AI]
	win := "5 小時額度"
	if e.Win == "w" {
		win = "每週額度"
	}
	if e.AI == "gemini" && e.Win == "5" {
		win = "目前用量額度"
	}
	if e.Kind == "soon" {
		mins := int(time.Until(time.Unix(e.At, 0)).Minutes() + 0.5)
		if mins < 1 {
			mins = 1
		}
		return name + " 額度即將重置", fmt.Sprintf("%s的%s再 %d 分鐘就會重置。", name, win, mins)
	}
	return name + " 額度已重置", fmt.Sprintf("%s的%s已經重置，可以繼續使用了。", name, win)
}

// 螢幕上的短字（英文字型）
func (e ResetEvent) Short(now time.Time) string {
	w := "5H"
	if e.Win == "w" {
		w = "WEEK"
	}
	if e.Kind == "soon" {
		m := int(time.Unix(e.At, 0).Sub(now).Minutes() + 0.5)
		if m < 1 {
			m = 1
		}
		return fmt.Sprintf("%s RESETS IN %dM", w, m)
	}
	return w + " QUOTA RESET"
}
