// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

// vr-stick ist der Stick-Ersteller für Windows (PLAN.md 3.12, F-STICK). Auf anderen Systemen gibt es
// ihn noch nicht (Q-18), dort schreibt man das Abbild mit dd.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "vr-stick gibt es derzeit nur für Windows (PLAN.md 3.12, Q-18).")
	fmt.Fprintln(os.Stderr, "Unter Linux: sudo dd if=vanilla-router-stick.img of=/dev/sdX bs=4M conv=fsync status=progress")
	os.Exit(1)
}
