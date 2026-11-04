// SPDX-License-Identifier: AGPL-3.0-or-later

package apply

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Setup passt ein frisch installiertes Ubuntu Server einmalig an (D-12, PLAN.md 3.2). Es werden keine
// Pakete entfernt, nur Dienste und Dateien eingestellt. Mehrfaches Ausführen ist unschädlich.
// Liefert die ausgeführten Schritte (für Protokoll und Probelauf).
func (a *Applier) Setup(dryRun bool) ([]string, error) {
	var steps []string
	var errs []error
	step := func(desc string, fn func() error) {
		steps = append(steps, desc)
		if dryRun {
			return
		}
		if err := fn(); err != nil {
			errs = append(errs, errors.New(desc+": "+err.Error()))
		}
	}
	// Dienste, die fehlen dürfen (je nach Ubuntu-Variante), werden ohne Fehler übersprungen.
	soft := func(args ...string) func() error {
		return func() error { a.Run.Run(nil, "systemctl", args...); return nil }
	}
	hard := func(args ...string) func() error {
		return func() error {
			out, err := a.Run.Run(nil, "systemctl", args...)
			if err != nil {
				return errors.New(strings.TrimSpace(string(out)))
			}
			return nil
		}
	}

	step("Ordner /etc/vanilla-router/secrets (0700) und /var/lib/vanilla-router anlegen", func() error {
		if err := os.MkdirAll(a.p("/etc/vanilla-router/secrets"), 0o700); err != nil {
			return err
		}
		os.Chmod(a.p("/etc/vanilla-router/secrets"), 0o700)
		return os.MkdirAll(a.p(a.stateDir()), 0o755)
	})
	step("cloud-init abschalten (/etc/cloud/cloud-init.disabled)", func() error {
		if err := os.MkdirAll(a.p("/etc/cloud"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(a.p("/etc/cloud/cloud-init.disabled"), []byte("vanilla-router: Netzwerk kommt von vrctl\n"), 0o644)
	})
	step("netplan-Dateien stilllegen (*.yaml → *.yaml.vr-aus), Netzwerk kommt nur von vrctl", func() error {
		matches, _ := filepath.Glob(a.p("/etc/netplan/*.yaml"))
		for _, m := range matches {
			if err := os.Rename(m, m+".vr-aus"); err != nil {
				return err
			}
		}
		if len(matches) > 0 {
			a.Run.Run(nil, "netplan", "generate") // entfernt die erzeugten Dateien unter /run
		}
		return nil
	})
	step("/etc/resolv.conf auf /run/systemd/resolve/resolv.conf zeigen lassen (Stub ist aus)", func() error {
		p := a.p("/etc/resolv.conf")
		if t, err := os.Readlink(p); err == nil && t == "../run/systemd/resolve/resolv.conf" {
			return nil
		}
		os.Remove(p)
		return os.Symlink("../run/systemd/resolve/resolv.conf", p)
	})
	step("unnötige Dienste abschalten: snapd, multipathd, iscsid, NetworkManager", soft("disable", "--now",
		"snapd.socket", "snapd.service", "snapd.seeded.service", "multipathd.socket", "multipathd.service",
		"iscsid.socket", "open-iscsi.service", "NetworkManager.service"))
	step("Netzwerkdienste einschalten: systemd-networkd, vr-firewall, vr-dnsmasq", hard("enable",
		"systemd-networkd.service", "vr-firewall.service", "vr-firewall-reload.timer", "vr-dnsmasq.service"))
	step("SSH ab Werk aus (F-SEC-04)", soft("disable", "--now", "ssh.socket", "ssh.service"))
	return steps, errors.Join(errs...)
}
