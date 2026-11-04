// SPDX-License-Identifier: AGPL-3.0-or-later

// Package web bettet das Frontend in das Binary ein.
package web

import "embed"

//go:embed static
var Files embed.FS
