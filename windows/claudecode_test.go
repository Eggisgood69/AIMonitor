package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeCodeEvents(t *testing.T) {
	c := NewClaudeCode()
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	ev := func(name string, extra string) {
		body := `{"session_id":"abcdef123456","cwd":"C:\\work\\shop-api","hook_event_name":"` + name + `"` + extra + `}`
		if err := c.Handle([]byte(body)); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	ev("SessionStart", "")
	if it := c.Items(); len(it) != 1 || it[0].Gen || it[0].Buttons[0] != "Claude Code · shop-api" {
		t.Fatalf("start: %+v", it)
	}
	ev("UserPromptSubmit", "")
	if it := c.Items()[0]; !it.Gen || it.Review {
		t.Fatalf("prompt: %+v", it)
	}
	ev("PermissionRequest", `,"tool_name":"Bash"`)
	if it := c.Items()[0]; !it.Gen || !it.Review {
		t.Fatalf("perm: %+v", it)
	}
	ev("PostToolUse", `,"tool_name":"Bash"`)
	if it := c.Items()[0]; !it.Gen || it.Review {
		t.Fatalf("post: %+v", it)
	}
	ev("Notification", `,"notification_type":"idle_prompt","message":"Claude is waiting for your input"`)
	if it := c.Items()[0]; it.Review {
		t.Fatalf("idle notification should not review: %+v", it)
	}
	ev("Stop", "")
	it := c.Items()[0]
	if it.Gen || it.Review || it.FinishedAt == 0 {
		t.Fatalf("stop: %+v", it)
	}
	// 狀態整合：在背景完成（終端機不在前景）→ 一直亮到你看過
	s, snow := newTestState()
	*snow = now
	s.ApplyScan([]Item{{Key: it.Key, AI: "claude", Gen: true}})
	*snow = snow.Add(time.Second)
	s.ApplyScan([]Item{it})
	if f := s.BuildFrame(); f.St != "done" {
		t.Fatalf("frame: %+v", f)
	}
	*snow = snow.Add(10 * time.Minute)
	s.ApplyScan([]Item{it})
	if f := s.BuildFrame(); f.St != "done" {
		t.Fatalf("background done should stay: %+v", f)
	}
	// 切到終端機（前景）= 看過：再亮 8 秒就熄
	c.SetForeground("WindowsTerminal")
	s.ApplyScan(c.Items())
	*snow = snow.Add(9 * time.Second)
	if f := s.BuildFrame(); f.St != "idle" {
		t.Fatalf("viewed in terminal: %+v", f)
	}
	c.SetForeground("chrome")
	if c.Items()[0].Focused {
		t.Fatal("chrome is not a terminal")
	}
	ev("SessionEnd", "")
	if len(c.Items()) != 0 {
		t.Fatal("end")
	}
	if c.Handle([]byte("nope")) == nil {
		t.Fatal("bad json accepted")
	}
}

// Claude 問你問題（不是權限提示）：看工具判斷，不看按鈕文字
func TestClaudeCodeQuestions(t *testing.T) {
	c := NewClaudeCode()
	now := time.Date(2026, 10, 18, 21, 15, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	ev := func(name, extra string) {
		if err := c.Handle([]byte(`{"session_id":"43e85d0e1234","cwd":"C:/w/2026-EAC-AirPurifier","hook_event_name":"` + name + `"` + extra + `}`)); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	wrapUp := func(ask string) string {
		return `,"tool_name":"mcp__ccd_turn__wrap_up","tool_input":{"recap":"兩份 spec 草案寫好了","ask":"` + ask + `"}`
	}
	it := func() Item { return c.Items()[0] }

	// 1. 回覆結尾問你（Claude App 的「下一步」方塊）：這輪結束時亮黃燈，送出下一則訊息才熄
	ev("UserPromptSubmit", "")
	ev("PreToolUse", wrapUp("兩份 spec 及預設值可以核准嗎？"))
	ev("PostToolUse", wrapUp("兩份 spec 及預設值可以核准嗎？"))
	if x := it(); x.Review {
		t.Fatalf("not yet (turn still running): %+v", x)
	}
	ev("Stop", "")
	if x := it(); !x.Review || !x.Asking || x.Gen || x.FinishedAt == 0 {
		t.Fatalf("question at end → waiting for reply: %+v", x)
	}
	now = now.Add(10 * time.Minute) // 不會因為 7 秒寬限就熄
	if x := it(); !x.Review {
		t.Fatalf("still waiting after 10 min: %+v", x)
	}
	ev("UserPromptSubmit", "")
	if x := it(); x.Review || x.Asking {
		t.Fatalf("answered: %+v", x)
	}

	// 2. 結尾沒有問題：只是完成
	ev("PreToolUse", wrapUp(""))
	ev("Stop", "")
	if x := it(); x.Review {
		t.Fatalf("no question → just done: %+v", x)
	}

	// 3. 中途問選擇題：題目出現就亮，回答後熄
	ev("UserPromptSubmit", "")
	ev("PreToolUse", `,"tool_name":"AskUserQuestion","tool_input":{"questions":[]}`)
	now = now.Add(time.Minute)
	if x := it(); !x.Review || !x.Asking || !x.Gen {
		t.Fatalf("AskUserQuestion: %+v", x)
	}
	ev("PostToolUse", `,"tool_name":"AskUserQuestion"`)
	if x := it(); x.Review {
		t.Fatalf("answered AskUserQuestion: %+v", x)
	}

	// 4. 長按 BOOT：「等你回覆」可以標為已讀；權限提示不行
	ev("PreToolUse", wrapUp("要提交嗎？"))
	ev("Stop", "")
	c.AckQuestions()
	if x := it(); x.Review {
		t.Fatalf("ack question: %+v", x)
	}
	ev("UserPromptSubmit", "")
	ev("PermissionRequest", `,"tool_name":"Bash"`)
	c.AckQuestions()
	if x := it(); !x.Review || x.Asking {
		t.Fatalf("permission prompt must stay: %+v", x)
	}

	// 5. 狀態整合：主燈黃燈，通知只跳「在等你回覆」，不另外跳「完成了」
	c2 := NewClaudeCode()
	c2.now = func() time.Time { return now }
	s, snow := newTestState()
	*snow = now
	s.ApplyScan(nil)
	*snow = snow.Add(time.Minute)
	now = *snow
	for _, e := range []string{`"UserPromptSubmit"`, `"PreToolUse"` + wrapUp("可以核准嗎？"), `"Stop"`} {
		if err := c2.Handle([]byte(`{"session_id":"43e85d0e1234","cwd":"C:/w/2026-EAC-AirPurifier","hook_event_name":` + e + `}`)); err != nil {
			t.Fatal(err)
		}
		s.ApplyScan(c2.Items())
	}
	*snow = snow.Add(alertReviewDelay + time.Second)
	now = *snow
	s.ApplyScan(c2.Items())
	if f := s.BuildFrame(); f.St != "review" {
		t.Fatalf("frame: %+v", f)
	}
	a := s.TakeAlerts()
	if len(a) != 1 || a[0].Title != "Claude Code · 2026-EAC-AirPurifier 在等你回覆" {
		t.Fatalf("alerts: %+v", a)
	}
}

// 實測情境（21:47:03）：裝新版重啟 AIMonitor，兩個「等你回覆」的黃燈不見了（等你回覆時對話不會再送事件）
func TestClaudeCodePersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cc-sessions.json")
	now := time.Date(2026, 10, 18, 21, 46, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	send := func(c *ClaudeCode, id, name, extra string) {
		if err := c.Handle([]byte(`{"session_id":"` + id + `","cwd":"C:/w/proj","hook_event_name":"` + name + `"` + extra + `}`)); err != nil {
			t.Fatal(err)
		}
	}
	byKey := func(c *ClaudeCode) map[string]Item {
		m := map[string]Item{}
		for _, it := range c.Items() {
			m[it.Key] = it
		}
		return m
	}

	old := NewClaudeCode()
	old.now = clock
	old.Load(path) // 第一次啟動：沒有檔案
	send(old, "d4674c44aaaa", "UserPromptSubmit", "")
	send(old, "d4674c44aaaa", "PreToolUse", `,"tool_name":"mcp__ccd_turn__wrap_up","tool_input":{"ask":"要繼續 Phase 3 嗎？"}`)
	send(old, "d4674c44aaaa", "Stop", "")              // 等你回覆
	send(old, "43e85d0eaaaa", "UserPromptSubmit", "")  // 還在跑
	send(old, "7fc4fb19aaaa", "PermissionRequest", "") // 等你允許
	send(old, "7fc4fb19aaaa", "Notification", `,"notification_type":"permission_prompt"`)

	// AIMonitor 重啟
	now = now.Add(time.Minute)
	c := NewClaudeCode()
	c.now = clock
	c.Load(path)
	m := byKey(c)
	if x := m["cc:d4674c44"]; !x.Review || !x.Asking {
		t.Fatalf("waiting for reply should survive restart: %+v", x)
	}
	if x := m["cc:43e85d0e"]; !x.Gen {
		t.Fatalf("working should survive restart: %+v", x)
	}
	if x := m["cc:7fc4fb19"]; !x.Review || x.Asking {
		t.Fatalf("permission prompt should survive restart: %+v", x)
	}

	// 讀回來的「生成中」10 分鐘都沒新事件：重啟期間已經結束了；等你回覆的仍然亮
	now = now.Add(ccRestoredStale + time.Minute)
	m = byKey(c)
	if x := m["cc:43e85d0e"]; x.Gen {
		t.Fatalf("stale restored working should stop: %+v", x)
	}
	if x := m["cc:d4674c44"]; !x.Review {
		t.Fatalf("still waiting for reply: %+v", x)
	}
	// 有新事件的對話照常
	send(c, "43e85d0eaaaa", "UserPromptSubmit", "")
	now = now.Add(20 * time.Minute)
	if x := byKey(c)["cc:43e85d0e"]; !x.Gen {
		t.Fatalf("new event after restart → normal working: %+v", x)
	}

	// 壞掉的檔案：當作沒有，不會當掉
	os.WriteFile(path, []byte("{broken"), 0o644)
	c3 := NewClaudeCode()
	c3.Load(path)
	if len(c3.Items()) != 0 {
		t.Fatal("broken file should load nothing")
	}
}

func TestClaudeCodeRequestAllowed(t *testing.T) {
	cases := []struct {
		origin, ctype string
		ok            bool
	}{
		{"", "application/json", true}, // Claude Code 的 http hook
		{"", "application/json; charset=utf-8", true},
		{"https://evil.example", "application/json", false}, // 網頁（會帶 Origin）
		{"null", "text/plain", false},
		{"", "text/plain", false}, // 不是 JSON（網頁不經預檢能送的格式）
		{"", "", false},
	}
	for _, c := range cases {
		if got := ccRequestAllowed(c.origin, c.ctype); got != c.ok {
			t.Errorf("origin=%q type=%q: got %v", c.origin, c.ctype, got)
		}
	}
}

func TestClaudeCodeResumeAfterRestart(t *testing.T) {
	// AIMonitor 中途重啟：沒看到 UserPromptSubmit，只收到工具事件，也要算生成中
	c := NewClaudeCode()
	if err := c.Handle([]byte(`{"session_id":"43e85d0e1234","hook_event_name":"PostToolUse"}`)); err != nil {
		t.Fatal(err)
	}
	if it := c.Items(); len(it) != 1 || !it[0].Gen {
		t.Fatalf("resume: %+v", it)
	}
}

func TestClaudeCodeReviewGrace(t *testing.T) {
	c := NewClaudeCode()
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	ev := func(name string, extra string) {
		if err := c.Handle([]byte(`{"session_id":"abcdef123456","hook_event_name":"` + name + `"` + extra + `}`)); err != nil {
			t.Fatal(err)
		}
	}
	ev("UserPromptSubmit", "")
	// 6 秒內就按了允許：沒有通知，7 秒後轉回生成中（長指令還在跑，沒有 PostToolUse）
	ev("PermissionRequest", "")
	now = now.Add(5 * time.Second)
	if it := c.Items()[0]; !it.Review {
		t.Fatalf("5s: %+v", it)
	}
	now = now.Add(3 * time.Second)
	if it := c.Items()[0]; it.Review || !it.Gen {
		t.Fatalf("8s without notification: %+v", it)
	}
	// 沒按：6 秒時收到通知，就一直維持需審核
	ev("PermissionRequest", "")
	now = now.Add(6 * time.Second)
	ev("Notification", `,"notification_type":"permission_prompt"`)
	now = now.Add(time.Minute)
	if it := c.Items()[0]; !it.Review {
		t.Fatalf("waiting 1m: %+v", it)
	}
	// 按了允許、工具跑完
	ev("PostToolUse", "")
	if it := c.Items()[0]; it.Review {
		t.Fatalf("post: %+v", it)
	}
	// 下一個提示重新計時（不會沿用上次的「還在等你」）
	ev("PermissionRequest", "")
	now = now.Add(8 * time.Second)
	if it := c.Items()[0]; it.Review {
		t.Fatalf("next prompt: %+v", it)
	}
}

func TestClaudeCodeStopFailure(t *testing.T) {
	c := NewClaudeCode()
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	ev := func(name string, extra string) {
		body := `{"session_id":"abcdef123456","hook_event_name":"` + name + `"` + extra + `}`
		if err := c.Handle([]byte(body)); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	ev("UserPromptSubmit", "")
	s, snow := newTestState()
	*snow = now
	s.ApplyScan(c.Items())
	ev("StopFailure", `,"error":"rate_limit"`)
	it := c.Items()[0]
	if !it.Failed || it.Msg != "RATE LIMIT" || it.Gen {
		t.Fatalf("failure: %+v", it)
	}
	*snow = snow.Add(time.Second)
	s.ApplyScan(c.Items())
	if f := s.BuildFrame(); f.St != "error" || f.Msg != "RATE LIMIT" {
		t.Fatalf("frame: %+v", f)
	}
	// 錯誤不像完成那樣 5 分鐘就熄
	*snow = snow.Add(6 * time.Minute)
	s.ApplyScan(c.Items())
	if f := s.BuildFrame(); f.St != "error" {
		t.Fatalf("after 6m: %+v", f)
	}
	// 長按 BOOT 標為已讀
	s.Acknowledge()
	if f := s.BuildFrame(); f.St != "idle" || f.Msg != "" {
		t.Fatalf("ack: %+v", f)
	}
	// 再送出任務就清掉錯誤
	ev("UserPromptSubmit", "")
	if it := c.Items()[0]; it.Failed || it.Msg != "" {
		t.Fatalf("retry: %+v", it)
	}
	// 錯誤種類
	for in, want := range map[string]string{`"server_error"`: "API ERROR", `"authentication_failed"`: "AUTH FAILED", `{"type":"billing_error"}`: "BILLING", `null`: "API ERROR"} {
		if got := ccErrMsg(json.RawMessage(in)); got != want {
			t.Errorf("ccErrMsg(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestClaudeCodeSettingsPatch(t *testing.T) {
	orig := `{
  "model": "opus",
  "permissions": {"allow": ["Bash(npm test)"]},
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "echo done"}]}]
  },
  "big": 12345678901234567890
}`
	out, err := ccPatchSettings([]byte(orig), true)
	if err != nil {
		t.Fatal(err)
	}
	if !ccInstalledIn(out) || !strings.Contains(string(out), "12345678901234567890") || !strings.Contains(string(out), "Bash(npm test)") {
		t.Fatalf("install lost data:\n%s", out)
	}
	var m map[string]interface{}
	json.Unmarshal(out, &m)
	hooks := m["hooks"].(map[string]interface{})
	if len(hooks["Stop"].([]interface{})) != 2 || len(hooks) != len(ccEvents) {
		t.Fatalf("hooks: %v", hooks)
	}
	// 再裝一次不會重複
	out2, _ := ccPatchSettings(out, true)
	if strings.Count(string(out2), ccURL) != len(ccEvents) {
		t.Fatalf("duplicate install:\n%s", out2)
	}
	// 移除後回到原本的 hooks
	out3, _ := ccPatchSettings(out2, false)
	if ccInstalledIn(out3) || !strings.Contains(string(out3), "echo done") {
		t.Fatalf("remove:\n%s", out3)
	}
	// 沒有設定檔
	out4, err := ccPatchSettings(nil, true)
	if err != nil || !ccInstalledIn(out4) {
		t.Fatalf("empty: %v %s", err, out4)
	}
	// 壞掉的檔案不碰
	if _, err := ccPatchSettings([]byte("{ // comment\n}"), true); err == nil {
		t.Fatal("broken json should fail")
	}
}
