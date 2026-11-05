package main

import (
	"testing"
	"time"
)

func TestExtensionTabs(t *testing.T) {
	x := NewExtension()
	now := time.Date(2026, 11, 5, 20, 0, 0, 0, time.UTC)
	x.now = func() time.Time { return now }
	post := func(body string) {
		if err := x.Handle([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	post(`{"tabs":[{"id":7,"ai":"gemini","stop":true,"view":"https://gemini.google.com/app/a","active":false}]}`)
	it := x.Items()
	if len(it) != 1 || it[0].Key != "ext:gemini:7" || !it[0].Gen || it[0].Focused {
		t.Fatalf("background tab generating: %+v", it)
	}
	// 停止鈕消失不到 1.5 秒：還在生成
	now = now.Add(time.Second)
	post(`{"tabs":[{"id":7,"ai":"gemini","stop":false,"view":"https://gemini.google.com/app/a"}]}`)
	now = now.Add(time.Second)
	if it := x.Items(); !it[0].Gen {
		t.Fatalf("short gap: %+v", it)
	}
	now = now.Add(time.Second)
	if it := x.Items(); it[0].Gen || it[0].FinishedAt == 0 {
		t.Fatalf("should be done: %+v", it)
	}
	// 生成中換到別的對話：不算完成
	post(`{"tabs":[{"id":7,"ai":"gemini","stop":true,"view":"https://gemini.google.com/app/b"}]}`)
	fin := x.Items()[0].FinishedAt
	post(`{"tabs":[{"id":7,"ai":"gemini","stop":false,"view":"https://gemini.google.com/app/c"}]}`)
	now = now.Add(3 * time.Second)
	if it := x.Items(); it[0].Gen || it[0].FinishedAt != fin {
		t.Fatalf("switched conversation should not finish: %+v", it)
	}
	// 分頁關掉、不認得的 AI 不收
	post(`{"tabs":[{"id":9,"ai":"evil","stop":true}]}`)
	if it := x.Items(); len(it) != 0 {
		t.Fatalf("closed tab / unknown ai: %+v", it)
	}
	// 很久沒收到：瀏覽器關了
	post(`{"tabs":[{"id":3,"ai":"claude","stop":true,"active":true}]}`)
	now = now.Add(extStale + time.Second)
	if it := x.Items(); len(it) != 0 {
		t.Fatalf("stale: %+v", it)
	}
	if c, _, _ := x.Status(); c {
		t.Fatal("stale should not be connected")
	}
}

func TestExtRequestAllowed(t *testing.T) {
	if !extRequestAllowed("chrome-extension://abcdefgh", "application/json") {
		t.Fatal("extension should be allowed")
	}
	for _, o := range []string{"", "https://evil.example", "null"} {
		if extRequestAllowed(o, "application/json") {
			t.Fatalf("origin %q should be rejected", o)
		}
	}
	if extRequestAllowed("chrome-extension://abcdefgh", "text/plain") {
		t.Fatal("non-JSON should be rejected")
	}
}
