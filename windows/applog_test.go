package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 實測（2026-11-07）：Claude Code 用內建瀏覽器執行 JavaScript，App 問「允許 Claude 在以下位置執行 JavaScript？」，
// hooks 沒有任何事件；切到別的對話時畫面上也看不到允許鈕 → 看 Claude App 的 main.log
func TestAppPermsFromMainLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "main.log")
	write := func(s string) {
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		f.WriteString(s)
		f.Close()
	}
	write("2026-11-07 21:00:08 [info] Mapping internal session local_051ca40c-c80d-4d95-a061-e90ea4d60c6a to CLI session 6caf129a-a9d0-4a73-bbec-f4bb1a97ed5f\n")
	a := NewAppPerms(p)
	now := time.Date(2026, 11, 7, 21, 25, 10, 0, time.Local)
	a.now = func() time.Time { return now }
	a.Poll()
	const cli = "6caf129a-a9d0-4a73-bbec-f4bb1a97ed5f"
	if _, ok := a.Waiting(cli); ok {
		t.Fatal("nothing asked yet")
	}
	// 一行還沒寫完：不算
	write("2026-11-07 21:25:06 [info] Emitted tool permission request 8b9b25a6-426f-4a8e-a647-71b4f248e576 for browser:javascript_tool in sess")
	a.Poll()
	if _, ok := a.Waiting(cli); ok {
		t.Fatal("half line should wait")
	}
	write("ion local_051ca40c-c80d-4d95-a061-e90ea4d60c6a\n")
	a.Poll()
	at, ok := a.Waiting(cli)
	if !ok || at.Format("15:04:05") != "21:25:06" {
		t.Fatalf("should be waiting since 21:25:06: %v %v", at, ok)
	}
	if _, ok := a.Waiting("other"); ok {
		t.Fatal("other session should not wait")
	}
	write("2026-11-07 21:26:07 [info] Received permission response for 8b9b25a6-426f-4a8e-a647-71b4f248e576: deny (tool: browser:javascript_tool)\n")
	a.Poll()
	if _, ok := a.Waiting(cli); ok {
		t.Fatal("answered")
	}
	// App 換了新的紀錄檔（變小）：從頭讀
	os.WriteFile(p, []byte("2026-11-07 21:30:00 [info] Emitted tool permission request r2 for browser:screenshot in session local_051ca40c-c80d-4d95-a061-e90ea4d60c6a\n"), 0o644)
	a.Poll()
	if _, ok := a.Waiting(cli); !ok {
		t.Fatal("new log file should be read from the start")
	}
	// 一直沒回答（App 當掉）：6 小時後不算
	now = now.Add(alPermStale + time.Hour)
	if _, ok := a.Waiting(cli); ok {
		t.Fatal("stale request")
	}
}

type fakePerms map[string]time.Time

func (f fakePerms) Waiting(id string) (time.Time, bool) { t, ok := f[id]; return t, ok }

func TestClaudeCodeAppPermission(t *testing.T) {
	c := NewClaudeCode()
	now := time.Date(2026, 11, 7, 21, 25, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	perms := fakePerms{}
	c.appPerms = perms
	if err := c.Handle([]byte(`{"session_id":"6caf129a-a9d0","hook_event_name":"PreToolUse","tool_name":"mcp__Claude_Browser__javascript_tool"}`)); err != nil {
		t.Fatal(err)
	}
	if it := c.Items()[0]; !it.Gen || it.Review {
		t.Fatalf("working: %+v", it)
	}
	perms["6caf129a-a9d0"] = now
	if it := c.Items()[0]; !it.Review || it.ReviewSince != now.UnixMilli() {
		t.Fatalf("app is asking: %+v", it)
	}
	delete(perms, "6caf129a-a9d0")
	if it := c.Items()[0]; it.Review {
		t.Fatalf("answered: %+v", it)
	}
}
