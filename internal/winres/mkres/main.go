// SPDX-License-Identifier: AGPL-3.0-or-later

// mkres writes a Windows application manifest and the project logo as an icon into a .syso
// (go generate in cmd/vr-stick).
//
//	go run ./internal/winres/mkres -o cmd/vr-stick/rsrc_windows_amd64.syso cmd/vr-stick/app.manifest
package main

import (
	"flag"
	"fmt"
	"os"

	"vanilla-router/internal/logo"
	"vanilla-router/internal/winres"
)

func main() {
	out := flag.String("o", "", "output file (.syso)")
	flag.Parse()
	if *out == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: mkres -o file.syso app.manifest")
		os.Exit(2)
	}
	xml, err := os.ReadFile(flag.Arg(0))
	if err == nil {
		// icon group 1 (LoadIcon(hInstance, 1)), images from ID 1, manifest 1
		res := append(winres.Icon(logo.ICO(), 1, 1), winres.Resource{Type: 24, ID: 1, Data: xml})
		err = os.WriteFile(*out, winres.Build(res), 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
