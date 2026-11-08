// SPDX-License-Identifier: AGPL-3.0-or-later

// Package stick reads the defaults the stick creator brings from Windows (vr/stick.json on
// the stick's EFI partition, PLAN.md 3.9.1, 3.12). The file has a fixed size and is padded with
// spaces, so that the creator can overwrite it in the image without knowing FAT.
package stick

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"vanilla-router/internal/config"
)

// Size is the reserved size of the file in the image.
const Size = 4096

// RelPath is the location on the stick's EFI partition.
const RelPath = "vr/stick.json"

// Info holds the defaults from Windows. Empty fields: no default.
type Info struct {
	Schema   int    `json:"schema"`
	Keymap   string `json:"keymap"`   // console keyboard, e.g. de
	Timezone string `json:"timezone"` // IANA, e.g. Europe/Berlin
}

// Mounts are the places where the stick's EFI partition can be mounted
// (systemd-gpt-auto-generator takes /efi or /boot).
var Mounts = []string{"/efi", "/boot/efi", "/boot"}

// Find looks for stick.json. ok = false: there is none (stick written with another program, or empty).
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

// Parse reads the file. An empty reservation (only spaces) counts as "no defaults".
func Parse(b []byte) (Info, error) {
	b = bytes.TrimRight(b, " \n\r\t\x00")
	if len(bytes.TrimSpace(b)) == 0 {
		return Info{}, errors.New("empty")
	}
	var in Info
	if err := json.Unmarshal(b, &in); err != nil {
		return Info{}, err
	}
	if in.Schema != 1 {
		return Info{}, errors.New("unknown schema")
	}
	return in, nil
}

// Encode creates the content of the reserved file: JSON, padded with spaces to Size.
func (in Info) Encode() ([]byte, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	if len(b) > Size {
		return nil, errors.New("stick.json too big")
	}
	return append(b, bytes.Repeat([]byte{' '}, Size-len(b))...), nil
}

// Blank reports whether b is an empty reservation (only spaces, size Size). The stick creator
// only overwrites such a place in the image.
func Blank(b []byte) bool {
	return len(b) == Size && len(bytes.Trim(b, " ")) == 0
}

// Apply takes the defaults into the configuration. Invalid values are discarded individually,
// so that a broken value never prevents the installation.
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
	if in.Keymap != "" {
		try(func(s *config.System) { s.Keymap = in.Keymap })
	}
}
