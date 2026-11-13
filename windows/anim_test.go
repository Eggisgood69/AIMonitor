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
