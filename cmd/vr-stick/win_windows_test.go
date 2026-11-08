// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"testing"

	"vanilla-router/internal/windefaults"
)

// TestWindowsDefaults reads the keyboard and time zone of this computer (read-only).
func TestWindowsDefaults(t *testing.T) {
	klid, zone := windowsDefaults()
	in := windefaults.Info(klid, zone)
	t.Logf("Windows: %q %q → %+v", klid, zone, in)
	if klid == "" || zone == "" {
		t.Fatal("Windows values missing")
	}
	if in.Timezone == "" {
		t.Errorf("not translated: %+v", in)
	}
}
