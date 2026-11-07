package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type cxFixture struct {
	t    *testing.T
	c    *Codex
	now  *time.Time
	path string
}

func newCxFixture(t *testing.T, id, originator, source string) *cxFixture {
	now := time.Date(2026, 11, 7, 15, 21, 0, 0, time.Local)
	dir := t.TempDir()
	day := filepath.Join(dir, "2026", "11", "07")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &cxFixture{t: t, c: NewCodex(dir), now: &now, path: filepath.Join(day, "rollout-2026-11-07T15-20-00-"+id+".jsonl")}
	f.c.now = func() time.Time { return *f.now }
	f.write(fmt.Sprintf(`{"timestamp":"%s","type":"session_meta","payload":{"id":"%s","cwd":"C:\\proj\\Demo","originator":"%s","source":%s,"base_instructions":{"text":"%s"}}}`,
		now.UTC().Format(time.RFC3339Nano), id, originator, source, strings.Repeat("x", 100<<10)))
	return f
}

func (f *cxFixture) write(line string) {
	fh, err := os.OpenFile(f.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		f.t.Fatal(err)
	}
	fh.WriteString(line + "\n")
	fh.Close()
	os.Chtimes(f.path, *f.now, *f.now)
}

func (f *cxFixture) event(kind string) {
	f.write(fmt.Sprintf(`{"timestamp":"%s","type":"event_msg","payload":{"type":"%s","turn_id":"t1"}}`, f.now.UTC().Format(time.RFC3339Nano), kind))
}

func (f *cxFixture) step(d time.Duration) { *f.now = f.now.Add(d) }

func (f *cxFixture) merge(ui ...Item) []Item {
	f.c.listAt = time.Time{} // 測試裡每次都重新列資料夾
	return f.c.Merge(ui)
}

func findKey(list []Item, prefix string) *Item {
	for i := range list {
		if strings.HasPrefix(list[i].Key, prefix) {
			return &list[i]
		}
	}
	return nil
}

const cxID = "01a1b4e1-ff55-72f2-9305-ff4595f1fb40"

// 開始 → 完成 → 中斷不算完成；長的 session_meta（超過緩衝區）也讀得到
func TestCodexTurns(t *testing.T) {
	f := newCxFixture(t, cxID, "Codex Desktop", `"vscode"`)
	if it := findKey(f.merge(), "cx:"); it != nil {
		t.Fatalf("還沒開始就有項目：%+v", it)
	}
	f.event("task_started")
	it := findKey(f.merge(), "cx:")
	if it == nil || !it.Gen || it.Key != "cx:95f1fb40" || it.AI != "chatgpt" || it.Buttons[0] != "Codex · Demo" {
		t.Fatalf("開始後：%+v", it)
	}
	f.step(15 * time.Second)
	f.event("task_complete")
	it = findKey(f.merge(), "cx:")
	if it.Gen || it.FinishedAt != f.now.UnixMilli() {
		t.Fatalf("完成後：%+v", it)
	}
	f.step(10 * time.Second)
	f.event("task_started")
	f.merge()
	f.step(3 * time.Second)
	f.event("turn_aborted")
	fin := it.FinishedAt
	it = findKey(f.merge(), "cx:")
	if it.Gen || it.FinishedAt != fin {
		t.Fatalf("中斷不該算完成：%+v", it)
	}
}

// 寫到一半的行要等寫完才處理
func TestCodexPartialLine(t *testing.T) {
	f := newCxFixture(t, cxID, "Codex Desktop", `"vscode"`)
	line := fmt.Sprintf(`{"timestamp":"%s","type":"event_msg","payload":{"type":"task_started"}}`, f.now.UTC().Format(time.RFC3339Nano))
	fh, _ := os.OpenFile(f.path, os.O_APPEND|os.O_WRONLY, 0o644)
	fh.WriteString(line[:20])
	fh.Close()
	if it := findKey(f.merge(), "cx:"); it != nil {
		t.Fatalf("半行不該處理：%+v", it)
	}
	fh, _ = os.OpenFile(f.path, os.O_APPEND|os.O_WRONLY, 0o644)
	fh.WriteString(line[20:] + "\n")
	fh.Close()
	if it := findKey(f.merge(), "cx:"); it == nil || !it.Gen {
		t.Fatalf("整行寫完後：%+v", it)
	}
}

// 子代理的紀錄不亮燈
func TestCodexSubagent(t *testing.T) {
	f := newCxFixture(t, cxID, "Codex Desktop", `{"subagent":{"other":"guardian"}}`)
	f.event("task_started")
	if it := findKey(f.merge(), "cx:"); it != nil {
		t.Fatalf("子代理不該有項目：%+v", it)
	}
}

// 很久沒寫紀錄的生成中：當作已經結束（不亮完成）
func TestCodexStale(t *testing.T) {
	f := newCxFixture(t, cxID, "Codex Desktop", `"vscode"`)
	f.event("task_started")
	f.merge()
	f.step(cxStale + time.Minute)
	it := findKey(f.merge(), "cx:")
	if it == nil || it.Gen || it.FinishedAt != 0 {
		t.Fatalf("太久沒動靜：%+v", it)
	}
}

// 大的舊紀錄只讀最後一段：最後是完成 → 舊的完成（狀態不是生成中）
func TestCodexTail(t *testing.T) {
	f := newCxFixture(t, cxID, "Codex Desktop", `"vscode"`)
	for i := 0; i < 400; i++ {
		f.write(fmt.Sprintf(`{"timestamp":"%s","type":"response_item","payload":{"type":"message","text":"%s"}}`, f.now.UTC().Format(time.RFC3339Nano), strings.Repeat("y", 1000)))
	}
	f.event("task_started")
	f.step(time.Second)
	f.event("task_complete")
	it := findKey(f.merge(), "cx:")
	if it == nil || it.Gen || it.FinishedAt == 0 {
		t.Fatalf("讀尾段：%+v", it)
	}
}

// ChatGPT App 畫面上同時開始的對話：配成同一個，只留紀錄這項；視窗關掉後畫面一直說生成中也不影響
func TestCodexPairWithApp(t *testing.T) {
	f := newCxFixture(t, cxID, "Codex Desktop", `"vscode"`)
	f.event("task_started")
	ui := Item{Key: "app:chatgpt", AI: "chatgpt", Focused: true, Gen: true, GenSince: f.now.UnixMilli() + 800, View: "功能詢問"}
	out := f.merge(ui)
	if findKey(out, "app:chatgpt") != nil {
		t.Fatalf("配對後不該再送畫面那項：%+v", out)
	}
	if it := findKey(out, "cx:"); it == nil || !it.Gen || !it.Focused {
		t.Fatalf("紀錄這項要借用正在看：%+v", it)
	}
	// 切到別的對話：畫面上變成背景項目（同一個標題），還是同一個
	f.step(4 * time.Second)
	other := Item{Key: "app:chatgpt", AI: "chatgpt", Focused: true, View: "別的對話"}
	bg := Item{Key: "app:chatgpt:bg:功能詢問", AI: "chatgpt", Gen: true, GenSince: f.now.UnixMilli()}
	out = f.merge(other, bg)
	if findKey(out, "app:chatgpt:bg:") != nil || findKey(out, "app:chatgpt") == nil {
		t.Fatalf("背景項目要併掉、別的對話照舊：%+v", out)
	}
	if it := findKey(out, "cx:"); it.Focused {
		t.Fatalf("切走後不是正在看：%+v", it)
	}
	// 等你核准：紀錄沒有這個事件，借畫面的待核准
	bg.Review, bg.ReviewSince = true, f.now.UnixMilli()
	if it := findKey(f.merge(other, bg), "cx:"); !it.Review {
		t.Fatalf("要借畫面的待核准：%+v", it)
	}
	// 關掉視窗：畫面停在生成中，紀錄說完成了 → 完成
	bg.Review = false
	f.step(10 * time.Second)
	f.event("task_complete")
	out = f.merge(bg)
	if it := findKey(out, "cx:"); it.Gen || it.FinishedAt == 0 || findKey(out, "app:chatgpt:bg:") != nil {
		t.Fatalf("紀錄完成要蓋過停住的畫面：%+v", out)
	}
	// 點開那個對話：正在看
	ui = Item{Key: "app:chatgpt", AI: "chatgpt", Focused: true, View: "功能詢問"}
	if it := findKey(f.merge(ui), "cx:"); !it.Focused {
		t.Fatalf("點開後要算正在看：%+v", it)
	}
}

// 新對話：紀錄晚幾秒才出現、來源是 codex_work_desktop、生成到一半標題從第一句話換成自動產生的標題（2026-11 實測亮了兩個燈）
func TestCodexPairRenamed(t *testing.T) {
	f := newCxFixture(t, cxID, "codex_work_desktop", `"vscode"`)
	f.event("task_started")
	start := f.now.UnixMilli()
	f.step(4 * time.Second)
	ui := Item{Key: "app:chatgpt", AI: "chatgpt", Focused: true, Gen: true, GenSince: start + 300, View: "讀一下 windows/state.go"}
	if out := f.merge(ui); findKey(out, "app:chatgpt") != nil {
		t.Fatalf("要配對：%+v", out)
	}
	f.step(5 * time.Second)
	ui.View = "說明 rank 規則"
	if out := f.merge(ui); findKey(out, "app:chatgpt") != nil {
		t.Fatalf("改標題後還是同一個：%+v", out)
	}
	// 之後切到背景：用新標題
	f.step(5 * time.Second)
	bg := Item{Key: "app:chatgpt:bg:說明 rank 規則", AI: "chatgpt", Gen: true, GenSince: f.now.UnixMilli()}
	other := Item{Key: "app:chatgpt", AI: "chatgpt", Focused: true, View: "別的對話", GenSince: start + 300}
	out := f.merge(other, bg)
	if findKey(out, "app:chatgpt:bg:") != nil || findKey(out, "app:chatgpt") == nil {
		t.Fatalf("背景項目要併掉、停下來的別的對話不能被舊的生成誤認：%+v", out)
	}
}

func (f *cxFixture) raw(typ, payload string) {
	f.write(fmt.Sprintf(`{"timestamp":"%s","type":"%s","payload":%s}`, f.now.UTC().Format(time.RFC3339Nano), typ, payload))
}

// 等你核准：「要求核准」模式下，要求權限的呼叫還沒有結果 → 黃燈；視窗看得到時以畫面為準
func TestCodexLogReview(t *testing.T) {
	f := newCxFixture(t, cxID, "codex_work_desktop", `"vscode"`)
	f.event("task_started")
	f.raw("turn_context", `{"turn_id":"t1","approval_policy":"on-request","approvals_reviewer":"user"}`)
	f.raw("response_item", `{"type":"custom_tool_call","call_id":"c1","name":"exec","input":"text(await tools.request_permissions({permissions:{network:{enabled:true}},reason:\"下載\"}))"}`)
	if it := findKey(f.merge(), "cx:"); it.Review {
		t.Fatalf("剛送出不算（可能馬上就有結果）：%+v", it)
	}
	f.step(2 * time.Second)
	it := findKey(f.merge(), "cx:")
	if !it.Review || it.ReviewSince != f.now.Add(-2*time.Second).UnixMilli() {
		t.Fatalf("沒有結果要亮黃燈：%+v", it)
	}
	// 視窗看得到、畫面上沒有待核准：以畫面為準
	ui := Item{Key: "app:chatgpt", AI: "chatgpt", Focused: true, Gen: true, GenSince: f.now.UnixMilli() - 2000, View: "下載"}
	if it := findKey(f.merge(ui), "cx:"); it.Review {
		t.Fatalf("看得到畫面時以畫面為準：%+v", it)
	}
	ui.Hidden, ui.Focused = true, false
	if it := findKey(f.merge(ui), "cx:"); !it.Review {
		t.Fatalf("視窗關掉時用紀錄：%+v", it)
	}
	f.raw("response_item", `{"type":"custom_tool_call_output","call_id":"c1","output":"ok"}`)
	if it := findKey(f.merge(ui), "cx:"); it.Review || !it.Gen {
		t.Fatalf("有結果後回到生成中：%+v", it)
	}
	// 沙箱外執行指令（function_call 的寫法）也算；「代我核准」模式不算
	f.raw("response_item", `{"type":"function_call","call_id":"c2","name":"shell","arguments":"{\"cmd\":\"go test\",\"sandbox_permissions\":\"require_escalated\"}"}`)
	f.step(2 * time.Second)
	if it := findKey(f.merge(ui), "cx:"); !it.Review {
		t.Fatalf("沙箱外執行要算：%+v", it)
	}
	f.raw("turn_context", `{"turn_id":"t2","approval_policy":"on-request","approvals_reviewer":"auto_review"}`)
	if it := findKey(f.merge(ui), "cx:"); it.Review {
		t.Fatalf("代我核准不算：%+v", it)
	}
	// 一般工具呼叫不算
	f.raw("turn_context", `{"turn_id":"t3","approval_policy":"on-request","approvals_reviewer":"user"}`)
	f.event("task_complete")
	f.step(time.Second)
	f.event("task_started")
	f.raw("response_item", `{"type":"custom_tool_call","call_id":"c3","name":"exec","input":"text(await tools.exec_command({cmd:\"go build\"}))"}`)
	f.step(5 * time.Second)
	if it := findKey(f.merge(ui), "cx:"); it.Review {
		t.Fatalf("一般指令不算：%+v", it)
	}
}

// AIMonitor 重開時正在生成：開始時間對不上，但兩邊都只有一個 → 同一個
func TestCodexPairAfterRestart(t *testing.T) {
	f := newCxFixture(t, cxID, "Codex Desktop", `"vscode"`)
	f.event("task_started")
	f.step(5 * time.Minute)
	ui := Item{Key: "app:chatgpt", AI: "chatgpt", Focused: true, Gen: true, GenSince: f.now.UnixMilli(), View: "功能詢問"}
	if out := f.merge(ui); findKey(out, "app:chatgpt") != nil {
		t.Fatalf("只有一對時要配對：%+v", out)
	}
}

// 聊天模式（不寫紀錄）或開始時間對不上：畫面項目照舊
func TestCodexNoPair(t *testing.T) {
	f := newCxFixture(t, cxID, "Codex Desktop", `"vscode"`)
	f.event("task_started")
	ui := Item{Key: "app:chatgpt", AI: "chatgpt", Focused: true, Gen: true, GenSince: f.now.UnixMilli() + 20000, View: "聊天"}
	bg := Item{Key: "app:chatgpt:bg:另一個", AI: "chatgpt", Gen: true, GenSince: f.now.UnixMilli() + 20000}
	out := f.merge(ui, bg)
	if findKey(out, "app:chatgpt") == nil || findKey(out, "cx:") == nil {
		t.Fatalf("時間對不上、又有兩個可能時不該配對：%+v", out)
	}
	// Codex CLI（不是 ChatGPT App）不跟畫面配對
	g := newCxFixture(t, cxID, "codex_cli_rs", `"cli"`)
	g.event("task_started")
	ui.GenSince = g.now.UnixMilli()
	if out := g.merge(ui); findKey(out, "app:chatgpt") == nil {
		t.Fatalf("CLI 不該跟 App 配對：%+v", out)
	}
}

// 經過 State：配對後只亮一個燈；背景完成要等點開那個對話才熄
func TestCodexWithState(t *testing.T) {
	f := newCxFixture(t, cxID, "Codex Desktop", `"vscode"`)
	s := NewState()
	s.now = func() time.Time { return *f.now }
	f.event("task_started")
	ui := Item{Key: "app:chatgpt", AI: "chatgpt", Focused: true, Gen: true, GenSince: f.now.UnixMilli(), View: "功能詢問"}
	s.ApplyScan(f.merge(ui))
	if fr := s.BuildFrame(); fr.AI != "chatgpt" || fr.St != "working" || len(fr.O) != 0 {
		t.Fatalf("生成中只要一個燈：%+v", fr)
	}
	// 關掉視窗（畫面停在生成中、也不是前景），紀錄完成
	ui.Focused = false
	f.step(15 * time.Second)
	f.event("task_complete")
	s.ApplyScan(f.merge(ui))
	if fr := s.BuildFrame(); fr.St != "done" || len(fr.O) != 0 {
		t.Fatalf("背景完成：%+v", fr)
	}
	f.step(time.Minute)
	s.ApplyScan(f.merge(ui))
	if fr := s.BuildFrame(); fr.St != "done" {
		t.Fatalf("沒看過要一直亮：%+v", fr)
	}
	// 打開視窗看那個對話：8 秒後熄
	ui = Item{Key: "app:chatgpt", AI: "chatgpt", Focused: true, View: "功能詢問"}
	s.ApplyScan(f.merge(ui))
	f.step(9 * time.Second)
	s.ApplyScan(f.merge(ui))
	if fr := s.BuildFrame(); fr.St != "idle" {
		t.Fatalf("看過 8 秒後要熄：%+v", fr)
	}
}
