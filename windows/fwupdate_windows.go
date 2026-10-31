package main

// 一鍵更新螢幕韌體：程式內建每種板子的最新韌體，透過 ESP32 晶片內建的燒錄協定寫入。

import (
	_ "embed"
	"errors"
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
)

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
		if s.Verified && (port == "" || s.Port == port) {
			cand = append(cand, s)
		}
	}
	if len(cand) == 0 {
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

func firmwareStatus() map[string]interface{} {
	fwMu.Lock()
	defer fwMu.Unlock()
	return map[string]interface{}{
		"bundled": fwVersion(), "running": fwRunning, "port": fwPort, "progress": fwProgress, "msg": fwMsg, "err": fwErr,
	}
}
