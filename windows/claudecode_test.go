package main

import (
	"encoding/json"
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
	// 狀態整合：完成後顯示 done，5 分鐘後熄（終端機無法判斷是否正在看）
	s, snow := newTestState()
	*snow = now
	s.ApplyScan([]Item{{Key: it.Key, AI: "claude", Gen: true}})
	*snow = snow.Add(time.Second)
	s.ApplyScan([]Item{it})
	if f := s.BuildFrame(); f.St != "done" {
		t.Fatalf("frame: %+v", f)
	}
	*snow = snow.Add(6 * time.Minute)
	s.ApplyScan([]Item{it})
	if f := s.BuildFrame(); f.St != "idle" {
		t.Fatalf("after 6m: %+v", f)
	}
	ev("SessionEnd", "")
	if len(c.Items()) != 0 {
		t.Fatal("end")
	}
	if c.Handle([]byte("nope")) == nil {
		t.Fatal("bad json accepted")
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
