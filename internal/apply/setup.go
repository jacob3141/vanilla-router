// SPDX-License-Identifier: AGPL-3.0-or-later

package apply

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Setup adjusts a freshly installed Ubuntu Server once (D-12, PLAN.md 3.2). No packages are
// removed, only services and files are set. Running it several times does no harm.
// Returns the steps executed (for the log and dry run).
//
// offline != "": the system is not running (installer). systemctl then works with --root=offline and
// without --now, netplan generate is skipped. a.Root must point to the same target in this case.
func (a *Applier) Setup(dryRun bool, offline string) ([]string, error) {
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
	// Services that may be missing (depending on the Ubuntu variant) are skipped without an error.
	sysctl := func(args []string) []string {
		if offline == "" {
			return args
		}
		out := []string{"--root=" + offline}
		for _, a := range args {
			if a != "--now" {
				out = append(out, a)
			}
		}
		return out
	}
	soft := func(args ...string) func() error {
		return func() error { a.Run.Run(nil, "systemctl", sysctl(args)...); return nil }
	}
	hard := func(args ...string) func() error {
		return func() error {
			out, err := a.Run.Run(nil, "systemctl", sysctl(args)...)
			if err != nil {
				return errors.New(strings.TrimSpace(string(out)))
			}
			return nil
		}
	}

	step("create folders /etc/vanilla-router/secrets (0700) and /var/lib/vanilla-router", func() error {
		if err := os.MkdirAll(a.p("/etc/vanilla-router/secrets"), 0o700); err != nil {
			return err
		}
		os.Chmod(a.p("/etc/vanilla-router/secrets"), 0o700)
		return os.MkdirAll(a.p(a.stateDir()), 0o755)
	})
	step("turn off cloud-init (/etc/cloud/cloud-init.disabled)", func() error {
		if err := os.MkdirAll(a.p("/etc/cloud"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(a.p("/etc/cloud/cloud-init.disabled"), []byte("vanilla-router: network comes from vrctl\n"), 0o644)
	})
	step("retire netplan files (*.yaml → *.yaml.vr-off), network only comes from vrctl", func() error {
		matches, _ := filepath.Glob(a.p("/etc/netplan/*.yaml"))
		for _, m := range matches {
			if err := os.Rename(m, m+".vr-off"); err != nil {
				return err
			}
		}
		if len(matches) > 0 && offline == "" {
			a.Run.Run(nil, "netplan", "generate") // removes the generated files under /run
		}
		return nil
	})
	step("point /etc/resolv.conf at /run/systemd/resolve/resolv.conf (stub is off)", func() error {
		p := a.p("/etc/resolv.conf")
		if t, err := os.Readlink(p); err == nil && t == "../run/systemd/resolve/resolv.conf" {
			return nil
		}
		os.Remove(p)
		return os.Symlink("../run/systemd/resolve/resolv.conf", p)
	})
	step("turn off unneeded services: snapd, multipathd, iscsid, NetworkManager", soft("disable", "--now",
		"snapd.socket", "snapd.service", "snapd.seeded.service", "multipathd.socket", "multipathd.service",
		"iscsid.socket", "open-iscsi.service", "NetworkManager.service"))
	step("turn on network services: systemd-networkd, vr-firewall, vr-dnsmasq, health check", hard("enable",
		"systemd-networkd.service", "vr-firewall.service", "vr-firewall-reload.timer", "vr-dnsmasq.service",
		"vr-health.service", "vr-webui-helper.service", "vr-webui.service", "vr-complete.timer"))
	step("SSH off by default (F-SEC-04)", soft("disable", "--now", "ssh.socket", "ssh.service"))
	return steps, errors.Join(errs...)
}
