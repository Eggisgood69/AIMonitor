package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStatsFromScans(t *testing.T) {
	s, now := newTestState()
	step := func(it Item) { *now = now.Add(time.Second); s.ApplyScan([]Item{it}) }
	step(Item{Key: "app:claude", AI: "claude"})
	for i := 0; i < 10; i++ { // 生成 10 秒
		step(Item{Key: "app:claude", AI: "claude", Gen: true})
	}
	for i := 0; i < 5; i++ { // 等你按允許 5 秒（不算）
		step(Item{Key: "app:claude", AI: "claude", Gen: true, Review: true})
	}
	for i := 0; i < 3; i++ {
		step(Item{Key: "app:claude", AI: "claude", Gen: true})
	}
	step(Item{Key: "app:claude", AI: "claude", FinishedAt: ms(*now)})
	fin := s.TakeFinished()
	if len(fin) != 1 || fin[0].AI != "claude" || fin[0].Dur != 13*time.Second {
		t.Fatalf("finish: %+v", fin)
	}

	path := filepath.Join(t.TempDir(), "stats.json")
	st := NewStats(path)
	day := time.Date(2026, 10, 17, 23, 0, 0, 0, time.Local)
	st.now = func() time.Time { return day }
	st.Add("claude", 13*time.Second)
	st.Add("claude", 60*time.Second)
	st.Add("gemini", 90*time.Second)
	td := st.Today()
	if len(td) != 2 || td[0][0] != "gemini" || td[1][1] != 2 || td[1][2] != int64(73) {
		t.Fatalf("today: %v", td)
	}
	// 重新載入
	st2 := NewStats(path)
	st2.now = st.now
	if len(st2.Today()) != 2 {
		t.Fatalf("reload: %v", st2.Today())
	}
	// 隔天歸零，舊資料保留 7 天
	day = day.Add(2 * time.Hour)
	st2.now = func() time.Time { return day }
	if len(st2.Today()) != 0 {
		t.Fatalf("new day: %v", st2.Today())
	}
	for i := 0; i < 9; i++ {
		day = day.AddDate(0, 0, 1)
		st2.Add("chatgpt", time.Second)
	}
	if len(st2.Days) != 7 {
		t.Fatalf("keep 7: %d", len(st2.Days))
	}
	if w := st2.Week(); len(w) != 7 || w[6]["count"] == nil {
		t.Fatalf("week: %v", w)
	}
}
