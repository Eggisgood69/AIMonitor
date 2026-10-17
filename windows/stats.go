package main

// 今日統計：每個 AI 今天生成了幾次、總共生成多久（不含等你按「允許」的時間）。
// 依當地日期分天，保留最近 7 天，存成 stats.json。

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

type DayStat struct {
	Count map[string]int   `json:"count"`
	Secs  map[string]int64 `json:"secs"`
}

type Stats struct {
	mu   sync.Mutex
	Days map[string]*DayStat `json:"days"` // "2026-10-17" → 當天統計
	path string
	now  func() time.Time
}

var statsAIs = []string{"claude", "chatgpt", "gemini"}

func NewStats(path string) *Stats {
	st := &Stats{Days: map[string]*DayStat{}, path: path, now: time.Now}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			json.Unmarshal(b, st)
			if st.Days == nil {
				st.Days = map[string]*DayStat{}
			}
		}
	}
	return st
}

func dayKey(t time.Time) string { return t.Format("2006-01-02") }

// 完成一次生成
func (st *Stats) Add(ai string, dur time.Duration) {
	if ai == "" || ai == "none" {
		return
	}
	st.mu.Lock()
	k := dayKey(st.now())
	d := st.Days[k]
	if d == nil {
		d = &DayStat{Count: map[string]int{}, Secs: map[string]int64{}}
		st.Days[k] = d
	}
	d.Count[ai]++
	d.Secs[ai] += int64(dur.Round(time.Second) / time.Second)
	// 只留最近 7 天
	keys := make([]string, 0, len(st.Days))
	for kk := range st.Days {
		keys = append(keys, kk)
	}
	sort.Strings(keys)
	for len(keys) > 7 {
		delete(st.Days, keys[0])
		keys = keys[1:]
	}
	b, _ := json.Marshal(st)
	st.mu.Unlock()
	if st.path != "" {
		os.WriteFile(st.path, b, 0o644)
	}
}

// 今天的統計，給螢幕用：[["claude",12,1080], ...]（依時間多到少，沒用到的不列）
func (st *Stats) Today() [][]interface{} {
	st.mu.Lock()
	defer st.mu.Unlock()
	d := st.Days[dayKey(st.now())]
	out := [][]interface{}{}
	if d == nil {
		return out
	}
	for _, ai := range statsAIs {
		if d.Count[ai] > 0 {
			out = append(out, []interface{}{ai, d.Count[ai], d.Secs[ai]})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i][2].(int64) > out[j][2].(int64) })
	return out
}

// 最近 7 天（舊到新），給設定視窗畫長條圖
func (st *Stats) Week() []map[string]interface{} {
	st.mu.Lock()
	defer st.mu.Unlock()
	now := st.now()
	var out []map[string]interface{}
	for i := 6; i >= 0; i-- {
		t := now.AddDate(0, 0, -i)
		m := map[string]interface{}{"day": t.Format("01/02"), "wd": int(t.Weekday())}
		if d := st.Days[dayKey(t)]; d != nil {
			m["count"], m["secs"] = d.Count, d.Secs
		}
		out = append(out, m)
	}
	return out
}
