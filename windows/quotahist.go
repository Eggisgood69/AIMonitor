package main

// 額度歷史（%LOCALAPPDATA%\AIMonitor\quotahist.json）：統計分頁畫額度用量曲線用。
// 每次讀到額度（Claude 每 2 分鐘、ChatGPT / Gemini 背景網頁）就看要不要記一點：數字變了才記，
// 沒變的話 10 分鐘記一點（線才連得起來、看得出那段時間沒在用）。保留 7 天，只存時間與兩個百分比

import (
	"encoding/json"
	"math"
	"os"
	"sync"
	"time"
)

const (
	quotaHistKeep  = 7 * 24 * time.Hour
	quotaHistEvery = 10 * time.Minute // 數字沒變時多久記一點
	quotaHistSave  = time.Minute      // 最多多久寫一次檔
)

// 一個點：時間（Unix 秒）、5 小時（或該家的短週期）與每週（長週期）用量 %，-1 = 沒資料
type QSample struct {
	T  int64   `json:"t"`
	P5 float64 `json:"p"`
	PW float64 `json:"w"`
}

type QuotaHist struct {
	mu       sync.Mutex
	path     string
	m        map[string][]QSample
	lastSave time.Time
	now      func() time.Time
}

func NewQuotaHist(path string) *QuotaHist { return newQuotaHistAt(path, time.Now) }

func newQuotaHistAt(path string, now func() time.Time) *QuotaHist {
	h := &QuotaHist{path: path, m: map[string][]QSample{}, now: now}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &h.m)
	}
	h.mu.Lock()
	h.pruneLocked()
	h.mu.Unlock()
	return h
}

func histRound(v float64) float64 {
	if v < 0 {
		return -1
	}
	return math.Round(v*10) / 10
}

func (h *QuotaHist) pruneLocked() {
	cut := h.now().Add(-quotaHistKeep).Unix()
	for ai, s := range h.m {
		i := 0
		for i < len(s) && s[i].T < cut {
			i++
		}
		if i == len(s) {
			delete(h.m, ai)
		} else if i > 0 {
			h.m[ai] = append([]QSample(nil), s[i:]...)
		}
	}
}

// 讀到一次額度。兩個都沒資料就不記
func (h *QuotaHist) Add(ai string, p5, pw float64) {
	if h == nil || (p5 < 0 && pw < 0) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	smp := QSample{now.Unix(), histRound(p5), histRound(pw)}
	s := h.m[ai]
	if n := len(s); n > 0 {
		last := s[n-1]
		if smp.T < last.T {
			return // 時鐘往回跳：不記
		}
		if last.P5 == smp.P5 && last.PW == smp.PW && time.Duration(smp.T-last.T)*time.Second < quotaHistEvery {
			return
		}
	}
	h.m[ai] = append(s, smp)
	h.pruneLocked()
	if now.Sub(h.lastSave) >= quotaHistSave {
		h.saveLocked()
	}
}

func (h *QuotaHist) saveLocked() {
	if h.path == "" {
		return
	}
	b, err := json.Marshal(h.m)
	if err != nil {
		return
	}
	tmp := h.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, h.path)
		h.lastSave = h.now()
	}
}

// 結束程式前存一次（Add 最多每分鐘寫一次檔）
func (h *QuotaHist) Save() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.saveLocked()
}

// since 之後的點（複製一份）
func (h *QuotaHist) Get(ai string, since time.Time) []QSample {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	cut := since.Unix()
	out := []QSample{}
	for _, s := range h.m[ai] {
		if s.T >= cut {
			out = append(out, s)
		}
	}
	return out
}
