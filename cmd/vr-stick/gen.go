// SPDX-License-Identifier: AGPL-3.0-or-later

// Das Anwendungsmanifest (app.manifest) wird als Ressource eingebunden. Die Datei gilt für alle
// Systeme, weil go generate Dateien mit Build-Bedingungen überspringt.

//go:generate go run ../../internal/winres/mkres -o rsrc_windows_amd64.syso app.manifest

package main
