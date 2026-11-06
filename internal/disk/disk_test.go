// SPDX-License-Identifier: AGPL-3.0-or-later

package disk

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func mk(t *testing.T, root, devPath string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, "devices", devPath)
	for k, v := range files {
		p := filepath.Join(dir, k)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	name := filepath.Base(devPath)
	os.MkdirAll(filepath.Join(root, "block"), 0o755)
	rel, _ := filepath.Rel(filepath.Join(root, "block"), dir)
	if err := os.Symlink(rel, filepath.Join(root, "block", name)); err != nil {
		t.Fatal(err)
	}
}

func TestListAndCandidates(t *testing.T) {
	root := t.TempDir()
	gb := func(n int64) string { return strconv.FormatInt(n*1e9/512, 10) }
	mk(t, root, "pci0000:00/0000:00:1d.0/0000:03:00.0/nvme/nvme0/nvme0n1", map[string]string{"size": gb(256), "removable": "0", "queue/rotational": "0", "device/model": "Example NVMe 256GB  "})
	mk(t, root, "pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0/block/sda", map[string]string{"size": gb(1000), "removable": "0", "queue/rotational": "1", "device/model": "Example HDD"})
	mk(t, root, "pci0000:00/0000:00:14.0/usb2/2-1/2-1:1.0/host6/target6:0:0/6:0:0:0/block/sdb", map[string]string{"size": gb(32), "removable": "1", "queue/rotational": "0", "device/model": "Stick"})
	mk(t, root, "pci0000:00/0000:00:14.0/usb2/2-2/2-2:1.0/host7/target7:0:0/7:0:0:0/block/sdc", map[string]string{"size": gb(8), "removable": "1", "queue/rotational": "0"})
	mk(t, root, "virtual/block/loop0", map[string]string{"size": gb(1)})
	mk(t, root, "virtual/block/zram0", map[string]string{"size": gb(4)})

	ds, err := List(root, "sdb")
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 4 {
		t.Fatalf("%d disks: %+v", len(ds), ds)
	}
	c := Candidates(ds)
	var names []string
	for _, d := range c {
		names = append(names, d.Name)
	}
	// sda (1 TB) before nvme0n1 (256 GB), stick sdb excluded (boot), sdc too small
	if strings.Join(names, " ") != "sda nvme0n1" {
		t.Errorf("candidates %v", names)
	}
	if l := c[1].Label(); l != "NVMe SSD Example NVMe 256GB (256 GB)" {
		t.Errorf("Label %q", l)
	}
	if l := c[0].Label(); l != "Hard disk Example HDD (1.0 TB)" {
		t.Errorf("Label %q", l)
	}
}

func TestPartPath(t *testing.T) {
	for dev, want := range map[string]string{"/dev/sda": "/dev/sda2", "/dev/nvme0n1": "/dev/nvme0n1p2", "/dev/mmcblk0": "/dev/mmcblk0p2", "/dev/vda": "/dev/vda2"} {
		if got := PartPath(dev, 2); got != want {
			t.Errorf("%s → %s", dev, got)
		}
	}
}

func TestBootDisk(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "devices/x/block/sdb/sdb1"), 0o755)
	os.WriteFile(filepath.Join(root, "devices/x/block/sdb/sdb1/partition"), []byte("1\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "class/block"), 0o755)
	os.Symlink("../../devices/x/block/sdb/sdb1", filepath.Join(root, "class/block/sdb1"))
	mi := filepath.Join(root, "mountinfo")
	os.WriteFile(mi, []byte("22 1 8:17 / / rw,relatime shared:1 - ext4 /dev/sdb1 rw\n30 22 0:5 / /dev rw - devtmpfs udev rw\n"), 0o644)
	if got := BootDisk(root, mi, "/"); got != "sdb" {
		t.Errorf("BootDisk = %q", got)
	}
	if got := BootDisk(root, mi, "/run/missing"); got != "" {
		t.Errorf("%q", got)
	}
}

func TestSfdiskScript(t *testing.T) {
	s := SfdiskScript()
	if !strings.HasPrefix(s, "label: gpt\nsize=1024MiB, type=C12A7328") || !strings.Contains(s, TypeRootX64) {
		t.Error(s)
	}
}
