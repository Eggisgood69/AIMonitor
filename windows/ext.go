package main

// 瀏覽器擴充功能（windows/extension/）：網頁版的 Claude / ChatGPT / Gemini 由它偵測。
// Chrome 只讓 Windows 讀到每個視窗正在顯示的那個分頁，切到別的分頁就看不到停止鈕；
// 擴充功能在每個 AI 分頁裡看按鈕，背景分頁也看得到。它每次有變化（沒變化每 5 秒）把所有 AI 分頁送過來，
// 停止鈕消失 1.5 秒才算完成、換了對話不算完成——跟 monitor.ps1 的 Update-Item 一樣，在這裡判斷。

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// 這麼久沒收到 = 瀏覽器關了 / 擴充功能停用了。背景分頁的計時器會被 Chrome 放慢（最慢一分鐘一次），所以要寬一點
const extStale = 3 * time.Minute

type extTab struct {
	ID     int    `json:"id"`
	AI     string `json:"ai"`
	Stop   bool   `json:"stop"`
	Review bool   `json:"review"`
	View   string `json:"view"` // 網址（不含 ? #）：換了對話就不算完成
	Active bool   `json:"active"`
}

type extTrack struct {
	ai, view                string
	stop, active            bool
	gen                     bool
	genSince, genOffAt      int64
	review                  bool
	reviewSince, finishedAt int64
}

type Extension struct {
	mu     sync.Mutex
	tabs   map[int]*extTrack
	lastAt time.Time
	now    func() time.Time
}

func NewExtension() *Extension {
	return &Extension{tabs: map[int]*extTrack{}, now: time.Now}
}

func extAI(ai string) bool { return ai == "claude" || ai == "chatgpt" || ai == "gemini" }

// 收到擴充功能送來的所有 AI 分頁（不在清單裡的 = 關掉了）
func (x *Extension) Handle(body []byte) error {
	var in struct {
		Tabs []extTab `json:"tabs"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	now := x.now()
	ms := now.UnixMilli()
	x.lastAt = now
	seen := map[int]bool{}
	for _, t := range in.Tabs {
		if !extAI(t.AI) {
			continue
		}
		seen[t.ID] = true
		s := x.tabs[t.ID]
		if s == nil {
			s = &extTrack{}
			x.tabs[t.ID] = s
		}
		if s.gen && t.View != s.view && !t.Stop {
			s.gen, s.genOffAt = false, 0 // 生成中換到別的對話：停止鈕跟著畫面消失，不算完成
		}
		s.ai, s.view, s.stop, s.active = t.AI, t.View, t.Stop, t.Active
		if t.Stop {
			if !s.gen {
				s.gen, s.genSince = true, ms
			}
			s.genOffAt = 0
		} else if s.gen && s.genOffAt == 0 {
			s.genOffAt = ms // 停止鈕不見了：從這時開始算 1.5 秒
		}
		if t.Review && !s.review {
			s.reviewSince = ms
		}
		s.review = t.Review
	}
	for id := range x.tabs {
		if !seen[id] {
			delete(x.tabs, id)
		}
	}
	return nil
}

// 有沒有在跟擴充功能連線（設定視窗顯示用）
func (x *Extension) Status() (connected bool, tabs int, last time.Time) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return !x.lastAt.IsZero() && x.now().Sub(x.lastAt) < extStale, len(x.tabs), x.lastAt
}

// 轉成偵測項目（Key = "ext:<ai>:<分頁 id>"）
func (x *Extension) Items() []Item {
	x.mu.Lock()
	defer x.mu.Unlock()
	now := x.now()
	if x.lastAt.IsZero() || now.Sub(x.lastAt) > extStale {
		x.tabs = map[int]*extTrack{}
		return nil
	}
	ms := now.UnixMilli()
	var out []Item
	for id, s := range x.tabs {
		// 停止鈕會短暫消失，要連續消失 1.5 秒才算真的結束
		if s.gen && !s.stop {
			if s.genOffAt == 0 {
				s.genOffAt = ms
			}
			if ms-s.genOffAt > 1500 {
				s.gen, s.finishedAt = false, ms
			}
		}
		out = append(out, Item{
			Key: fmt.Sprintf("ext:%s:%d", s.ai, id), AI: s.ai, Focused: s.active,
			Gen: s.gen, GenSince: s.genSince, Review: s.review, ReviewSince: s.reviewSince, FinishedAt: s.finishedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// 只收擴充功能送來的：Origin 是 chrome-extension://（Chrome / Edge / Brave 都是），Content-Type 是 JSON。
// 一般網頁送的請求 Origin 是 https://…，進不來
func extRequestAllowed(origin, contentType string) bool {
	return strings.HasPrefix(origin, "chrome-extension://") && strings.HasPrefix(strings.ToLower(contentType), "application/json")
}
