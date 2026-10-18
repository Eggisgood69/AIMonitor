package main

// Claude Code（終端機的 Agent 模式）：透過 Claude Code 官方的 hooks，
// 在「送出任務 / 需要你允許 / 工具執行完 / 完成」時把事件 POST 到 AIMonitor，
// 轉成和桌面 App、瀏覽器分頁一樣的偵測項目（Key = "cc:<session>"）。

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const ccPort = "47322"
const ccURL = "http://127.0.0.1:" + ccPort + "/cc"

// 你按下允許時 Claude Code 不會送任何事件（要等工具跑完才有 PostToolUse）。
// 但提示等了約 6 秒還沒人回答時，一定會送 permission_prompt 通知；
// 所以 7 秒內沒收到通知 = 你已經回答了，黃燈轉回綠燈，不用等長指令跑完。
const ccReviewGrace = 7 * time.Second

type ccSession struct {
	ID          string
	Cwd         string
	Gen, Review bool
	GenSince    int64
	ReviewSince int64
	AskAt       int64 // 最近一次 PermissionRequest
	Waiting     bool  // 收到 permission_prompt 通知：確定還在等你
	Asking      bool  // 黃燈是因為 Claude 問你問題（不是權限提示）：等你回覆
	AskAfter    bool  // 這輪結尾會問你問題（Claude App 的「下一步」方塊）：Stop 時亮黃燈
	FinishedAt  int64
	Failed      bool   // 上一次是因為 API 錯誤結束（StopFailure）
	ErrMsg      string // 螢幕上的短字，例如 RATE LIMIT
	Last        time.Time
}

type ClaudeCode struct {
	mu       sync.Mutex
	sessions map[string]*ccSession
	lastAt   time.Time
	now      func() time.Time
	termFg   bool      // 前景視窗是終端機 / 編輯器：當作你正在看 Claude Code
	store    string    // 對話狀態存檔路徑（AIMonitor 重啟後讀回來；空 = 不存）
	loadedAt time.Time // 從檔案讀回來的時間
}

// 從檔案讀回來的「生成中」：這麼久都沒有新事件，就當作在 AIMonitor 重啟期間已經結束（漏掉了 Stop）
const ccRestoredStale = 10 * time.Minute

// AIMonitor 重啟（例如裝新版）後讀回對話狀態：等你回覆 / 允許的對話不會再送事件，不存的話黃燈就消失了
func (c *ClaudeCode) Load(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = path
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved map[string]*ccSession
	if json.Unmarshal(b, &saved) != nil {
		return
	}
	now := c.now()
	c.loadedAt = now
	for id, s := range saved {
		if s != nil && now.Sub(s.Last) < 3*time.Hour {
			c.sessions[id] = s
		}
	}
}

// 寫檔（呼叫時要持有 c.mu）；先寫暫存檔再改名，寫到一半不會壞掉
func (c *ClaudeCode) saveLocked() {
	if c.store == "" {
		return
	}
	b, err := json.Marshal(c.sessions)
	if err != nil {
		return
	}
	tmp := c.store + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, c.store)
	}
}

// 會跑 Claude Code 的終端機、編輯器（前景行程名稱，小寫、不含 .exe）
var terminalProcs = map[string]bool{
	"windowsterminal": true, "cmd": true, "powershell": true, "pwsh": true, "conhost": true,
	"wezterm-gui": true, "alacritty": true, "mintty": true, "code": true, "cursor": true,
}

// 偵測程式每次掃描回報前景視窗的行程名稱
func (c *ClaudeCode) SetForeground(proc string) {
	c.mu.Lock()
	c.termFg = terminalProcs[strings.ToLower(proc)]
	c.mu.Unlock()
}

func NewClaudeCode() *ClaudeCode {
	return &ClaudeCode{sessions: map[string]*ccSession{}, now: time.Now}
}

type ccEvent struct {
	SessionID        string          `json:"session_id"`
	Cwd              string          `json:"cwd"`
	HookEventName    string          `json:"hook_event_name"`
	NotificationType string          `json:"notification_type"`
	Message          string          `json:"message"`
	Error            json.RawMessage `json:"error"` // StopFailure 的錯誤種類（rate_limit、server_error …）
	ToolName         string          `json:"tool_name"`
	ToolInput        json.RawMessage `json:"tool_input"`
}

// Claude 問你問題用的工具（看工具，不看按鈕文字：按鈕每次都不一樣）
//
//	now：題目一出現就在等你（選擇題 AskUserQuestion、核准計畫 ExitPlanMode）
//	after：這輪結束時才問（Claude App 的「下一步」方塊 = wrap_up 工具帶 ask）
func ccAskKind(tool string, input json.RawMessage) string {
	switch {
	case tool == "AskUserQuestion" || tool == "ExitPlanMode":
		return "now"
	case strings.HasSuffix(tool, "wrap_up"):
		var in struct {
			Ask string `json:"ask"`
		}
		if json.Unmarshal(input, &in) == nil && strings.TrimSpace(in.Ask) != "" {
			return "after"
		}
	}
	return ""
}

// StopFailure 的錯誤種類 → 螢幕上的短字（螢幕只有英文字型，用大寫）
func ccErrMsg(raw json.RawMessage) string {
	var kind string
	if json.Unmarshal(raw, &kind) != nil {
		var obj struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &obj) == nil {
			kind = obj.Type
		}
	}
	switch k := strings.ToLower(kind); {
	case strings.Contains(k, "rate_limit"), strings.Contains(k, "overloaded"):
		return "RATE LIMIT"
	case strings.Contains(k, "auth"):
		return "AUTH FAILED"
	case strings.Contains(k, "billing"):
		return "BILLING"
	case strings.Contains(k, "max_output_tokens"):
		return "TOO LONG"
	default:
		return "API ERROR"
	}
}

// 收到一個 hook 事件
func (c *ClaudeCode) Handle(body []byte) error {
	var e ccEvent
	if err := json.Unmarshal(body, &e); err != nil {
		return err
	}
	if e.SessionID == "" || e.HookEventName == "" {
		return errors.New("missing session_id / hook_event_name")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	ms := now.UnixMilli()
	c.lastAt = now
	s := c.sessions[e.SessionID]
	if s == nil {
		if e.HookEventName == "SessionEnd" {
			return nil
		}
		s = &ccSession{ID: e.SessionID}
		c.sessions[e.SessionID] = s
	}
	if e.Cwd != "" {
		s.Cwd = e.Cwd
	}
	s.Last = now
	startGen := func() {
		if !s.Gen {
			s.Gen, s.GenSince = true, ms
		}
	}
	switch e.HookEventName {
	case "UserPromptSubmit":
		s.Gen, s.GenSince, s.Review = true, ms, false
		s.Failed, s.ErrMsg = false, ""
		s.Asking, s.AskAfter = false, false
	case "PermissionRequest":
		startGen()
		if !s.Review {
			s.Review, s.ReviewSince = true, ms
		}
		s.AskAt, s.Waiting = ms, false
	case "Notification":
		// 只有「等你允許」的通知才算需審核（idle_prompt 等其他通知不理）
		if e.NotificationType == "permission_prompt" || (e.NotificationType == "" && strings.Contains(strings.ToLower(e.Message), "permission")) {
			startGen()
			if !s.Review {
				s.Review, s.ReviewSince = true, ms
			}
			s.Waiting = true
		}
	case "PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionDenied", "SubagentStop":
		// 正在用工具 = 正在工作（AIMonitor 中途重啟、沒看到 UserPromptSubmit 時也能補回來）；
		// 你按了允許（或拒絕）之後，它也是靠這些事件繼續工作
		startGen()
		if e.HookEventName == "PreToolUse" {
			switch ccAskKind(e.ToolName, e.ToolInput) {
			case "now": // 題目出現了：等你回答（回答後的 PostToolUse 會熄）
				if !s.Review {
					s.ReviewSince = ms
				}
				s.Review, s.Waiting, s.Asking = true, true, true
				return nil
			case "after":
				s.AskAfter = true
			}
		}
		s.Review, s.Asking = false, false
	case "Stop":
		if s.Gen || s.Review {
			s.FinishedAt = ms
		}
		s.Gen, s.Review = false, false
		s.Failed, s.ErrMsg = false, ""
		if s.AskAfter { // 結尾問了你問題：等你回覆（送出下一則訊息才熄）
			s.Review, s.ReviewSince, s.Waiting, s.Asking, s.AskAfter = true, ms, true, true, false
		}
	case "StopFailure":
		// API 錯誤（額度用完、伺服器錯誤…）：紅燈，就算 AIMonitor 晚開、沒看到開始也要亮
		s.FinishedAt = ms
		s.Gen, s.Review = false, false
		s.Failed, s.ErrMsg = true, ccErrMsg(e.Error)
	case "SessionEnd":
		delete(c.sessions, e.SessionID)
	}
	c.saveLocked()
	return nil
}

// 轉成偵測項目（每次掃描時和桌面 App / 瀏覽器的結果合併）
func (c *ClaudeCode) Items() []Item {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	var out []Item
	changed := false
	defer func() {
		if changed {
			c.saveLocked()
		}
	}()
	for id, s := range c.sessions {
		if s.Review && !s.Waiting && s.AskAt != 0 && now.UnixMilli()-s.AskAt > ccReviewGrace.Milliseconds() {
			s.Review = false // 沒收到「還在等你」的通知：你已經按了允許，工具正在跑
			changed = true
		}
		if s.Gen && !s.Review && !c.loadedAt.IsZero() && s.Last.Before(c.loadedAt) && now.Sub(c.loadedAt) > ccRestoredStale {
			s.Gen = false // 讀回來的生成中一直沒新事件：重啟期間已經結束了（不算完成，不亮綠燈）
			changed = true
		}
		idle := !s.Gen && !s.Review
		// 很久沒動靜的工作階段就移除（終端機直接關掉時不一定有 SessionEnd）
		if (idle && now.Sub(s.Last) > 15*time.Minute) || now.Sub(s.Last) > 3*time.Hour {
			delete(c.sessions, id)
			changed = true
			continue
		}
		short := shortID(id)
		label := "Claude Code"
		if s.Cwd != "" {
			label += " · " + filepath.Base(strings.ReplaceAll(s.Cwd, "\\", "/"))
		}
		out = append(out, Item{
			Key: "cc:" + short, AI: "claude", Focused: c.termFg,
			Gen: s.Gen, GenSince: s.GenSince, Review: s.Review, ReviewSince: s.ReviewSince, FinishedAt: s.FinishedAt,
			Failed: s.Failed, Msg: s.ErrMsg, Asking: s.Review && s.Asking,
			Buttons: []string{label},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// 只接受 Claude Code 的 http hook：不帶 Origin（瀏覽器發的請求一定會帶）、Content-Type 是 JSON
// （網頁要送 JSON 必須先過 CORS 預檢，我們不回應預檢，所以網頁送不進來）
func ccRequestAllowed(origin, contentType string) bool {
	return origin == "" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "application/json")
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// 長按 BOOT：「等你回覆」的黃燈標為已讀（權限提示不熄：工作真的卡在那裡）
func (c *ClaudeCode) AckQuestions() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.sessions {
		if s.Asking && !s.Gen {
			s.Review, s.Asking = false, false
		}
	}
	c.saveLocked()
}

func (c *ClaudeCode) Status() (sessions int, last time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sessions), c.lastAt
}

// ---------- ~/.claude/settings.json 的 hooks 設定 ----------

// 要掛上的事件（Notification 只聽「等你允許」）
var ccEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse", "PermissionDenied", "Notification", "Stop", "StopFailure", "SessionEnd"}

func isOurGroup(g interface{}) bool {
	m, ok := g.(map[string]interface{})
	if !ok {
		return false
	}
	hs, _ := m["hooks"].([]interface{})
	for _, h := range hs {
		if hm, ok := h.(map[string]interface{}); ok {
			if u, _ := hm["url"].(string); strings.Contains(u, "127.0.0.1:"+ccPort+"/cc") {
				return true
			}
		}
	}
	return false
}

// 回傳加上（install=true）或移除（false）AIMonitor hooks 後的設定內容；其他設定原封不動
func ccPatchSettings(orig []byte, install bool) ([]byte, error) {
	root := map[string]interface{}{}
	if len(bytes.TrimSpace(orig)) > 0 {
		d := json.NewDecoder(bytes.NewReader(orig))
		d.UseNumber()
		if err := d.Decode(&root); err != nil {
			return nil, errors.New("settings.json 格式無法解析，為了安全不修改它：" + err.Error())
		}
	}
	hooks, _ := root["hooks"].(map[string]interface{})
	if hooks == nil {
		if root["hooks"] != nil {
			return nil, errors.New("settings.json 的 hooks 格式不是預期的物件，為了安全不修改它")
		}
		hooks = map[string]interface{}{}
	}
	// 先移除舊的（避免重複）
	for ev, v := range hooks {
		arr, ok := v.([]interface{})
		if !ok {
			continue
		}
		var keep []interface{}
		for _, g := range arr {
			if !isOurGroup(g) {
				keep = append(keep, g)
			}
		}
		if len(keep) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = keep
		}
	}
	if install {
		for _, ev := range ccEvents {
			h := map[string]interface{}{"type": "http", "url": ccURL, "timeout": 2}
			g := map[string]interface{}{"hooks": []interface{}{h}}
			if ev == "Notification" {
				g["matcher"] = "permission_prompt"
			}
			arr, _ := hooks[ev].([]interface{})
			hooks[ev] = append(arr, g)
		}
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = hooks
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func ccInstalledIn(b []byte) bool {
	return bytes.Contains(b, []byte("127.0.0.1:"+ccPort+"/cc"))
}
