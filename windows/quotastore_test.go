package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuotaStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota.json")
	now := time.Date(2026, 11, 7, 22, 0, 0, 0, time.UTC)

	a := NewState()
	a.now = func() time.Time { return now }
	a.LoadQuotas(path) // 沒有檔案：不出錯
	a.SetQuota("chatgpt", Quota{P5: 6, PW: 16, R5: 1, RW: 2, WLabel: "本月（30 天）"})
	a.SetQuota("gemini", Quota{P5: 3, PW: 9, SLabel: "今天", WLabel: "本週"})

	b, _ := os.ReadFile(path)
	if strings.Contains(strings.ToLower(string(b)), "email") {
		t.Fatalf("存檔不能有帳號資料：%s", b)
	}

	// 重開：5 分鐘後讀回來還有
	c := NewState()
	c.now = func() time.Time { return now.Add(5 * time.Minute) }
	c.LoadQuotas(path)
	q := c.GetQuota("chatgpt")
	if q == nil || q.P5 != 6 || q.PW != 16 || !q.T.Equal(now) {
		t.Fatalf("讀回的額度不對：%+v", q)
	}
	// 週期標籤也要讀回（重開後還沒讀到之前，設定視窗才不會把 Gemini「今天」寫成「5 小時」）
	if q.WLabel != "本月（30 天）" {
		t.Fatalf("ChatGPT 的週期標籤沒讀回：%+v", q)
	}
	if g := c.GetQuota("gemini"); g == nil || g.SLabel != "今天" || g.WLabel != "本週" {
		t.Fatalf("Gemini 的週期標籤沒讀回：%+v", g)
	}

	// 太舊（超過 quotaFresh）不要
	d := NewState()
	d.now = func() time.Time { return now.Add(quotaFresh + time.Minute) }
	d.LoadQuotas(path)
	if d.GetQuota("chatgpt") != nil {
		t.Fatal("超過 quotaFresh 的額度不應該讀回")
	}
}
