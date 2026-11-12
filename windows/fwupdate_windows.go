package main

// 一鍵更新螢幕韌體：程式內建每種板子的最新韌體，透過 ESP32 晶片內建的燒錄協定寫入。

import (
	_ "embed"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed fw/aimon-c3.bin
var fwImageC3 []byte

//go:embed fw/aimon-s3a175.bin
var fwImageS3A175 []byte

// 板子型號（螢幕 hello 回報的 hw）→ 韌體
var fwImages = map[string][]byte{
	"c3":     fwImageC3,     // ESP32-2424S012（1.28 吋 LCD）
	"s3a175": fwImageS3A175, // Waveshare ESP32-S3-Touch-AMOLED-1.75
}

//go:embed fw/version.txt
var fwVersionRaw string

func fwVersion() string { return strings.TrimSpace(fwVersionRaw) }

var (
	fwMu       sync.Mutex
	fwRunning  bool
	fwPort     string // 正在更新的埠：這個埠的一般連線要讓出來
	fwProgress int
	fwMsg      string
	fwErr      string
	fwMicOff   = map[string]bool{} // 更新中的螢幕（key）：麥克風先關掉
	fwDoneAt   time.Time           // 上次更新完成：「更新完成」只顯示一下，之後設定視窗回到每塊螢幕的版本與按鈕
)

func micHeldOff(key string) bool {
	fwMu.Lock()
	defer fwMu.Unlock()
	return fwMicOff[key]
}

func setMicHeldOff(key string, off bool) {
	fwMu.Lock()
	if off {
		fwMicOff[key] = true
	} else {
		delete(fwMicOff, key)
	}
	fwMu.Unlock()
}

// 螢幕在麥克風模式（TinyUSB）時燒錄協定進不去：先叫它關掉麥克風、重開回一般 USB（埠會換一個），
// 等同一塊螢幕（同一個 id）從一般 USB 的埠回應了，再對那個埠燒錄
func micOffForUpdate(target serialStatus) (serialStatus, error) {
	key := target.Key()
	setMicHeldOff(key, true)
	setFw(1, "關閉麥克風，等螢幕重新開機…")
	logf("firmware update: turning off mic on %s first", target.Port)
	sendFrameNow()
	for i := 0; i < 60; i++ {
		time.Sleep(500 * time.Millisecond)
		for _, s := range screenList() {
			if s.Key() == key && s.Verified && !s.Mic && !s.Net {
				return s, nil
			}
		}
		if i%10 == 9 {
			sendFrameNow()
		}
	}
	return target, errors.New("螢幕沒有從麥克風模式回來，請拔掉 USB 再插上後重試")
}

func fwPausedPort() string {
	fwMu.Lock()
	defer fwMu.Unlock()
	return fwPort
}

// port 空白 = 自動選：唯一的螢幕，或第一個需要更新的螢幕
func startFirmwareUpdate(port string) {
	fwMu.Lock()
	if fwRunning {
		fwMu.Unlock()
		return
	}
	fwRunning, fwProgress, fwMsg, fwErr = true, 0, "準備中…", ""
	fwMu.Unlock()
	go func() {
		err := doFirmwareUpdate(port)
		fwMu.Lock()
		fwRunning = false
		if err != nil {
			fwErr = err.Error()
			fwMsg = ""
			logf("firmware update failed: %v", err)
		} else {
			fwMsg = "更新完成，螢幕已重新開機"
			fwProgress = 100
			fwDoneAt = time.Now()
			logf("firmware updated to %s on %s", fwVersion(), fwPort)
		}
		fwMu.Unlock()
		if err == nil {
			// 螢幕重開機時 USB 會重新列舉好幾次：等它開機完成再連，免得連上又斷
			time.Sleep(3 * time.Second)
		}
		fwMu.Lock()
		fwPort = ""
		fwMu.Unlock()
	}()
}

func setFw(p int, msg string) {
	fwMu.Lock()
	fwProgress, fwMsg = p, msg
	fwMu.Unlock()
}

// 選要更新的螢幕：電腦上可能有好幾個序列埠（其他 USB 裝置、好幾個螢幕），只用已經和螢幕連線、而且有回應的
func pickFirmwareTarget(port string, scs []serialStatus) (serialStatus, error) {
	var cand []serialStatus
	for _, s := range scs {
		// WiFi 連線要韌體 1.12.0 起（ota.go）；更舊的要插 USB 更新一次
		if s.Verified && (!s.Net || s.OTA) && (port == "" || s.Port == port) {
			cand = append(cand, s)
		}
	}
	if len(cand) == 0 {
		for _, s := range scs {
			if s.Verified && s.Net && !s.OTA && (port == "" || s.Port == port) {
				return serialStatus{}, errors.New("這塊螢幕的韌體還不能經 WiFi 更新，請插 USB 更新一次")
			}
		}
		if port != "" {
			return serialStatus{}, errors.New(port + " 的螢幕目前沒有連線，請確認螢幕已連線後再試")
		}
		return serialStatus{}, errors.New("螢幕目前沒有連線，請確認 USB 已插上、螢幕已連線後再試")
	}
	for _, s := range cand {
		if versionLess(s.FW, fwVersion()) {
			return s, nil
		}
	}
	if port == "" && len(cand) > 1 {
		return serialStatus{}, errors.New("所有螢幕都已是最新版")
	}
	return cand[0], nil // 指定的螢幕：已是最新也重新寫入
}

func doFirmwareUpdate(port string) error {
	target, err := pickFirmwareTarget(port, screenList())
	if err != nil {
		return err
	}
	if target.Net {
		return wifiFirmwareUpdate(target)
	}
	if target.Mic {
		defer setMicHeldOff(target.Key(), false) // 更新完螢幕重開，照設定再開麥克風
		if target, err = micOffForUpdate(target); err != nil {
			return err
		}
	}
	img := fwImages[target.Board()]
	if len(img) == 0 {
		return errors.New("沒有這種螢幕（" + target.Board() + "）的韌體")
	}
	var pi *portInfo
	for _, p := range findDisplayPortInfos() {
		if p.Name == target.Port {
			p := p
			pi = &p
		}
	}
	if pi == nil {
		return errors.New("找不到 " + target.Port + "，請確認 USB 已插上")
	}
	// 請這個埠的一般連線讓出序列埠（其他螢幕照常顯示）
	fwMu.Lock()
	fwPort = pi.Name
	fwMu.Unlock()
	for i := 0; i < 30; i++ {
		serMu.Lock()
		_, c := screens[pi.Name]
		serMu.Unlock()
		if !c {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	setFw(2, "連線到 "+pi.Name+"…")
	var sp *serialPort
	for i := 0; i < 10; i++ {
		if sp, err = openSerial(pi.Name); err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err != nil {
		return errors.New("無法開啟 " + pi.Name + "：" + err.Error() + "（燒錄工具或其他程式正在使用？）")
	}
	defer sp.Close()
	setFw(5, "進入燒錄模式…")
	return FlashApp(sp, pi.VID == "VID_303A", img, func(done, total int) {
		setFw(5+done*93/total, "寫入中…")
	}, logf)
}

// 經 WiFi 更新（ota.go）：請 WiFi 那條一般連線讓出來，另開一條連線送韌體
func wifiFirmwareUpdate(target serialStatus) error {
	img := fwImages[target.Board()]
	if len(img) == 0 {
		return errors.New("沒有這種螢幕（" + target.Board() + "）的韌體")
	}
	key := target.ID
	settingsMu.Lock()
	wkey := ""
	if s := settings.Screens[key]; s != nil {
		wkey = s.WKey
	}
	settingsMu.Unlock()
	if wkey == "" || target.IP == "" {
		return errors.New("找不到這塊螢幕的 WiFi 設定，請插 USB 更新")
	}
	fwMu.Lock()
	fwPort = target.Port
	fwMu.Unlock()
	for i := 0; i < 50; i++ {
		serMu.Lock()
		_, c := screens["wifi:"+key]
		serMu.Unlock()
		if !c {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	setFw(2, "連線到螢幕…")
	var c net.Conn
	var err error
	addr := net.JoinHostPort(target.IP, strconv.Itoa(wifiPort))
	for i := 0; i < 5; i++ { // 螢幕要看到舊連線斷了才收新的（最多約 20 ms），保險起見多試幾次
		if c, err = net.DialTimeout("tcp", addr, 3*time.Second); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		return errors.New("連不上螢幕（" + target.IP + "），請確認螢幕有接電")
	}
	defer c.Close()
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}
	logf("firmware update over wifi: sending %d bytes to %s", len(img), target.Port)
	setFw(5, "經 WiFi 傳送中…")
	err = otaSend(c, wkey, img, func(done, total int) {
		if done < total {
			setFw(5+done*90/total, "經 WiFi 傳送中…")
		} else {
			setFw(96, "螢幕檢查韌體、重新開機…")
		}
	})
	if err == nil { // 螢幕重開、連上 WiFi 約 5 秒：早點再連，不用等退避
		wifiMu.Lock()
		wifiFails[key] = 0
		wifiNext[key] = time.Now().Add(4 * time.Second)
		wifiMu.Unlock()
	}
	return err
}

func firmwareStatus() map[string]interface{} {
	fwMu.Lock()
	defer fwMu.Unlock()
	msg := fwMsg
	if !fwRunning && fwErr == "" && time.Since(fwDoneAt) > 15*time.Second { // 以前一直停在「更新完成」，要重開程式才看得到「重新寫入」
		msg = ""
	}
	return map[string]interface{}{
		"bundled": fwVersion(), "running": fwRunning, "port": fwPort, "progress": fwProgress, "msg": msg, "err": fwErr,
	}
}
