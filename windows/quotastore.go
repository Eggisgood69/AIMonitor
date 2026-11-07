package main

// 額度存檔（%LOCALAPPDATA%\AIMonitor\quota.json）：AIMonitor 重開後，ChatGPT / Gemini 的額度要等背景網頁開好才讀得到
// （ChatGPT 曾經晚了一分鐘），這段時間螢幕沒有額度。存下最後讀到的值，啟動時讀回，跟平常一樣只用 quotaFresh 內的。
// 只存數字與時間（P5 / PW / R5 / RW / T），不存帳號、email。

import (
	"encoding/json"
	"os"
	"time"
)

// 讀回存檔的額度（太舊的不要），之後每次 SetQuota 都寫回這個檔案
func (s *State) LoadQuotas(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quotaPath = path
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved map[string]Quota
	if json.Unmarshal(b, &saved) != nil {
		return
	}
	now := s.now()
	for ai, q := range saved {
		if q.T.IsZero() || now.Sub(q.T) >= quotaFresh || q.T.After(now.Add(time.Minute)) {
			continue
		}
		if _, ok := s.quota[ai]; !ok {
			c := q
			s.quota[ai] = &c
		}
	}
}

// 呼叫時要拿著 s.mu
func (s *State) saveQuotasLocked() {
	if s.quotaPath == "" {
		return
	}
	out := map[string]Quota{}
	for ai, q := range s.quota {
		out[ai] = *q
	}
	b, err := json.Marshal(out)
	if err != nil {
		return
	}
	tmp := s.quotaPath + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, s.quotaPath)
	}
}
