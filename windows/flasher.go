package main

// 韌體更新：ESP32-C3 晶片內建（ROM）燒錄協定，與 esptool 相同。
// 只寫入應用程式區（0x10000），bootloader 與分割表維持不動。
// 這個檔案不依賴 Windows（序列埠透過 flashPort 介面），可以用模擬的 ROM 做單元測試。

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 燒錄需要的序列埠操作
type flashPort interface {
	Write(b []byte) error
	Read(b []byte) (int, error) // 最多等一小段時間；沒資料回傳 0
	SetDTR(on bool) error
	SetRTS(on bool) error
	SetBaud(baud int) error
	Purge()
}

const (
	cmdFlashBegin  = 0x02
	cmdFlashData   = 0x03
	cmdFlashEnd    = 0x04
	cmdSync        = 0x08
	cmdSetParams   = 0x0B
	cmdSpiAttach   = 0x0D
	cmdChangeBaud  = 0x0F
	cmdSpiFlashMD5 = 0x13

	flashBlockSize = 0x400 // ROM 模式每包 1KB
	appOffset      = 0x10000
	flashSize      = 4 * 1024 * 1024
)

type esp struct {
	p    flashPort
	buf  []byte // 尚未解析的接收資料
	logf func(string, ...interface{})
}

// ---- SLIP ----
func slipEncode(b []byte) []byte {
	out := []byte{0xC0}
	for _, c := range b {
		switch c {
		case 0xC0:
			out = append(out, 0xDB, 0xDC)
		case 0xDB:
			out = append(out, 0xDB, 0xDD)
		default:
			out = append(out, c)
		}
	}
	return append(out, 0xC0)
}

func slipDecode(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == 0xDB && i+1 < len(b) {
			i++
			if b[i] == 0xDC {
				out = append(out, 0xC0)
			} else {
				out = append(out, 0xDB)
			}
			continue
		}
		out = append(out, b[i])
	}
	return out
}

// 讀一個完整的 SLIP 封包
func (e *esp) readPacket(timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	tmp := make([]byte, 1024)
	for {
		// 找出 C0 ... C0
		if s := bytes.IndexByte(e.buf, 0xC0); s >= 0 {
			if end := bytes.IndexByte(e.buf[s+1:], 0xC0); end >= 0 {
				raw := e.buf[s+1 : s+1+end]
				e.buf = e.buf[s+1+end+1:]
				if len(raw) == 0 { // 連續的 C0，略過
					e.buf = append([]byte{0xC0}, e.buf...)
					continue
				}
				return slipDecode(raw), nil
			}
			e.buf = e.buf[s:] // 丟掉封包前的雜訊（例如韌體的開機訊息）
		} else {
			e.buf = e.buf[:0]
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timeout")
		}
		n, err := e.p.Read(tmp)
		if err != nil {
			return nil, err
		}
		e.buf = append(e.buf, tmp[:n]...)
	}
}

func checksum(data []byte) uint32 {
	c := uint32(0xEF)
	for _, b := range data {
		c ^= uint32(b)
	}
	return c
}

// 送出指令並等待對應的回應；回傳 (value, data)
func (e *esp) command(op byte, data []byte, chk uint32, timeout time.Duration) (uint32, []byte, error) {
	pkt := make([]byte, 8+len(data))
	pkt[0] = 0x00
	pkt[1] = op
	binary.LittleEndian.PutUint16(pkt[2:], uint16(len(data)))
	binary.LittleEndian.PutUint32(pkt[4:], chk)
	copy(pkt[8:], data)
	if err := e.p.Write(slipEncode(pkt)); err != nil {
		return 0, nil, err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r, err := e.readPacket(time.Until(deadline))
		if err != nil {
			return 0, nil, err
		}
		if len(r) < 8 || r[0] != 0x01 || r[1] != op {
			continue // 不是這個指令的回應（例如多餘的 SYNC 回應）
		}
		val := binary.LittleEndian.Uint32(r[4:8])
		body := r[8:]
		// ESP32 系列 ROM：最後 4 個位元組是狀態（第一個 0 = 成功）
		if len(body) >= 4 {
			st := body[len(body)-4:]
			if st[0] != 0 {
				return val, body, fmt.Errorf("指令 0x%02X 失敗（錯誤碼 0x%02X）", op, st[1])
			}
			body = body[:len(body)-4]
		}
		return val, body, nil
	}
	return 0, nil, errors.New("timeout")
}

func (e *esp) check(op byte, data []byte, timeout time.Duration) error {
	_, _, err := e.command(op, data, 0, timeout)
	return err
}

// 讓晶片重開機進入燒錄模式
func (e *esp) enterBootloader(usbJTAG bool) {
	p := e.p
	if usbJTAG {
		// ESP32-C3 內建 USB（與 esptool 的 USBJTAGSerialReset 相同）
		p.SetRTS(false)
		p.SetDTR(false)
		time.Sleep(100 * time.Millisecond)
		p.SetDTR(true)
		p.SetRTS(false)
		time.Sleep(100 * time.Millisecond)
		p.SetRTS(true)
		p.SetDTR(false)
		p.SetRTS(true) // Windows 只有在設定 RTS 時才會一起送出 DTR
		time.Sleep(100 * time.Millisecond)
		p.SetDTR(false)
		p.SetRTS(false)
	} else {
		// CH340 / CP210x 的傳統自動重置電路
		p.SetDTR(false)
		p.SetRTS(true)
		time.Sleep(100 * time.Millisecond)
		p.SetDTR(true)
		p.SetRTS(false)
		time.Sleep(50 * time.Millisecond)
		p.SetDTR(false)
	}
}

func (e *esp) sync() error {
	data := append([]byte{0x07, 0x07, 0x12, 0x20}, bytes.Repeat([]byte{0x55}, 32)...)
	for i := 0; i < 7; i++ {
		_, _, err := e.command(cmdSync, data, 0, 150*time.Millisecond)
		if err == nil {
			// ROM 會對一次 SYNC 回好幾個回應，把多餘的讀掉
			for j := 0; j < 8; j++ {
				if _, err := e.readPacket(60 * time.Millisecond); err != nil {
					break
				}
			}
			e.buf = e.buf[:0]
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("無法和晶片同步")
}

func (e *esp) connect(usbJTAG bool) error {
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		// 輪流嘗試兩種重置方式（內建 USB / CH340）
		e.enterBootloader(usbJTAG == (attempt%2 == 0))
		time.Sleep(150 * time.Millisecond)
		e.p.Purge()
		e.buf = e.buf[:0]
		if last = e.sync(); last == nil {
			return nil
		}
	}
	return last
}

type FlashProgress func(done, total int)

// FlashApp：把 image 寫入 0x10000 並驗證，完成後重新開機
func FlashApp(p flashPort, usbJTAG bool, image []byte, progress FlashProgress, logf func(string, ...interface{})) (err error) {
	defer func() {
		if err != nil { // 失敗時也把晶片重新開機回一般模式，螢幕不會停在空白
			p.SetDTR(false)
			p.SetRTS(true)
			time.Sleep(120 * time.Millisecond)
			p.SetRTS(false)
		}
	}()
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}
	if len(image) == 0 || len(image) > 0x140000 {
		return errors.New("韌體檔案大小不正確")
	}
	if image[0] != 0xE9 {
		return errors.New("韌體檔案格式不正確")
	}
	e := &esp{p: p, logf: logf}
	if err := e.connect(usbJTAG); err != nil {
		return fmt.Errorf("進入燒錄模式失敗：%w", err)
	}
	logf("flash: synced")

	// 提高速度（內建 USB 不受影響；CH340 會變快）
	if !usbJTAG {
		if err := e.check(cmdChangeBaud, le32(460800, 0), time.Second); err == nil {
			time.Sleep(50 * time.Millisecond)
			if p.SetBaud(460800) == nil {
				time.Sleep(50 * time.Millisecond)
				p.Purge()
				e.buf = e.buf[:0]
			}
		}
	}
	if err := e.check(cmdSpiAttach, make([]byte, 8), 3*time.Second); err != nil {
		return fmt.Errorf("SPI attach：%w", err)
	}
	if err := e.check(cmdSetParams, le32(0, flashSize, 64*1024, 4096, 256, 0xFFFF), 3*time.Second); err != nil {
		return fmt.Errorf("設定 Flash 參數：%w", err)
	}

	blocks := (len(image) + flashBlockSize - 1) / flashBlockSize
	eraseSize := uint32(blocks * flashBlockSize)
	// 擦除時間跟大小有關：每 MB 給 10 秒，最少 15 秒
	eraseTimeout := time.Duration(15+int(eraseSize)/(1024*1024)*10) * time.Second
	if err := e.check(cmdFlashBegin, le32(eraseSize, uint32(blocks), flashBlockSize, appOffset, 0), eraseTimeout); err != nil {
		return fmt.Errorf("開始寫入：%w", err)
	}
	for i := 0; i < blocks; i++ {
		chunk := make([]byte, flashBlockSize)
		for j := range chunk {
			chunk[j] = 0xFF
		}
		copy(chunk, image[i*flashBlockSize:])
		data := append(le32(flashBlockSize, uint32(i), 0, 0), chunk...)
		var werr error
		for retry := 0; retry < 3; retry++ {
			if _, _, werr = e.command(cmdFlashData, data, checksum(chunk), 5*time.Second); werr == nil {
				break
			}
		}
		if werr != nil {
			return fmt.Errorf("寫入第 %d 塊：%w", i, werr)
		}
		if progress != nil {
			progress(i+1, blocks)
		}
	}
	// 驗證 MD5（寫入的範圍包含補上的 0xFF）
	padded := make([]byte, blocks*flashBlockSize)
	for j := range padded {
		padded[j] = 0xFF
	}
	copy(padded, image)
	want := md5.Sum(padded)
	_, body, merr := e.command(cmdSpiFlashMD5, le32(appOffset, uint32(len(padded)), 0, 0), 0, 30*time.Second)
	if merr != nil {
		return fmt.Errorf("驗證：%w", merr)
	}
	got := strings.ToLower(strings.TrimSpace(string(body)))
	if len(body) == 16 {
		got = hex.EncodeToString(body)
	}
	if got != hex.EncodeToString(want[:]) {
		return fmt.Errorf("驗證失敗（MD5 不符）")
	}
	logf("flash: verified %s", got)

	// 結束（留在燒錄模式），再用 RTS 重置開機
	e.command(cmdFlashEnd, le32(1), 0, 2*time.Second)
	p.SetDTR(false)
	p.SetRTS(true)
	time.Sleep(120 * time.Millisecond)
	p.SetRTS(false)
	return nil
}

func le32(v ...uint32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[i*4:], x)
	}
	return b
}

// 比較版本字串 "1.2.0"
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			fmt.Sscanf(pa[i], "%d", &x)
		}
		if i < len(pb) {
			fmt.Sscanf(pb[i], "%d", &y)
		}
		if x != y {
			return x < y
		}
	}
	return false
}
