package main

// 自訂動畫的 Windows 部分：選檔（系統的開啟檔案視窗）、ffmpeg 轉檔、傳給螢幕、設定視窗的指令與狀態（anim.go 是可測試的部分）

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	comdlg32             = windows.NewLazySystemDLL("comdlg32.dll")
	procGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
)

type openFileNameW struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

// 系統的「開啟檔案」視窗；取消回傳 ""
func pickVideoFile() string {
	filter := utf16.Encode([]rune("影片或 GIF\x00*.mp4;*.mov;*.m4v;*.webm;*.mkv;*.avi;*.gif;*.webp\x00所有檔案\x00*.*\x00\x00"))
	title, _ := windows.UTF16PtrFromString("選擇要放在螢幕上的影片")
	file := make([]uint16, 1024)
	ofn := openFileNameW{
		hwndOwner:   hwnd,
		lpstrFilter: &filter[0],
		lpstrFile:   &file[0],
		nMaxFile:    uint32(len(file)),
		lpstrTitle:  title,
		flags:       0x1000 | 0x800 | 0x80000 | 0x8, // FILEMUSTEXIST | PATHMUSTEXIST | EXPLORER | NOCHANGEDIR
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(file)
}

// 找 ffmpeg：PATH、winget 裝的（裝完不用重開 AIMonitor 也找得到）、AIMonitor 資料夾
func findFFmpeg() string {
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	la := os.Getenv("LOCALAPPDATA")
	cands := []string{filepath.Join(la, "Microsoft", "WinGet", "Links", "ffmpeg.exe"), filepath.Join(appDir, "ffmpeg.exe")}
	if m, _ := filepath.Glob(filepath.Join(la, "Microsoft", "WinGet", "Packages", "*FFmpeg*", "*", "bin", "ffmpeg.exe")); len(m) > 0 {
		cands = append(cands, m...)
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

var (
	animStMu  sync.Mutex
	animJob   string                // 轉檔中 / 上次轉檔失敗的原因（設定視窗顯示）
	animProg  = map[string]string{} // 每塊螢幕（key）的傳送進度
	animTried = map[string]time.Time{}
	animSel   uint32 // 選的那份的 CRC（0 = Clawd），受 animStMu 保護
)

// 設定改了（選擇 / 刪除 / 啟動時）重算要顯示哪一份
func refreshAnimSel() {
	settingsMu.Lock()
	id := settings.Anim
	settingsMu.Unlock()
	var crc uint32
	if id != "" {
		if a, ok := findAnim(animDir(appDir), id); ok {
			crc = a.CRC
		}
	}
	animStMu.Lock()
	animSel = crc
	animStMu.Unlock()
}

func animWant() uint32 {
	animStMu.Lock()
	defer animStMu.Unlock()
	return animSel
}

func setAnimProg(key, s string) {
	animStMu.Lock()
	if s == "" {
		delete(animProg, key)
	} else {
		animProg[key] = s
	}
	animStMu.Unlock()
}

// 只在目前顯示的是這句時才收掉（不蓋掉傳送中 / 失敗的訊息）
func clearAnimProg(key, s string) {
	animStMu.Lock()
	if animProg[key] == s {
		delete(animProg, key)
	}
	animStMu.Unlock()
}

// 選一個影片 → ffmpeg 轉成畫格 → 收進動畫庫並選用
func addAnimFromFile() {
	path := pickVideoFile()
	if path == "" {
		return
	}
	ff := findFFmpeg()
	animStMu.Lock()
	if strings.HasPrefix(animJob, "轉檔中") {
		animStMu.Unlock()
		return
	}
	if ff == "" {
		animJob = "找不到 ffmpeg"
		animStMu.Unlock()
		return
	}
	animJob = "轉檔中…"
	animStMu.Unlock()
	go func() {
		vf := fmt.Sprintf("fps=%d,scale=%d:%d:force_original_aspect_ratio=increase:flags=lanczos,crop=%d:%d", animFPS, animW, animH, animW, animH)
		cmd := exec.Command(ff, "-hide_banner", "-v", "error", "-i", path, "-an", "-t", fmt.Sprint(animMaxFrames/animFPS),
			"-vf", vf, "-frames:v", fmt.Sprint(animMaxFrames), "-f", "rawvideo", "-pix_fmt", "rgb565le", "pipe:1")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		var a AnimInfo
		if err == nil {
			a, err = saveAnim(animDir(appDir), name, out.Bytes(), time.Now().Unix())
		} else if e := strings.TrimSpace(errb.String()); e != "" {
			err = fmt.Errorf("%s", firstLine(e))
		}
		animStMu.Lock()
		if err != nil {
			animJob = "轉檔失敗：" + err.Error()
			animStMu.Unlock()
			logf("anim: convert %s failed: %v", name, err)
			return
		}
		animJob = ""
		animStMu.Unlock()
		logf("anim: added %s (%d frames, crc %08x)", name, a.N, a.CRC)
		settingsMu.Lock()
		settings.Anim = a.ID
		settingsMu.Unlock()
		saveSettings()
		refreshAnimSel()
		sendFrameNow()
	}()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160]
	}
	return strings.TrimSpace(s)
}

// 設定視窗的指令：anim-add、anim-sel:<id>（空 = Clawd）、anim-del:<id>
func animCmd(cmd string) bool {
	switch {
	case cmd == "anim-add":
		addAnimFromFile()
	case strings.HasPrefix(cmd, "anim-sel:"):
		id := strings.TrimPrefix(cmd, "anim-sel:")
		if id != "" {
			if _, ok := findAnim(animDir(appDir), id); !ok {
				return true
			}
		}
		settingsMu.Lock()
		settings.Anim = id
		settingsMu.Unlock()
		saveSettings()
		refreshAnimSel()
		sendFrameNow()
	case strings.HasPrefix(cmd, "anim-del:"):
		id := strings.TrimPrefix(cmd, "anim-del:")
		deleteAnim(animDir(appDir), id)
		settingsMu.Lock()
		if settings.Anim == id {
			settings.Anim = ""
		}
		settingsMu.Unlock()
		saveSettings()
		refreshAnimSel()
		sendFrameNow()
	case cmd == "anim-clear":
		animStMu.Lock()
		animJob = ""
		animStMu.Unlock()
	default:
		return false
	}
	return true
}

func animStatus() interface{} {
	settingsMu.Lock()
	sel := settings.Anim
	settingsMu.Unlock()
	animStMu.Lock()
	job := animJob
	prog := map[string]string{}
	for k, v := range animProg {
		prog[k] = v
	}
	animStMu.Unlock()
	return map[string]interface{}{"list": listAnims(animDir(appDir)), "sel": sel, "ffmpeg": findFFmpeg() != "", "job": job, "prog": prog}
}

// serveScreen 每一圈呼叫：AMOLED 上沒有選的那份動畫就傳過去（傳的時候畫面暫停送，傳完回傳 true）
func animMaybeUpload(sc *screenConn, sp screenLink) bool {
	want := animWant()
	serMu.Lock()
	st := sc.stat
	serMu.Unlock()
	key := st.Key()
	switch animDecide(want, st.Verified, st.Board(), st.Mic, st.Anim, st.AV) {
	case animDoOldFW:
		setAnimProg(key, animOldFWMsg) // 設定視窗提示要更新韌體，不傳
		return false
	case animDoSkip:
		if st.Verified {
			clearAnimProg(key, animOldFWMsg) // 換回 Clawd、或韌體已更新：收掉上面那句
		}
		return false
	}
	clearAnimProg(key, animOldFWMsg)
	animStMu.Lock()
	if t, ok := animTried[key+fmt.Sprint(want)]; ok && time.Since(t) < time.Minute {
		animStMu.Unlock()
		return false
	}
	animTried[key+fmt.Sprint(want)] = time.Now()
	animStMu.Unlock()
	settingsMu.Lock()
	id := settings.Anim
	settingsMu.Unlock()
	a, ok := findAnim(animDir(appDir), id)
	if !ok || a.CRC != want {
		return false
	}
	raw, err := loadAnimRaw(animDir(appDir), a)
	if err != nil {
		setAnimProg(key, "讀不到動畫檔："+err.Error())
		return false
	}
	logf("%s: uploading animation %s (%d KB)", st.Port, a.Name, len(raw)/1024)
	t0 := time.Now()
	setAnimProg(key, "傳送中 0%")
	err = animUpload(sc, sp, a, raw, func(p int) { setAnimProg(key, fmt.Sprintf("傳送中 %d%%", p)) })
	if err != nil {
		logf("%s: animation upload failed: %v", st.Port, err)
		setAnimProg(key, "傳送失敗："+err.Error()+"（1 分鐘後再試）")
		time.Sleep(6 * time.Second) // 螢幕 5 秒沒收到資料才回去收 JSON
		return true
	}
	logf("%s: animation uploaded in %s", st.Port, time.Since(t0).Round(100*time.Millisecond))
	setAnimProg(key, "")
	serMu.Lock()
	sc.stat.AV = a.CRC
	serMu.Unlock()
	sendFrameNow()
	return true
}

func animUpload(sc *screenConn, sp screenLink, a AnimInfo, raw []byte, prog func(int)) error {
	buf := make([]byte, 512)
	var line []byte
	// 等螢幕回某一行；其他行照常處理
	wait := func(d time.Duration, match func(m map[string]interface{}) (bool, error)) error {
		end := time.Now().Add(d)
		for time.Now().Before(end) {
			n, err := sp.Read(buf)
			if err != nil {
				return err
			}
			for _, c := range buf[:n] {
				if c != '\n' && c != '\r' {
					if len(line) < 8192 {
						line = append(line, c)
					}
					continue
				}
				l := strings.TrimSpace(string(line))
				line = line[:0]
				var m map[string]interface{}
				if !strings.HasPrefix(l, "{") || json.Unmarshal([]byte(l), &m) != nil {
					continue
				}
				if done, err := match(m); done || err != nil {
					return err
				}
				handleDeviceLine(sc, l)
			}
		}
		return fmt.Errorf("螢幕沒有回應")
	}
	bad := func(m map[string]interface{}) error {
		if m["anim"] == "bad" {
			why, _ := m["why"].(string)
			switch why {
			case "mem":
				if ps, ok := m["ps"].(float64); ok { // 韌體 1.13.0 起回報 PSRAM 剩多少（KB），寫進紀錄方便查
					psb, _ := m["psb"].(float64)
					return fmt.Errorf("螢幕記憶體不夠（PSRAM 剩 %.0f KB、最大一塊 %.0f KB，這份要 %d KB）", ps, psb, len(raw)/1024)
				}
				return fmt.Errorf("螢幕記憶體不夠")
			case "crc":
				return fmt.Errorf("資料傳壞了")
			case "size":
				return fmt.Errorf("螢幕韌體不支援這個大小，請更新韌體")
			}
			return fmt.Errorf("螢幕拒收")
		}
		return nil
	}
	if err := sp.Write([]byte(animHeader(a))); err != nil {
		return err
	}
	// 第一次要格式化螢幕的儲存區，可能要等十幾秒
	if err := wait(60*time.Second, func(m map[string]interface{}) (bool, error) {
		return m["anim"] == "rdy", bad(m)
	}); err != nil {
		return err
	}
	total := len(raw)
	for sent := 0; sent < total; {
		end := sent + 16384 // 跟韌體 anim.h 的 ANIM_ACK 一樣：螢幕的 USB 接收緩衝只有 24 KB，送太多會被丟掉
		if end > total {
			end = total
		}
		for off := sent; off < end; off += 4096 {
			e := off + 4096
			if e > end {
				e = end
			}
			if err := sp.Write(raw[off:e]); err != nil {
				return err
			}
		}
		sent = end
		prog(sent * 100 / total)
		if sent < total {
			err := wait(15*time.Second, func(m map[string]interface{}) (bool, error) {
				ak, _ := m["ak"].(float64)
				return ak >= float64(sent), bad(m)
			})
			if err != nil {
				return err
			}
		} else {
			err := wait(30*time.Second, func(m map[string]interface{}) (bool, error) {
				if m["anim"] == "ok" {
					if c, _ := m["crc"].(float64); uint32(c) != a.CRC {
						return true, fmt.Errorf("螢幕收到的不一樣")
					}
					if m["saved"] != 1.0 {
						logf("%s: animation not saved to flash (shown until reboot)", sc.stat.Port)
					}
					return true, nil
				}
				return false, bad(m)
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
