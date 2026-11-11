package main

// 螢幕的 WiFi（只有 AMOLED，韌體 1.11.0 起；韌體那邊見 firmware/aimon/wifi.h）。
//   設定：設定視窗的 WiFi 分頁經 USB（或已連上的 WiFi）叫螢幕掃描（{"wscan":1}），選好後送 SSID / 密碼 / 配對金鑰（{"wset":...}）。
//         密碼只存在螢幕上（電腦不存）；配對金鑰、螢幕最後回報的 IP 存在 settings.json 的 screens.<id>（wkey / wip / wssid）
//   連線：USB 沒插著時電腦用 TCP 連到螢幕的 47323（wifiscreen_windows.go），第一行送配對金鑰，之後跟 USB 一樣。
//         IP 換了用 mDNS 名稱 aimon-<id 後 6 碼>.local 找（mdnsLookup，自己送查詢）
// 這個檔不依賴 Windows，可以在 CI 上測試

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const wifiPort = 47323

// 掃描到的一個 WiFi
type wifiAP struct {
	SSID string `json:"ssid"`
	RSSI int    `json:"rssi"`
	Sec  int    `json:"sec"` // 0 = 開放、1 = 要密碼、2 = 企業版（帳號密碼登入，螢幕不支援）
}

// 螢幕回報的 WiFi 狀態（{"wst":{...}}）
type wifiState struct {
	St   int    `json:"st"` // 0 沒設定、1 連線中、2 已連線、3 密碼可能不對、4 找不到這個 WiFi
	SSID string `json:"ssid"`
	IP   string `json:"ip,omitempty"`
	RSSI int    `json:"rssi,omitempty"`
	R    int    `json:"r"` // 上次斷線的原因碼（ESP-IDF wifi_err_reason_t），除錯用
}

// 一塊螢幕最近一次掃描
type wifiScan struct {
	Running bool     `json:"running"`
	Err     bool     `json:"err"`
	APs     []wifiAP `json:"aps"`
	T       int64    `json:"t"` // 開始或完成的時間（Unix 秒）
}

// SSID 是附近任何人都能取的名字，會顯示在設定視窗（innerHTML）：去掉控制字元、不合法的 UTF-8，最長 32 位元組（ui.js 也會再跳脫）
func cleanSSID(s string) string {
	s = strings.ToValidUTF8(s, "")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || r == utf8.RuneError {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > 32 {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func cleanIPv4(s string) string {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil || !strings.Contains(s, ".") {
		return ""
	}
	return ip.To4().String()
}

func jsonInt(v interface{}) int {
	f, _ := v.(float64)
	return int(f)
}

func parseWifiState(v interface{}) (wifiState, bool) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return wifiState{}, false
	}
	ws := wifiState{St: jsonInt(m["st"]), R: jsonInt(m["r"])}
	if ws.St < 0 || ws.St > 4 {
		ws.St = 1
	}
	if s, ok := m["ssid"].(string); ok {
		ws.SSID = cleanSSID(s)
	}
	if s, ok := m["ip"].(string); ok {
		ws.IP = cleanIPv4(s)
	}
	if r := jsonInt(m["rssi"]); r >= -127 && r <= 0 {
		ws.RSSI = r
	}
	return ws, true
}

// 螢幕送來的掃描結果：[[ssid, rssi, sec], ...]
func parseWifiScan(v interface{}, failed bool, now int64) wifiScan {
	sc := wifiScan{Err: failed, T: now, APs: []wifiAP{}}
	arr, _ := v.([]interface{})
	for _, e := range arr {
		a, ok := e.([]interface{})
		if !ok || len(a) < 3 || len(sc.APs) >= 30 {
			continue
		}
		s, _ := a[0].(string)
		s = cleanSSID(s)
		if s == "" {
			continue
		}
		ap := wifiAP{SSID: s, RSSI: jsonInt(a[1]), Sec: jsonInt(a[2])}
		if ap.RSSI < -127 || ap.RSSI > 0 {
			ap.RSSI = -100
		}
		if ap.Sec < 0 || ap.Sec > 2 {
			ap.Sec = 1
		}
		sc.APs = append(sc.APs, ap)
	}
	return sc
}

// 設定視窗送來的 SSID / 密碼。ESP32 只支援 WPA 個人版：密碼 8-63 個 ASCII 字元（或 64 個十六進位），開放網路沒有密碼
func validWifiCreds(ssid, pass string) error {
	if ssid == "" || len(ssid) > 32 {
		return errors.New("WiFi 名稱要 1-32 個字元（中文字一個算 3 個）")
	}
	if cleanSSID(ssid) != ssid {
		return errors.New("WiFi 名稱有不支援的字元")
	}
	if pass == "" {
		return nil
	}
	for _, r := range pass {
		if r < 0x20 || r > 0x7e {
			return errors.New("密碼只能是英文、數字、符號")
		}
	}
	if len(pass) == 64 {
		if _, err := hex.DecodeString(pass); err == nil {
			return nil
		}
	}
	if len(pass) < 8 || len(pass) > 63 {
		return errors.New("密碼要 8-63 個字元")
	}
	return nil
}

// 配對金鑰：螢幕只接受送對這把鑰匙的 WiFi 連線（同一個網路裡的其他東西不能改螢幕的畫面、設定）
func newWifiKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// 螢幕的 mDNS 名稱（韌體 wifi.h 用同一個規則）
func wifiHost(id string) string {
	if devID(id) == "" {
		return ""
	}
	return "aimon-" + id[6:]
}

// ---------------- mDNS：用名稱找螢幕（IP 換了的時候）----------------
// Windows 內建的解析不一定會對 .local 送 mDNS（2026-11-11 實測：螢幕有回答，Resolve-DnsName / GetAddrInfo 都說找不到），
// 所以自己送：查詢要求單播回應（QU），回應直接回到送出的埠。Windows 防火牆對「送出多播後 3 秒內的單播回應」預設放行

func mdnsQuery(name string) []byte {
	b := []byte{0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0} // id 0、一個問題
	for _, l := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		b = append(b, byte(len(l)))
		b = append(b, l...)
	}
	return append(b, 0, 0, 1, 0x80, 1) // A、QU + IN
}

// 讀 DNS 封包裡 off 開始的名稱（含壓縮指標），回傳名稱與名稱後面的位置
func dnsName(p []byte, off int) (string, int, bool) {
	var parts []string
	end, jumps := -1, 0
	for {
		if off >= len(p) {
			return "", 0, false
		}
		l := int(p[off])
		switch {
		case l == 0:
			if end < 0 {
				end = off + 1
			}
			return strings.Join(parts, "."), end, true
		case l&0xC0 == 0xC0:
			if off+1 >= len(p) || jumps > 16 {
				return "", 0, false
			}
			if end < 0 {
				end = off + 2
			}
			off = (l&0x3F)<<8 | int(p[off+1])
			jumps++
		default:
			if off+1+l > len(p) {
				return "", 0, false
			}
			parts = append(parts, string(p[off+1:off+1+l]))
			off += 1 + l
		}
	}
}

// 從 mDNS 回應裡找 name 的 A 紀錄（回答與附加紀錄都看）
func mdnsParseA(p []byte, name string) string {
	if len(p) < 12 {
		return ""
	}
	qd, rr := int(p[4])<<8|int(p[5]), (int(p[6])<<8|int(p[7]))+(int(p[8])<<8|int(p[9]))+(int(p[10])<<8|int(p[11]))
	off := 12
	for i := 0; i < qd; i++ {
		_, o, ok := dnsName(p, off)
		if !ok || o+4 > len(p) {
			return ""
		}
		off = o + 4
	}
	for i := 0; i < rr; i++ {
		n, o, ok := dnsName(p, off)
		if !ok || o+10 > len(p) {
			return ""
		}
		typ, ln := int(p[o])<<8|int(p[o+1]), int(p[o+8])<<8|int(p[o+9])
		if o+10+ln > len(p) {
			return ""
		}
		if typ == 1 && ln == 4 && strings.EqualFold(n, strings.TrimSuffix(name, ".")) {
			return net.IP(p[o+10 : o+14]).String()
		}
		off = o + 10 + ln
	}
	return ""
}

// 從每張網卡（IPv4、非 loopback）送查詢，等 timeout 內的第一個回答
func mdnsLookup(name string, timeout time.Duration) string {
	ifs, _ := net.InterfaceAddrs()
	res := make(chan string, len(ifs)+1)
	n := 0
	for _, a := range ifs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.To4() == nil || ipn.IP.IsLoopback() || ipn.IP.IsLinkLocalUnicast() {
			continue
		}
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ipn.IP})
		if err != nil {
			continue
		}
		n++
		go func(c *net.UDPConn) {
			defer c.Close()
			c.WriteTo(mdnsQuery(name), &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
			c.SetReadDeadline(time.Now().Add(timeout))
			buf := make([]byte, 1500)
			for {
				k, _, err := c.ReadFrom(buf)
				if err != nil {
					res <- ""
					return
				}
				if ip := mdnsParseA(buf[:k], name); ip != "" {
					res <- ip
					return
				}
			}
		}(c)
	}
	for ; n > 0; n-- {
		if ip := <-res; ip != "" {
			return ip
		}
	}
	return ""
}

// 送給螢幕的 {"wset":...}；不把 < > & 換成 <（螢幕上的 ArduinoJson 也看得懂，但沒必要）
func wsetLine(ssid, pass, key string) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(map[string]interface{}{"wset": map[string]string{"s": ssid, "p": pass, "k": key}})
	return buf.Bytes() // Encode 會加上換行
}
