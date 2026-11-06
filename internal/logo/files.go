// SPDX-License-Identifier: AGPL-3.0-or-later

package logo

// File is a generated logo file in the repo (path relative to the root folder).
type File struct {
	Path string
	Data func() []byte
}

// Files are all logo files that mklogo writes and the test compares.
func Files() []File {
	return []File{
		{"docs/logo/logo.svg", SVG},
		{"docs/logo/logo-512.png", func() []byte { return PNG(512) }},
		{"docs/logo/logo.ico", ICO},
		{"web/static/favicon.svg", SVG},
	}
}
