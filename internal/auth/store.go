// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store verwaltet das Passwort und den Einrichtungscode im Datenordner.
//
//	auth.json    Passwort-Hash (0600)
//	setup-code   einmaliger Code für die Einrichtungsseite, existiert nur bis zur Einrichtung
type Store struct {
	dir string
	mu  sync.Mutex
}

type authFile struct {
	Hash    string    `json:"hash"`
	Changed time.Time `json:"changed"`
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) authPath() string  { return filepath.Join(s.dir, "auth.json") }
func (s *Store) setupPath() string { return filepath.Join(s.dir, "setup-code") }

func (s *Store) load() (*authFile, error) {
	b, err := os.ReadFile(s.authPath())
	if err != nil {
		return nil, err
	}
	var a authFile
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// Configured meldet, ob schon ein Passwort gesetzt ist.
func (s *Store) Configured() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.load()
	return err == nil && a.Hash != ""
}

// Verify prüft das Passwort. Ohne gesetztes Passwort schlägt es immer fehl.
func (s *Store) Verify(pw string) bool {
	s.mu.Lock()
	a, err := s.load()
	s.mu.Unlock()
	if err != nil || a.Hash == "" {
		// Gleich viel Arbeit wie ein echter Vergleich, damit die Antwortzeit nichts verrät
		HashPassword(pw)
		return false
	}
	return VerifyPassword(a.Hash, pw)
}

// SetPassword speichert einen neuen Hash atomar und löscht den Einrichtungscode.
func (s *Store) SetPassword(pw string) error {
	if err := ValidatePassword(pw); err != nil {
		return err
	}
	hash, err := HashPassword(pw)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(authFile{Hash: hash, Changed: time.Now().UTC()}, "", "  ")
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeFileAtomic(s.authPath(), b, 0o600); err != nil {
		return err
	}
	os.Remove(s.setupPath())
	return nil
}

// SetupCode liefert den Einrichtungscode und legt ihn beim ersten Aufruf an.
// Format: XXXX-XXXX (Base32 ohne verwechselbare Füllzeichen).
func (s *Store) SetupCode() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, err := os.ReadFile(s.setupPath()); err == nil {
		return strings.TrimSpace(string(b)), nil
	}
	raw := make([]byte, 5)
	rand.Read(raw)
	c := base32.StdEncoding.EncodeToString(raw) // 8 Zeichen
	code := c[:4] + "-" + c[4:8]
	if err := writeFileAtomic(s.setupPath(), []byte(code+"\n"), 0o600); err != nil {
		return "", err
	}
	return code, nil
}

// ImportSetupCode übernimmt den Code, den der Installer angezeigt hat (S9), solange noch kein
// Passwort gesetzt ist. Danach gilt genau dieser Code für die erste Anmeldung.
func (s *Store) ImportSetupCode(code string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(strings.ReplaceAll(code, "-", "")) != 8 {
		return errors.New("Einrichtungscode hat nicht 8 Zeichen")
	}
	if s.Configured() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeFileAtomic(s.setupPath(), []byte(code+"\n"), 0o600)
}

// CheckSetupCode vergleicht ohne Rücksicht auf Groß-/Kleinschreibung und Bindestrich.
func (s *Store) CheckSetupCode(given string) bool {
	s.mu.Lock()
	b, err := os.ReadFile(s.setupPath())
	s.mu.Unlock()
	if err != nil {
		return false
	}
	norm := func(x string) string {
		return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(x)))
	}
	want, got := norm(string(b)), norm(given)
	return len(want) == 8 && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if path == "" {
		return errors.New("leerer Pfad")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
