package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
)

// 模擬螢幕（wifi.h wifiOta）：key 對了才回 auth；mode 決定怎麼回
func fakeOtaScreen(t *testing.T, c net.Conn, key, mode string, got *[]byte) {
	defer c.Close()
	r := bufio.NewReader(c)
	line, _ := r.ReadString('\n')
	if !strings.Contains(line, `"`+key+`"`) {
		return // 金鑰不對：斷線
	}
	c.Write([]byte("{\"wst\":{\"st\":2}}\n{\"auth\":1}\n"))
	line, _ = r.ReadString('\n')
	var m struct {
		OTA struct {
			N   int    `json:"n"`
			SHA string `json:"sha"`
		} `json:"ota"`
	}
	if json.Unmarshal([]byte(line), &m) != nil || m.OTA.N == 0 {
		t.Errorf("bad ota header %q", line)
		return
	}
	if mode == "busy" {
		c.Write([]byte(`{"ota":"err","why":"no space"}` + "\n"))
		return
	}
	c.Write([]byte(`{"ota":"rdy"}` + "\n"))
	buf := make([]byte, m.OTA.N)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Errorf("read image: %v", err)
		return
	}
	*got = buf
	sum := sha256.Sum256(buf)
	if mode == "corrupt" || hex.EncodeToString(sum[:]) != m.OTA.SHA {
		c.Write([]byte(`{"ota":"err","why":"sha mismatch"}` + "\n"))
		return
	}
	c.Write([]byte(`{"ota":"ok"}` + "\n"))
}

func TestOtaSend(t *testing.T) {
	img := testImageFor(10*otaChunk+123, chipS3)
	for _, tc := range []struct {
		mode, key string
		wantErr   string
	}{
		{"ok", "k1", ""},
		{"ok", "wrong", "沒有回應"},
		{"busy", "k1", "寫入失敗"},
		{"corrupt", "k1", "傳送中斷"},
	} {
		a, b := net.Pipe()
		var got []byte
		done := make(chan struct{})
		go func() { fakeOtaScreen(t, b, "k1", tc.mode, &got); close(done) }()
		last := 0
		err := otaSend(a, tc.key, img, func(d, n int) { last = d })
		a.Close()
		<-done
		if tc.wantErr == "" {
			if err != nil {
				t.Fatalf("%s: %v", tc.mode, err)
			}
			if string(got) != string(img) || last != len(img) {
				t.Fatalf("%s: image not delivered intact (got %d bytes, progress %d)", tc.mode, len(got), last)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("%s/%s: err = %v, want %q", tc.mode, tc.key, err, tc.wantErr)
		}
	}
}
