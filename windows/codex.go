package main

// Codex（ChatGPT App 的 Codex 模式、Codex CLI、IDE 擴充）：讀 ~/.codex/sessions 底下每個對話的紀錄檔（rollout-*.jsonl），
// task_started = 開始、task_complete = 完成、turn_aborted = 你中斷了（不算完成），轉成偵測項目（Key = "cx:<id 後 8 碼>"）。
// 為什麼不只看畫面：ChatGPT App 視窗關掉（縮到系統匣）後，UI Automation 讀到的畫面會停在關掉那一刻，
// 停止鈕、「處理中」一直都在，做完了也以為還在生成（2026-11 實測：晚了 36 秒，重新打開視窗才知道）。
// 紀錄裡沒有「等你核准」的事件，需審核還是看畫面（側邊欄「待核准」），視窗關掉時就沒有黃燈。
//
// ChatGPT App 畫面上的對話（app:chatgpt、app:chatgpt:bg:<標題>）跟紀錄是同一件事，不能亮兩個燈：
// 紀錄裡沒有對話標題，所以用「同時開始生成」配對（畫面上的對話開始生成的 5 秒內有 Codex 對話也開始）。
// 配對後畫面那一項不送出，改由紀錄這一項亮燈，只借用畫面的「正在看」和「待核准」。ChatGPT 聊天模式不寫紀錄，照舊看畫面。

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	cxPairWindow = 5 * time.Second  // 畫面上開始生成跟紀錄裡 task_started 差多少以內算同一個對話
	cxStale      = 30 * time.Minute // 生成中卻這麼久沒寫紀錄：App 當掉或被關掉，當作已經結束（不亮完成）
	cxKeep       = 12 * time.Hour   // 最後一次動靜超過這麼久的對話不再追蹤
	cxTail       = 256 << 10        // 第一次看到的紀錄檔只讀最後這麼多（App 更新時搬進來的舊對話有好幾 MB）
	cxListEvery  = 3 * time.Second  // 多久重新列一次資料夾（新對話）
	cxAskGrace   = 1500 * time.Millisecond
)

type cxThread struct {
	ID         string
	Cwd        string
	Desktop    bool // ChatGPT App 裡的 Codex（originator 含 desktop）：才會跟畫面配對
	Sub        bool // 子代理、guardian 審查：不單獨亮燈（主對話本身就還在跑）
	Gen        bool
	GenSince   int64
	FinishedAt int64
	off        int64 // 讀到檔案的哪裡
	mod        time.Time
	started    bool // 已經讀過開頭的 session_meta
	midLine    bool // 從檔案中間開始讀：第一行不完整，跳過
	reviewer   string           // 這一輪的 approvals_reviewer：user =「要求核准」、auto_review =「代我核准」
	asks       map[string]int64 // 要你核准的工具呼叫（call_id → 送出時間），有結果就移除
}

type Codex struct {
	mu      sync.Mutex
	dir     string // ~/.codex/sessions
	now     func() time.Time
	threads map[string]*cxThread // 紀錄檔路徑 → 狀態
	pairs   map[string]string    // ChatGPT App 的對話標題 → Codex 對話 id
	// 畫面項目的這一次生成（key@開始時間）→ Codex 對話 id：新對話生成到一半標題會從第一句話換成自動產生的標題，
	// 只靠標題的話配對就斷了、亮兩個燈（2026-11 實測）
	genPairs map[string]string
	listAt   time.Time
}

func NewCodex(dir string) *Codex {
	return &Codex{dir: dir, now: time.Now, threads: map[string]*cxThread{}, pairs: map[string]string{}, genPairs: map[string]string{}}
}

// 每次掃描時呼叫：讀新的紀錄，把 Codex 對話加進偵測結果，拿掉跟它重複的 ChatGPT App 畫面項目
func (c *Codex) Merge(list []Item) []Item {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshLocked()
	return c.mergeLocked(list)
}

func (c *Codex) refreshLocked() {
	now := c.now()
	if c.dir == "" {
		return
	}
	if now.Sub(c.listAt) >= cxListEvery {
		c.listAt = now
		// 資料夾是對話開始的日期（本地時間）：今天、昨天、前天（跨午夜還在用的對話）
		for d := 0; d < 3; d++ {
			t := now.AddDate(0, 0, -d)
			day := filepath.Join(c.dir, t.Format("2006"), t.Format("01"), t.Format("02"))
			files, _ := filepath.Glob(filepath.Join(day, "rollout-*.jsonl"))
			for _, f := range files {
				if _, ok := c.threads[f]; ok {
					continue
				}
				if fi, err := os.Stat(f); err == nil && now.Sub(fi.ModTime()) < cxKeep {
					c.threads[f] = &cxThread{}
				}
			}
		}
	}
	for path, t := range c.threads {
		fi, err := os.Stat(path)
		if err != nil || (now.Sub(fi.ModTime()) > cxKeep && !t.Gen) {
			delete(c.threads, path)
			continue
		}
		if fi.Size() != t.off || !fi.ModTime().Equal(t.mod) {
			c.readLocked(path, t, fi.Size())
			t.mod = fi.ModTime()
		}
		if t.Gen && now.Sub(t.mod) > cxStale {
			t.Gen = false
		}
	}
}

// 讀紀錄檔新增的部分（只處理完整的行；寫到一半的那行等下次）
func (c *Codex) readLocked(path string, t *cxThread, size int64) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if !t.started {
		t.started = true
		// 第一行是 session_meta（對話 id、資料夾、是不是子代理）；很長（含整份系統提示），只讀到換行
		r := bufio.NewReaderSize(f, 64<<10)
		if line, err := r.ReadSlice('\n'); err == nil || err == bufio.ErrBufferFull {
			c.metaLocked(t, line, r)
		}
		if size > cxTail {
			t.off, t.midLine = size-cxTail, true // 舊對話只看最後一段：只需要知道最後的狀態
		}
	}
	if size < t.off {
		t.off = 0 // 檔案變短（被重寫）：從頭讀
	}
	if _, err := f.Seek(t.off, io.SeekStart); err != nil {
		return
	}
	buf, err := io.ReadAll(io.LimitReader(f, size-t.off))
	if err != nil {
		return
	}
	end := bytes.LastIndexByte(buf, '\n')
	if end < 0 {
		return
	}
	t.off += int64(end + 1)
	for i, line := range bytes.Split(buf[:end+1], []byte{'\n'}) {
		if i == 0 && t.midLine {
			continue
		}
		c.lineLocked(t, line)
	}
	t.midLine = false
}

func (c *Codex) metaLocked(t *cxThread, first []byte, r *bufio.Reader) {
	// session_meta 那行可能超過緩衝區：把剩下的接起來（最多 4 MB）
	line := append([]byte(nil), first...)
	for len(line) < 4<<20 && !bytes.HasSuffix(line, []byte{'\n'}) {
		more, err := r.ReadSlice('\n')
		line = append(line, more...)
		if err != nil && err != bufio.ErrBufferFull {
			break
		}
	}
	var m struct {
		Type    string `json:"type"`
		Payload struct {
			ID         string          `json:"id"`
			Cwd        string          `json:"cwd"`
			Originator string          `json:"originator"`
			Source     json.RawMessage `json:"source"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &m) != nil || m.Type != "session_meta" {
		return
	}
	t.ID, t.Cwd = m.Payload.ID, m.Payload.Cwd
	// 舊對話是「Codex Desktop」，2026-11 新開的對話是「codex_work_desktop」
	t.Desktop = strings.Contains(strings.ToLower(m.Payload.Originator), "desktop")
	t.Sub = bytes.Contains(m.Payload.Source, []byte(`"subagent"`))
}

// 要你核准的工具呼叫：要求權限（例如連網路、寫外部資料夾），或要在沙箱外執行指令（2026-11 實測的寫法）
var cxAskMarks = []string{"tools.request_permissions(", `sandbox_permissions:"require_escalated"`, `"sandbox_permissions":"require_escalated"`}

func (c *Codex) lineLocked(t *cxThread, line []byte) {
	// 快速過濾：只解析會用到的行（其他行很多、很長）
	if !bytes.Contains(line, []byte(`"task_`)) && !bytes.Contains(line, []byte(`"turn_aborted"`)) &&
		!bytes.Contains(line, []byte(`"turn_context"`)) && !bytes.Contains(line, []byte(`"call_id"`)) {
		return
	}
	var e struct {
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Payload   struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Input     string `json:"input"`     // custom_tool_call
			Arguments string `json:"arguments"` // function_call
			Reviewer  string `json:"approvals_reviewer"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &e) != nil {
		return
	}
	ts, err := time.Parse(time.RFC3339Nano, e.Timestamp)
	if err != nil {
		return
	}
	ms := ts.UnixMilli()
	p := e.Payload
	switch e.Type {
	case "turn_context":
		t.reviewer = p.Reviewer
		return
	case "response_item":
		switch p.Type {
		case "custom_tool_call", "function_call":
			for _, m := range cxAskMarks {
				if p.CallID != "" && (strings.Contains(p.Input, m) || strings.Contains(p.Arguments, m)) {
					if t.asks == nil {
						t.asks = map[string]int64{}
					}
					t.asks[p.CallID] = ms
					break
				}
			}
		case "custom_tool_call_output", "function_call_output":
			delete(t.asks, p.CallID) // 有結果了：你回答了（或不用問）
		}
		return
	case "event_msg":
	default:
		return
	}
	switch p.Type {
	case "task_started":
		if !t.Gen {
			t.Gen, t.GenSince = true, ms
		}
		t.asks = nil
	case "task_complete":
		if t.Gen {
			t.FinishedAt = ms
		} else if t.FinishedAt == 0 {
			t.FinishedAt = ms // 只讀到最後一段、沒看到開始：記下完成時間（舊的完成不會亮燈）
		}
		t.Gen, t.asks = false, nil
	case "turn_aborted":
		t.Gen, t.asks = false, nil // 你中斷的：不算完成
	}
}

// 紀錄看起來在等你核准：「要求核准」模式下，要求權限 / 沙箱外執行的呼叫送出 cxAskGrace 後還沒有結果。
// 「代我核准」大多自動判斷、不會問你，不算。按過「一律允許」的指令不會再問、但紀錄分不出來，
// 所以只在看不到畫面時用（視窗關掉、Codex CLI），看得到畫面時以畫面上的「待核准」為準
func (t *cxThread) logReview(nowMs int64) (bool, int64) {
	if !t.Gen || t.reviewer != "user" {
		return false, 0
	}
	var since int64
	for _, at := range t.asks {
		if nowMs-at >= cxAskGrace.Milliseconds() && (since == 0 || at < since) {
			since = at
		}
	}
	return since != 0, since
}

func cxShort(id string) string {
	if len(id) > 8 {
		return id[len(id)-8:] // 前幾碼是時間，同一分鐘開的對話都一樣
	}
	return id
}

// ChatGPT App 畫面項目代表的對話標題
func chatgptUITitle(it Item) string {
	if it.Key == "app:chatgpt" {
		return it.View
	}
	return strings.TrimPrefix(it.Key, "app:chatgpt:bg:")
}

func (c *Codex) mergeLocked(list []Item) []Item {
	nowMs := c.now().UnixMilli()
	byID := map[string]*cxThread{}
	cx := map[string]*Item{}
	var order []string
	for _, t := range c.threads {
		if t.ID == "" || t.Sub || (t.GenSince == 0 && t.FinishedAt == 0) {
			continue
		}
		last := t.GenSince
		if t.FinishedAt > last {
			last = t.FinishedAt
		}
		if !t.Gen && nowMs-last > cxKeep.Milliseconds() {
			continue
		}
		label := "Codex"
		if t.Cwd != "" {
			label += " · " + filepath.Base(strings.ReplaceAll(t.Cwd, "\\", "/"))
		}
		byID[t.ID] = t
		rv, rvSince := t.logReview(nowMs)
		cx[t.ID] = &Item{Key: "cx:" + cxShort(t.ID), AI: "chatgpt", Gen: t.Gen, GenSince: t.GenSince, FinishedAt: t.FinishedAt,
			Review: rv, ReviewSince: rvSince, Buttons: []string{label}}
		order = append(order, t.ID)
	}
	for title, id := range c.pairs {
		if byID[id] == nil {
			delete(c.pairs, title)
		}
	}
	for g, id := range c.genPairs {
		if byID[id] == nil {
			delete(c.genPairs, g)
		}
	}
	paired := map[string]bool{}
	for _, id := range c.pairs {
		paired[id] = true
	}
	// AIMonitor 剛重開（例如裝新版）時正在生成的對話：開始時間是重開前，對不上。
	// 畫面上沒配對的生成中只有一個、紀錄裡也只有一個時，就是同一個（兩個以上分不出來，不猜）
	var uiOnly []string
	var cxOnly []string
	for _, it := range list {
		if title := chatgptUITitle(it); strings.HasPrefix(it.Key, "app:chatgpt") && it.Gen && title != "" && c.pairs[title] == "" {
			uiOnly = append(uiOnly, title)
		}
	}
	for id, t := range byID {
		if t.Desktop && t.Gen && !paired[id] {
			cxOnly = append(cxOnly, id)
		}
	}
	if len(uiOnly) == 1 && len(cxOnly) == 1 {
		c.pairs[uiOnly[0]], paired[cxOnly[0]] = cxOnly[0], true
	}
	out := list[:0:0]
	for _, it := range list {
		title := ""
		if strings.HasPrefix(it.Key, "app:chatgpt") {
			title = chatgptUITitle(it)
		}
		if title == "" {
			out = append(out, it)
			continue
		}
		id, ok := c.pairs[title]
		genKey := it.Key + "@" + strconv.FormatInt(it.GenSince, 10)
		if !ok && it.Gen {
			// 同一次生成、標題改了（只在生成中認：停了以後 GenSince 不變，換到別的對話會被誤認）
			if id, ok = c.genPairs[genKey]; ok {
				c.pairs[title] = id
			}
		}
		if !ok && it.Gen {
			// 畫面上這個對話剛開始生成：找同時開始的 ChatGPT App Codex 對話
			for tid, t := range byID {
				if t.Desktop && t.Gen && !paired[tid] {
					if d := t.GenSince - it.GenSince; d > -cxPairWindow.Milliseconds() && d < cxPairWindow.Milliseconds() {
						id, ok = tid, true
						c.pairs[title], paired[tid] = tid, true
						break
					}
				}
			}
		}
		x := cx[id]
		if !ok || x == nil {
			out = append(out, it)
			continue
		}
		if it.Gen {
			c.genPairs[genKey] = id
		}
		// 同一個對話：用紀錄的狀態，借畫面的「正在看」；看得到畫面時「待核准」以畫面為準（視窗關掉時畫面停住，改用紀錄）
		x.Focused = x.Focused || it.Focused
		if !it.Hidden {
			x.Review, x.ReviewSince = x.Gen && it.Review, it.ReviewSince
		}
	}
	sort.Strings(order)
	for _, id := range order {
		out = append(out, *cx[id])
	}
	return out
}
