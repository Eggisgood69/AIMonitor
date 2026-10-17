package main

// 系統匣圖示：顏色跟著狀態燈變（外圈 = 狀態、中間 = 目前的 AI），
// 左鍵打開設定視窗，右鍵選單（打開設定 / 播放測試畫面 / 結束程式）。

import (
	"fmt"
	"math"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	gdi32                = windows.NewLazySystemDLL("gdi32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procShellNotifyIcon  = shell32.NewProc("Shell_NotifyIconW")
	procRegisterClassEx  = user32.NewProc("RegisterClassExW")
	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procRegisterWinMsg   = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenu       = user32.NewProc("AppendMenuW")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procPostMessage      = user32.NewProc("PostMessageW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procCreateIconInd    = user32.NewProc("CreateIconIndirect")
	procCreateDIBSection = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap     = gdi32.NewProc("CreateBitmap")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procGetModuleHandle  = kernel32.NewProc("GetModuleHandleW")
)

const (
	nimAdd       = 0
	nimModify    = 1
	nimDelete    = 2
	nimSetVer    = 4
	nifMessage   = 1
	nifIcon      = 2
	nifTip       = 4
	nifShowTip   = 0x80
	wmTray       = 0x8000 + 11 // WM_APP + 11
	wmLButtonUp  = 0x0202
	wmRButtonUp  = 0x0205
	wmContext    = 0x007B
	ninSelect    = 0x0400
	ninKeySelect = 0x0401
	mfString     = 0
	mfSeparator  = 0x800
	tpmReturnCmd = 0x100
	tpmRightBtn  = 0x2
	tpmBottom    = 0x20
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

var (
	trayHwnd     uintptr
	trayMu       sync.Mutex
	trayKey      string
	trayIcons    = map[string]uintptr{}
	trayCreated  uint32 // 「工作列重新建立」訊息（檔案總管重開時要重新加入圖示）
	trayAdded    bool
	trayLastTip  string
	trayLastIcon uintptr
)

// 在主執行緒呼叫（wv.Dispatch 裡）
func trayInit() {
	inst, _, _ := procGetModuleHandle.Call(0)
	cls, _ := windows.UTF16PtrFromString("AIMonitorTray")
	wc := wndClassEx{
		LpfnWndProc:   syscall.NewCallback(trayWndProc),
		HInstance:     inst,
		LpszClassName: cls,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	title, _ := windows.UTF16PtrFromString("AIMonitorTray")
	trayHwnd, _, _ = procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), 0, 0, 0, 0, 0, 0, 0, inst, 0)
	if trayHwnd == 0 {
		logf("tray: create window failed")
		return
	}
	tc, _ := windows.UTF16PtrFromString("TaskbarCreated")
	r, _, _ := procRegisterWinMsg.Call(uintptr(unsafe.Pointer(tc)))
	trayCreated = uint32(r)
	trayApply(true)
	go func() {
		for range time.Tick(time.Second) {
			if trayNeedsUpdate() {
				wv.Dispatch(func() { trayApply(false) })
			}
		}
	}()
}

func trayRemove() {
	if trayHwnd == 0 {
		return
	}
	d := notifyIconData{HWnd: trayHwnd, UID: 1}
	d.CbSize = uint32(unsafe.Sizeof(d))
	procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&d)))
}

func trayWndProc(h, msg, wp, lp uintptr) uintptr {
	switch {
	case msg == wmTray:
		ev := lp & 0xFFFF
		switch ev {
		case wmLButtonUp, ninSelect, ninKeySelect:
			showWindow()
			wv.Navigate(restURL)
		case wmRButtonUp, wmContext:
			trayMenu()
		}
		return 0
	case trayCreated != 0 && uint32(msg) == trayCreated:
		trayAdded = false
		trayApply(true)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(h, msg, wp, lp)
	return r
}

func trayMenu() {
	m, _, _ := procCreatePopupMenu.Call()
	add := func(id uintptr, s string) {
		p, _ := windows.UTF16PtrFromString(s)
		procAppendMenu.Call(m, mfString, id, uintptr(unsafe.Pointer(p)))
	}
	add(1, "打開設定")
	add(2, "播放測試畫面")
	add(3, "重新讀取額度")
	procAppendMenu.Call(m, mfSeparator, 0, 0)
	add(9, "結束程式")
	var pt struct{ X, Y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForeground.Call(trayHwnd)
	cmd, _, _ := procTrackPopupMenu.Call(m, tpmReturnCmd|tpmRightBtn|tpmBottom, uintptr(pt.X), uintptr(pt.Y), 0, trayHwnd, 0)
	procPostMessage.Call(trayHwnd, 0, 0, 0) // 讓選單正確關閉（Windows 的老規矩）
	procDestroyMenu.Call(m)
	switch cmd {
	case 1:
		showWindow()
		wv.Navigate(restURL)
	case 2:
		playDemo()
	case 3:
		go func() { pollUsage(); pollProviders("") }()
	case 9:
		trayRemove()
		wv.Terminate()
	}
}

// ---- 圖示內容 ----

var stNames = map[string]string{"idle": "待命", "done": "完成", "working": "生成中", "review": "需要審核", "error": "錯誤"}

func trayState() (key, tip string, ring, center uint32, dim bool) {
	serMu.Lock()
	conn := serStat.Connected
	serMu.Unlock()
	lastFrameMu.Lock()
	f := lastFrame
	lastFrameMu.Unlock()

	ring = 0x8E8E93
	switch f.St {
	case "working", "done":
		ring = 0x30D158
	case "review":
		ring = 0xFFD60A
	case "error":
		ring = 0xFF453A
	}
	center = 0x8E8E93
	switch f.AI {
	case "claude":
		center = 0xF0855A
	case "chatgpt":
		center = 0xE5E5EA
	case "gemini":
		center = 0x64A8FF
	}
	dim = f.St == "idle" || f.St == ""
	tip = appTitle
	if name, ok := aiNames[f.AI]; ok {
		tip += "\n" + name + " · " + stNames[f.St]
		if f.P5 != nil && *f.P5 >= 0 {
			tip += fmt.Sprintf(" · 額度 %d%%", int(math.Round(*f.P5)))
		}
	} else {
		tip += "\n目前沒有使用中的 AI"
	}
	if !conn {
		tip += "\n螢幕未連線"
	}
	key = fmt.Sprintf("%06x/%06x/%v", ring, center, dim)
	return
}

func trayNeedsUpdate() bool {
	key, tip, _, _, _ := trayState()
	trayMu.Lock()
	defer trayMu.Unlock()
	return key != trayKey || tip != trayLastTip || !trayAdded
}

func trayApply(force bool) {
	if trayHwnd == 0 {
		return
	}
	key, tip, ring, center, dim := trayState()
	trayMu.Lock()
	ico := trayIcons[key]
	if ico == 0 {
		ico = makeTrayIcon(ring, center, dim)
		trayIcons[key] = ico
	}
	trayKey, trayLastTip = key, tip
	trayMu.Unlock()

	d := notifyIconData{HWnd: trayHwnd, UID: 1, UFlags: nifMessage | nifIcon | nifTip | nifShowTip, UCallbackMessage: wmTray, HIcon: ico}
	d.CbSize = uint32(unsafe.Sizeof(d))
	u := windows.StringToUTF16(tip)
	if len(u) > 127 {
		u = append(u[:126], 0)
	}
	copy(d.SzTip[:], u)
	if !trayAdded {
		r, _, _ := procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&d)))
		if r != 0 {
			trayAdded = true
			d.UVersion = 4 // NOTIFYICON_VERSION_4
			procShellNotifyIcon.Call(nimSetVer, uintptr(unsafe.Pointer(&d)))
		}
		return
	}
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&d)))
	_ = force
}

// 畫一個和程式圖示同風格的小圖：外圈狀態色、中間 AI 品牌色的小圓
func makeTrayIcon(ring, center uint32, dim bool) uintptr {
	s, _, _ := procGetSystemMetrics.Call(49) // SM_CXSMICON（已含 DPI 縮放）
	size := int(s)
	if size < 16 {
		size = 16
	}
	if size > 64 {
		size = 64
	}
	type bih struct {
		Size                         uint32
		Width, Height                int32
		Planes, BitCount             uint16
		Compression, SizeImage       uint32
		XPels, YPels, ClrUsed, ClrIm int32
	}
	h := bih{Width: int32(size), Height: -int32(size), Planes: 1, BitCount: 32}
	h.Size = uint32(unsafe.Sizeof(h))
	var bits unsafe.Pointer
	hbm, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&h)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbm == 0 || bits == nil {
		return 0
	}
	px := unsafe.Slice((*uint32)(bits), size*size)
	c := float64(size) / 2
	rOut := c - 0.5
	rIn := rOut - math.Max(2.2, float64(size)*0.15)
	rDot := rIn - math.Max(1.6, float64(size)*0.1)
	ringA := 1.0
	if dim {
		ringA = 0.75
	}
	cov := func(d, edge float64) float64 { // 邊緣反鋸齒
		v := edge - d + 0.5
		return math.Max(0, math.Min(1, v))
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)+0.5-c, float64(y)+0.5-c
			d := math.Hypot(dx, dy)
			aRing := cov(d, rOut) * (1 - cov(d, rIn)) * ringA
			aDot := cov(d, rDot)
			var col uint32
			a := 0.0
			switch {
			case aDot > 0:
				col, a = center, aDot
			case aRing > 0:
				col, a = ring, aRing
			}
			A := uint32(a*255 + 0.5)
			r, g, b := (col>>16)&0xFF, (col>>8)&0xFF, col&0xFF
			px[y*size+x] = A<<24 | r<<16 | g<<8 | b // 圖示用一般（非預乘）透明度
		}
	}
	mask, _, _ := procCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, 0)
	ii := struct {
		FIcon            uint32
		XHot, YHot       uint32
		HbmMask, HbmColr uintptr
	}{FIcon: 1, HbmMask: mask, HbmColr: hbm}
	ico, _, _ := procCreateIconInd.Call(uintptr(unsafe.Pointer(&ii)))
	procDeleteObject.Call(hbm)
	procDeleteObject.Call(mask)
	return ico
}
