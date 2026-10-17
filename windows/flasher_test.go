package main

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"math/rand"
	"sync"
	"testing"
)

// 模擬 ESP32-C3 的 ROM 燒錄程式
type fakeROM struct {
	mu        sync.Mutex
	out       []byte // 要回給電腦的資料
	in        []byte // 收到但還沒解析完的資料
	inBoot    bool
	needJTAG  bool // true：只有內建 USB 的重置序列能進入燒錄模式
	dtr, rts  bool
	rtsLowDTR int // 記錄重置序列
	flash     []byte
	beginOff  uint32
	seq       uint32
	failOnce  map[uint32]bool
	baud      int
	ended     bool
	resets    int
	t         *testing.T
}

func newFakeROM(t *testing.T, needJTAG bool) *fakeROM {
	f := &fakeROM{flash: bytes.Repeat([]byte{0xFF}, flashSize), needJTAG: needJTAG, failOnce: map[uint32]bool{3: true}, baud: 115200, t: t}
	// 韌體正常執行時會一直輸出 JSON（模擬雜訊）
	f.out = append(f.out, []byte("{\"aimon\":2,\"fw\":\"1.1.0\"}\r\n")...)
	return f
}

func (f *fakeROM) SetDTR(on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dtr = on
	return nil
}

func (f *fakeROM) SetRTS(on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// 內建 USB：RTS 拉高時 DTR 為低 → 重置；之後若 DTR 高過 → 燒錄模式（簡化模型）
	// 傳統電路：RTS 高 + DTR 低 → EN 低（重置），接著 DTR 高 + RTS 低 → IO0 低、EN 高 → 燒錄模式
	if f.rts && !on {
		f.resets++
	}
	prev := f.rts
	f.rts = on
	if f.needJTAG {
		// 只接受 USB-JTAG 序列（特徵：在 RTS 從高變低之前，DTR 曾經在 RTS 低時被拉高）
		if !prev && on && f.rtsLowDTR > 0 {
			f.inBoot = true
		}
		if !on && f.dtr {
			f.rtsLowDTR++
		}
	} else {
		if prev && !on && f.dtr {
			f.inBoot = true
		}
	}
	return nil
}

func (f *fakeROM) SetBaud(b int) error { f.baud = b; return nil }
func (f *fakeROM) Purge()              { f.mu.Lock(); f.out = nil; f.mu.Unlock() }

func (f *fakeROM) Read(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := copy(b, f.out)
	f.out = f.out[n:]
	return n, nil
}

func (f *fakeROM) reply(op byte, val uint32, body []byte, ok bool) {
	st := []byte{0, 0, 0, 0}
	if !ok {
		st = []byte{1, 0x07, 0, 0}
	}
	body = append(append([]byte{}, body...), st...)
	pkt := make([]byte, 8+len(body))
	pkt[0], pkt[1] = 1, op
	binary.LittleEndian.PutUint16(pkt[2:], uint16(len(body)))
	binary.LittleEndian.PutUint32(pkt[4:], val)
	copy(pkt[8:], body)
	f.out = append(f.out, slipEncode(pkt)...)
}

func (f *fakeROM) Write(b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.inBoot {
		return nil // 一般模式：忽略
	}
	f.in = append(f.in, b...)
	for {
		s := bytes.IndexByte(f.in, 0xC0)
		if s < 0 {
			return nil
		}
		e := bytes.IndexByte(f.in[s+1:], 0xC0)
		if e < 0 {
			return nil
		}
		raw := slipDecode(f.in[s+1 : s+1+e])
		f.in = f.in[s+1+e+1:]
		if len(raw) < 8 {
			continue
		}
		op := raw[1]
		size := binary.LittleEndian.Uint16(raw[2:])
		chk := binary.LittleEndian.Uint32(raw[4:])
		data := raw[8:]
		if int(size) != len(data) {
			f.t.Errorf("op %02x size mismatch %d vs %d", op, size, len(data))
		}
		switch op {
		case cmdSync:
			for i := 0; i < 8; i++ {
				f.reply(op, 0, nil, true)
			}
		case cmdSpiAttach, cmdSetParams:
			f.reply(op, 0, nil, true)
		case cmdChangeBaud:
			f.reply(op, 0, nil, true)
		case cmdFlashBegin:
			if len(data) != 20 {
				f.t.Errorf("FLASH_BEGIN expects 20 bytes on C3 ROM, got %d", len(data))
			}
			f.beginOff = binary.LittleEndian.Uint32(data[12:])
			f.seq = 0
			f.reply(op, 0, nil, true)
		case cmdFlashData:
			l := binary.LittleEndian.Uint32(data[0:])
			seq := binary.LittleEndian.Uint32(data[4:])
			payload := data[16:]
			ok := int(l) == len(payload) && checksum(payload) == chk && seq == f.seq
			if f.failOnce[seq] { // 模擬一次傳輸錯誤
				delete(f.failOnce, seq)
				ok = false
			}
			if ok {
				copy(f.flash[f.beginOff+seq*flashBlockSize:], payload)
				f.seq++
			}
			f.reply(op, 0, nil, ok)
		case cmdSpiFlashMD5:
			addr := binary.LittleEndian.Uint32(data[0:])
			n := binary.LittleEndian.Uint32(data[4:])
			sum := md5.Sum(f.flash[addr : addr+n])
			f.reply(op, 0, []byte(hex.EncodeToString(sum[:])), true)
		case cmdFlashEnd:
			f.ended = true
			f.reply(op, 0, nil, true)
		default:
			f.t.Errorf("unexpected op %02x", op)
		}
	}
}

func testImage(n int) []byte {
	img := make([]byte, n)
	rand.New(rand.NewSource(1)).Read(img)
	img[0] = 0xE9
	// 放一些需要 SLIP 跳脫的位元組
	img[100], img[101], img[102] = 0xC0, 0xDB, 0xC0
	return img
}

func TestFlashUSBJTAG(t *testing.T) {
	rom := newFakeROM(t, true)
	img := testImage(300*1024 + 123)
	var last int
	err := FlashApp(rom, true, img, func(d, total int) { last = d }, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rom.flash[appOffset:appOffset+len(img)], img) {
		t.Fatal("flash content mismatch")
	}
	if rom.flash[appOffset+len(img)] != 0xFF {
		t.Fatal("padding should be 0xFF")
	}
	if !rom.ended || last != (len(img)+flashBlockSize-1)/flashBlockSize {
		t.Fatalf("ended=%v last=%d", rom.ended, last)
	}
	if rom.flash[0] != 0xFF {
		t.Fatal("bootloader area must not be touched")
	}
}

func TestFlashCH340Fallback(t *testing.T) {
	// 以為是內建 USB，實際是 CH340：第一種重置失敗後要自動改用另一種
	rom := newFakeROM(t, false)
	img := testImage(64*1024 + 7)
	if err := FlashApp(rom, true, img, nil, t.Logf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rom.flash[appOffset:appOffset+len(img)], img) {
		t.Fatal("flash content mismatch")
	}
}

func TestFlashRejectsBadImage(t *testing.T) {
	rom := newFakeROM(t, true)
	if err := FlashApp(rom, true, []byte{1, 2, 3}, nil, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestSlipRoundTrip(t *testing.T) {
	in := []byte{0xC0, 1, 0xDB, 0xDC, 0xDD, 0xC0}
	enc := slipEncode(in)
	if !bytes.Equal(slipDecode(enc[1:len(enc)-1]), in) {
		t.Fatal("slip roundtrip")
	}
}

func TestVersionLess(t *testing.T) {
	if !versionLess("1.1.0", "1.2.0") || versionLess("1.2.0", "1.2.0") || !versionLess("", "1.0.0") || versionLess("1.10.0", "1.9.9") {
		t.Fatal("versionLess")
	}
}
