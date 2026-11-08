package main

import "testing"

func TestDevInfoRejectsMarkup(t *testing.T) {
	ok := map[string]func(string) string{"1.10.41": devFW, "s3a175": devHW, "c3": devHW, "a1b2c3d4e5f6": devID}
	for v, f := range ok {
		if f(v) != v {
			t.Errorf("%q 應該收", v)
		}
	}
	bad := []string{"", "<img src=x onerror=alert(1)>", `1.0.0"><button data-cmd="quit">`, "c3 ", "S3A175", "1.10", "a1b2c3d4e5f6\n"}
	for _, v := range bad {
		if devFW(v) != "" || devHW(v) != "" || devID(v) != "" {
			t.Errorf("%q 不應該收", v)
		}
	}
}
