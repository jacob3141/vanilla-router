// SPDX-License-Identifier: AGPL-3.0-or-later

package apply

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// The stick installs a small base system that works without internet, so that PPPoE lines can enter their
// credentials in the web interface first (D-17). Complete fetches the rest as soon as the router is online.
const (
	// OnlinePackagesFile lists the packages to fetch, one per line (image/packages-online.txt).
	OnlinePackagesFile = "/usr/share/vanilla-router/online-packages"
	// CompleteStamp marks a finished system. vr-complete.service does not run once it exists. It lies on the root
	// subvolume, not in the state folder: a rollback takes the packages away, so it must take the stamp as well.
	CompleteStamp = "/var/lib/vanilla-router-online-complete"
)

// aptEnv makes apt run without questions and keep local configuration files.
var aptEnv = []string{"DEBIAN_FRONTEND=noninteractive", "LC_ALL=C"}

// Complete installs the remaining packages and adjusts the system again, because new packages bring services
// that Setup turns off. Without internet it returns an error and vr-complete.timer tries again later.
// Running it several times does no harm.
func (a *Applier) Complete() error {
	if _, err := os.Stat(a.p(CompleteStamp)); err == nil {
		return nil
	}
	raw, err := os.ReadFile(a.p(OnlinePackagesFile))
	if err != nil {
		return err
	}
	pkgs := strings.Fields(string(raw))
	if len(pkgs) == 0 {
		return errors.New(OnlinePackagesFile + " is empty")
	}
	apt := func(args ...string) error {
		out, err := a.Run.Run(aptEnv, "apt-get", args...)
		if err != nil {
			return fmt.Errorf("apt-get %s: %v\n%s", args[0], err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// Not Error-Mode=any: the step needs Ubuntu's archive, and an unreachable vanilla-router repository must not
	// block it. Without internet the install below fails for lack of a candidate, and the timer tries again.
	// A missing Release file is an error even then, so only Ubuntu's archive is read here.
	if err := apt("update", "-o", "Dir::Etc::sourcelist=sources.list.d/ubuntu.sources", "-o", "Dir::Etc::sourceparts=-"); err != nil {
		return err
	}
	dpkg := []string{"-y", "-o", "DPkg::Lock::Timeout=600",
		"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}
	if err := apt(append([]string{"install"}, append(dpkg, pkgs...)...)...); err != nil {
		return err
	}
	// The image only keeps the firmware of network cards (image/target/mkosi.postinst.chroot). Ubuntu splits
	// the firmware into linux-firmware-*, the package linux-firmware alone would restore almost nothing.
	out, err := a.Run.Run(aptEnv, "dpkg-query", "-W", "-f=${db:Status-Abbrev} ${Package}\n", "linux-firmware*")
	if err != nil {
		return fmt.Errorf("dpkg-query: %v", err)
	}
	// The pattern also lists packages that dpkg only knows by name, such as linux-firmware-raspi2.
	var fw []string
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "ii" {
			fw = append(fw, f[1])
		}
	}
	if len(fw) > 0 {
		if err := apt(append([]string{"install", "--reinstall"}, append(dpkg, fw...)...)...); err != nil {
			return err
		}
	}
	if _, err := a.Setup(false, ""); err != nil {
		return err
	}
	return os.WriteFile(a.p(CompleteStamp), []byte("done\n"), 0o644)
}
