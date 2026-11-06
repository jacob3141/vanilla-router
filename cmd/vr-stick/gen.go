// SPDX-License-Identifier: AGPL-3.0-or-later

// The application manifest (app.manifest) is embedded as a resource. This file applies to all
// systems because go generate skips files with build constraints.

//go:generate go run ../../internal/winres/mkres -o rsrc_windows_amd64.syso app.manifest

package main
