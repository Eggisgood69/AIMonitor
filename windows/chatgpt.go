package main

// ChatGPT 方案用量（/backend-api/wham/usage，與 Codex 共用的額度時窗）。
// 不依賴 Windows，可以用模擬的回應做單元測試。
//
// 回應格式（2026-10，非公開 API）：
//
//	{"plan_type":"plus","rate_limit":{"allowed":true,
//	  "primary_window":  {"used_percent":3, "limit_window_seconds":18000,  "reset_after_seconds":9000, "reset_at":1792240000},
//	  "secondary_window":{"used_percent":12,"limit_window_seconds":604800, "reset_after_seconds":...,  "reset_at":...}}}
//
// 付費方案通常是 5 小時 + 每週兩個時窗；免費方案目前只有一個 30 天的時窗。
// 不靠 primary / secondary 的順序判斷，而是看時窗長度：短於一天的是「5 小時」那格，最長的是「每週 / 每月」那格。

import (
	"encoding/json"
	"time"
)

type chatgptWindow struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds float64  `json:"limit_window_seconds"`
	ResetAfterSeconds  *float64 `json:"reset_after_seconds"`
	ResetAt            float64  `json:"reset_at"`
}

type chatgptUsage struct {
	Quota
	Plan       string
	Len5, LenW int64 // 時窗長度（秒），0 = 不知道
}

func (w *chatgptWindow) reset(now time.Time) int64 {
	if w.ResetAt > 0 {
		return int64(w.ResetAt)
	}
	if w.ResetAfterSeconds != nil {
		return now.Unix() + int64(*w.ResetAfterSeconds)
	}
	return 0
}

// 解析 wham/usage 的回應。ok=false：格式不對或沒有任何時窗
func parseChatGPTUsage(raw []byte, now time.Time) (u chatgptUsage, ok bool) {
	var d struct {
		PlanType  string `json:"plan_type"`
		RateLimit *struct {
			Primary   *chatgptWindow `json:"primary_window"`
			Secondary *chatgptWindow `json:"secondary_window"`
		} `json:"rate_limit"`
	}
	if json.Unmarshal(raw, &d) != nil || d.RateLimit == nil {
		return u, false
	}
	var ws []*chatgptWindow
	for _, w := range []*chatgptWindow{d.RateLimit.Primary, d.RateLimit.Secondary} {
		if w != nil && w.UsedPercent != nil {
			ws = append(ws, w)
		}
	}
	if len(ws) == 0 {
		return u, false
	}
	u = chatgptUsage{Quota: Quota{P5: -1, PW: -1}, Plan: d.PlanType}
	var short, long *chatgptWindow
	for _, w := range ws {
		switch {
		case w.LimitWindowSeconds > 0 && w.LimitWindowSeconds < 86400:
			if short == nil || w.LimitWindowSeconds < short.LimitWindowSeconds {
				short = w
			}
		case w.LimitWindowSeconds >= 86400:
			if long == nil || w.LimitWindowSeconds > long.LimitWindowSeconds {
				long = w
			}
		}
	}
	// 沒有時窗長度（舊格式）：照順序，primary = 5 小時、secondary = 每週
	if short == nil && long == nil {
		short = d.RateLimit.Primary
		if d.RateLimit.Secondary != nil && d.RateLimit.Secondary.UsedPercent != nil {
			long = d.RateLimit.Secondary
		}
		if short != nil && short.UsedPercent == nil {
			short = nil
		}
	}
	if short != nil {
		u.P5, u.R5, u.Len5 = *short.UsedPercent, short.reset(now), int64(short.LimitWindowSeconds)
	}
	if long != nil {
		u.PW, u.RW, u.LenW = *long.UsedPercent, long.reset(now), int64(long.LimitWindowSeconds)
	}
	return u, true
}
