// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"testing"
	"time"
)

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("richtig-langes-passwort")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "richtig-langes-passwort") {
		t.Error("richtiges Passwort abgelehnt")
	}
	if VerifyPassword(h, "falsches-passwort-123") {
		t.Error("falsches Passwort akzeptiert")
	}
	if VerifyPassword("md5$abc", "x") || VerifyPassword("pbkdf2-sha256$10$AA$AA", "x") {
		t.Error("ungültiges oder zu schwaches Format akzeptiert")
	}
}

func TestValidatePassword(t *testing.T) {
	if ValidatePassword("kurz") == nil {
		t.Error("zu kurzes Passwort akzeptiert")
	}
	if ValidatePassword("zwölf zeichen") != nil {
		t.Error("Passwort mit 13 Zeichen (inkl. Umlaut) abgelehnt")
	}
}

func TestStoreSetupFlow(t *testing.T) {
	s := NewStore(t.TempDir())
	if s.Configured() {
		t.Fatal("neuer Store gilt als eingerichtet")
	}
	code, err := s.SetupCode()
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := s.SetupCode(); again != code {
		t.Error("Einrichtungscode ändert sich bei jedem Aufruf")
	}
	if !s.CheckSetupCode(" " + code[:4] + code[5:] + " ") {
		t.Error("Code ohne Bindestrich und mit Leerzeichen abgelehnt")
	}
	if s.CheckSetupCode("AAAA-AAAA") && code != "AAAA-AAAA" {
		t.Error("falscher Code akzeptiert")
	}
	if err := s.SetPassword("ein-sicheres-passwort"); err != nil {
		t.Fatal(err)
	}
	if !s.Configured() || !s.Verify("ein-sicheres-passwort") || s.Verify("anderes-passwort!") {
		t.Error("Passwort nach Einrichtung falsch gespeichert")
	}
	if s.CheckSetupCode(code) {
		t.Error("Einrichtungscode gilt nach der Einrichtung noch")
	}
}

func TestSessionsExpire(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := NewSessions(30*time.Minute, 12*time.Hour)
	s.now = func() time.Time { return now }
	tok := s.Create("192.168.42.10")
	if !s.Touch(tok) {
		t.Fatal("neue Sitzung ungültig")
	}
	now = now.Add(29 * time.Minute)
	if !s.Touch(tok) {
		t.Fatal("Sitzung nach 29 min Untätigkeit abgelaufen")
	}
	now = now.Add(31 * time.Minute)
	if s.Touch(tok) {
		t.Error("Sitzung nach 31 min Untätigkeit noch gültig")
	}

	tok = s.Create("192.168.42.10")
	for i := 0; i < 25; i++ { // 25 × 29 min > 12 h, aber nie untätig
		now = now.Add(29 * time.Minute)
		s.Touch(tok)
	}
	if s.Touch(tok) {
		t.Error("Sitzung nach mehr als 12 h noch gültig")
	}
	if s.Touch("") || s.Touch("erfunden") {
		t.Error("unbekanntes Token akzeptiert")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := NewLimiter()
	l.now = func() time.Time { return now }
	for i := 0; i < freeAttempts; i++ {
		l.Fail("a")
		if l.Wait("a") != 0 {
			t.Fatalf("Bremse schon nach %d Fehlversuchen", i+1)
		}
	}
	l.Fail("a")
	if w := l.Wait("a"); w != time.Second {
		t.Errorf("Wartezeit nach 4. Fehlversuch = %v, erwartet 1s", w)
	}
	l.Fail("a")
	if w := l.Wait("a"); w != 2*time.Second {
		t.Errorf("Wartezeit nach 5. Fehlversuch = %v, erwartet 2s", w)
	}
	for i := 0; i < 30; i++ {
		l.Fail("a")
	}
	if w := l.Wait("a"); w != maxDelay {
		t.Errorf("Wartezeit nicht auf %v begrenzt: %v", maxDelay, w)
	}
	if l.Wait("b") != 0 {
		t.Error("andere IP wird mitgebremst")
	}
	l.Success("a")
	if l.Wait("a") != 0 {
		t.Error("Bremse nach erfolgreichem Login nicht zurückgesetzt")
	}
}
