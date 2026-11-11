package main

// 經 WiFi 連到螢幕（只有 AMOLED，wifi.go 有整體說明）：
//   設定過 WiFi 的螢幕（settings.json screens.<id>.wkey）沒插 USB 時，每幾秒試著用 TCP 連到它記下的 IP，
//   連不上就用 mDNS 名稱（aimon-<id 後 6 碼>.local）找。連上後第一行送配對金鑰，之後跟 USB 共用 serveScreen。
//   同一塊螢幕插上 USB 就改用 USB（WiFi 那條斷掉），拔掉後幾秒內再接回 WiFi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TCP 連線包成 screenLink：Read 最多等 60 ms（跟序列埠一樣），逾時當作沒資料
type tcpLink struct{ c net.Conn }

func (t tcpLink) Write(b []byte) error {
	t.c.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err := t.c.Write(b)
	return err
}

func (t tcpLink) Read(b []byte) (int, error) {
	t.c.SetReadDeadline(time.Now().Add(60 * time.Millisecond))
	n, err := t.c.Read(b)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return n, nil
	}
	return n, err
}

var (
	wifiMu      sync.Mutex
	wifiRunning = map[string]bool{}      // 這塊螢幕正在 WiFi 連線中（或正在試）
	wifiNext    = map[string]time.Time{} // 連不上時多久後再試
	wifiFails   = map[string]int{}
	wifiErr     = map[string]string{}    // 上次連不上的原因（設定視窗顯示）
	usbSeen     = map[string]time.Time{} // 每塊螢幕最後一次有 USB 連線的時間
)

// USB 剛消失的這段時間先不從 WiFi 連：打開 / 關掉麥克風時螢幕會換一個 COM 埠，中間約 1-2 秒看不到 USB，
// 以前 WiFi 會趁這個空檔連上、馬上又因為 USB 回來而斷掉
const usbGrace = 10 * time.Second

// 這塊螢幕（id）現在有 USB 連線（已回應 hello）
func usbHasID(id string) bool {
	serMu.Lock()
	defer serMu.Unlock()
	for _, sc := range screens {
		if !sc.stat.Net && sc.stat.Verified && sc.stat.ID == id {
			return true
		}
	}
	return false
}

func fwUpdating() bool {
	fwMu.Lock()
	defer fwMu.Unlock()
	return fwRunning
}

func wifiLoop() {
	for {
		time.Sleep(3 * time.Second)
		type target struct{ key, wkey, ip string }
		var ts []target
		settingsMu.Lock()
		for k, s := range settings.Screens {
			if s != nil && s.WKey != "" && devID(k) != "" {
				ts = append(ts, target{k, s.WKey, s.WIP})
			}
		}
		settingsMu.Unlock()
		if fwUpdating() { // 燒錄中：螢幕會重開好幾次，不要從 WiFi 插進來
			continue
		}
		for _, t := range ts {
			onUSB := usbHasID(t.key)
			wifiMu.Lock()
			if onUSB {
				usbSeen[t.key] = time.Now()
			}
			skip := onUSB || time.Since(usbSeen[t.key]) < usbGrace || wifiRunning[t.key] || time.Now().Before(wifiNext[t.key])
			if !skip {
				wifiRunning[t.key] = true
			}
			wifiMu.Unlock()
			if !skip {
				go wifiConnect(t.key, t.wkey, t.ip)
			}
		}
	}
}

// 先試記下的 IP，不行再用 mDNS 名稱找（DHCP 換了 IP）
func wifiDial(key, ip string) (net.Conn, string, error) {
	try := func(addr string) (net.Conn, error) {
		return net.DialTimeout("tcp", net.JoinHostPort(addr, strconv.Itoa(wifiPort)), 2*time.Second)
	}
	if ip != "" {
		if c, err := try(ip); err == nil {
			return c, ip, nil
		}
	}
	if h := wifiHost(key); h != "" {
		// 自己送 mDNS 查詢（Windows 內建的解析實測找不到），不行再問系統（路由器的 DNS 可能也認得螢幕的主機名稱）
		cands := []string{mdnsLookup(h+".local", 1500*time.Millisecond)}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		addrs, _ := net.DefaultResolver.LookupIPAddr(ctx, h)
		cancel()
		for _, a := range addrs {
			if v4 := a.IP.To4(); v4 != nil {
				cands = append(cands, v4.String())
			}
		}
		for _, a := range cands {
			if a != "" && a != ip {
				if c, err := try(a); err == nil {
					return c, a, nil
				}
			}
		}
	}
	return nil, "", errors.New("找不到螢幕")
}

func wifiConnect(key, wkey, ip string) {
	defer func() {
		wifiMu.Lock()
		delete(wifiRunning, key)
		wifiMu.Unlock()
	}()
	c, addr, err := wifiDial(key, ip)
	if err != nil {
		wifiMu.Lock()
		n := wifiFails[key]
		wifiFails[key] = n + 1
		d := 10 * time.Second << min(n, 3) // 10、20、40、80 秒
		if d > time.Minute {
			d = time.Minute
		}
		wifiNext[key] = time.Now().Add(d)
		wifiErr[key] = "找不到螢幕：確認螢幕有接電、跟電腦在同一個網路"
		wifiMu.Unlock()
		return
	}
	if addr != ip {
		settingsMu.Lock()
		if s := settings.Screens[key]; s != nil && s.WKey == wkey {
			s.WIP = addr
		}
		settingsMu.Unlock()
		saveSettings()
	}
	verified, why := runWifiScreen(key, wkey, addr, c)
	wifiMu.Lock()
	if verified {
		wifiFails[key] = 0
		delete(wifiErr, key)
		wifiNext[key] = time.Now().Add(2 * time.Second)
	} else { // 連上了卻沒回應：配對金鑰不對（螢幕被別台電腦設定過、或 NVS 清掉了）
		wifiFails[key]++
		wifiNext[key] = time.Now().Add(30 * time.Second)
		wifiErr[key] = "螢幕不接受這台電腦（" + why + "），請插 USB 重新設定 WiFi"
	}
	wifiMu.Unlock()
}

// 一條 WiFi 連線：送配對金鑰、打招呼，之後跟 USB 一樣。回傳螢幕有沒有回應過
func runWifiScreen(key, wkey, ip string, c net.Conn) (bool, string) {
	defer c.Close()
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(15 * time.Second)
	}
	link := tcpLink{c}
	port := "WiFi " + ip
	if err := link.Write([]byte(`{"auth":"` + wkey + `"}` + "\n" + `{"hello":1}` + "\n")); err != nil {
		return false, "write: " + err.Error()
	}
	logf("connected %s (screen %s)", port, key)
	// ID 先填上：回應 hello 之前送的畫面也用這塊螢幕自己的方向、亮度
	sc := &screenConn{stat: serialStatus{Connected: true, Port: port, Net: true, IP: ip, ID: key, HW: "s3a175"}, out: make(chan []byte, 16)}
	mapKey := "wifi:" + key
	serMu.Lock()
	screens[mapKey] = sc
	serMu.Unlock()
	sendFrameNow()
	why, openedAt := serveScreen(sc, link, port, false, func() string {
		if usbHasID(key) {
			return "screen is on USB now"
		}
		serMu.Lock()
		v, id := sc.stat.Verified, sc.stat.ID
		serMu.Unlock()
		if v && id != key {
			return "a different screen answered at this address"
		}
		return ""
	})
	serMu.Lock()
	verified := sc.stat.Verified
	if screens[mapKey] == sc {
		delete(screens, mapKey)
	}
	serMu.Unlock()
	logf("disconnected %s after %s (%s)", port, time.Since(openedAt).Round(time.Second), why)
	return verified, why
}

// 螢幕回報 WiFi 狀態（{"wst":...}）：記在連線狀態上；連上了就記下 IP（之後沒插 USB 時連這個 IP）
func noteWifiState(sc *screenConn, v interface{}) {
	ws, ok := parseWifiState(v)
	if !ok {
		return
	}
	serMu.Lock()
	sc.stat.WiFi = &ws
	id := sc.stat.ID
	serMu.Unlock()
	if ws.St != 2 || ws.IP == "" || devID(id) == "" {
		return
	}
	settingsMu.Lock()
	s := settings.Screens[id]
	changed := s != nil && s.WKey != "" && (s.WIP != ws.IP || s.WSSID != ws.SSID)
	if changed {
		s.WIP, s.WSSID = ws.IP, ws.SSID
	}
	settingsMu.Unlock()
	if changed {
		saveSettings()
	}
}

// 這塊螢幕的連線：USB 優先
func screenFor(key string) *screenConn {
	serMu.Lock()
	defer serMu.Unlock()
	var w *screenConn
	for _, sc := range screens {
		if !sc.stat.Verified || sc.stat.Key() != key {
			continue
		}
		if !sc.stat.Net {
			return sc
		}
		w = sc
	}
	return w
}

// 設定視窗的 WiFi 指令：wscan:<key>、wifi:{"key","ssid","pass"}、wforget:<key>。
// 回傳 "ok" 或 "err:<原因>"；第二個值 = 是不是 WiFi 指令
func wifiCmd(cmd string) (string, bool) {
	switch {
	case strings.HasPrefix(cmd, "wscan:"):
		sc := screenFor(strings.TrimPrefix(cmd, "wscan:"))
		if sc == nil {
			return "err:螢幕沒有連線", true
		}
		serMu.Lock()
		sc.stat.Scan = &wifiScan{Running: true, T: time.Now().Unix(), APs: []wifiAP{}}
		serMu.Unlock()
		enqueue(sc, []byte("{\"wscan\":1}\n"))
		return "ok", true

	case strings.HasPrefix(cmd, "wifi:"):
		var r struct{ Key, SSID, Pass string }
		if json.Unmarshal([]byte(strings.TrimPrefix(cmd, "wifi:")), &r) != nil || devID(r.Key) == "" {
			return "err:資料格式不對", true
		}
		if err := validWifiCreds(r.SSID, r.Pass); err != nil {
			return "err:" + err.Error(), true
		}
		sc := screenFor(r.Key)
		if sc == nil {
			return "err:螢幕沒有連線，請用 USB 插上螢幕", true
		}
		settingsMu.Lock()
		if settings.Screens == nil {
			settings.Screens = map[string]*ScreenSet{}
		}
		s := settings.Screens[r.Key]
		if s == nil {
			s = &ScreenSet{}
			settings.Screens[r.Key] = s
		}
		if s.WKey == "" {
			s.WKey = newWifiKey()
		}
		s.WSSID, s.WIP = r.SSID, "" // 換了網路，IP 等螢幕連上後回報
		wkey := s.WKey
		settingsMu.Unlock()
		saveSettings()
		wifiMu.Lock()
		delete(wifiErr, r.Key)
		wifiFails[r.Key] = 0
		delete(wifiNext, r.Key)
		wifiMu.Unlock()
		serMu.Lock()
		if sc.stat.WiFi != nil { // 等螢幕回報新的狀態前先顯示「連線中」
			sc.stat.WiFi = &wifiState{St: 1, SSID: r.SSID}
		}
		serMu.Unlock()
		enqueue(sc, wsetLine(r.SSID, r.Pass, wkey)) // 密碼不寫進紀錄、也不存在電腦上
		logf("wifi: sent new network settings to screen %s", r.Key)
		return "ok", true

	case strings.HasPrefix(cmd, "wforget:"):
		key := strings.TrimPrefix(cmd, "wforget:")
		if sc := screenFor(key); sc != nil {
			enqueue(sc, wsetLine("", "", ""))
		}
		settingsMu.Lock()
		if s := settings.Screens[key]; s != nil {
			s.WKey, s.WIP, s.WSSID = "", "", ""
		}
		settingsMu.Unlock()
		saveSettings()
		wifiMu.Lock()
		delete(wifiErr, key)
		wifiMu.Unlock()
		logf("wifi: forgot network on screen %s", key)
		return "ok", true
	}
	return "", false
}

// 設定視窗用：設定過 WiFi 的螢幕
type wifiConf struct {
	Key  string `json:"key"`
	SSID string `json:"ssid"`
	IP   string `json:"ip"`
	Err  string `json:"err,omitempty"`
}

func wifiStatusList() []wifiConf {
	var out []wifiConf
	settingsMu.Lock()
	for k, s := range settings.Screens {
		if s != nil && s.WKey != "" {
			out = append(out, wifiConf{Key: k, SSID: s.WSSID, IP: s.WIP})
		}
	}
	settingsMu.Unlock()
	wifiMu.Lock()
	for i := range out {
		out[i].Err = wifiErr[out[i].Key]
	}
	wifiMu.Unlock()
	return out
}
