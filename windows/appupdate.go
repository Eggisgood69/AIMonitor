package main

// 電腦端程式的新版檢查與下載（可跨平台測試；Windows 的背景檢查、取代執行中的舊版在 appupdate_windows.go）。
// 讀 GitHub 這個 repo 最新的 Release（不含草稿、預先發行），比對版本；下載 Release 附的 AIMonitor.exe，
// 用同一個 Release 的 AIMonitor.exe.sha256 驗證。只用 HTTPS，不送任何使用者資料（只有 User-Agent: AIMonitor/<版本>）。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const updateRepo = "Eggisgood69/AIMonitor"
const updateAPI = "https://api.github.com/repos/" + updateRepo + "/releases/latest"
const updateExeName = "AIMonitor.exe"
const updateSumName = "AIMonitor.exe.sha256"
const updateMaxExe = 100 << 20 // exe 最大 100 MB（現在約 10 MB），超過就當作有問題

var updateUserAgent = "AIMonitor" // Windows 端會加上版本號

// 一個 Release 裡更新要用的東西
type ReleaseInfo struct {
	Version string `json:"version"` // 去掉 v 的版本號，例如 1.12.0
	Tag     string `json:"tag"`
	URL     string `json:"url"` // Release 頁面（給使用者看說明）
	ExeURL  string `json:"-"`
	ExeSize int64  `json:"-"`
	SumURL  string `json:"-"`
}

// 解析 GitHub API 的 releases/latest 回應
func parseRelease(b []byte) (ReleaseInfo, error) {
	var r struct {
		Tag        string `json:"tag_name"`
		HTMLURL    string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return ReleaseInfo{}, fmt.Errorf("Release 資料格式不對：%v", err)
	}
	if r.Draft || r.Prerelease {
		return ReleaseInfo{}, errors.New("最新的 Release 是草稿或預先發行版")
	}
	v := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(r.Tag), "v"), "V")
	if !validVersion(v) {
		return ReleaseInfo{}, fmt.Errorf("看不懂 Release 的版本號「%s」", r.Tag)
	}
	ri := ReleaseInfo{Version: v, Tag: r.Tag, URL: r.HTMLURL}
	for _, a := range r.Assets {
		switch {
		case strings.EqualFold(a.Name, updateExeName):
			ri.ExeURL, ri.ExeSize = a.URL, a.Size
		case strings.EqualFold(a.Name, updateSumName):
			ri.SumURL = a.URL
		}
	}
	if ri.ExeURL == "" || ri.SumURL == "" {
		return ri, fmt.Errorf("Release %s 沒有附 %s 或 %s", r.Tag, updateExeName, updateSumName)
	}
	if !strings.HasPrefix(ri.ExeURL, "https://") || !strings.HasPrefix(ri.SumURL, "https://") {
		return ri, errors.New("下載網址不是 HTTPS")
	}
	if ri.ExeSize > updateMaxExe {
		return ri, fmt.Errorf("%s 太大（%d bytes）", updateExeName, ri.ExeSize)
	}
	return ri, nil
}

// x.y.z，每段都是數字
func validVersion(v string) bool {
	p := strings.Split(v, ".")
	if len(p) != 3 {
		return false
	}
	for _, s := range p {
		if s == "" || len(s) > 6 {
			return false
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// latest 比 cur 新
func newerVersion(latest, cur string) bool {
	return validVersion(latest) && versionLess(cur, latest)
}

// .sha256 檔的內容：「<64 位十六進位>」或 sha256sum 格式「<雜湊>  AIMonitor.exe」
func parseSHA256File(s string) (string, error) {
	f := strings.Fields(strings.TrimPrefix(s, "\ufeff"))
	if len(f) == 0 {
		return "", errors.New("SHA256 檔是空的")
	}
	h := strings.ToLower(f[0])
	if len(h) != 64 {
		return "", errors.New("SHA256 檔格式不對")
	}
	if _, err := hex.DecodeString(h); err != nil {
		return "", errors.New("SHA256 檔格式不對")
	}
	return h, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func httpGet(ctx context.Context, c *http.Client, url, accept string) (*http.Response, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, errors.New("只接受 HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", updateUserAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// 一定只走 HTTPS（轉址到 http 也不跟）
func updateHTTPClient(base *http.Client) *http.Client {
	c := *base
	if c.Timeout == 0 {
		c.Timeout = 5 * time.Minute
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("轉址到非 HTTPS 的網址")
		}
		if len(via) >= 10 {
			return errors.New("轉址太多次")
		}
		return nil
	}
	return &c
}

// 讀最新的 Release
func fetchLatestRelease(ctx context.Context, c *http.Client, apiURL string) (ReleaseInfo, error) {
	resp, err := httpGet(ctx, updateHTTPClient(c), apiURL, "application/vnd.github+json")
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return ReleaseInfo{}, errors.New("還沒有任何 Release")
		}
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ReleaseInfo{}, err
	}
	return parseRelease(b)
}

// 下載 exe 到 dir，SHA256 對了才回傳路徑；不對就刪掉
func downloadRelease(ctx context.Context, c *http.Client, ri ReleaseInfo, dir string, progress func(done, total int64)) (string, error) {
	c = updateHTTPClient(c)
	resp, err := httpGet(ctx, c, ri.SumURL, "")
	if err != nil {
		return "", fmt.Errorf("下載 SHA256 失敗：%v", err)
	}
	sb, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if err != nil {
		return "", fmt.Errorf("下載 SHA256 失敗：%v", err)
	}
	want, err := parseSHA256File(string(sb))
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, updateExeName)
	tmp := path + ".part"
	resp, err = httpGet(ctx, c, ri.ExeURL, "application/octet-stream")
	if err != nil {
		return "", fmt.Errorf("下載失敗：%v", err)
	}
	defer resp.Body.Close()
	total := ri.ExeSize
	if total <= 0 {
		total = resp.ContentLength
	}
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	var done int64
	buf := make([]byte, 64*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > updateMaxExe {
				f.Close()
				os.Remove(tmp)
				return "", errors.New("下載的檔案太大")
			}
			h.Write(buf[:n])
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(tmp)
				return "", werr
			}
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			return "", fmt.Errorf("下載中斷：%v", rerr)
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(tmp)
		return "", errors.New("下載的檔案 SHA256 不符，已刪除（可能下載不完整，請再試一次）")
	}
	os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return path, nil
}
