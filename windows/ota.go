package main

// 經 WiFi 更新 AMOLED 的韌體（韌體 1.12.0 起，hello 有 "ota":1；螢幕端是 wifi.h wifiOta）：
//   另開一條 TCP 連線 → {"auth":"<配對金鑰>"} → {"ota":{"n":大小,"sha":"<SHA256>"}} → 等螢幕回 {"ota":"rdy"} →
//   直接送韌體的位元組 → 等 {"ota":"ok"}（螢幕驗證 SHA256 與映像檔、切換開機分割區後重開）。
// 失敗時螢幕回 {"ota":"err","why":"..."}，舊版照常開機。這個檔案不依賴 Windows，有 go test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

const otaChunk = 4096

// 螢幕回報的失敗原因 → 設定視窗的說明
func otaWhy(why string) string {
	switch why {
	case "no space", "write":
		return "螢幕寫入失敗，請插 USB 更新"
	case "sha mismatch", "closed", "timeout":
		return "傳送中斷，請再試一次"
	case "image invalid":
		return "螢幕檢查韌體沒通過，請插 USB 更新"
	case "mic on":
		return "螢幕的麥克風開著，請先關掉"
	}
	return "螢幕回報錯誤（" + why + "）"
}

// otaSend：c 是剛連上的 TCP 連線。progress(已送, 全部) 在每一塊送出後呼叫
func otaSend(c net.Conn, wkey string, img []byte, progress func(done, total int)) error {
	r := bufio.NewReader(c)
	// 讀到有 field 這個欄位的那一行（螢幕可能先送別的，例如 WiFi 狀態）
	readMsg := func(field string, timeout time.Duration) (map[string]interface{}, error) {
		c.SetReadDeadline(time.Now().Add(timeout))
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return nil, err
			}
			var m map[string]interface{}
			if json.Unmarshal(bytes.TrimSpace(line), &m) != nil {
				continue
			}
			if _, ok := m[field]; ok {
				return m, nil
			}
		}
	}
	write := func(b []byte, timeout time.Duration) error {
		c.SetWriteDeadline(time.Now().Add(timeout))
		_, err := c.Write(b)
		return err
	}

	if err := write([]byte(`{"auth":"`+wkey+`"}`+"\n"), 5*time.Second); err != nil {
		return fmt.Errorf("連不上螢幕：%w", err)
	}
	if _, err := readMsg("auth", 5*time.Second); err != nil {
		return errors.New("螢幕沒有回應（可能正在跟別台電腦連線，或要插 USB 重新設定 WiFi）")
	}
	sum := sha256.Sum256(img)
	hdr, _ := json.Marshal(map[string]interface{}{"ota": map[string]interface{}{"n": len(img), "sha": hex.EncodeToString(sum[:])}})
	if err := write(append(hdr, '\n'), 5*time.Second); err != nil {
		return fmt.Errorf("傳送失敗：%w", err)
	}
	m, err := readMsg("ota", 10*time.Second)
	if err != nil {
		return errors.New("螢幕沒有回應更新要求")
	}
	if m["ota"] != "rdy" {
		why, _ := m["why"].(string)
		return errors.New(otaWhy(why))
	}
	for off := 0; off < len(img); off += otaChunk {
		end := min(off+otaChunk, len(img))
		// 螢幕每 4 KB 要擦一次 Flash，偶爾也停下來畫進度：一塊給 20 秒
		if err := write(img[off:end], 20*time.Second); err != nil {
			return fmt.Errorf("傳送中斷（%d%%）：%w", off*100/len(img), err)
		}
		if progress != nil {
			progress(end, len(img))
		}
	}
	m, err = readMsg("ota", 30*time.Second)
	if err != nil {
		return errors.New("螢幕沒有回報結果，請看螢幕是否已重新開機")
	}
	if m["ota"] != "ok" {
		why, _ := m["why"].(string)
		return errors.New(otaWhy(why))
	}
	return nil
}
