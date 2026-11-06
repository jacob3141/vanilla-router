// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

// vr-stick is the stick creator for Windows (PLAN.md 3.12, F-STICK). It does not exist yet on other
// systems (Q-18), where the image is written with dd.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "vr-stick is currently only available for Windows (PLAN.md 3.12, Q-18).")
	fmt.Fprintln(os.Stderr, "On Linux: sudo dd if=vanilla-router-stick.img of=/dev/sdX bs=4M conv=fsync status=progress")
	os.Exit(1)
}
