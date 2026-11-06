// SPDX-License-Identifier: AGPL-3.0-or-later

package windisk

import "testing"

// TestList only runs on Windows and only reads (no administrator rights needed).
func TestList(t *testing.T) {
	disks, err := List()
	if err != nil {
		t.Fatal(err)
	}
	prot := protectedDisks()
	if len(prot) == 0 {
		t.Error("system drive not recognized")
	}
	t.Logf("protected: %v", prot)
	for _, d := range disks {
		if prot[d.Number] {
			t.Errorf("protected disk in the list: %+v", d)
		}
		if d.Size <= 0 || d.Sector < 512 {
			t.Errorf("%+v", d)
		}
		t.Logf("USB: %d %q %d bytes, sector %d, %v", d.Number, d.Name(), d.Size, d.Sector, d.Letters)
	}
}
