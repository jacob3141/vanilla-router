// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"testing"
	"time"
)

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct-long-password")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "correct-long-password") {
		t.Error("correct password rejected")
	}
	if VerifyPassword(h, "wrong-password-123") {
		t.Error("wrong password accepted")
	}
	if VerifyPassword("md5$abc", "x") || VerifyPassword("pbkdf2-sha256$10$AA$AA", "x") {
		t.Error("invalid or too weak format accepted")
	}
}

func TestValidatePassword(t *testing.T) {
	if ValidatePassword("short") == nil {
		t.Error("too short password accepted")
	}
	if ValidatePassword("zwölf zeichen") != nil {
		t.Error("password with 13 characters (including an umlaut) rejected")
	}
}

func TestStoreSetupFlow(t *testing.T) {
	s := NewStore(t.TempDir())
	if s.Configured() {
		t.Fatal("new store counts as set up")
	}
	code, err := s.SetupCode()
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := s.SetupCode(); again != code {
		t.Error("setup code changes on every call")
	}
	if !s.CheckSetupCode(" " + code[:4] + code[5:] + " ") {
		t.Error("code without a hyphen and with spaces rejected")
	}
	if s.CheckSetupCode("AAAA-AAAA") && code != "AAAA-AAAA" {
		t.Error("wrong code accepted")
	}
	if err := s.SetPassword("a-secure-password"); err != nil {
		t.Fatal(err)
	}
	if !s.Configured() || !s.Verify("a-secure-password") || s.Verify("another-password!") {
		t.Error("password stored wrongly after setup")
	}
	if s.CheckSetupCode(code) {
		t.Error("setup code still valid after setup")
	}
}

func TestSessionsExpire(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := NewSessions(30*time.Minute, 12*time.Hour)
	s.now = func() time.Time { return now }
	tok := s.Create("192.168.42.10")
	if !s.Touch(tok) {
		t.Fatal("new session invalid")
	}
	now = now.Add(29 * time.Minute)
	if !s.Touch(tok) {
		t.Fatal("session expired after 29 min of inactivity")
	}
	now = now.Add(31 * time.Minute)
	if s.Touch(tok) {
		t.Error("session still valid after 31 min of inactivity")
	}

	tok = s.Create("192.168.42.10")
	for i := 0; i < 25; i++ { // 25 × 29 min > 12 h, but never inactive
		now = now.Add(29 * time.Minute)
		s.Touch(tok)
	}
	if s.Touch(tok) {
		t.Error("session still valid after more than 12 h")
	}
	if s.Touch("") || s.Touch("made-up") {
		t.Error("unknown token accepted")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := NewLimiter()
	l.now = func() time.Time { return now }
	for i := 0; i < freeAttempts; i++ {
		l.Fail("a")
		if l.Wait("a") != 0 {
			t.Fatalf("throttled already after %d failed attempts", i+1)
		}
	}
	l.Fail("a")
	if w := l.Wait("a"); w != time.Second {
		t.Errorf("wait after 4th failed attempt = %v, expected 1s", w)
	}
	l.Fail("a")
	if w := l.Wait("a"); w != 2*time.Second {
		t.Errorf("wait after 5th failed attempt = %v, expected 2s", w)
	}
	for i := 0; i < 30; i++ {
		l.Fail("a")
	}
	if w := l.Wait("a"); w != maxDelay {
		t.Errorf("wait not limited to %v: %v", maxDelay, w)
	}
	if l.Wait("b") != 0 {
		t.Error("another IP is throttled too")
	}
	l.Success("a")
	if l.Wait("a") != 0 {
		t.Error("throttle not reset after a successful login")
	}
}
