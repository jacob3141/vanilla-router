// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"testing"

	"vanilla-router/internal/windefaults"
)

// TestWindowsVorgaben liest Sprache, Tastatur und Zeitzone dieses Rechners (nur lesend).
func TestWindowsVorgaben(t *testing.T) {
	loc, klid, zone := windowsLocale()
	in := windefaults.Info(loc, klid, zone)
	t.Logf("Windows: %q %q %q → %+v, Deutsch: %v", loc, klid, zone, in, germanUI())
	if loc == "" || klid == "" || zone == "" {
		t.Fatal("Windows-Werte fehlen")
	}
	if in.Locale == "" || in.Timezone == "" {
		t.Errorf("nicht übersetzt: %+v", in)
	}
}
