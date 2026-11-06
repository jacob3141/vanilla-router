// SPDX-License-Identifier: AGPL-3.0-or-later

// Package snapshot backs up the system before updates and automatically rolls back a broken update
// (M5, P-06, F-UPD-02/03, Q-03). Without snapper (universe, D-10), only with btrfs and GRUB:
//
//  1. Before each apt run: snapshot of @ to @snapshots/<id>, boot entry "state before the update"
//     in ESP/vr/rollback.cfg, status in ESP/vr/grubenv: vr_pending=1 (next start is an attempt).
//  2. GRUB (42_vanilla_router): if vr_pending=1 and vr_trial is already 1, the last attempt never reached
//     the health check → the default is the snapshot. Otherwise set vr_trial=1 and start normally.
//     The environment is on the ESP (FAT), because GRUB cannot write to btrfs.
//  3. vr-health.service: if the home network works, the start is confirmed (vr_pending=0, vr_trial=0).
//     If started from a snapshot (vr.rollback=<id> on the kernel line), it becomes permanent:
//     @ becomes @broken-<time>, the snapshot becomes the new @.
package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Runner runs programs.
type Runner interface {
	Run(env []string, name string, args ...string) ([]byte, error)
}

// Manager manages snapshots.
type Manager struct {
	Run      Runner
	Root     string // "/" in production
	SnapDir  string // mount point of @snapshots, default /.snapshots
	ESP      string // default /boot/efi
	Device   string // btrfs device for mounting the top level, default from findmnt
	RootUUID string // UUID of the btrfs, default from findmnt
	Now      func() time.Time
	Keep     int // keep this many update snapshots, default 5
}

// Info describes a snapshot.
type Info struct {
	ID      string    `json:"id"`
	Created time.Time `json:"created"`
	Kind    string    `json:"kind"` // pre-update, manual
	Note    string    `json:"note,omitempty"`
}

func (m *Manager) p(rel string) string { return filepath.Join(m.Root, filepath.FromSlash(rel)) }
func (m *Manager) snapDir() string {
	if m.SnapDir == "" {
		return "/.snapshots"
	}
	return m.SnapDir
}
func (m *Manager) esp() string {
	if m.ESP == "" {
		return "/boot/efi"
	}
	return m.ESP
}
func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) run(name string, args ...string) (string, error) {
	out, err := m.Run.Run([]string{"LC_ALL=C"}, name, args...)
	if err != nil {
		return "", fmt.Errorf("%s %s: %v %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (m *Manager) grubenv() string { return filepath.Join(m.esp(), "vr", "grubenv") }

func (m *Manager) setEnv(kv ...string) error {
	if err := os.MkdirAll(m.p(filepath.Join(m.esp(), "vr")), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(m.p(m.grubenv())); errors.Is(err, os.ErrNotExist) {
		if _, err := m.run("grub-editenv", m.p(m.grubenv()), "create"); err != nil {
			return err
		}
	}
	_, err := m.run("grub-editenv", append([]string{m.p(m.grubenv()), "set"}, kv...)...)
	return err
}

// List returns the snapshots, oldest first.
func (m *Manager) List() ([]Info, error) {
	files, err := filepath.Glob(m.p(filepath.Join(m.snapDir(), "*.json")))
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var i Info
		if json.Unmarshal(b, &i) == nil && i.ID != "" {
			out = append(out, i)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}

// PreUpdate creates a snapshot before an apt run. Several dpkg calls in one apt run
// result in only one snapshot (at least 10 minutes apart).
func (m *Manager) PreUpdate(note string) (*Info, error) {
	list, err := m.List()
	if err != nil {
		return nil, err
	}
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Kind == "pre-update" && m.now().Sub(list[i].Created) < 10*time.Minute {
			return nil, nil
		}
	}
	info, err := m.Create("pre-update", note)
	if err != nil {
		return nil, err
	}
	if err := m.writeRollbackEntry(*info); err != nil {
		return nil, err
	}
	if err := m.setEnv("vr_pending=1", "vr_trial=0", "vr_snapshot="+info.ID); err != nil {
		return nil, err
	}
	m.Prune()
	return info, nil
}

// Create creates a snapshot of /.
func (m *Manager) Create(kind, note string) (*Info, error) {
	id := m.now().UTC().Format("20060102-150405") + "-" + kind
	dst := filepath.Join(m.snapDir(), id)
	if _, err := m.run("btrfs", "subvolume", "snapshot", m.p("/"), m.p(dst)); err != nil {
		return nil, err
	}
	info := &Info{ID: id, Created: m.now(), Kind: kind, Note: note}
	b, _ := json.MarshalIndent(info, "", "  ")
	if err := os.WriteFile(m.p(dst+".json"), b, 0o644); err != nil {
		return nil, err
	}
	return info, nil
}

// Pending reports whether the running start is an attempt after an update (vr_pending=1).
func (m *Manager) Pending() bool {
	out, err := m.run("grub-editenv", m.p(m.grubenv()), "list")
	if err != nil {
		return false
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == "vr_pending=1" {
			return true
		}
	}
	return false
}

// writeRollbackEntry writes the GRUB entry for the snapshot to the ESP. Kernel and initrd come
// from the snapshot itself, so they always match the system in it. Directly with rw: the fstab in the
// snapshot names subvol=@, a later remount from ro to rw would fail on that.
func (m *Manager) writeRollbackEntry(i Info) error {
	uuid, err := m.rootUUID()
	if err != nil {
		return err
	}
	sub := "@snapshots/" + i.ID
	when := i.Created.Local().Format("02.01.2006 15:04")
	cfg := fmt.Sprintf(`# Generated by vanilla-router (vrctl snapshot), do not edit by hand.
menuentry 'vanilla-router: state before the update of %s' --id vr-rollback {
	insmod btrfs
	search --no-floppy --fs-uuid --set=root %s
	linux /%s/boot/vmlinuz root=UUID=%s rootflags=subvol=%s rw quiet vr.rollback=%s
	initrd /%s/boot/initrd.img
}
`, when, uuid, sub, uuid, sub, i.ID, sub)
	if err := os.MkdirAll(m.p(filepath.Join(m.esp(), "vr")), 0o755); err != nil {
		return err
	}
	return os.WriteFile(m.p(filepath.Join(m.esp(), "vr", "rollback.cfg")), []byte(cfg), 0o644)
}

func (m *Manager) rootUUID() (string, error) {
	if m.RootUUID != "" {
		return m.RootUUID, nil
	}
	return m.run("findmnt", "-no", "UUID", "/")
}

// Prune deletes old update snapshots, the newest Keep stay.
func (m *Manager) Prune() {
	keep := m.Keep
	if keep == 0 {
		keep = 5
	}
	list, _ := m.List()
	var upd []Info
	for _, i := range list {
		if i.Kind == "pre-update" {
			upd = append(upd, i)
		}
	}
	for len(upd) > keep {
		old := upd[0]
		upd = upd[1:]
		dst := filepath.Join(m.snapDir(), old.ID)
		if _, err := m.run("btrfs", "subvolume", "delete", m.p(dst)); err == nil {
			os.Remove(m.p(dst + ".json"))
		}
	}
}

// BootedSnapshot returns the ID if started from a rollback entry.
func (m *Manager) BootedSnapshot() string {
	b, err := os.ReadFile(m.p("/proc/cmdline"))
	if err != nil {
		return ""
	}
	for _, f := range strings.Fields(string(b)) {
		if v, ok := strings.CutPrefix(f, "vr.rollback="); ok {
			return v
		}
	}
	return ""
}

// MarkGood confirms the current start. If the system ran from a snapshot, it permanently
// becomes the system (finishing the rollback) and true is returned.
func (m *Manager) MarkGood() (rolledBack bool, err error) {
	if id := m.BootedSnapshot(); id != "" {
		if err := m.MakePermanent(id); err != nil {
			return false, err
		}
		rolledBack = true
	}
	return rolledBack, m.setEnv("vr_pending=0", "vr_trial=0")
}

// MakePermanent makes snapshot id the new @. The old @ stays as @broken-<time> for analysis.
// Takes effect at the next start, the running system stays untouched.
func (m *Manager) MakePermanent(id string) error {
	if strings.ContainsAny(id, "/ ") || id == "" {
		return fmt.Errorf("invalid snapshot ID %q", id)
	}
	dev := m.Device
	if dev == "" {
		var err error
		if dev, err = m.run("findmnt", "-no", "SOURCE", "--target", m.snapDir()); err != nil {
			return err
		}
		dev, _, _ = strings.Cut(dev, "[") // „/dev/sda2[/@snapshots]“
	}
	top := m.p("/run/vr-btrfs-top")
	if err := os.MkdirAll(top, 0o700); err != nil {
		return err
	}
	if _, err := m.run("mount", "-o", "subvolid=5", dev, top); err != nil {
		return err
	}
	defer m.run("umount", top)
	broken := "@broken-" + m.now().UTC().Format("20060102-150405")
	if err := os.Rename(filepath.Join(top, "@"), filepath.Join(top, broken)); err != nil {
		return fmt.Errorf("renaming @: %w", err)
	}
	if _, err := m.run("btrfs", "subvolume", "snapshot", filepath.Join(top, "@snapshots", id), filepath.Join(top, "@")); err != nil {
		os.Rename(filepath.Join(top, broken), filepath.Join(top, "@")) // back, break nothing
		return err
	}
	return nil
}
