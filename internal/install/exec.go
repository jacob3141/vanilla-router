// SPDX-License-Identifier: AGPL-3.0-or-later

package install

import (
	"bytes"
	"os"
	"os/exec"
)

func execRun(env []string, stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(append(os.Environ(), "LC_ALL=C"), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.Bytes(), err
}

// CheckRoot reports an error if the program is not running as root.
func CheckRoot() error {
	if os.Geteuid() != 0 {
		return errNoRoot
	}
	return nil
}
