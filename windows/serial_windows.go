package main

// USB 序列埠：從登錄檔找出螢幕（Espressif / CH340 / CP210x），115200 8N1。

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var usbVIDs = []string{"VID_303A", "VID_1A86", "VID_10C4"} // Espressif 內建 USB、CH340、CP210x

// 目前插著的序列埠（HARDWARE\DEVICEMAP\SERIALCOMM）
func activePorts() map[string]bool {
	out := map[string]bool{}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE)
	if err != nil {
		return out
	}
	defer k.Close()
	names, _ := k.ReadValueNames(-1)
	for _, n := range names {
		if v, _, err := k.GetStringValue(n); err == nil {
			out[strings.ToUpper(v)] = true
		}
	}
	return out
}

type portInfo struct {
	Name string
	VID  string // VID_303A = ESP32-C3 內建 USB
}

// 找出螢幕可能使用的 COM 埠
func findDisplayPorts() []string {
	var out []string
	for _, p := range findDisplayPortInfos() {
		out = append(out, p.Name)
	}
	return out
}

func findDisplayPortInfos() []portInfo {
	active := activePorts()
	var found []portInfo
	usb, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Enum\USB`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer usb.Close()
	devs, _ := usb.ReadSubKeyNames(-1)
	seen := map[string]bool{}
	for _, d := range devs {
		ud := strings.ToUpper(d)
		match, vid := false, ""
		for _, v := range usbVIDs {
			if strings.HasPrefix(ud, v) {
				match, vid = true, v
			}
		}
		if !match {
			continue
		}
		dk, err := registry.OpenKey(usb, d, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		insts, _ := dk.ReadSubKeyNames(-1)
		for _, in := range insts {
			pk, err := registry.OpenKey(dk, in+`\Device Parameters`, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			p, _, err := pk.GetStringValue("PortName")
			pk.Close()
			p = strings.ToUpper(p)
			if err == nil && active[p] && !seen[p] {
				seen[p] = true
				found = append(found, portInfo{Name: p, VID: vid})
			}
		}
		dk.Close()
	}
	return found
}

type serialPort struct {
	h    windows.Handle
	name string
	dtr  bool // 目前的 DTR（SetRTS 要用同樣的值再送一次）
}

func openSerial(name string) (*serialPort, error) {
	path, _ := windows.UTF16PtrFromString(`\\.\` + name)
	h, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, err
	}
	var dcb windows.DCB
	dcb.DCBlength = uint32(unsafe.Sizeof(dcb))
	if err := windows.GetCommState(h, &dcb); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	dcb.BaudRate = 115200
	dcb.ByteSize = 8
	dcb.Parity = 0   // NOPARITY
	dcb.StopBits = 0 // ONESTOPBIT
	// fBinary=1；DTR 開啟、RTS 關閉：不會讓 ESP32 重開機或進入燒錄模式
	const fBinary = 0x1
	const dtrEnable = 0x1 << 4
	dcb.Flags = fBinary | dtrEnable
	if err := windows.SetCommState(h, &dcb); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	to := windows.CommTimeouts{
		ReadIntervalTimeout:        0xFFFFFFFF,
		ReadTotalTimeoutMultiplier: 0xFFFFFFFF,
		ReadTotalTimeoutConstant:   60, // 沒資料時最多等 60ms
		WriteTotalTimeoutConstant:  500,
	}
	if err := windows.SetCommTimeouts(h, &to); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return &serialPort{h: h, name: name, dtr: true}, nil
}

func (s *serialPort) Write(b []byte) error {
	var n uint32
	if err := windows.WriteFile(s.h, b, &n, nil); err != nil {
		return err
	}
	if int(n) != len(b) {
		return errors.New("short write")
	}
	return nil
}

func (s *serialPort) Read(b []byte) (int, error) {
	var n uint32
	err := windows.ReadFile(s.h, b, &n, nil)
	return int(n), err
}

func (s *serialPort) Close() { windows.CloseHandle(s.h) }

// ---- 燒錄用（flashPort 介面）----
func (s *serialPort) SetDTR(on bool) error {
	s.dtr = on
	f := uint32(windows.CLRDTR)
	if on {
		f = windows.SETDTR
	}
	return windows.EscapeCommFunction(s.h, f)
}

func (s *serialPort) SetRTS(on bool) error {
	f := uint32(windows.CLRRTS)
	if on {
		f = windows.SETRTS
	}
	if err := windows.EscapeCommFunction(s.h, f); err != nil {
		return err
	}
	// Windows 的 usbser.sys 只改 RTS 不會送給裝置，要再設一次 DTR（同樣的值）才會連同 RTS 一起送出
	// （與 esptool 的做法相同）。少了這步，燒錄後的 RTS 重置根本沒送到螢幕 → 螢幕全黑
	return s.SetDTR(s.dtr)
}

func (s *serialPort) SetBaud(baud int) error {
	var dcb windows.DCB
	dcb.DCBlength = uint32(unsafe.Sizeof(dcb))
	if err := windows.GetCommState(s.h, &dcb); err != nil {
		return err
	}
	dcb.BaudRate = uint32(baud)
	return windows.SetCommState(s.h, &dcb)
}

func (s *serialPort) Purge() {
	windows.PurgeComm(s.h, windows.PURGE_RXCLEAR|windows.PURGE_TXCLEAR)
}
