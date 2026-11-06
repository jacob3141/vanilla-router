// SPDX-License-Identifier: AGPL-3.0-or-later

// Package web embeds the frontend in the binary.
package web

import "embed"

//go:embed static
var Files embed.FS
