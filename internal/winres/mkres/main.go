// SPDX-License-Identifier: AGPL-3.0-or-later

// mkres schreibt ein Windows-Anwendungsmanifest und das Projektlogo als Icon in eine .syso
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
	out := flag.String("o", "", "Ausgabedatei (.syso)")
	flag.Parse()
	if *out == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Aufruf: mkres -o datei.syso app.manifest")
		os.Exit(2)
	}
	xml, err := os.ReadFile(flag.Arg(0))
	if err == nil {
		// Icon-Gruppe 1 (LoadIcon(hInstance, 1)), Bilder ab ID 1, Manifest 1
		res := append(winres.Icon(logo.ICO(), 1, 1), winres.Resource{Type: 24, ID: 1, Data: xml})
		err = os.WriteFile(*out, winres.Build(res), 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
