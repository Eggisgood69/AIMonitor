package main

import "testing"

func TestPickFirmwareTarget(t *testing.T) {
	cur := fwVersion()
	c3 := serialStatus{Connected: true, Verified: true, Port: "COM5", FW: cur}
	s3 := serialStatus{Connected: true, Verified: true, Port: "COM8", FW: "1.0.0", HW: "s3a175"}
	other := serialStatus{Connected: true, Port: "COM3"} // 不回應的其他 USB 裝置

	// 沒指定：選需要更新的那個，不會選到不回應的埠
	if got, err := pickFirmwareTarget("", []serialStatus{other, c3, s3}); err != nil || got.Port != "COM8" {
		t.Fatalf("auto pick = %v, %v", got.Port, err)
	}
	// 指定的螢幕已是最新也可以重新寫入
	if got, err := pickFirmwareTarget("COM5", []serialStatus{c3, s3}); err != nil || got.Port != "COM5" {
		t.Fatalf("explicit pick = %v, %v", got.Port, err)
	}
	// 好幾個螢幕都是最新：不知道要寫哪個
	if _, err := pickFirmwareTarget("", []serialStatus{c3, {Verified: true, Port: "COM9", FW: cur, HW: "s3a175"}}); err == nil {
		t.Fatal("all up to date should be an error")
	}
	// 只有一個最新的螢幕：重新寫入它
	if got, err := pickFirmwareTarget("", []serialStatus{c3}); err != nil || got.Port != "COM5" {
		t.Fatalf("single screen = %v, %v", got.Port, err)
	}
	if _, err := pickFirmwareTarget("COM3", []serialStatus{other}); err == nil {
		t.Fatal("unverified port must not be flashed")
	}
	// 舊韌體不回報型號 = C3
	if (serialStatus{}).Board() != "c3" || s3.Board() != "s3a175" {
		t.Fatal("Board()")
	}
	for hw, img := range fwImages {
		want := map[string]int{"c3": chipC3, "s3a175": chipS3}[hw]
		if imageChip(img) != want {
			t.Fatalf("bundled firmware for %s is for chip %d", hw, imageChip(img))
		}
	}
}
