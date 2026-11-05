// SPDX-License-Identifier: AGPL-3.0-or-later

package windisk

import "testing"

// TestList läuft nur unter Windows und liest nur (keine Administratorrechte nötig).
func TestList(t *testing.T) {
	disks, err := List()
	if err != nil {
		t.Fatal(err)
	}
	prot := protectedDisks()
	if len(prot) == 0 {
		t.Error("Systemlaufwerk nicht erkannt")
	}
	t.Logf("geschützt: %v", prot)
	for _, d := range disks {
		if prot[d.Number] {
			t.Errorf("geschützter Datenträger in der Liste: %+v", d)
		}
		if d.Size <= 0 || d.Sector < 512 {
			t.Errorf("%+v", d)
		}
		t.Logf("USB: %d %q %d Bytes, Sektor %d, %v", d.Number, d.Name(), d.Size, d.Sector, d.Letters)
	}
}
