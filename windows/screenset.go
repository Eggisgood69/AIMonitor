package main

import "strconv"

// 每塊螢幕自己的設定（方向、亮度、外接 LED、自動轉向）。沒設的項目用共用設定（Settings 的 Rotation / Brightness / Led / NoAutoRot），
// 所以新插上的螢幕一開始跟以前一樣。以螢幕回報的 id（晶片 MAC，韌體 1.10.6 起）當 key，換 USB 孔也認得；舊韌體沒有 id 就用埠名稱
type ScreenSet struct {
	Bri  *int  `json:"bri,omitempty"`
	Rot  *int  `json:"rot,omitempty"`
	Led  *int  `json:"led,omitempty"`
	ARot *bool `json:"arot,omitempty"`
	Mic  *bool `json:"mic,omitempty"` // USB 麥克風（只有 AMOLED 有），沒有共用值：沒設過 = 關閉
	MG   *int  `json:"mg,omitempty"`  // 麥克風增益（ES7210 的 0-14，見 micGainDefault），沒設過 = 預設
	// WiFi（只有 AMOLED，wifi.go）：配對金鑰（空白 = 沒設定 WiFi）、螢幕最後回報的 IP、WiFi 名稱（顯示用）。密碼只存在螢幕上
	WKey  string `json:"wkey,omitempty"`
	WIP   string `json:"wip,omitempty"`
	WSSID string `json:"wssid,omitempty"`
}

// 麥克風增益預設 10 = 30 dB（0-10 每階 3 dB，11 = 33、12 = 34.5、13 = 36、14 = 37.5 dB）
const micGainDefault = 10

// 一塊螢幕實際用的值
type screenEff struct {
	Bri  int  `json:"bri"`
	Rot  int  `json:"rot"`
	Led  int  `json:"led"`
	ARot bool `json:"arot"`
	Mic  bool `json:"mic"`
	MG   int  `json:"mg"`
}

// 共用值 d 套上這塊螢幕自己的設定
func (d screenEff) with(s *ScreenSet) screenEff {
	if s == nil {
		return d
	}
	if s.Bri != nil {
		d.Bri = *s.Bri
	}
	if s.Rot != nil {
		d.Rot = *s.Rot & 3
	}
	if s.Led != nil {
		d.Led = *s.Led
	}
	if s.ARot != nil {
		d.ARot = *s.ARot
	}
	if s.Mic != nil {
		d.Mic = *s.Mic
	}
	if s.MG != nil {
		d.MG = *s.MG
	}
	return d
}

// 設定一塊螢幕的一個項目（設定視窗的 scr:<key>:<項目>:<值>）。值不合法就不改、回傳 false。
// bri：5-255；led：0-100；rot：0-3 或 auto（自動轉向，只有 AMOLED 有用；選固定方向時關掉自動轉向）；mic：0 / 1；mg：0-14
func applyScreenSetting(m map[string]*ScreenSet, key, k, v string) bool {
	if key == "" {
		return false
	}
	s := m[key]
	if s == nil {
		s = &ScreenSet{}
	}
	n, err := strconv.Atoi(v)
	switch {
	case k == "bri" && err == nil && n >= 5 && n <= 255:
		s.Bri = &n
	case k == "led" && err == nil && n >= 0 && n <= 100:
		s.Led = &n
	case k == "rot" && v == "auto":
		t := true
		s.ARot = &t
	case k == "mg" && err == nil && n >= 0 && n <= 14:
		s.MG = &n
	case k == "mic" && (v == "0" || v == "1"):
		on := v == "1"
		s.Mic = &on
	case k == "rot" && err == nil && n >= 0 && n <= 3:
		f := false
		s.Rot, s.ARot = &n, &f
	default:
		return false
	}
	m[key] = s
	return true
}
