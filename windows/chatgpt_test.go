package main

import (
	"testing"
	"time"
)

func TestParseChatGPTUsage(t *testing.T) {
	now := time.Unix(1792220000, 0)
	cases := []struct {
		name       string
		raw        string
		ok         bool
		p5, pw     float64
		r5, rw     int64
		len5, lenw int64
		plan       string
	}{
		{
			// 付費方案（模擬，等實際付費帳號驗證）：5 小時 + 每週
			name: "plus 5h + weekly",
			raw: `{"user_id":"…","plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false,
				"primary_window":{"used_percent":3,"limit_window_seconds":18000,"reset_after_seconds":9000,"reset_at":1792229000},
				"secondary_window":{"used_percent":12,"limit_window_seconds":604800,"reset_after_seconds":300000,"reset_at":1792520000}}}`,
			ok: true, p5: 3, pw: 12, r5: 1792229000, rw: 1792520000, len5: 18000, lenw: 604800, plan: "plus",
		},
		{
			// 順序顛倒也要對：看時窗長度，不看 primary / secondary
			name: "weekly first",
			raw: `{"plan_type":"pro","rate_limit":{
				"primary_window":{"used_percent":40,"limit_window_seconds":604800,"reset_at":1792520000},
				"secondary_window":{"used_percent":7,"limit_window_seconds":18000,"reset_at":1792229000}}}`,
			ok: true, p5: 7, pw: 40, r5: 1792229000, rw: 1792520000, len5: 18000, lenw: 604800, plan: "pro",
		},
		{
			// 免費方案（實際回應，2026-10-18）：只有一個 30 天時窗 → 放在長時窗，沒有 5 小時
			name: "free 30 days",
			raw: `{"user_id":"…","account_id":"…","email":"…","plan_type":"free","rate_limit":{"allowed":true,"limit_reached":false,
				"primary_window":{"used_percent":0,"limit_window_seconds":2592000,"reset_after_seconds":2592000,"reset_at":1794907216},
				"secondary_window":null}}`,
			ok: true, p5: -1, pw: 0, rw: 1794907216, lenw: 2592000, plan: "free",
		},
		{
			// 只有 reset_after_seconds（沒有 reset_at）：從現在起算
			name: "relative reset",
			raw:  `{"rate_limit":{"primary_window":{"used_percent":55,"limit_window_seconds":18000,"reset_after_seconds":600}}}`,
			ok:   true, p5: 55, pw: -1, r5: 1792220600, len5: 18000,
		},
		{
			// 舊格式沒有時窗長度：照順序，primary = 5 小時、secondary = 每週
			name: "no window length",
			raw:  `{"rate_limit":{"primary_window":{"used_percent":20},"secondary_window":{"used_percent":5}}}`,
			ok:   true, p5: 20, pw: 5,
		},
		{name: "no rate_limit", raw: `{"plan_type":"free"}`},
		{name: "no windows", raw: `{"rate_limit":{"allowed":true,"primary_window":null,"secondary_window":null}}`},
		{name: "not json", raw: `<html>`},
	}
	for _, c := range cases {
		u, ok := parseChatGPTUsage([]byte(c.raw), now)
		if ok != c.ok {
			t.Errorf("%s: ok=%v", c.name, ok)
			continue
		}
		if !ok {
			continue
		}
		if u.P5 != c.p5 || u.PW != c.pw || u.R5 != c.r5 || u.RW != c.rw || u.Len5 != c.len5 || u.LenW != c.lenw || u.Plan != c.plan {
			t.Errorf("%s: got %+v", c.name, u)
		}
	}
}
