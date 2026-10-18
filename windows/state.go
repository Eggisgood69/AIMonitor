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
	ccSeenWindow       = 4 * time.Second  // Claude Code 完成的同時、桌面 App 的停止鈕也消失 → 是你正在看的對話
	alertDoneDelay     = 5 * time.Second  // 背景完成 / 錯誤：等這麼久還是「沒看過」才跳通知（先讓上面的比對完成）
	alertReviewDelay   = 7 * time.Second  // 背景需審核：等這麼久還沒處理才跳通知
	doneOnTop          = time.Minute      // 背景完成通知後，在主燈最上面停留多久（之後排到生成中後面，避免來回對話時看不到呼吸燈）
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
	Failed      bool     `json:"failed"` // 上一次是出錯結束：完成時改亮紅燈
	Msg         string   `json:"msg"`    // 錯誤的短字（螢幕顯示）
	Buttons     []string `json:"buttons"`

	doneUntil     int64 // 完成 / 錯誤亮到何時；forever = 你還沒看過（背景發生的）
	alertedFin    int64 // 已經為哪一次完成跳過通知
	alertedReview int64 // 已經為哪一次需審核跳過通知
	pairedFin     int64 // app:claude：這次完成已經併到對應的 Claude Code 對話
}

// 背景發生、要跳 Windows 通知的事件
type Alert struct {
	AI, Kind    string // Kind：done / error / review
	Title, Body string
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

type pendingFinish struct {
	Finish
	at int64 // 完成時間
}

type State struct {
	mu       sync.Mutex
	pending  []pendingFinish // Claude App 的完成：等幾秒確認不是 Claude Code 同一個對話，才算進統計
	startMs  int64           // 第一次掃描的時間（之前就發生的事不跳通知）
	alerts   []Alert
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

// 主燈的優先順序：「你沒看到的事」優先，你正在看的對話自己看得到。
//
//	70 背景需審核      60 正在看的需審核
//	50 背景錯誤（沒看過）  40 背景完成（沒看過，通知後 1 分鐘內）
//	30 背景生成中      20 正在看的生成中
//	15 背景完成（沒看過，超過 1 分鐘：還是一直亮，但沒有 AI 在跑時才輪到主燈）
//	10 看過的錯誤 / 完成（亮 8 秒）  0 待機
func rank(it *Item, st string, nowMs int64) int {
	switch st {
	case "review":
		if it.Focused {
			return 60
		}
		return 70
	case "error", "done":
		if it.doneUntil != forever {
			return 10
		}
		if st == "error" {
			return 50
		}
		if nowMs-it.FinishedAt > (alertDoneDelay + doneOnTop).Milliseconds() {
			return 15
		}
		return 40
	case "working":
		if it.Focused {
			return 20
		}
		return 30
	}
	return 0
}

// 通知、設定視窗用的名稱
func itemLabel(it *Item) string {
	switch {
	case strings.HasPrefix(it.Key, "cc:"):
		if len(it.Buttons) > 0 {
			return it.Buttons[0] // Claude Code · 專案資料夾
		}
		return "Claude Code"
	case strings.HasPrefix(it.Key, "app:"):
		return aiNames[it.AI] + " App"
	default:
		return aiNames[it.AI] + " 網頁"
	}
}

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
	if s.startMs == 0 {
		s.startMs = nowMs
	}
	seen := map[string]bool{}
	for i := range list {
		it := list[i]
		seen[it.Key] = true
		prev := s.items[it.Key]
		if prev != nil && prev.Gen && !prev.Review {
			s.genAcc[it.Key] += dt
		}
		if prev != nil {
			it.doneUntil, it.alertedFin, it.alertedReview, it.pairedFin = prev.doneUntil, prev.alertedFin, prev.alertedReview, prev.pairedFin
			if it.FinishedAt != 0 && it.FinishedAt != prev.FinishedAt {
				finished = append(finished, it.AI)
				fin := Finish{it.AI, time.Duration(s.genAcc[it.Key]) * time.Millisecond}
				if it.Key == "app:claude" {
					s.pending = append(s.pending, pendingFinish{fin, it.FinishedAt})
				} else {
					s.done = append(s.done, fin)
				}
				s.genAcc[it.Key] = 0
				if it.Focused {
					it.doneUntil = nowMs + doneShowWhenViewed.Milliseconds() // 你看著它完成：亮 8 秒
				} else {
					it.doneUntil = forever // 背景完成：等你切回去看（或長按 BOOT）才熄
				}
			}
		} else if it.FinishedAt != 0 && !it.Failed {
			it.doneUntil = 0 // 程式剛啟動時看到的舊完成狀態不亮
		} else if it.Failed {
			it.doneUntil = forever // 錯誤要等你看過（或長按 BOOT）才熄
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
	s.matchClaudeApp()
	s.flushPending(nowMs)
	s.collectAlerts(nowMs)
	return finished
}

// Claude Code 的 hooks 不會說你正在看哪個對話。在桌面 App 裡跑的對話完成時，
// App 畫面上的「停止」鈕也會同時消失：時間對得上，就是你正在看的那個（只亮 8 秒、不跳通知）
//
// 時間對得上 = 同一個對話：
//   - App 在前景（當時或之後切回來）→ 兩個都算看過，跟 App 一起熄
//   - App 不在前景 → 還是沒看過，但只留 Claude Code 這一項（名稱較清楚），App 那項不重複亮燈、不重複通知
func (s *State) matchClaudeApp() {
	app := s.items["app:claude"]
	if app == nil || app.FinishedAt == 0 {
		return
	}
	for k, it := range s.items {
		if !strings.HasPrefix(k, "cc:") || it.doneUntil != forever || it.FinishedAt == 0 {
			continue
		}
		if d := it.FinishedAt - app.FinishedAt; d <= -ccSeenWindow.Milliseconds() || d >= ccSeenWindow.Milliseconds() {
			continue
		}
		if app.Focused {
			if app.pairedFin == app.FinishedAt {
				it.doneUntil = s.now().UnixMilli() + doneShowWhenViewed.Milliseconds() // 之後才切回來看：從現在再亮 8 秒
			} else {
				it.doneUntil = it.FinishedAt + doneShowWhenViewed.Milliseconds() // 看著它完成
			}
		} else if app.doneUntil == forever || app.pairedFin == app.FinishedAt {
			app.doneUntil = 0 // 同一件事：App 這項不亮、不通知
			app.alertedFin = app.FinishedAt
			app.pairedFin = app.FinishedAt
		}
	}
}

// 今日統計：Claude App 的完成等 ccSeenWindow，期間有 Claude Code 對話同時完成 = 同一個對話，只算一次
func (s *State) flushPending(nowMs int64) {
	keep := s.pending[:0]
	for _, p := range s.pending {
		if nowMs-p.at < ccSeenWindow.Milliseconds() {
			keep = append(keep, p)
			continue
		}
		dup := false
		for k, it := range s.items {
			if d := it.FinishedAt - p.at; strings.HasPrefix(k, "cc:") && it.FinishedAt != 0 &&
				d > -ccSeenWindow.Milliseconds() && d < ccSeenWindow.Milliseconds() {
				dup = true
			}
		}
		if !dup {
			s.done = append(s.done, p.Finish)
		}
	}
	s.pending = keep
}

// 背景發生、你還沒看到的事 → Windows 通知（每次只跳一次）
func (s *State) collectAlerts(nowMs int64) {
	appFocused := s.items["app:claude"] != nil && s.items["app:claude"].Focused
	for _, it := range s.items {
		label := itemLabel(it)
		if it.doneUntil == forever && it.FinishedAt > s.startMs && it.FinishedAt != it.alertedFin &&
			nowMs-it.FinishedAt >= alertDoneDelay.Milliseconds() {
			it.alertedFin = it.FinishedAt
			if it.Failed {
				body := "工作中斷了，切過去看看"
				if it.Msg != "" {
					body = it.Msg + "：" + body
				}
				s.alerts = append(s.alerts, Alert{it.AI, "error", label + " 出錯", body})
			} else {
				s.alerts = append(s.alerts, Alert{it.AI, "done", label + " 完成了", "背景的工作跑完了，切過去看看結果"})
			}
		}
		// 需審核：你正在看它（或正在用 Claude App，可能就在看這個提示）就不跳
		if it.Review && !it.Focused && it.ReviewSince != it.alertedReview &&
			nowMs-it.ReviewSince >= alertReviewDelay.Milliseconds() &&
			!(strings.HasPrefix(it.Key, "cc:") && appFocused) {
			it.alertedReview = it.ReviewSince
			s.alerts = append(s.alerts, Alert{it.AI, "review", label + " 需要你允許", "正在等你按「允許」，不處理工作會停在這裡"})
		}
	}
}

// 取出要跳的通知
func (s *State) TakeAlerts() []Alert {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.alerts
	s.alerts = nil
	return a
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
	case it.FinishedAt != 0 && nowMs < it.doneUntil && it.Failed:
		return "error", it.FinishedAt
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
	rank  int
}

func (s *State) BuildFrame() Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	nowMs := now.UnixMilli()

	var list []ranked
	for _, it := range s.items {
		st, since := status(it, nowMs)
		list = append(list, ranked{it, st, since, rank(it, st, nowMs)})
	}
	// 依優先度、時間排序（結果穩定）
	sort.Slice(list, func(i, j int) bool {
		if list[i].rank != list[j].rank {
			return list[i].rank > list[j].rank
		}
		if list[i].since != list[j].since {
			return list[i].since > list[j].since
		}
		return list[i].it.Key < list[j].it.Key
	})

	// 最需要注意的優先（見 rank）；同一級時，你正在看的 App / 分頁優先
	var main *ranked
	for i := range list {
		if main == nil {
			main = &list[i]
		}
		if list[i].rank != list[0].rank {
			break
		}
		if list[i].it.Focused {
			main = &list[i]
			break
		}
	}
	// 你切到另一個 AI（例如從 Claude App 切到 Gemini 網頁）：主燈跟著你看的那個 AI（連額度一起），
	// 其他 AI 的完成 / 生成中放在小圓點。需審核、錯誤例外：工作停了，不管看哪裡都顯示
	if main != nil && main.rank < 50 && !main.it.Focused {
		for i := range list {
			if list[i].it.Focused && list[i].it.AI != main.it.AI {
				main = &list[i]
				break
			}
		}
		// 跟到 Claude App 時，畫面上的對話通常也是一個 Claude Code 對話：用它（名稱清楚、小圓點不重複）
		if main.it.Key == "app:claude" {
			for i := range list {
				if strings.HasPrefix(list[i].it.Key, "cc:") && list[i].st == main.st {
					main = &list[i]
					break
				}
			}
		}
	}

	_, tz := now.Zone()
	f := Frame{AI: "none", St: "idle", Now: now.Unix(), O: [][]string{}, TZ: tz}
	ai := ""
	if main != nil {
		ai = main.it.AI
		f.St = main.st
		if f.St == "error" {
			f.Msg = main.it.Msg
		}
		f.Age = (nowMs - main.since) / 1000
		if f.Age < 0 {
			f.Age = 0
		}
		// 小圓點：每個對話一個點。例外：Claude App 畫面上的對話，通常也會以 Claude Code 對話出現一次
		// （畫面偵測 + hooks），狀態相同時就當成同一個，不重複畫
		ccSt := map[string]bool{}
		for _, r := range list {
			if strings.HasPrefix(r.it.Key, "cc:") {
				ccSt[r.st] = true
			}
		}
		for _, r := range list {
			if r.it == main.it || r.st == "idle" || len(f.O) >= 5 { // 螢幕最多畫 5 個點
				continue
			}
			if r.it.Key == "app:claude" && ccSt[r.st] {
				continue
			}
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
