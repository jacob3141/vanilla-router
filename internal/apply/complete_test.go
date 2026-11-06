// SPDX-License-Identifier: AGPL-3.0-or-later

package apply

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type completeRunner struct {
	calls []string
	fail  string // first word of the apt-get subcommand that fails
}

func (r *completeRunner) Run(env []string, name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, call)
	if name == "dpkg-query" {
		return []byte("ii  linux-firmware\nii  linux-firmware-realtek\nii  linux-firmware-intel-wireless\nun  linux-firmware-raspi2\n"), nil
	}
	if name == "apt-get" && r.fail != "" && args[0] == r.fail {
		return []byte("simulated"), errors.New("exit status 100")
	}
	return nil, nil
}

func completeSetup(t *testing.T) (*Applier, *completeRunner, string) {
	t.Helper()
	r := &completeRunner{}
	root := t.TempDir()
	a := &Applier{Root: root, Run: r}
	p := filepath.Join(root, OnlinePackagesFile)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("ubuntu-server\n"), 0o644)
	os.MkdirAll(filepath.Join(root, filepath.Dir(CompleteStamp)), 0o755)
	return a, r, filepath.Join(root, CompleteStamp)
}

func TestCompleteFetchesPackagesAndAllFirmware(t *testing.T) {
	a, r, stamp := completeSetup(t)
	if err := a.Complete(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Error("stamp missing:", err)
	}
	has := func(parts ...string) bool {
		return slices.ContainsFunc(r.calls, func(c string) bool {
			for _, p := range parts {
				if !strings.Contains(c, p) {
					return false
				}
			}
			return true
		})
	}
	if !has("apt-get update", "Error-Mode=any") {
		t.Error("update must fail if no server is reachable:", r.calls)
	}
	if !has("apt-get install", "ubuntu-server") {
		t.Error("packages not installed:", r.calls)
	}
	// Ubuntu splits the firmware: the package linux-firmware alone would restore almost nothing.
	if !has("apt-get install --reinstall", "linux-firmware-realtek", "linux-firmware-intel-wireless") {
		t.Error("firmware not restored:", r.calls)
	}
	if has("--reinstall", "linux-firmware-raspi2") {
		t.Error("package that is not installed was named:", r.calls)
	}
	if !has("systemctl", "ModemManager.service") {
		t.Error("services of the new packages not turned off again:", r.calls)
	}
}

func TestCompleteWithoutInternetTriesAgainLater(t *testing.T) {
	a, r, stamp := completeSetup(t)
	r.fail = "update"
	if err := a.Complete(); err == nil {
		t.Fatal("expected an error so that the timer tries again")
	}
	if _, err := os.Stat(stamp); err == nil {
		t.Error("stamp written although nothing was fetched")
	}
}

func TestCompleteRunsOnlyOnce(t *testing.T) {
	a, r, stamp := completeSetup(t)
	os.WriteFile(stamp, []byte("done\n"), 0o644)
	if err := a.Complete(); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 0 {
		t.Error("ran again:", r.calls)
	}
}
