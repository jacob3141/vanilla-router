// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"testing"

	"vanilla-router/internal/windefaults"
)

// TestWindowsDefaults reads the language, keyboard and time zone of this computer (read-only).
func TestWindowsDefaults(t *testing.T) {
	loc, klid, zone := windowsLocale()
	in := windefaults.Info(loc, klid, zone)
	t.Logf("Windows: %q %q %q → %+v, Deutsch: %v", loc, klid, zone, in, germanUI())
	if loc == "" || klid == "" || zone == "" {
		t.Fatal("Windows values missing")
	}
	if in.Locale == "" || in.Timezone == "" {
		t.Errorf("not translated: %+v", in)
	}
}
