// SPDX-License-Identifier: AGPL-3.0-or-later

package stick

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vanilla-router/internal/config"
)

func defaultConfig(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.Default(config.DefaultInput{WANMAC: "02:00:00:00:00:01", LANMACs: []string{"02:00:00:00:00:02"},
		Rand: bytes.NewReader([]byte{1, 2, 3, 4, 5})})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFindMitAuffuellung(t *testing.T) {
	dir := t.TempDir()
	body := `{"schema":1,"locale":"de_AT.UTF-8","keymap":"de","timezone":"UTC"}`
	padded := body + strings.Repeat(" ", Size-len(body))
	os.MkdirAll(filepath.Join(dir, "vr"), 0o755)
	os.WriteFile(filepath.Join(dir, "vr", "stick.json"), []byte(padded), 0o644)
	in, ok := Find([]string{"/gibt/es/nicht", dir})
	if !ok || in.Locale != "de_AT.UTF-8" || in.Timezone != "UTC" {
		t.Fatalf("%+v %v", in, ok)
	}
	c := defaultConfig(t)
	in.Apply(c)
	if c.System.Locale != "de_AT.UTF-8" || c.System.Timezone != "UTC" {
		t.Errorf("%+v", c.System)
	}
}

func TestLeereReservierung(t *testing.T) {
	if _, err := Parse([]byte(strings.Repeat(" ", Size))); err == nil {
		t.Error("leere Reservierung muss als „keine Vorgaben“ gelten")
	}
}

func TestUngueltigeWerteVerworfen(t *testing.T) {
	c := defaultConfig(t)
	Info{Schema: 1, Locale: "de_DE.UTF-8; rm -rf /", Timezone: "../../etc/passwd", Keymap: "us"}.Apply(c)
	if c.System.Locale != "de_DE.UTF-8" || c.System.Timezone != "Europe/Berlin" || c.System.Keymap != "us" {
		t.Errorf("%+v", c.System)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
