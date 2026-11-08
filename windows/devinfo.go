package main

// 螢幕 hello 回報的 fw / hw / id 會顯示在設定視窗（注入在 claude.ai 頁面上），
// 任何會回 hello 的 USB 裝置都能送字串過來 → 只收韌體真的會送的格式，其他當作沒送（ui.js 也會再跳脫一次）。

import "regexp"

var (
	devFWRE = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,4}$`) // FW_VERSION，例如 1.10.41
	devHWRE = regexp.MustCompile(`^[a-z0-9]{1,16}$`)            // BOARD_ID：c3 / s3a175（以後的新板子也是小寫英數）
	devIDRE = regexp.MustCompile(`^[0-9a-f]{12}$`)              // 晶片 MAC（"%04x%08lx"）
)

func devFW(s string) string { return devOK(devFWRE, s) }
func devHW(s string) string { return devOK(devHWRE, s) }
func devID(s string) string { return devOK(devIDRE, s) }

// 紀錄用：沒送或格式不對的寫成 -
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func devOK(re *regexp.Regexp, s string) string {
	if re.MatchString(s) {
		return s
	}
	return ""
}
