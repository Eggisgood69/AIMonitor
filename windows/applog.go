package main

// Claude App 自己的紀錄檔（%LOCALAPPDATA%\Claude\logs\main.log）：App 跳出的權限詢問會寫在這裡。
// 有些詢問不經過 Claude Code 的權限系統（例如內建瀏覽器的「允許 Claude 在以下位置執行 JavaScript？」），
// hooks 不會送任何事件，畫面上的「允許一次」也只有正在看那個對話時才讀得到；切到別的對話就沒人知道它在等你。
// 紀錄檔每一次詢問都有一對（2026-11 實測，App 4.23118）：
//
//	2026-11-07 21:25:06 [info] Emitted tool permission request <id> for browser:javascript_tool in session local_<內部 id>
//	2026-11-07 21:26:07 [info] Received permission response for <id>: deny (tool: browser:javascript_tool)
//	2026-11-02 21:00:08 [info] Mapping internal session local_<內部 id> to CLI session <Claude Code 的 session_id>
//
// 等你回答的期間，對應的 Claude Code 對話亮黃燈（ClaudeCode.Items 查 AppPerms.Waiting）。

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	alEmitRE = regexp.MustCompile(`^(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d) .*Emitted tool permission request (\S+) for (\S+) in session (local_[0-9a-f-]+)`)
	alRespRE = regexp.MustCompile(`Received permission response for ([^\s:]+)`)
	alMapRE  = regexp.MustCompile(`Mapping internal session (local_[0-9a-f-]+) to CLI session ([0-9a-f-]+)`)
)

// 一直沒有回答紀錄的詢問（例如 App 當掉）最多算多久
const alPermStale = 6 * time.Hour

type alPerm struct {
	local, tool string
	at          time.Time
}

type AppPerms struct {
	mu      sync.Mutex
	cli     map[string]string // local_<id> → Claude Code session_id
	pending map[string]alPerm // 詢問 id → 還沒回答的詢問
	path    string
	off     int64 // 讀到檔案的哪裡
	rest    []byte
	now     func() time.Time
}

func NewAppPerms(path string) *AppPerms {
	return &AppPerms{cli: map[string]string{}, pending: map[string]alPerm{}, path: path, off: -1, now: time.Now}
}

// 處理紀錄的一行
func (a *AppPerms) feed(line string) {
	if !strings.Contains(line, "ermission") && !strings.Contains(line, "Mapping internal session") {
		return
	}
	if m := alMapRE.FindStringSubmatch(line); m != nil {
		a.cli[m[1]] = m[2]
		return
	}
	if m := alEmitRE.FindStringSubmatch(line); m != nil {
		at, err := time.ParseInLocation("2006-01-02 15:04:05", m[1], time.Local)
		if err != nil {
			at = a.now()
		}
		a.pending[m[2]] = alPerm{local: m[4], tool: m[3], at: at}
		return
	}
	if m := alRespRE.FindStringSubmatch(line); m != nil {
		delete(a.pending, m[1])
	}
}

// 讀紀錄檔新增的部分（每次掃描呼叫）。第一次只讀最後 2 MB（足夠找回對應表與還沒回答的詢問）；
// 檔案變小 = App 換了新檔（main.log 滿 10 MB 會改名成 main1.log）→ 從頭讀
func (a *AppPerms) Poll() {
	f, err := os.Open(a.path)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	size := st.Size()
	skip := false // 從檔案中間開始讀：第一行是半截，丟掉
	if a.off < 0 {
		a.off = size - 2<<20
		if a.off < 0 {
			a.off = 0
		}
		skip = a.off > 0
		a.rest = nil
	}
	if size < a.off {
		a.off, a.rest = 0, nil
	}
	if size == a.off {
		return
	}
	if _, err := f.Seek(a.off, io.SeekStart); err != nil {
		return
	}
	b, _ := io.ReadAll(io.LimitReader(f, size-a.off))
	a.off += int64(len(b))
	if skip {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			return
		}
		b = b[i+1:]
	}
	buf := append(a.rest, b...)
	last := bytes.LastIndexByte(buf, '\n')
	if last < 0 {
		a.rest = buf // 還沒寫完的一行：下次接著讀
		return
	}
	a.rest = append([]byte(nil), buf[last+1:]...)
	sc := bufio.NewScanner(bytes.NewReader(buf[:last+1]))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		a.feed(sc.Text())
	}
}

// 這個 Claude Code 對話在等你回答 App 的權限詢問嗎？回傳開始問的時間
func (a *AppPerms) Waiting(cliID string) (time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	var first time.Time
	for id, p := range a.pending {
		if now.Sub(p.at) > alPermStale {
			delete(a.pending, id)
			continue
		}
		if a.cli[p.local] == cliID && (first.IsZero() || p.at.Before(first)) {
			first = p.at
		}
	}
	return first, !first.IsZero()
}
