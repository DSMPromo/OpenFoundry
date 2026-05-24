package handlers

import (
	"testing"
	"time"
)

func TestPasswordResetSMTPConfigConfigured(t *testing.T) {
	if (PasswordResetSMTPConfig{}).configured() {
		t.Error("empty config should not be configured")
	}
	if !(PasswordResetSMTPConfig{Host: "smtp.example.com"}).configured() {
		t.Error("Host should opt-in")
	}
}

func TestPasswordResetSMTPAddr(t *testing.T) {
	if got := (PasswordResetSMTPConfig{Host: "smtp.example.com"}).addr(); got != "smtp.example.com:587" {
		t.Errorf("default port: %q", got)
	}
	if got := (PasswordResetSMTPConfig{Host: "smtp.example.com", Port: 2525}).addr(); got != "smtp.example.com:2525" {
		t.Errorf("explicit port: %q", got)
	}
}

func TestBuildResetURL(t *testing.T) {
	cases := []struct{ pub, tok, want string }{
		{"", "abc", "/auth/reset-password?token=abc"},
		{"https://app.example.com", "abc", "https://app.example.com/auth/reset-password?token=abc"},
		{"https://app.example.com/", "abc", "https://app.example.com/auth/reset-password?token=abc"},
		{"  https://app.example.com  ", "abc", "https://app.example.com/auth/reset-password?token=abc"},
	}
	for _, c := range cases {
		if got := buildResetURL(c.pub, c.tok); got != c.want {
			t.Errorf("buildResetURL(%q, %q) = %q, want %q", c.pub, c.tok, got, c.want)
		}
	}
}

// _ keeps the time import in use even when other tests are skipped;
// we reference it for TTL sanity below.
var _ = time.Hour
