package main

import (
	"strings"
	"testing"
)

func TestRedactIDs(t *testing.T) {
	in := `wham/usage 200 {"user_id":"user-abc123","account_id":"acct-0000-test","email":"someone@example.com","plan_type":"free","rate_limit":{"allowed":true,"primary_window":{"used_percent":3,"limit_window_seconds":18000}},"note":"mail me at a.b@c.io"}`
	out := redactIDs(in)
	for _, leak := range []string{"user-abc123", "acct-0000-test", "someone@example.com", "a.b@c.io"} {
		if strings.Contains(out, leak) {
			t.Fatalf("leaked %q: %s", leak, out)
		}
	}
	for _, keep := range []string{`"plan_type":"free"`, `"used_percent":3`, `"limit_window_seconds":18000`} {
		if !strings.Contains(out, keep) {
			t.Fatalf("lost %q: %s", keep, out)
		}
	}
}
