package main

// Claude Code（終端機的 Agent 模式）：透過 Claude Code 官方的 hooks，
// 在「送出任務 / 需要你允許 / 工具執行完 / 完成」時把事件 POST 到 AIMonitor，
// 轉成和桌面 App、瀏覽器分頁一樣的偵測項目（Key = "cc:<session>"）。

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const ccPort = "47322"
const ccURL = "http://127.0.0.1:" + ccPort + "/cc"

type ccSession struct {
	ID          string
	Cwd         string
	Gen, Review bool
	GenSince    int64
	ReviewSince int64
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
	case "PermissionRequest":
		startGen()
		if !s.Review {
			s.Review, s.ReviewSince = true, ms
		}
	case "Notification":
		// 只有「等你允許」的通知才算需審核（idle_prompt 等其他通知不理）
		if e.NotificationType == "permission_prompt" || (e.NotificationType == "" && strings.Contains(strings.ToLower(e.Message), "permission")) {
			startGen()
			if !s.Review {
				s.Review, s.ReviewSince = true, ms
			}
		}
	case "PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionDenied", "SubagentStop":
		if s.Gen { // 你按了允許（或拒絕）之後，它繼續工作
			s.Review = false
		}
	case "Stop":
		if s.Gen || s.Review {
			s.FinishedAt = ms
		}
		s.Gen, s.Review = false, false
		s.Failed, s.ErrMsg = false, ""
	case "StopFailure":
		// API 錯誤（額度用完、伺服器錯誤…）：紅燈，就算 AIMonitor 晚開、沒看到開始也要亮
		s.FinishedAt = ms
		s.Gen, s.Review = false, false
		s.Failed, s.ErrMsg = true, ccErrMsg(e.Error)
	case "SessionEnd":
		delete(c.sessions, e.SessionID)
	}
	return nil
}

// 轉成偵測項目（每次掃描時和桌面 App / 瀏覽器的結果合併）
func (c *ClaudeCode) Items() []Item {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	var out []Item
	for id, s := range c.sessions {
		idle := !s.Gen && !s.Review
		// 很久沒動靜的工作階段就移除（終端機直接關掉時不一定有 SessionEnd）
		if (idle && now.Sub(s.Last) > 15*time.Minute) || now.Sub(s.Last) > 3*time.Hour {
			delete(c.sessions, id)
			continue
		}
		short := id
		if len(short) > 8 {
			short = short[:8]
		}
		label := "Claude Code"
		if s.Cwd != "" {
			label += " · " + filepath.Base(strings.ReplaceAll(s.Cwd, "\\", "/"))
		}
		out = append(out, Item{
			Key: "cc:" + short, AI: "claude",
			Gen: s.Gen, GenSince: s.GenSince, Review: s.Review, ReviewSince: s.ReviewSince, FinishedAt: s.FinishedAt,
			Failed: s.Failed, Msg: s.ErrMsg,
			Buttons: []string{label},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
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
