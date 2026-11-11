package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestQuotaHist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quotahist.json")
	now := time.Unix(1792220000, 0)
	clock := func() time.Time { return now }
	h := newQuotaHistAt(path, clock)

	h.Add("claude", 10, 50)
	now = now.Add(2 * time.Minute)
	h.Add("claude", 10.04, 50) // 四捨五入後一樣、還沒 10 分鐘：不記
	now = now.Add(2 * time.Minute)
	h.Add("claude", 12, 50) // 變了：記
	now = now.Add(11 * time.Minute)
	h.Add("claude", 12, 50) // 沒變但超過 10 分鐘：記
	h.Add("claude", -1, -1) // 沒資料：不記
	got := h.Get("claude", time.Unix(0, 0))
	if len(got) != 3 || got[0].P5 != 10 || got[1].P5 != 12 || got[2].T != now.Unix() {
		t.Fatalf("samples = %+v", got)
	}
	if g := h.Get("claude", now.Add(-time.Minute)); len(g) != 1 {
		t.Fatalf("since filter = %+v", g)
	}

	// 存檔、讀回；超過 7 天的丟掉
	h.Save()
	now = now.Add(7*24*time.Hour - 5*time.Minute)
	h2 := newQuotaHistAt(path, clock)
	if g := h2.Get("claude", time.Unix(0, 0)); len(g) != 1 {
		t.Fatalf("after prune = %+v", g)
	}
	if h2.Get("gemini", time.Unix(0, 0)) == nil {
		t.Fatal("Get should return an empty slice, not nil (JSON [] for the UI)")
	}
}
