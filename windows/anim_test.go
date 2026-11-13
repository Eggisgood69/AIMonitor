package main

import (
	"strings"
	"testing"
)

func TestSaveListLoadAnim(t *testing.T) {
	dir := t.TempDir()
	raw := make([]byte, animFrameLen*3+100) // 多出來的不滿一格，丟掉
	for i := range raw {
		raw[i] = byte(i * 7)
	}
	a, err := saveAnim(dir, "貓", raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.N != 3 || a.Ms != 1000/animFPS || !strings.HasPrefix(a.Preview, "data:image/png;base64,") {
		t.Fatalf("bad info %+v", a)
	}
	l := listAnims(dir)
	if len(l) != 1 || l[0].ID != a.ID || l[0].Name != "貓" {
		t.Fatalf("list %+v", l)
	}
	b, err := loadAnimRaw(dir, a)
	if err != nil || len(b) != animFrameLen*3 {
		t.Fatalf("load %v %d", err, len(b))
	}
	if h := animHeader(a); !strings.Contains(h, `"w":140,"h":114,"n":3`) || !strings.HasSuffix(h, "\n") {
		t.Fatalf("header %q", h)
	}
	deleteAnim(dir, "../x")
	deleteAnim(dir, a.ID)
	if len(listAnims(dir)) != 0 {
		t.Fatal("not deleted")
	}
}

func TestSaveAnimCapsFrames(t *testing.T) {
	a, err := saveAnim(t.TempDir(), "x", make([]byte, animFrameLen*(animMaxFrames+5)), 1)
	if err != nil || a.N != animMaxFrames {
		t.Fatalf("%v %d", err, a.N)
	}
	if _, err := saveAnim(t.TempDir(), "x", make([]byte, 10), 1); err == nil {
		t.Fatal("empty should fail")
	}
}

// 舊韌體（hello 沒有 av）跟「新韌體但還沒存動畫」都是 AV = 0：舊的不能傳（不會回 rdy，卡 60 秒）
func TestAnimDecideOldFirmware(t *testing.T) {
	const want = 0x1234
	cases := []struct {
		name     string
		want     uint32
		verified bool
		board    string
		mic, sup bool
		have     uint32
		expect   int
	}{
		{"沒選動畫", 0, true, "s3a175", false, false, 0, animDoSkip},
		{"1.28 吋", want, true, "c3", false, false, 0, animDoSkip},
		{"還沒回 hello", want, false, "s3a175", false, false, 0, animDoSkip},
		{"舊韌體", want, true, "s3a175", false, false, 0, animDoOldFW},
		{"舊韌體麥克風模式", want, true, "s3a175", true, false, 0, animDoOldFW},
		{"新韌體沒存", want, true, "s3a175", false, true, 0, animDoUpload},
		{"新韌體存別份", want, true, "s3a175", false, true, 0x99, animDoUpload},
		{"新韌體已經有", want, true, "s3a175", false, true, want, animDoSkip},
		{"新韌體麥克風模式", want, true, "s3a175", true, true, 0, animDoSkip},
	}
	for _, c := range cases {
		if got := animDecide(c.want, c.verified, c.board, c.mic, c.sup, c.have); got != c.expect {
			t.Errorf("%s: got %d, want %d", c.name, got, c.expect)
		}
	}
}
