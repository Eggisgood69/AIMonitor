package main

import (
	"encoding/json"
	"testing"
	"time"
)

func newTestState() (*State, *time.Time) {
	t := time.Unix(1792220000, 0)
	s := NewState()
	s.now = func() time.Time { return t }
	return s, &t
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func TestFocusedGeneratingThenDone(t *testing.T) {
	s, now := newTestState()
	s.ApplyScan([]Item{{Key: "app:claude", AI: "claude", Focused: true}})
	if f := s.BuildFrame(); f.AI != "claude" || f.St != "idle" {
		t.Fatalf("idle: %+v", f)
	}
	s.ApplyScan([]Item{{Key: "app:claude", AI: "claude", Focused: true, Gen: true, GenSince: ms(*now)}})
	*now = now.Add(30 * time.Second)
	f := s.BuildFrame()
	if f.St != "working" || f.Age != 30 {
		t.Fatalf("working: %+v", f)
	}
	fin := s.ApplyScan([]Item{{Key: "app:claude", AI: "claude", Focused: true, FinishedAt: ms(*now)}})
	if len(fin) != 1 {
		t.Fatalf("finished not reported")
	}
	if f := s.BuildFrame(); f.St != "done" {
		t.Fatalf("done: %+v", f)
	}
	*now = now.Add(9 * time.Second) // 正在看：8 秒後熄
	if f := s.BuildFrame(); f.St != "idle" {
		t.Fatalf("done should expire while viewing: %+v", f)
	}
}

func TestBackgroundDoneStaysUntilViewed(t *testing.T) {
	s, now := newTestState()
	s.ApplyScan([]Item{{Key: "web:chatgpt", AI: "chatgpt", Gen: true, GenSince: ms(*now)}})
	*now = now.Add(5 * time.Second)
	s.ApplyScan([]Item{{Key: "web:chatgpt", AI: "chatgpt", FinishedAt: ms(*now)}})
	*now = now.Add(10 * time.Minute)
	if f := s.BuildFrame(); f.AI != "chatgpt" || f.St != "done" {
		t.Fatalf("background done should stay: %+v", f)
	}
	s.ApplyScan([]Item{{Key: "web:chatgpt", AI: "chatgpt", Focused: true, FinishedAt: ms(now.Add(-10 * time.Minute))}})
	if f := s.BuildFrame(); f.St != "done" {
		t.Fatalf("just viewed, still green: %+v", f)
	}
	*now = now.Add(9 * time.Second)
	if f := s.BuildFrame(); f.St != "idle" {
		t.Fatalf("viewed → idle: %+v", f)
	}
}

func TestFocusedWinsAndOthers(t *testing.T) {
	s, now := newTestState()
	s.ApplyScan([]Item{
		{Key: "app:claude", AI: "claude", Focused: true, Gen: true, GenSince: ms(*now)},
		{Key: "web:gemini", AI: "gemini", Review: true, ReviewSince: ms(*now)},
	})
	f := s.BuildFrame()
	if f.AI != "claude" || f.St != "working" || len(f.O) != 1 || f.O[0][0] != "gemini" || f.O[0][1] != "review" {
		t.Fatalf("focus/others: %+v", f)
	}
	// 沒有在看任何 AI：顯示需審核的那個
	s.ApplyScan([]Item{
		{Key: "app:claude", AI: "claude", Gen: true, GenSince: ms(*now)},
		{Key: "web:gemini", AI: "gemini", Review: true, ReviewSince: ms(*now)},
	})
	if f := s.BuildFrame(); f.AI != "gemini" || f.St != "review" {
		t.Fatalf("priority: %+v", f)
	}
}

func TestQuotaAndNoItems(t *testing.T) {
	s, now := newTestState()
	if f := s.BuildFrame(); f.AI != "none" {
		t.Fatalf("empty: %+v", f)
	}
	raw := json.RawMessage(`{"five_hour":{"utilization":45.0,"resets_at":"2030-01-01T00:00:00.123+00:00"},"seven_day":{"utilization":63.4,"resets_at":"2030-01-05T00:00:00Z"},"seven_day_opus":null}`)
	q, ok := parseClaudeUsage(raw)
	if !ok || q.P5 != 45 || q.PW != 63.4 || q.R5 != 1893456000 {
		t.Fatalf("parse: %+v", q)
	}
	s.SetQuota("claude", q)
	f := s.BuildFrame()
	if f.AI != "claude" || f.P5 == nil || *f.P5 != 45 || *f.PW != 63.4 {
		t.Fatalf("quota frame: %+v", f)
	}
	b, _ := json.Marshal(f)
	t.Logf("%s", b)
	*now = now.Add(20 * time.Minute) // 過期
	if f := s.BuildFrame(); f.AI != "none" {
		t.Fatalf("stale quota: %+v", f)
	}
}

func TestStartupOldFinishNotGreen(t *testing.T) {
	s, now := newTestState()
	s.ApplyScan([]Item{{Key: "app:claude", AI: "claude", FinishedAt: ms(now.Add(-time.Hour))}})
	if f := s.BuildFrame(); f.St != "idle" {
		t.Fatalf("old finish should not light: %+v", f)
	}
}

func TestAcknowledge(t *testing.T) {
	s, now := newTestState()
	s.ApplyScan([]Item{{Key: "app:claude", AI: "claude", Gen: true}})
	s.ApplyScan([]Item{{Key: "app:claude", AI: "claude", FinishedAt: ms(*now)}})
	s.Acknowledge()
	if f := s.BuildFrame(); f.St != "idle" {
		t.Fatalf("ack: %+v", f)
	}
}

// 使用者實際收到的回應（2026-10）
func TestRealUsagePayload(t *testing.T) {
	raw := json.RawMessage(`{"five_hour":{"utilization":67,"resets_at":"2026-10-17T11:10:00.603189+00:00","limit_dollars":null,"used_dollars":null,"remaining_dollars":null,"locked_reason":null},"seven_day":{"utilization":12,"resets_at":"2026-10-17T09:00:00.603209+00:00","limit_dollars":null,"used_dollars":null,"remaining_dollars":null,"locked_reason":null},"seven_day_oauth_apps":null,"seven_day_opus":null,"seven_day_sonnet":null,"seven_day_cowork":null,"seven_day_omelette":null,"tangelo":null,"iguana_necktie":null,"omelette_promotional":null,"extra_usage":{"is_enabled":false,"monthly_limit":null,"used_credits":null,"utilization":null,"currency":null},"some_flag":true,"some_text":"x","some_num":3}`)
	q, ok := parseClaudeUsage(raw)
	if !ok || q.P5 != 67 || q.PW != 12 || q.R5 != time.Date(2026, 10, 17, 11, 10, 0, 0, time.UTC).Unix() {
		t.Fatalf("real payload: ok=%v %+v", ok, q)
	}
}

func TestHistory(t *testing.T) {
	s, now := newTestState()
	for i := 0; i <= 30; i++ { // 每 6 分鐘一筆，共 3 小時，從 10% 漲到 70%
		s.SetQuota("claude", Quota{P5: 10 + float64(i)*2, PW: 5})
		*now = now.Add(6 * time.Minute)
	}
	f := s.BuildFrame()
	if len(f.H) < 10 || f.H[len(f.H)-1] != 70 || f.H[0] > f.H[len(f.H)-1] {
		t.Fatalf("history: %v", f.H)
	}
}
