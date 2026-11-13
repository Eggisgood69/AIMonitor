package main

// 自訂動畫（取代 AMOLED 主畫面的 Clawd）：使用者選的影片轉成 animW×animH 的 RGB565 畫格，存在 %LOCALAPPDATA%\AIMonitor\anims：
//   <id>.raw  全部畫格（RGB565 little-endian，跟韌體 anim.h 一樣）
//   <id>.json 名稱、格數、每格 ms、CRC32、預覽圖
// 選了哪一份記在 settings.Anim（空的 = 原作者的 Clawd）。畫面的 "av" 送那份的 CRC32；螢幕上沒有就整份傳過去（anim_windows.go）。

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	animW, animH  = 140, 114 // 跟韌體 ui.h 的 ANIM_W / ANIM_H 一樣
	animFPS       = 15
	animMaxFrames = 120 // 8 秒（每份約 3.8 MB，螢幕的 PSRAM 放得下）
	animFrameLen  = animW * animH * 2
)

type AnimInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	N       int    `json:"n"`
	Ms      int    `json:"ms"`
	CRC     uint32 `json:"crc"`
	Preview string `json:"preview"` // 第一格的 PNG（data URL）
	Added   int64  `json:"added"`
}

var animMu sync.Mutex // 動畫資料夾

func animDir(base string) string { return filepath.Join(base, "anims") }

// 收進動畫庫：raw 是全部畫格
func saveAnim(dir, name string, raw []byte, added int64) (AnimInfo, error) {
	n := len(raw) / animFrameLen
	if n == 0 {
		return AnimInfo{}, fmt.Errorf("沒有讀到畫面")
	}
	if n > animMaxFrames {
		n = animMaxFrames
	}
	raw = raw[:n*animFrameLen]
	crc := crc32.ChecksumIEEE(raw)
	info := AnimInfo{ID: fmt.Sprintf("%08x", crc), Name: name, N: n, Ms: 1000 / animFPS, CRC: crc, Preview: animPreview(raw[:animFrameLen]), Added: added}
	animMu.Lock()
	defer animMu.Unlock()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return info, err
	}
	if err := os.WriteFile(filepath.Join(dir, info.ID+".raw"), raw, 0o644); err != nil {
		return info, err
	}
	b, _ := json.MarshalIndent(info, "", "  ")
	return info, os.WriteFile(filepath.Join(dir, info.ID+".json"), b, 0o644)
}

func listAnims(dir string) []AnimInfo {
	animMu.Lock()
	defer animMu.Unlock()
	fs, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	out := []AnimInfo{}
	for _, f := range fs {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var a AnimInfo
		if json.Unmarshal(b, &a) == nil && a.ID != "" {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Added < out[j].Added })
	return out
}

func findAnim(dir, id string) (AnimInfo, bool) {
	for _, a := range listAnims(dir) {
		if a.ID == id {
			return a, true
		}
	}
	return AnimInfo{}, false
}

func loadAnimRaw(dir string, a AnimInfo) ([]byte, error) {
	animMu.Lock()
	defer animMu.Unlock()
	b, err := os.ReadFile(filepath.Join(dir, a.ID+".raw"))
	if err != nil {
		return nil, err
	}
	if len(b) != a.N*animFrameLen || crc32.ChecksumIEEE(b) != a.CRC {
		return nil, fmt.Errorf("動畫檔損毀")
	}
	return b, nil
}

func deleteAnim(dir, id string) {
	if strings.ContainsAny(id, `/\.`) || id == "" {
		return
	}
	animMu.Lock()
	defer animMu.Unlock()
	os.Remove(filepath.Join(dir, id+".raw"))
	os.Remove(filepath.Join(dir, id+".json"))
}

// 一格 RGB565 → PNG data URL（設定視窗的預覽）
func animPreview(frame []byte) string {
	img := image.NewRGBA(image.Rect(0, 0, animW, animH))
	for i := 0; i < animW*animH; i++ {
		v := uint16(frame[2*i]) | uint16(frame[2*i+1])<<8
		r, g, b := uint8(v>>11&31), uint8(v>>5&63), uint8(v&31)
		img.Set(i%animW, i/animW, color.RGBA{r<<3 | r>>2, g<<2 | g>>4, b<<3 | b>>2, 255})
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// 傳給螢幕的開頭那一行
func animHeader(a AnimInfo) string {
	return fmt.Sprintf(`{"anim":{"w":%d,"h":%d,"n":%d,"ms":%d,"crc":%d,"len":%d}}`+"\n", animW, animH, a.N, a.Ms, a.CRC, a.N*animFrameLen)
}
