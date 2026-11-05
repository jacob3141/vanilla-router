// SPDX-License-Identifier: AGPL-3.0-or-later

package logo

// File ist eine erzeugte Logo-Datei im Repo (Pfad relativ zum Wurzelordner).
type File struct {
	Path string
	Data func() []byte
}

// Files sind alle Logo-Dateien, die mklogo schreibt und der Test vergleicht.
func Files() []File {
	return []File{
		{"docs/logo/logo.svg", SVG},
		{"docs/logo/logo-512.png", func() []byte { return PNG(512) }},
		{"docs/logo/logo.ico", ICO},
		{"web/static/favicon.svg", SVG},
	}
}
