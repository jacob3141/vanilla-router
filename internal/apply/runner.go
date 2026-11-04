// SPDX-License-Identifier: AGPL-3.0-or-later

package apply

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExecRunner führt Programme wirklich aus. Ausgaben kommen immer mit LC_ALL=C (L-17).
type ExecRunner struct{}

// Run startet name mit args. env ergänzt die Umgebung.
func (ExecRunner) Run(env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(append(os.Environ(), "LC_ALL=C"), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.Bytes(), err
}

// DryRunner ist für die Testinstanz der Oberfläche: Prüfungen (nft -c, dnsmasq --test) laufen echt,
// alles, was das System ändert oder neu lädt, wird nur protokolliert und gilt als erfolgreich.
type DryRunner struct {
	Log func(cmd string)
}

// Run führt nur prüfende Befehle aus.
func (d DryRunner) Run(env []string, name string, args ...string) ([]byte, error) {
	check := (name == "nft" && len(args) > 0 && args[0] == "-c") || (name == "dnsmasq" && len(args) > 0 && args[0] == "--test")
	if check && os.Geteuid() == 0 { // ohne root kann nft -c nicht prüfen, dann nur protokollieren
		return ExecRunner{}.Run(env, name, args...)
	}
	if d.Log != nil {
		d.Log(name + " " + strings.Join(args, " "))
	}
	return nil, nil
}

// DirSecrets liest Geheimnisse aus einem Ordner, eine Datei je Verweis (/etc/vanilla-router/secrets).
type DirSecrets struct{ Dir string }

var errBadRef = errors.New("ungültiger Verweis")

func (s DirSecrets) file(ref string) (string, error) {
	if ref == "" || strings.ContainsAny(ref, "/\\.") {
		return "", errBadRef
	}
	return filepath.Join(s.Dir, ref), nil
}

// Get liest ein Geheimnis. Die Datei muss 0600 sein, sonst wird sie abgelehnt.
func (s DirSecrets) Get(ref string) (string, error) {
	p, err := s.file(ref)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("Geheimnis %q fehlt (vrctl secret set %s)", ref, ref)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("Geheimnis %q ist für andere lesbar (Rechte %o), bitte auf 0600 setzen", ref, fi.Mode().Perm())
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// Set schreibt ein Geheimnis mit 0600.
func (s DirSecrets) Set(ref, value string) error {
	p, err := s.file(ref)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteString(value + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}
