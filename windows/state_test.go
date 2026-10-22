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
	// 你正在看的是另一個 AI：主燈跟著你看的（Claude 生成中），Gemini 的完成放小圓點
	f := s.BuildFrame()
	if f.AI != "claude" || f.St != "working" || len(f.O) != 1 || f.O[0][0] != "gemini" || f.O[0][1] != "done" {
		t.Fatalf("focused other AI wins: %+v", f)
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
	fgWorking := Item{Key: "web:claude", AI: "claude", Focused: true, Gen: true, GenSince: 1} // 同一個 AI 才比優先度
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
		{"6>7 正在看的生成中 > 看過的完成", fgWorking, Item{Key: "web:gemini", AI: "gemini", Focused: true, FinishedAt: 1}, "claude", "working"},
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

// 看過 / 沒看過：熄燈時機
func TestSeenRules(t *testing.T) {
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
	// 你正在看的 App 對話完成：Claude Code 的 Stop 與 App 停止鈕消失的時間對得上 → 看過（8 秒後熄）
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
	*now = now.Add(5 * time.Second)
	s.ApplyScan([]Item{app, cc1, cc2, gem})
	// 主燈：背景錯誤；Gemini 背景完成是小圓點；看過的 App 對話已經熄了（不在小圓點裡）
	f := s.BuildFrame()
	if f.St != "error" || f.Msg != "RATE LIMIT" || len(f.O) != 1 || f.O[0][0] != "gemini" || f.O[0][1] != "done" {
		t.Fatalf("frame: %+v", f)
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
}

// 實測情境（14:46:49）：Claude App 不在前景時，App 裡的對話完成。
// Claude Code（hooks）和 App（畫面偵測）是同一件事：只亮一個綠燈；之後切回 App 看，兩個一起熄
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
}

// 實測情境（15:48）：空氣清淨機的對話在背景完成，之後你在別的對話來回聊。
// 完成後 1 分鐘內綠燈恆亮；之後排到生成中後面（看得到呼吸燈），沒有 AI 在跑時又回到綠燈恆亮
func TestBackgroundDoneDemotes(t *testing.T) {
	s, now := newTestState()
	s.ApplyScan(nil)
	*now = now.Add(time.Minute)
	bg := Item{Key: "cc:43e85d0e", AI: "claude", Gen: true, GenSince: ms(*now)}
	s.ApplyScan([]Item{bg})
	*now = now.Add(time.Second)
	bg = Item{Key: "cc:43e85d0e", AI: "claude", FinishedAt: ms(*now)}
	chat := Item{Key: "cc:7fc4fb19", AI: "claude", Gen: true, GenSince: ms(*now)}
	s.ApplyScan([]Item{bg, chat})
	if f := s.BuildFrame(); f.St != "done" {
		t.Fatalf("just finished in background: %+v", f)
	}
	*now = now.Add(doneOnTop + time.Second)
	s.ApplyScan([]Item{bg, chat})
	f := s.BuildFrame()
	if f.St != "working" || len(f.O) != 1 || f.O[0][1] != "done" {
		t.Fatalf("after 1 min, chatting shows breathing, done stays as a dot: %+v", f)
	}
	// 聊完了（沒有 AI 在跑）：沒看過的完成又回到主燈
	*now = now.Add(time.Second)
	chat = Item{Key: "cc:7fc4fb19", AI: "claude", Focused: true, FinishedAt: ms(*now)}
	s.ApplyScan([]Item{bg, chat})
	*now = now.Add(9 * time.Second)
	if f := s.BuildFrame(); f.St != "done" || f.Age < 60 {
		t.Fatalf("idle again → unseen done back on top: %+v", f)
	}
	// 錯誤不降級
	s.ApplyScan([]Item{{Key: "cc:43e85d0e", AI: "claude", FinishedAt: bg.FinishedAt, Failed: true}, chat})
	*now = now.Add(10 * time.Minute)
	chat = Item{Key: "cc:7fc4fb19", AI: "claude", Gen: true, GenSince: ms(*now)}
	s.ApplyScan([]Item{{Key: "cc:43e85d0e", AI: "claude", FinishedAt: bg.FinishedAt, Failed: true}, chat})
	if f := s.BuildFrame(); f.St != "error" {
		t.Fatalf("error should stay on top: %+v", f)
	}
}

// 小圓點：每個對話一個點（同 AI 同狀態不合併）；Claude App 畫面上那個與 Claude Code 重複時只算一次
func TestOtherDotsPerConversation(t *testing.T) {
	s, now := newTestState()
	s.ApplyScan([]Item{
		{Key: "app:claude", AI: "claude", Focused: true, Gen: true, GenSince: ms(*now)},
		{Key: "cc:aaaa1111", AI: "claude", Gen: true, GenSince: ms(*now) - 3000},
		{Key: "cc:bbbb2222", AI: "claude", Gen: true, GenSince: ms(*now) - 2000},
		{Key: "cc:cccc3333", AI: "claude", Gen: true, GenSince: ms(*now) - 1000},
		{Key: "web:gemini", AI: "gemini", Gen: true, GenSince: ms(*now)},
	})
	f := s.BuildFrame()
	// 主燈 1 個 + 小圓點：另外 2 個 Claude Code、Gemini（App 那項和 Claude Code 重複，不畫）
	if f.St != "working" || len(f.O) != 3 {
		t.Fatalf("one dot per conversation: %+v", f)
	}
	// 最多 5 個點
	var many []Item
	for i := 0; i < 8; i++ {
		many = append(many, Item{Key: "cc:" + string(rune('a'+i)) + "0000000", AI: "claude", Gen: true, GenSince: ms(*now)})
	}
	s.ApplyScan(many)
	if f := s.BuildFrame(); len(f.O) != 5 {
		t.Fatalf("at most 5 dots: %+v", f)
	}
}

// 實測情境（16:52）：Claude 有沒看過的完成，你從 Claude App 切到 Gemini 網頁 → 主燈跟著 Gemini；
// 需審核、錯誤例外
func TestSwitchToAnotherAI(t *testing.T) {
	s, now := newTestState()
	s.ApplyScan(nil)
	*now = now.Add(time.Minute)
	cc := Item{Key: "cc:46b9e869", AI: "claude", Gen: true, GenSince: ms(*now)}
	gem := Item{Key: "web:gemini", AI: "gemini"}
	s.ApplyScan([]Item{cc, gem})
	*now = now.Add(time.Second)
	cc = Item{Key: "cc:46b9e869", AI: "claude", FinishedAt: ms(*now)}
	s.ApplyScan([]Item{cc, gem})
	if f := s.BuildFrame(); f.AI != "claude" || f.St != "done" {
		t.Fatalf("not looking at Gemini yet: %+v", f)
	}
	gem.Focused = true
	s.ApplyScan([]Item{cc, gem})
	f := s.BuildFrame()
	if f.AI != "gemini" || f.St != "idle" || len(f.O) != 1 || f.O[0][0] != "claude" || f.O[0][1] != "done" {
		t.Fatalf("switched to Gemini → follow Gemini, Claude done as a dot: %+v", f)
	}
	// Claude 出錯：不管看哪裡都顯示
	cc.Failed = true
	s.ApplyScan([]Item{cc, gem})
	if f := s.BuildFrame(); f.AI != "claude" || f.St != "error" {
		t.Fatalf("error stays on top: %+v", f)
	}
	// Claude 需要允許：也一樣
	rv := Item{Key: "cc:46b9e869", AI: "claude", Gen: true, Review: true, ReviewSince: ms(*now)}
	s.ApplyScan([]Item{rv, gem})
	if f := s.BuildFrame(); f.AI != "claude" || f.St != "review" {
		t.Fatalf("review stays on top: %+v", f)
	}
}

// 實測情境（23:25:10）：只有一個對話，Claude Code 一完成，App 的畫面偵測還要 2-3 秒才跟上，
// 這段時間不能多畫一個「生成中」的點；真的另一個 App 對話在跑（超過 appLag）才畫
func TestNoDuplicateDotWhileAppLags(t *testing.T) {
	s, now := newTestState()
	s.ApplyScan(nil)
	*now = now.Add(time.Minute)
	app := Item{Key: "app:claude", AI: "claude", Focused: true, Gen: true, GenSince: ms(*now)}
	cc := Item{Key: "cc:0cccab18", AI: "claude", Gen: true, GenSince: ms(*now)}
	s.ApplyScan([]Item{app, cc})
	if f := s.BuildFrame(); len(f.O) != 0 {
		t.Fatalf("both working: one conversation, no dot: %+v", f)
	}
	*now = now.Add(20 * time.Second)
	cc = Item{Key: "cc:0cccab18", AI: "claude", FinishedAt: ms(*now)}
	s.ApplyScan([]Item{app, cc}) // App 還沒發現停止鈕消失
	if f := s.BuildFrame(); len(f.O) != 0 {
		t.Fatalf("app lagging behind Claude Code: no duplicate dot: %+v", f)
	}
	*now = now.Add(2 * time.Second)
	if f := s.BuildFrame(); len(f.O) != 0 {
		t.Fatalf("still within appLag: %+v", f)
	}
	// 過了 appLag，App 還在生成中 = 真的是另一個（一般聊天）對話：畫點
	*now = now.Add(appLag)
	s.ApplyScan([]Item{app, cc})
	if f := s.BuildFrame(); len(f.O) != 1 || f.O[0][1] != "working" {
		t.Fatalf("a real second conversation after appLag: %+v", f)
	}
}

// Claude App 裡切到別的對話：原本的對話改由側邊欄「執行中」回報（app:claude:bg:<標題>）
func TestClaudeAppBackgroundChat(t *testing.T) {
	s, now := newTestState()
	s.ApplyScan(nil)
	*now = now.Add(time.Minute)
	app := Item{Key: "app:claude", AI: "claude", Focused: true}
	bg := Item{Key: "app:claude:bg:Chat A", AI: "claude", Gen: true, GenSince: ms(*now)}
	s.ApplyScan([]Item{app, bg})
	if f := s.BuildFrame(); f.St != "working" || len(f.O) != 0 {
		t.Fatalf("background chat running: main light working: %+v", f)
	}
	*now = now.Add(30 * time.Second)
	bg = Item{Key: "app:claude:bg:Chat A", AI: "claude", FinishedAt: ms(*now)}
	s.ApplyScan([]Item{app, bg})
	if f := s.BuildFrame(); f.St != "done" {
		t.Fatalf("background chat finished: done until viewed: %+v", f)
	}
	if d := s.TakeFinished(); len(d) != 1 || d[0].AI != "claude" {
		t.Fatalf("counted once in stats: %+v", d)
	}
	*now = now.Add(5 * time.Minute)
	s.ApplyScan([]Item{app, bg})
	if f := s.BuildFrame(); f.St != "done" {
		t.Fatalf("still unseen: %+v", f)
	}
	// 點開它：亮 8 秒後熄
	bg.Focused = true
	s.ApplyScan([]Item{app, bg})
	*now = now.Add(doneShowWhenViewed + time.Second)
	if f := s.BuildFrame(); f.St != "idle" {
		t.Fatalf("viewed: off: %+v", f)
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
