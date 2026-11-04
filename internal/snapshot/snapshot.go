// SPDX-License-Identifier: AGPL-3.0-or-later

// Package snapshot sichert das System vor Updates und nimmt ein kaputtes Update automatisch zurück
// (M5, P-06, F-UPD-02/03, Q-03). Ohne snapper (universe, D-10), nur mit btrfs und GRUB:
//
//  1. Vor jedem apt-Lauf: Snapshot von @ nach @snapshots/<id>, Boot-Eintrag „Stand vor dem Update“
//     in ESP/vr/rollback.cfg, Status in ESP/vr/grubenv: vr_pending=1 (nächster Start ist ein Versuch).
//  2. GRUB (42_vanilla_router): Ist vr_pending=1 und vr_trial schon 1, kam der letzte Versuch nie bei
//     der Gesundheitsprüfung an → Standard ist der Snapshot. Sonst vr_trial=1 setzen und normal starten.
//     Die Umgebung liegt auf der ESP (FAT), weil GRUB auf btrfs nicht schreiben kann.
//  3. vr-health.service: Läuft das Heimnetz, wird der Start bestätigt (vr_pending=0, vr_trial=0).
//     Wurde aus einem Snapshot gestartet (vr.rollback=<id> in der Kernel-Zeile), wird er dauerhaft:
//     @ wird zu @kaputt-<zeit>, der Snapshot wird das neue @.
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

// Runner führt Programme aus.
type Runner interface {
	Run(env []string, name string, args ...string) ([]byte, error)
}

// Manager verwaltet Snapshots.
type Manager struct {
	Run      Runner
	Root     string // "/" im Betrieb
	SnapDir  string // Einhängepunkt von @snapshots, Vorgabe /.snapshots
	ESP      string // Vorgabe /boot/efi
	Device   string // btrfs-Gerät für das Einhängen der obersten Ebene, Vorgabe aus findmnt
	RootUUID string // UUID des btrfs, Vorgabe aus findmnt
	Now      func() time.Time
	Keep     int // so viele Update-Snapshots aufheben, Vorgabe 5
}

// Info beschreibt einen Snapshot.
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

// List liefert die Snapshots, älteste zuerst.
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

// PreUpdate legt vor einem apt-Lauf einen Snapshot an. Mehrere dpkg-Aufrufe in einem apt-Lauf
// ergeben nur einen Snapshot (Abstand mindestens 10 Minuten).
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

// Create legt einen Snapshot von / an.
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

// Pending meldet, ob der laufende Start ein Versuch nach einem Update ist (vr_pending=1).
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

// writeRollbackEntry schreibt den GRUB-Eintrag für den Snapshot auf die ESP. Kernel und initrd kommen
// aus dem Snapshot selbst, passen also immer zum System darin. Direkt mit rw: Die fstab im Snapshot
// nennt subvol=@, ein späteres Umhängen von ro auf rw würde daran scheitern.
func (m *Manager) writeRollbackEntry(i Info) error {
	uuid, err := m.rootUUID()
	if err != nil {
		return err
	}
	sub := "@snapshots/" + i.ID
	when := i.Created.Local().Format("02.01.2006 15:04")
	cfg := fmt.Sprintf(`# Von vanilla-router erzeugt (vrctl snapshot), nicht von Hand ändern.
menuentry 'vanilla-router: Stand vor dem Update vom %s' --id vr-rollback {
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

// Prune löscht alte Update-Snapshots, die neuesten Keep bleiben.
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

// BootedSnapshot liefert die ID, wenn aus einem Rücknahme-Eintrag gestartet wurde.
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

// MarkGood bestätigt den aktuellen Start. Lief das System aus einem Snapshot, wird dieser dauerhaft
// zum System (Rücknahme abschließen) und es wird true geliefert.
func (m *Manager) MarkGood() (rolledBack bool, err error) {
	if id := m.BootedSnapshot(); id != "" {
		if err := m.MakePermanent(id); err != nil {
			return false, err
		}
		rolledBack = true
	}
	return rolledBack, m.setEnv("vr_pending=0", "vr_trial=0")
}

// MakePermanent macht den Snapshot id zum neuen @. Das alte @ bleibt als @kaputt-<zeit> zur Analyse.
// Wirkt beim nächsten Start, das laufende System bleibt unberührt.
func (m *Manager) MakePermanent(id string) error {
	if strings.ContainsAny(id, "/ ") || id == "" {
		return fmt.Errorf("ungültige Snapshot-Kennung %q", id)
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
	broken := "@kaputt-" + m.now().UTC().Format("20060102-150405")
	if err := os.Rename(filepath.Join(top, "@"), filepath.Join(top, broken)); err != nil {
		return fmt.Errorf("@ umbenennen: %w", err)
	}
	if _, err := m.run("btrfs", "subvolume", "snapshot", filepath.Join(top, "@snapshots", id), filepath.Join(top, "@")); err != nil {
		os.Rename(filepath.Join(top, broken), filepath.Join(top, "@")) // zurück, nichts kaputt machen
		return err
	}
	return nil
}
