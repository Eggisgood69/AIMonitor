package main

import (
	"testing"
	"time"
)

func TestResetWatcher(t *testing.T) {
	now := time.Date(2026, 10, 17, 18, 0, 0, 0, time.UTC)
	r := NewResetWatcher()
	reset := now.Add(2 * time.Hour).Unix()
	r.Observe("claude", Quota{P5: 50, PW: 20, R5: reset, RW: reset + 86400}, 90, now)
	if len(r.Tick(now.Add(3*time.Hour))) != 0 {
		t.Fatal("below threshold should not remind")
	}
	r.Observe("claude", Quota{P5: 95, PW: 20, R5: reset, RW: reset + 86400}, 90, now)
	r.Observe("claude", Quota{P5: 100, PW: 20, R5: reset + 3, RW: reset + 86400}, 90, now.Add(time.Minute)) // 同一時窗
	if ev := r.Tick(now.Add(time.Hour)); len(ev) != 0 {
		t.Fatalf("too early: %v", ev)
	}
	ev := r.Tick(time.Unix(reset, 0).Add(-4 * time.Minute))
	if len(ev) != 1 || ev[0].Kind != "soon" || ev[0].Win != "5" {
		t.Fatalf("soon: %v", ev)
	}
	if ev := r.Tick(time.Unix(reset, 0).Add(-2 * time.Minute)); len(ev) != 0 {
		t.Fatalf("soon twice: %v", ev)
	}
	ev = r.Tick(time.Unix(reset+3, 0))
	if len(ev) != 1 || ev[0].Kind != "reset" {
		t.Fatalf("reset: %v", ev)
	}
	if ev := r.Tick(time.Unix(reset, 0).Add(time.Hour)); len(ev) != 0 {
		t.Fatalf("after reset: %v", ev)
	}
	// 門檻 0 = 關閉
	r.Observe("gemini", Quota{P5: 100, R5: reset}, 0, now)
	if len(r.Tick(time.Unix(reset, 0))) != 0 {
		t.Fatal("disabled")
	}
	// 快重置時才達到門檻：不再補發「即將重置」，只在重置時提醒
	r.Observe("gemini", Quota{P5: 100, R5: reset}, 90, time.Unix(reset, 0).Add(-time.Minute))
	ev = r.Tick(time.Unix(reset, 0).Add(-30 * time.Second))
	if len(ev) != 0 {
		t.Fatalf("late soon: %v", ev)
	}
	ev = r.Tick(time.Unix(reset, 0))
	if len(ev) != 1 || ev[0].AI != "gemini" || ev[0].Kind != "reset" {
		t.Fatalf("gemini reset: %v", ev)
	}
}

// 用了重置券：時窗提早換掉、新時窗用量低。舊時窗的「再 5 分鐘重置」不能照舊時間跳出來
func TestResetWatcherVoucherDropsOldWindow(t *testing.T) {
	r := NewResetWatcher()
	t0 := time.Date(2026, 11, 15, 20, 0, 0, 0, time.UTC)
	oldReset := t0.Add(2 * time.Hour).Unix() // 22:00
	r.Observe("claude", Quota{P5: 95, R5: oldReset, PW: 30, RW: t0.Add(100 * time.Hour).Unix()}, 90, t0)
	// 21:30 用了重置券：新時窗 02:00 重置、用量 10%
	t1 := t0.Add(90 * time.Minute)
	r.Observe("claude", Quota{P5: 10, R5: t0.Add(6 * time.Hour).Unix(), PW: 2, RW: t0.Add(100 * time.Hour).Unix()}, 90, t1)
	for _, at := range []time.Duration{115, 120, 121} { // 21:55、22:00、22:01
		if ev := r.Tick(t0.Add(at * time.Minute)); len(ev) != 0 {
			t.Fatalf("舊時窗的提醒還在：%+v", ev)
		}
	}
	// 沒讀到資料（reset = 0）時不要清掉
	r2 := NewResetWatcher()
	r2.Observe("claude", Quota{P5: 95, R5: oldReset}, 90, t0)
	r2.Observe("claude", Quota{}, 90, t1)
	if ev := r2.Tick(t0.Add(115 * time.Minute)); len(ev) != 1 || ev[0].Kind != "soon" {
		t.Fatalf("沒資料時提醒不該消失：%+v", ev)
	}
}
