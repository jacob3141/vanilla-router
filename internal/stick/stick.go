// SPDX-License-Identifier: AGPL-3.0-or-later

// Package stick liest die Vorgaben, die der Stick-Ersteller aus Windows mitgibt (vr/stick.json auf
// der EFI-Partition des Sticks, PLAN.md 3.9.1, 3.12). Die Datei hat eine feste Größe und ist mit
// Leerzeichen aufgefüllt, damit der Ersteller sie im Abbild überschreiben kann, ohne FAT zu kennen.
package stick

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"vanilla-router/internal/config"
)

// Size ist die reservierte Größe der Datei im Abbild.
const Size = 4096

// RelPath ist der Ort auf der EFI-Partition des Sticks.
const RelPath = "vr/stick.json"

// Info sind die Vorgaben aus Windows. Leere Felder: keine Vorgabe.
type Info struct {
	Schema   int    `json:"schema"`
	Locale   string `json:"locale"`   // z. B. de_DE.UTF-8
	Keymap   string `json:"keymap"`   // Konsolen-Tastatur, z. B. de
	Timezone string `json:"timezone"` // IANA, z. B. Europe/Berlin
}

// Mounts sind die Orte, an denen die EFI-Partition des Sticks eingehängt sein kann
// (systemd-gpt-auto-generator nimmt /efi oder /boot).
var Mounts = []string{"/efi", "/boot/efi", "/boot"}

// Find sucht stick.json. ok = false: Es gibt keine (Stick mit fremdem Programm geschrieben oder leer).
func Find(mounts []string) (Info, bool) {
	for _, m := range mounts {
		b, err := os.ReadFile(filepath.Join(m, filepath.FromSlash(RelPath)))
		if err != nil {
			continue
		}
		if in, err := Parse(b); err == nil {
			return in, true
		}
	}
	return Info{}, false
}

// Parse liest die Datei. Eine leere Reservierung (nur Leerzeichen) zählt als „keine Vorgaben“.
func Parse(b []byte) (Info, error) {
	b = bytes.TrimRight(b, " \n\r\t\x00")
	if len(bytes.TrimSpace(b)) == 0 {
		return Info{}, errors.New("leer")
	}
	var in Info
	if err := json.Unmarshal(b, &in); err != nil {
		return Info{}, err
	}
	if in.Schema != 1 {
		return Info{}, errors.New("unbekanntes Schema")
	}
	return in, nil
}

// Apply übernimmt die Vorgaben in die Konfiguration. Ungültige Werte werden einzeln verworfen,
// damit eine kaputte Angabe nie die Installation verhindert.
func (in Info) Apply(c *config.Config) {
	try := func(set func(*config.System)) {
		old := c.System
		set(&c.System)
		if c.Validate() != nil {
			c.System = old
		}
	}
	if in.Timezone != "" {
		if _, err := os.Stat(filepath.Join("/usr/share/zoneinfo", filepath.FromSlash(in.Timezone))); err == nil || in.Timezone == "UTC" {
			try(func(s *config.System) { s.Timezone = in.Timezone })
		}
	}
	if in.Locale != "" {
		try(func(s *config.System) { s.Locale = in.Locale })
	}
	if in.Keymap != "" {
		try(func(s *config.System) { s.Keymap = in.Keymap })
	}
}

// Language ist eine Auswahl für S1, wenn es keine stick.json gibt.
type Language struct {
	Label                    string
	Locale, Keymap, Timezone string
}

// Languages für die Auswahl in S1. Texte des Assistenten sind vorerst deutsch (F-INST-10).
var Languages = []Language{
	{"Deutsch (Deutschland)", "de_DE.UTF-8", "de", "Europe/Berlin"},
	{"Deutsch (Österreich)", "de_AT.UTF-8", "de", "Europe/Vienna"},
	{"Deutsch (Schweiz)", "de_CH.UTF-8", "ch", "Europe/Zurich"},
	{"English", "en_US.UTF-8", "us", "UTC"},
}

// Info liefert die Vorgaben einer Sprache.
func (l Language) Info() Info {
	return Info{Schema: 1, Locale: l.Locale, Keymap: l.Keymap, Timezone: l.Timezone}
}
