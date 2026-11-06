// SPDX-License-Identifier: AGPL-3.0-or-later

package snapshot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRunner imitates btrfs and grub-editenv with folders and files.
type fakeRunner struct {
	t     *testing.T
	calls []string
	env   map[string]string
	fail  string
}

func (f *fakeRunner) Run(env []string, name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, call)
	if f.fail != "" && strings.HasPrefix(call, f.fail) {
		return nil, errors.New("exit status 1")
	}
	switch {
	case name == "btrfs" && args[1] == "snapshot":
		return nil, os.MkdirAll(args[3], 0o755)
	case name == "btrfs" && args[1] == "delete":
		return nil, os.RemoveAll(args[2])
	case name == "grub-editenv" && args[1] == "create":
		return nil, os.WriteFile(args[0], []byte("# GRUB Environment Block\n"), 0o644)
	case name == "grub-editenv" && args[1] == "set":
		for _, kv := range args[2:] {
			k, v, _ := strings.Cut(kv, "=")
			f.env[k] = v
		}
	case name == "findmnt":
		return []byte("/dev/sda2[/@snapshots]\n"), nil
	}
	return nil, nil
}

func setup(t *testing.T) (*Manager, *fakeRunner, *time.Time) {
	root := t.TempDir()
	for _, d := range []string{".snapshots", "boot/efi", "proc"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	r := &fakeRunner{t: t, env: map[string]string{}}
	now := time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC)
	m := &Manager{Run: r, Root: root, RootUUID: "11111111-2222-3333-4444-555555555555", Keep: 2,
		Now: func() time.Time { return now }}
	return m, r, &now
}

func TestPreUpdate(t *testing.T) {
	m, r, now := setup(t)
	info, err := m.PreUpdate("apt")
	if err != nil || info == nil {
		t.Fatal(info, err)
	}
	if info.ID != "20261104-030000-pre-update" {
		t.Errorf("ID %s", info.ID)
	}
	if r.env["vr_pending"] != "1" || r.env["vr_trial"] != "0" || r.env["vr_snapshot"] != info.ID {
		t.Errorf("grubenv %v", r.env)
	}
	cfg, _ := os.ReadFile(filepath.Join(m.Root, "boot/efi/vr/rollback.cfg"))
	for _, want := range []string{"--id vr-rollback", "rootflags=subvol=@snapshots/20261104-030000-pre-update",
		"vr.rollback=20261104-030000-pre-update", "initrd /@snapshots/20261104-030000-pre-update/boot/initrd.img"} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("rollback.cfg without %q:\n%s", want, cfg)
		}
	}
	// second dpkg call in the same apt run: no new snapshot
	*now = now.Add(2 * time.Minute)
	if info, _ := m.PreUpdate("apt"); info != nil {
		t.Error("duplicate snapshot")
	}
	// Cleanup: only Keep=2 stay
	for i := 0; i < 3; i++ {
		*now = now.Add(time.Hour)
		if _, err := m.PreUpdate("apt"); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := m.List()
	if len(list) != 2 {
		t.Errorf("%d snapshots after cleanup: %+v", len(list), list)
	}
}

func TestMarkGoodNormal(t *testing.T) {
	m, r, _ := setup(t)
	os.WriteFile(filepath.Join(m.Root, "proc/cmdline"), []byte("BOOT_IMAGE=/@/boot/vmlinuz root=UUID=x ro quiet\n"), 0o644)
	rb, err := m.MarkGood()
	if err != nil || rb {
		t.Fatal(rb, err)
	}
	if r.env["vr_pending"] != "0" || r.env["vr_trial"] != "0" {
		t.Errorf("%v", r.env)
	}
}

func TestMarkGoodAfterRollback(t *testing.T) {
	m, r, _ := setup(t)
	top := filepath.Join(m.Root, "run/vr-btrfs-top")
	os.MkdirAll(filepath.Join(top, "@"), 0o755)
	os.MkdirAll(filepath.Join(top, "@snapshots/20261104-030000-pre-update"), 0o755)
	os.WriteFile(filepath.Join(m.Root, "proc/cmdline"), []byte("root=UUID=x rootflags=subvol=@snapshots/20261104-030000-pre-update ro vr.rollback=20261104-030000-pre-update\n"), 0o644)
	rb, err := m.MarkGood()
	if err != nil || !rb {
		t.Fatal(rb, err)
	}
	if _, err := os.Stat(filepath.Join(top, "@broken-20261104-030000")); err != nil {
		t.Error("old @ not kept")
	}
	if _, err := os.Stat(filepath.Join(top, "@")); err != nil {
		t.Error("new @ is missing")
	}
	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "mount -o subvolid=5 /dev/sda2 ") || !strings.Contains(joined, "umount "+top) {
		t.Errorf("mounting the top level:\n%s", joined)
	}
}

func TestMakePermanentErrorRestores(t *testing.T) {
	m, r, _ := setup(t)
	top := filepath.Join(m.Root, "run/vr-btrfs-top")
	os.MkdirAll(filepath.Join(top, "@"), 0o755)
	r.fail = "btrfs subvolume snapshot"
	if err := m.MakePermanent("20261104-030000-pre-update"); err == nil {
		t.Fatal("error expected")
	}
	if _, err := os.Stat(filepath.Join(top, "@")); err != nil {
		t.Error("@ not renamed back after an error")
	}
	if err := m.MakePermanent("../x"); err == nil {
		t.Error("path accepted")
	}
}
