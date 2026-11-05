package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		latest, cur string
		want        bool
	}{
		{"1.12.0", "1.11.17", true},
		{"1.11.18", "1.11.17", true},
		{"1.11.17", "1.11.17", false},
		{"1.11.9", "1.11.17", false}, // 數字比較，不是字串比較
		{"2.0.0", "1.99.99", true},
		{"1.10.0", "1.11.0", false},
		{"", "1.11.17", false},
		{"1.12", "1.11.17", false},
		{"1.12.0-beta", "1.11.17", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.latest, c.cur); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.latest, c.cur, got, c.want)
		}
	}
}

const sampleRelease = `{
  "tag_name": "v1.12.0", "html_url": "https://github.com/Eggisgood69/AIMonitor/releases/tag/v1.12.0",
  "draft": false, "prerelease": false,
  "assets": [
    {"name": "AIMonitor.exe", "size": 9512960, "browser_download_url": "https://github.com/Eggisgood69/AIMonitor/releases/download/v1.12.0/AIMonitor.exe"},
    {"name": "AIMonitor.exe.sha256", "size": 80, "browser_download_url": "https://github.com/Eggisgood69/AIMonitor/releases/download/v1.12.0/AIMonitor.exe.sha256"}
  ]}`

func TestParseRelease(t *testing.T) {
	ri, err := parseRelease([]byte(sampleRelease))
	if err != nil {
		t.Fatal(err)
	}
	if ri.Version != "1.12.0" || ri.Tag != "v1.12.0" || ri.ExeSize != 9512960 ||
		!strings.HasSuffix(ri.ExeURL, "/AIMonitor.exe") || !strings.HasSuffix(ri.SumURL, ".sha256") {
		t.Fatalf("parsed wrong: %+v", ri)
	}

	bad := map[string]string{
		"no sha256": strings.Replace(sampleRelease, `"AIMonitor.exe.sha256"`, `"other.txt"`, 1),
		"no exe":    strings.Replace(sampleRelease, `"name": "AIMonitor.exe",`, `"name": "x.zip",`, 1),
		"draft":     strings.Replace(sampleRelease, `"draft": false`, `"draft": true`, 1),
		"pre":       strings.Replace(sampleRelease, `"prerelease": false`, `"prerelease": true`, 1),
		"tag":       strings.Replace(sampleRelease, `"v1.12.0"`, `"latest"`, 1),
		"http":      strings.Replace(sampleRelease, `"https://github.com/Eggisgood69/AIMonitor/releases/download/v1.12.0/AIMonitor.exe"`, `"http://x/AIMonitor.exe"`, 1),
		"huge":      strings.Replace(sampleRelease, `9512960`, `999999999`, 1),
		"json":      `{`,
	}
	for name, b := range bad {
		if _, err := parseRelease([]byte(b)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseSHA256File(t *testing.T) {
	h := strings.Repeat("ab", 32)
	for _, s := range []string{h, strings.ToUpper(h) + "\n", h + "  AIMonitor.exe\n", "\ufeff" + h + " *AIMonitor.exe"} {
		got, err := parseSHA256File(s)
		if err != nil || got != h {
			t.Errorf("parseSHA256File(%q) = %q, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "abc", strings.Repeat("zz", 32)} {
		if _, err := parseSHA256File(s); err == nil {
			t.Errorf("parseSHA256File(%q): expected error", s)
		}
	}
}

// 假的 GitHub：API + 下載（HTTPS）
func fakeGitHub(t *testing.T, exe []byte, sum string) *httptest.Server {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/latest", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "AIMonitor") {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		fmt.Fprintf(w, `{"tag_name":"v9.0.0","assets":[{"name":"AIMonitor.exe","size":%d,"browser_download_url":"%s/dl/exe"},
			{"name":"AIMonitor.exe.sha256","browser_download_url":"%s/dl/sum"}]}`, len(exe), srv.URL, srv.URL)
	})
	mux.HandleFunc("/dl/exe", func(w http.ResponseWriter, r *http.Request) { // 跟 GitHub 一樣轉址到另一個網址
		http.Redirect(w, r, "/obj/exe", http.StatusFound)
	})
	mux.HandleFunc("/obj/exe", func(w http.ResponseWriter, r *http.Request) { w.Write(exe) })
	mux.HandleFunc("/dl/sum", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sum+"  AIMonitor.exe\n") })
	srv = httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadRelease(t *testing.T) {
	exe := []byte(strings.Repeat("MZ fake exe ", 20000))
	s := sha256.Sum256(exe)
	srv := fakeGitHub(t, exe, hex.EncodeToString(s[:]))
	ctx := context.Background()
	ri, err := fetchLatestRelease(ctx, srv.Client(), srv.URL+"/repos/latest")
	if err != nil {
		t.Fatal(err)
	}
	if ri.Version != "9.0.0" {
		t.Fatalf("version %q", ri.Version)
	}
	dir := t.TempDir()
	var last int64
	p, err := downloadRelease(ctx, srv.Client(), ri, dir, func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != string(exe) || last != int64(len(exe)) || filepath.Base(p) != "AIMonitor.exe" {
		t.Fatalf("downloaded wrong file (%d bytes, progress %d)", len(got), last)
	}
	if h, _ := sha256File(p); h != hex.EncodeToString(s[:]) {
		t.Fatal("sha256File mismatch")
	}
}

func TestDownloadReleaseBadHash(t *testing.T) {
	exe := []byte("tampered")
	srv := fakeGitHub(t, exe, strings.Repeat("0", 64))
	ctx := context.Background()
	ri, err := fetchLatestRelease(ctx, srv.Client(), srv.URL+"/repos/latest")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := downloadRelease(ctx, srv.Client(), ri, dir, nil); err == nil || !strings.Contains(err.Error(), "SHA256") {
		t.Fatalf("expected SHA256 error, got %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("bad download left files behind: %v", ents)
	}
}

func TestFetchRejectsHTTP(t *testing.T) {
	if _, err := fetchLatestRelease(context.Background(), http.DefaultClient, "http://api.github.com/x"); err == nil {
		t.Fatal("expected error for http URL")
	}
}
