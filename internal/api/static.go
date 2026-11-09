// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

// staticHandler serves the web interface. The embedded files have no modification time, so without help a
// browser has nothing to validate its copy against and may keep an old version after an update. A hash of
// each file serves as its ETag, and no-cache makes the browser ask every time. An unchanged file is then
// only a short 304 answer.
func staticHandler(files fs.FS) http.Handler {
	tags := map[string]string{}
	// A file that cannot be read has no tag and is still served by the file server below
	fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, err := fs.ReadFile(files, p); err == nil {
			sum := sha256.Sum256(b)
			tags["/"+p] = `"` + hex.EncodeToString(sum[:16]) + `"`
		}
		return nil
	})
	next := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasSuffix(p, "/") {
			p += "index.html"
		}
		w.Header().Set("Cache-Control", "no-cache")
		if tag, ok := tags[p]; ok {
			w.Header().Set("ETag", tag)
		}
		next.ServeHTTP(w, r)
	})
}
