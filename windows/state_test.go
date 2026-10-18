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
		{Key: "web:gemini", AI: "gemini", FinishedAt: ms(*now)},
	})
	*now = now.Add(time.Second)
	s.ApplyScan([]Item{
		{Key: "app:claude", AI: "claude", Focused: true, Gen: true, GenSince: ms(*now)},
		{Key: "web:gemini", AI: "gemini", FinishedAt: ms(*now)},
	})
	// 背景完成（沒看過）比正在看的生成中優先
	f := s.BuildFrame()
	if f.AI != "gemini" || f.St != "done" || len(f.O) != 1 || f.O[0][0] != "claude" || f.O[0][1] != "working" {
		t.Fatalf("background done over focused working: %+v", f)
	}
	// 需審核比正在看的優先（在 Claude App 裡看著 Claude Code 等你允許）
	s.ApplyScan([]Item{
		{Key: "app:claude", AI: "claude", Focused: true, Gen: true, GenSince: ms(*now)},
		{Key: "cc:abcdef12", AI: "claude", Gen: true, Review: true, ReviewSince: ms(*now)},
	})
	if f := s.BuildFrame(); f.St != "review" || len(f.O) != 1 || f.O[0][1] != "working" {
		t.Fatalf("review over focus: %+v", f)
	}
	// 正在看的待機，另一個對話還在跑：顯示在跑的那個
	s.ApplyScan([]Item{
		{Key: "app:claude", AI: "claude", Focused: true},
		{Key: "cc:43e85d0e", AI: "claude", Gen: true, GenSince: ms(*now)},
	})
	if f := s.BuildFrame(); f.St != "working" {
		t.Fatalf("idle focus should not hide working: %+v", f)
	}
	// 這個對話剛完成（App 畫面停在這裡、Claude Code 也回報完成），另一個對話還在跑：顯示生成中，完成放在小圓點
	s.ApplyScan([]Item{
		{Key: "app:claude", AI: "claude", Focused: true, Gen: true, GenSince: ms(*now)},
		{Key: "cc:46b9e869", AI: "claude", Gen: true, GenSince: ms(*now)},
		{Key: "cc:43e85d0e", AI: "claude", Gen: true, GenSince: ms(*now)},
	})
	*now = now.Add(time.Second)
	s.ApplyScan([]Item{
		{Key: "app:claude", AI: "claude", Focused: true, FinishedAt: ms(*now)},
		{Key: "cc:46b9e869", AI: "claude", FinishedAt: ms(*now)},
		{Key: "cc:43e85d0e", AI: "claude", Gen: true, GenSince: ms(*now)},
	})
	if f := s.BuildFrame(); f.St != "working" || len(f.O) == 0 || f.O[0][1] != "done" {
		t.Fatalf("working over done: %+v", f)
	}
	// 另一個也跑完了：才輪到完成
	*now = now.Add(time.Second)
	s.ApplyScan([]Item{
		{Key: "app:claude", AI: "claude", Focused: true, FinishedAt: ms(now.Add(-time.Second))},
		{Key: "cc:46b9e869", AI: "claude", FinishedAt: ms(now.Add(-time.Second))},
		{Key: "cc:43e85d0e", AI: "claude", FinishedAt: ms(*now)},
	})
	if f := s.BuildFrame(); f.St != "done" {
		t.Fatalf("all finished → done: %+v", f)
	}
	// 全部待機：還是顯示正在看的那個
	s.ApplyScan([]Item{
		{Key: "app:chatgpt", AI: "chatgpt", Focused: true},
		{Key: "web:gemini", AI: "gemini"},
	})
	if f := s.BuildFrame(); f.AI != "chatgpt" || f.St != "idle" {
		t.Fatalf("all idle → focused: %+v", f)
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

// 規則表逐列比較：上一列一定贏過下一列
//
//	1 背景需審核 > 2 正在看的需審核 > 3 背景錯誤 > 4 背景完成 > 5 背景生成中
//	> 6 正在看的生成中 > 7 看過的錯誤 / 完成（8 秒）> 8 待機
func TestRankTable(t *testing.T) {
	// 先跑一輪「生成中」，下一輪再給最後狀態（完成 / 錯誤要經過「剛完成」這一步）
	scan := func(items ...Item) Frame {
		s, now := newTestState()
		var before []Item
		for _, it := range items {
			b := it
			if it.FinishedAt != 0 {
				b.FinishedAt, b.Failed, b.Gen, b.GenSince = 0, false, true, ms(*now)
			}
			before = append(before, b)
		}
		s.ApplyScan(before)
		*now = now.Add(time.Second)
		for i := range items {
			if items[i].FinishedAt != 0 {
				items[i].FinishedAt = ms(*now)
			}
		}
		s.ApplyScan(items)
		return s.BuildFrame()
	}
	bgReview := Item{Key: "web:gemini", AI: "gemini", Gen: true, Review: true, ReviewSince: 1}
	fgReview := Item{Key: "web:chatgpt", AI: "chatgpt", Focused: true, Gen: true, Review: true, ReviewSince: 1}
	bgError := Item{Key: "cc:aaaa1111", AI: "claude", FinishedAt: 1, Failed: true, Msg: "RATE LIMIT"}
	bgDone := Item{Key: "web:gemini", AI: "gemini", FinishedAt: 1}
	bgWorking := Item{Key: "cc:bbbb2222", AI: "claude", Gen: true, GenSince: 1}
	fgWorking := Item{Key: "web:chatgpt", AI: "chatgpt", Focused: true, Gen: true, GenSince: 1}
	seenDone := Item{Key: "web:chatgpt", AI: "chatgpt", Focused: true, FinishedAt: 1}
	idle := Item{Key: "web:gemini", AI: "gemini"}

	cases := []struct {
		name     string
		win, los Item
		ai, st   string
	}{
		{"1>2 背景需審核 > 正在看的需審核", bgReview, fgReview, "gemini", "review"},
		{"2>3 正在看的需審核 > 背景錯誤", fgReview, bgError, "chatgpt", "review"},
		{"3>4 背景錯誤 > 背景完成", bgError, bgDone, "claude", "error"},
		{"4>5 背景完成 > 背景生成中", bgDone, bgWorking, "gemini", "done"},
		{"5>6 背景生成中 > 正在看的生成中", bgWorking, fgWorking, "claude", "working"},
		{"6>7 正在看的生成中 > 看過的完成", fgWorking, Item{Key: "web:gemini", AI: "gemini", Focused: true, FinishedAt: 1}, "chatgpt", "working"},
		{"7>8 看過的完成 > 待機", seenDone, idle, "chatgpt", "done"},
	}
	for _, c := range cases {
		f := scan(c.win, c.los)
		if f.AI != c.ai || f.St != c.st {
			t.Errorf("%s: got %s %s", c.name, f.AI, f.St)
		}
		f = scan(c.los, c.win) // 順序不影響結果
		if f.AI != c.ai || f.St != c.st {
			t.Errorf("%s（反向）: got %s %s", c.name, f.AI, f.St)
		}
	}
	if f := scan(bgError); f.Msg != "RATE LIMIT" {
		t.Errorf("error msg: %+v", f)
	}
}

// 看過 / 沒看過：熄燈時機與 Windows 通知
func TestSeenAndAlerts(t *testing.T) {
	s, now := newTestState()
	s.ApplyScan(nil) // 程式啟動
	*now = now.Add(time.Minute)
	run := []Item{
		{Key: "app:claude", AI: "claude", Focused: true, Gen: true, GenSince: ms(*now)},
		{Key: "cc:aaaa1111", AI: "claude", Gen: true, GenSince: ms(*now), Buttons: []string{"Claude Code · AIMonitor"}},
		{Key: "cc:bbbb2222", AI: "claude", Gen: true, GenSince: ms(*now), Buttons: []string{"Claude Code · 空氣清淨機"}},
		{Key: "web:gemini", AI: "gemini", Gen: true, GenSince: ms(*now)},
	}
	s.ApplyScan(run)
	// 你正在看的 App 對話完成：Claude Code 的 Stop 與 App 停止鈕消失的時間對得上 → 看過（不通知）
	*now = now.Add(10 * time.Second)
	fin := ms(*now)
	s.ApplyScan([]Item{
		{Key: "app:claude", AI: "claude", Focused: true, Gen: true, GenSince: 1},
		{Key: "cc:aaaa1111", AI: "claude", FinishedAt: fin, Buttons: run[1].Buttons},
		run[2], run[3],
	})
	*now = now.Add(2 * time.Second) // 偵測程式 1.5 秒後才確定 App 的停止鈕消失
	appFin := ms(*now)
	app := Item{Key: "app:claude", AI: "claude", Focused: true, FinishedAt: appFin}
	cc1 := Item{Key: "cc:aaaa1111", AI: "claude", FinishedAt: fin, Buttons: run[1].Buttons}
	s.ApplyScan([]Item{app, cc1, run[2], run[3]})
	// 另一個 Claude Code 對話（出錯）和 Gemini 分頁晚一點在背景結束 → 沒看過
	*now = now.Add(6 * time.Second)
	cc2 := Item{Key: "cc:bbbb2222", AI: "claude", FinishedAt: ms(*now), Failed: true, Msg: "RATE LIMIT", Buttons: run[2].Buttons}
	gem := Item{Key: "web:gemini", AI: "gemini", FinishedAt: ms(*now)}
	s.ApplyScan([]Item{app, cc1, cc2, gem})
	if a := s.TakeAlerts(); len(a) != 0 {
		t.Fatalf("alerts should wait %v: %+v", alertDoneDelay, a)
	}
	*now = now.Add(alertDoneDelay)
	s.ApplyScan([]Item{app, cc1, cc2, gem})
	a := s.TakeAlerts()
	kinds := map[string]string{}
	for _, x := range a {
		kinds[x.Title] = x.Kind
	}
	if len(a) != 2 || kinds["Claude Code · 空氣清淨機 出錯"] != "error" || kinds["Gemini 網頁 完成了"] != "done" {
		t.Fatalf("alerts: %+v", a)
	}
	// 主燈：背景錯誤；看過的 App 對話早就熄了
	if f := s.BuildFrame(); f.St != "error" || f.Msg != "RATE LIMIT" {
		t.Fatalf("frame: %+v", f)
	}
	// 通知只跳一次
	*now = now.Add(time.Minute)
	s.ApplyScan([]Item{app, cc1, cc2, gem})
	if a := s.TakeAlerts(); len(a) != 0 {
		t.Fatalf("alerts repeated: %+v", a)
	}
	// 沒看過的會一直亮；長按 BOOT 全部標為已讀
	*now = now.Add(30 * time.Minute)
	s.ApplyScan([]Item{app, cc1, cc2, gem})
	if f := s.BuildFrame(); f.St != "error" {
		t.Fatalf("unseen should stay: %+v", f)
	}
	s.Acknowledge()
	if f := s.BuildFrame(); f.St != "idle" {
		t.Fatalf("ack: %+v", f)
	}
	// 背景需審核：7 秒還沒處理才通知；Claude Code 的需審核在你用 Claude App 時不通知
	rv := Item{Key: "web:gemini", AI: "gemini", Gen: true, Review: true, ReviewSince: ms(*now)}
	ccrv := Item{Key: "cc:bbbb2222", AI: "claude", Gen: true, Review: true, ReviewSince: ms(*now), Buttons: run[2].Buttons}
	s.ApplyScan([]Item{app, ccrv, rv})
	*now = now.Add(alertReviewDelay)
	s.ApplyScan([]Item{app, ccrv, rv})
	if a := s.TakeAlerts(); len(a) != 1 || a[0].Kind != "review" || a[0].AI != "gemini" {
		t.Fatalf("review alert: %+v", a)
	}
}

// 實測情境（14:46:49）：Claude App 不在前景時，App 裡的對話完成。
// Claude Code（hooks）和 App（畫面偵測）是同一件事：只通知一次；之後切回 App 看，兩個一起熄
func TestClaudeAppPairing(t *testing.T) {
	s, now := newTestState()
	s.ApplyScan(nil)
	*now = now.Add(time.Minute)
	lbl := []string{"Claude Code · AIMonitor"}
	s.ApplyScan([]Item{
		{Key: "app:claude", AI: "claude", Gen: true, GenSince: ms(*now)},
		{Key: "cc:46b9e869", AI: "claude", Gen: true, GenSince: ms(*now), Buttons: lbl},
	})
	*now = now.Add(10 * time.Second)
	cc := Item{Key: "cc:46b9e869", AI: "claude", FinishedAt: ms(*now), Buttons: lbl}
	s.ApplyScan([]Item{{Key: "app:claude", AI: "claude", Gen: true, GenSince: 1}, cc})
	*now = now.Add(2 * time.Second)
	app := Item{Key: "app:claude", AI: "claude", FinishedAt: ms(*now)}
	s.ApplyScan([]Item{app, cc})
	*now = now.Add(10 * time.Second)
	s.ApplyScan([]Item{app, cc})
	a := s.TakeAlerts()
	if len(a) != 1 || a[0].Title != "Claude Code · AIMonitor 完成了" {
		t.Fatalf("want one alert from Claude Code: %+v", a)
	}
	f := s.BuildFrame()
	if f.St != "done" || len(f.O) != 0 {
		t.Fatalf("one green light, no duplicate dot: %+v", f)
	}
	// 30 秒後切回 Claude App：看過了，8 秒後一起熄
	*now = now.Add(30 * time.Second)
	app.Focused = true
	s.ApplyScan([]Item{app, cc})
	if f := s.BuildFrame(); f.St != "done" {
		t.Fatalf("just came back, still green: %+v", f)
	}
	*now = now.Add(9 * time.Second)
	s.ApplyScan([]Item{app, cc})
	if f := s.BuildFrame(); f.St != "idle" {
		t.Fatalf("viewed in app → off: %+v", f)
	}
	if a := s.TakeAlerts(); len(a) != 0 {
		t.Fatalf("no more alerts: %+v", a)
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
