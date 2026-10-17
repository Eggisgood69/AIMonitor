package main

// 一鍵更新螢幕韌體：程式內建最新韌體，透過 ESP32 晶片內建的燒錄協定寫入。

import (
	_ "embed"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed fw/aimon-app.bin
var fwImage []byte

//go:embed fw/version.txt
var fwVersionRaw string

func fwVersion() string { return strings.TrimSpace(fwVersionRaw) }

var (
	pauseSerial atomic.Bool
	fwMu        sync.Mutex
	fwRunning   bool
	fwProgress  int
	fwMsg       string
	fwErr       string
)

func startFirmwareUpdate() {
	fwMu.Lock()
	if fwRunning {
		fwMu.Unlock()
		return
	}
	fwRunning, fwProgress, fwMsg, fwErr = true, 0, "準備中…", ""
	fwMu.Unlock()
	go func() {
		err := doFirmwareUpdate()
		fwMu.Lock()
		fwRunning = false
		if err != nil {
			fwErr = err.Error()
			fwMsg = ""
			logf("firmware update failed: %v", err)
		} else {
			fwMsg = "更新完成，螢幕已重新開機"
			fwProgress = 100
			logf("firmware updated to %s", fwVersion())
		}
		fwMu.Unlock()
		if err == nil {
			// 螢幕重開機時 USB 會重新列舉好幾次：等它開機完成再連，免得連上又斷
			time.Sleep(3 * time.Second)
		}
		pauseSerial.Store(false)
	}()
}

func setFw(p int, msg string) {
	fwMu.Lock()
	fwProgress, fwMsg = p, msg
	fwMu.Unlock()
}

func doFirmwareUpdate() error {
	ports := findDisplayPortInfos()
	if len(ports) == 0 {
		return errors.New("找不到螢幕，請確認 USB 已插上")
	}
	// 電腦上可能有好幾個序列埠（例如其他 USB 裝置）：用目前已經和螢幕連線、而且有回應的那一個
	serMu.Lock()
	cur, verified := serStat.Port, serStat.Verified
	serMu.Unlock()
	target := -1
	for i, p := range ports {
		if p.Name == cur {
			target = i
		}
	}
	if target < 0 {
		if len(ports) > 1 {
			return errors.New("偵測到多個序列埠，但螢幕目前沒有連線，無法判斷是哪一個。請確認螢幕已連線後再試")
		}
		target = 0
	}
	_ = verified
	pi := ports[target]
	// 請一般連線讓出序列埠
	pauseSerial.Store(true)
	for i := 0; i < 30; i++ {
		serMu.Lock()
		c := serStat.Connected
		serMu.Unlock()
		if !c {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	setFw(2, "連線到 "+pi.Name+"…")
	var sp *serialPort
	var err error
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
	return FlashApp(sp, pi.VID == "VID_303A", fwImage, func(done, total int) {
		setFw(5+done*93/total, "寫入中…")
	}, logf)
}

func firmwareStatus() map[string]interface{} {
	fwMu.Lock()
	defer fwMu.Unlock()
	return map[string]interface{}{
		"bundled": fwVersion(), "running": fwRunning, "progress": fwProgress, "msg": fwMsg, "err": fwErr,
	}
}
