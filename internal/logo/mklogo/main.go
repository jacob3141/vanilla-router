// SPDX-License-Identifier: AGPL-3.0-or-later

// mklogo schreibt die Logo-Dateien des Projekts neu (aus dem Wurzelordner des Repos aufrufen):
//
//	go run ./internal/logo/mklogo
package main

import (
	"fmt"
	"os"

	"vanilla-router/internal/logo"
)

func main() {
	for _, f := range logo.Files() {
		if err := os.WriteFile(f.Path, f.Data(), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(f.Path)
	}
}
