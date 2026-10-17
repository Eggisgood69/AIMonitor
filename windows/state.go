package main

// 狀態整合：把偵測結果（App / 瀏覽器分頁）與 Claude 額度組合成要送給螢幕的畫面。
// 這個檔案不依賴 Windows，可以在任何平台跑單元測試。

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	doneShowWhenViewed = 8 * time.Second  // 正在看的 App / 分頁完成時，綠燈亮 8 秒
	ccDoneShow         = 5 * time.Minute  // Claude Code 完成時綠燈亮多久
	quotaFresh         = 15 * time.Minute // 額度資料多久內算有效
	forever            = int64(1) << 62
)

// 偵測程式（monitor.ps1）回報的一個項目
type Item struct {
	Key         string   `json:"key"` // app:claude、web:chatgpt …
	AI          string   `json:"ai"`
	Focused     bool     `json:"focused"`
	Gen         bool     `json:"gen"`
	GenSince    int64    `json:"genSince"`
	Review      bool     `json:"review"`
	ReviewSince int64    `json:"reviewSince"`
	FinishedAt  int64    `json:"finishedAt"`
	Buttons     []string `json:"buttons"`

	doneUntil int64
}

type Quota struct {
	P5, PW float64 // -1 = 沒資料
	R5, RW int64   // 重置時間 epoch 秒
	T      time.Time
}

type Frame struct {
	AI    string          `json:"ai"`
	St    string          `json:"st"`
	Msg   string          `json:"msg"`
	Age   int64           `json:"age"`
	Now   int64           `json:"now"`
	O     [][]string      `json:"o"`
	P5    *float64        `json:"p5,omitempty"`
	PW    *float64        `json:"pw,omitempty"`
	R5    int64           `json:"r5,omitempty"`
	RW    int64           `json:"rw,omitempty"`
	TZ    int             `json:"tz"`             // 時區（秒），螢幕保護的時鐘用
	Saver int             `json:"saver"`          // 螢幕保護分鐘數（0 = 關閉）
	Bri   int             `json:"bri,omitempty"`  // 背光亮度 5-255
	Rot   int             `json:"rot"`            // 螢幕方向 0-3（×90°）
	H     []float64       `json:"h,omitempty"`    // 最近 5 小時的 5 小時額度（折線用）
	D     [][]interface{} `json:"d,omitempty"`    // 今日統計 [[ai, 次數, 秒數], ...]
	Note  string          `json:"note,omitempty"` // 額度重置提醒（螢幕上的短字）
	NK    string          `json:"nk,omitempty"`   // soon / reset
}

type sample struct {
	t  time.Time
	p5 float64
}

type Finish struct {
	AI  string
	Dur time.Duration
}

type State struct {
	mu       sync.Mutex
	genAcc   map[string]int64 // 每個項目這次生成累積的毫秒數（不含需審核）
	lastScan int64
	done     []Finish
	items    map[string]*Item
	quota    map[string]*Quota
	hist     map[string][]sample
	now      func() time.Time
}

func NewState() *State {
	return &State{genAcc: map[string]int64{}, items: map[string]*Item{}, quota: map[string]*Quota{}, hist: map[string][]sample{}, now: time.Now}
}

// 最近 5 小時的額度，等分成 n 格（每格取該時段最後一筆，沒有資料就沿用前一格）
func (s *State) history(ai string, n int) []float64 {
	h := s.hist[ai]
	now := s.now()
	start := now.Add(-5 * time.Hour)
	if len(h) < 2 || h[len(h)-1].t.Sub(h[0].t) < 10*time.Minute {
		return nil
	}
	out := make([]float64, 0, n)
	last := -1.0
	j := 0
	for i := 0; i < n; i++ {
		end := start.Add(time.Duration(i+1) * 5 * time.Hour / time.Duration(n))
		for j < len(h) && !h[j].t.After(end) {
			last = h[j].p5
			j++
		}
		if last >= 0 {
			out = append(out, round1(last))
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

var priority = map[string]int{"review": 4, "error": 3, "done": 2, "working": 1, "idle": 0}

// 套用偵測程式的一次回報。回傳：是否有項目剛完成（用來觸發額度更新）
func (s *State) ApplyScan(list []Item) (finished []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowMs := s.now().UnixMilli()
	dt := nowMs - s.lastScan
	if s.lastScan == 0 || dt < 0 || dt > 3000 {
		dt = 0 // 太久沒掃描（電腦睡眠等）就不算
	}
	s.lastScan = nowMs
	seen := map[string]bool{}
	for i := range list {
		it := list[i]
		seen[it.Key] = true
		prev := s.items[it.Key]
		if prev != nil && prev.Gen && !prev.Review {
			s.genAcc[it.Key] += dt
		}
		if prev != nil {
			it.doneUntil = prev.doneUntil
			if it.FinishedAt != 0 && it.FinishedAt != prev.FinishedAt {
				finished = append(finished, it.AI)
				s.done = append(s.done, Finish{it.AI, time.Duration(s.genAcc[it.Key]) * time.Millisecond})
				s.genAcc[it.Key] = 0
				if it.Focused {
					it.doneUntil = nowMs + doneShowWhenViewed.Milliseconds()
				} else if strings.HasPrefix(it.Key, "cc:") {
					it.doneUntil = nowMs + ccDoneShow.Milliseconds() // 終端機無法判斷你有沒有在看：亮 5 分鐘
				} else {
					it.doneUntil = forever // 等你切回去看才熄
				}
			}
		} else if it.FinishedAt != 0 {
			it.doneUntil = 0 // 程式剛啟動時看到的舊完成狀態不亮
		}
		if it.Gen {
			it.doneUntil = 0
		}
		// 切回來看了：綠燈再亮 8 秒就熄
		if it.Focused && it.doneUntil == forever {
			it.doneUntil = nowMs + doneShowWhenViewed.Milliseconds()
		}
		cp := it
		s.items[it.Key] = &cp
	}
	for k := range s.items {
		if !seen[k] {
			delete(s.items, k)
			delete(s.genAcc, k)
		}
	}
	return finished
}

// 取出上次以來完成的生成（今日統計用）
func (s *State) TakeFinished() []Finish {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.done
	s.done = nil
	return d
}

// 螢幕上的 BOOT 鍵：全部標為已讀
func (s *State) Acknowledge() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.items {
		it.doneUntil = 0
	}
}

func (s *State) SetQuota(ai string, q Quota) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q.T = s.now()
	s.quota[ai] = &q
	if q.P5 >= 0 {
		h := append(s.hist[ai], sample{q.T, q.P5})
		cut := q.T.Add(-5 * time.Hour)
		for len(h) > 0 && h[0].t.Before(cut) {
			h = h[1:]
		}
		s.hist[ai] = h
	}
}

func (s *State) GetQuota(ai string) *Quota {
	s.mu.Lock()
	defer s.mu.Unlock()
	if q := s.quota[ai]; q != nil {
		c := *q
		return &c
	}
	return nil
}

func status(it *Item, nowMs int64) (string, int64) {
	switch {
	case it.Review:
		return "review", it.ReviewSince
	case it.Gen:
		return "working", it.GenSince
	case it.FinishedAt != 0 && nowMs < it.doneUntil:
		return "done", it.FinishedAt
	case it.FinishedAt != 0:
		return "idle", it.FinishedAt
	default:
		return "idle", nowMs
	}
}

type ranked struct {
	it    *Item
	st    string
	since int64
}

func (s *State) BuildFrame() Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	nowMs := now.UnixMilli()

	var list []ranked
	for _, it := range s.items {
		st, since := status(it, nowMs)
		list = append(list, ranked{it, st, since})
	}
	// 依優先度、時間排序（結果穩定）
	sort.Slice(list, func(i, j int) bool {
		if priority[list[i].st] != priority[list[j].st] {
			return priority[list[i].st] > priority[list[j].st]
		}
		if list[i].since != list[j].since {
			return list[i].since > list[j].since
		}
		return list[i].it.Key < list[j].it.Key
	})

	var main *ranked
	for i := range list {
		if list[i].it.Focused { // 你正在看的 App / 分頁優先
			main = &list[i]
			break
		}
	}
	if main == nil && len(list) > 0 {
		main = &list[0] // 沒在看任何 AI：顯示最需要注意的
	}

	_, tz := now.Zone()
	f := Frame{AI: "none", St: "idle", Now: now.Unix(), O: [][]string{}, TZ: tz}
	ai := ""
	if main != nil {
		ai = main.it.AI
		f.St = main.st
		f.Age = (nowMs - main.since) / 1000
		if f.Age < 0 {
			f.Age = 0
		}
		seenAI := map[string]bool{}
		for _, r := range list {
			if r.it == main.it || r.st == "idle" || len(f.O) >= 3 {
				continue
			}
			k := r.it.AI + "/" + r.st
			if seenAI[k] {
				continue
			}
			seenAI[k] = true
			f.O = append(f.O, []string{r.it.AI, r.st})
		}
	} else if q := s.quota["claude"]; q != nil && now.Sub(q.T) < quotaFresh {
		ai = "claude" // 沒有任何 AI 開著：顯示 Claude 帳號額度
	}
	if ai != "" {
		f.AI = ai
	}
	if q := s.quota[ai]; ai != "" && q != nil && now.Sub(q.T) < quotaFresh {
		p5, pw := q.P5, q.PW
		if q.R5 != 0 && q.R5 < now.Unix() {
			p5 = 0 // 已過重置時間
		}
		if q.RW != 0 && q.RW < now.Unix() {
			pw = 0
		}
		if p5 >= 0 {
			v := round1(p5)
			f.P5, f.R5 = &v, q.R5
			f.H = s.history(ai, 20)
		}
		if pw >= 0 {
			v := round1(pw)
			f.PW, f.RW = &v, q.RW
		}
	}
	return f
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

// 狀態頁顯示用
func (s *State) Snapshot() []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowMs := s.now().UnixMilli()
	var out []map[string]interface{}
	for _, it := range s.items {
		st, _ := status(it, nowMs)
		out = append(out, map[string]interface{}{
			"key": it.Key, "ai": it.AI, "st": st, "focused": it.Focused, "buttons": it.Buttons,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["key"].(string) < out[j]["key"].(string) })
	return out
}

// claude.ai 用量 API 的回應 → Quota
func parseClaudeUsage(raw json.RawMessage) (Quota, bool) {
	// 回應裡有很多其他欄位（null、物件、布林…），只挑需要的兩個解析
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return Quota{}, false
	}
	type window struct {
		Utilization *float64 `json:"utilization"`
		ResetsAt    string   `json:"resets_at"`
	}
	get := func(k string) *window {
		v, ok := all[k]
		if !ok || len(v) == 0 || string(v) == "null" {
			return nil
		}
		var w window
		if json.Unmarshal(v, &w) != nil || w.Utilization == nil {
			return nil
		}
		return &w
	}
	q := Quota{P5: -1, PW: -1}
	ok := false
	if w := get("five_hour"); w != nil {
		q.P5, q.R5, ok = *w.Utilization, parseTime(w.ResetsAt), true
	}
	if w := get("seven_day"); w != nil {
		q.PW, q.RW, ok = *w.Utilization, parseTime(w.ResetsAt), true
	}
	return q, ok
}

func parseTime(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999"} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t.Unix()
		}
	}
	return 0
}
