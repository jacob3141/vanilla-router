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
	// CompleteStamp marks a finished system. vr-complete.service does not run once it exists.
	CompleteStamp = "/var/lib/vanilla-router/online-complete"
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
	// Error-Mode=any: by default apt-get update reports success even if no server could be reached.
	if err := apt("update", "-o", "APT::Update::Error-Mode=any"); err != nil {
		return err
	}
	dpkg := []string{"-y", "-o", "DPkg::Lock::Timeout=600",
		"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}
	if err := apt(append([]string{"install"}, append(dpkg, pkgs...)...)...); err != nil {
		return err
	}
	// The image only keeps the firmware of network cards (image/target/mkosi.postinst.chroot).
	if err := apt(append([]string{"install", "--reinstall"}, append(dpkg, "linux-firmware")...)...); err != nil {
		return err
	}
	if _, err := a.Setup(false, ""); err != nil {
		return err
	}
	return os.WriteFile(a.p(CompleteStamp), []byte("done\n"), 0o644)
}
