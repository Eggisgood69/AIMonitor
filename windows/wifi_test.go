package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCleanSSID(t *testing.T) {
	cases := map[string]string{
		"MyHome":                       "MyHome",
		"a\x00b\nc\x7f":                "abc",
		"<img src=x onerror=alert(1)>": "<img src=x onerror=alert(1)>", // 跳脫交給 ui.js 的 H()
		"bad\xffutf8":                  "badutf8",
		strings.Repeat("家", 12):        strings.Repeat("家", 10), // 32 位元組內、不切斷一個字
	}
	for in, want := range cases {
		if got := cleanSSID(in); got != want {
			t.Errorf("cleanSSID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseWifi(t *testing.T) {
	var m map[string]interface{}
	json.Unmarshal([]byte(`{"wst":{"st":2,"ssid":"Home\u0007","ip":"192.168.1.23","rssi":-52,"r":0}}`), &m)
	ws, ok := parseWifiState(m["wst"])
	if !ok || ws.St != 2 || ws.SSID != "Home" || ws.IP != "192.168.1.23" || ws.RSSI != -52 {
		t.Fatalf("parseWifiState = %+v %v", ws, ok)
	}
	json.Unmarshal([]byte(`{"wst":{"st":9,"ip":"::1"}}`), &m)
	if ws, _ := parseWifiState(m["wst"]); ws.St != 1 || ws.IP != "" {
		t.Fatalf("bad state not sanitized: %+v", ws)
	}
	json.Unmarshal([]byte(`{"wscan":[["A",-40,1],["",-50,0],["B",-70,0],["C",5,7],"x"]}`), &m)
	sc := parseWifiScan(m["wscan"], false, 100)
	if len(sc.APs) != 3 || sc.APs[0] != (wifiAP{"A", -40, 1}) || sc.APs[1] != (wifiAP{"B", -70, 0}) || sc.APs[2] != (wifiAP{"C", -100, 1}) {
		t.Fatalf("parseWifiScan = %+v", sc.APs)
	}
}

func TestValidWifiCreds(t *testing.T) {
	ok := [][2]string{{"Home", "12345678"}, {"Open", ""}, {"家裡的網路", "p@ss w0rd!"}, {"Hex", strings.Repeat("ab", 32)}}
	for _, c := range ok {
		if err := validWifiCreds(c[0], c[1]); err != nil {
			t.Errorf("validWifiCreds(%q, %q) = %v", c[0], c[1], err)
		}
	}
	bad := [][2]string{{"", "12345678"}, {strings.Repeat("x", 33), ""}, {"Home", "short"}, {"Home", strings.Repeat("x", 64)}, {"Home", "密碼密碼密碼"}, {"a\nb", "12345678"}}
	for _, c := range bad {
		if validWifiCreds(c[0], c[1]) == nil {
			t.Errorf("validWifiCreds(%q, %q) should fail", c[0], c[1])
		}
	}
}

func TestWifiHelpers(t *testing.T) {
	if h := wifiHost("a1b2c3d4e5f6"); h != "aimon-d4e5f6" {
		t.Fatalf("wifiHost = %q", h)
	}
	if wifiHost("COM5") != "" {
		t.Fatal("wifiHost should reject non-id keys")
	}
	if k := newWifiKey(); len(k) != 32 || k == newWifiKey() {
		t.Fatalf("newWifiKey = %q", k)
	}
	l := string(wsetLine("A&B<", `p"w`, "k"))
	if l != `{"wset":{"k":"k","p":"p\"w","s":"A&B<"}}`+"\n" {
		t.Fatalf("wsetLine = %q", l)
	}
}

// 2026-11-11 實機抓到的螢幕回應（aimon-3f1fb8.local → 192.168.50.31），前面補上標頭：一個問題、一個回答
func TestMdnsParseA(t *testing.T) {
	name := "aimon-3f1fb8.local"
	q := mdnsQuery(name)
	if len(q) != 12+len(name)+2+4 || q[len(q)-2] != 0x80 {
		t.Fatalf("mdnsQuery = %x", q)
	}
	body := "\x0caimon-3f1fb8\x05local\x00\x00\x01\x80\x01" + // 問題
		"\xc0\x0c\x00\x01\x00\x01\x00\x00\x00\x78\x00\x04\xc0\xa8\x32\x1f" // 回答（名稱壓縮指標指回問題）
	p := append([]byte{0, 0, 0x84, 0, 0, 1, 0, 1, 0, 0, 0, 0}, body...)
	if ip := mdnsParseA(p, name); ip != "192.168.50.31" {
		t.Fatalf("mdnsParseA = %q", ip)
	}
	if ip := mdnsParseA(p, "aimon-000000.local"); ip != "" {
		t.Fatalf("other name should not match: %q", ip)
	}
	for i := range p { // 截斷的封包不能當掉
		mdnsParseA(p[:i], name)
	}
	loop := append([]byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0}, 0xc0, 12) // 指標指向自己
	if mdnsParseA(loop, name) != "" {
		t.Fatal("pointer loop")
	}
}
