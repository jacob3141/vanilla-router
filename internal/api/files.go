// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
)

// registerFiles leitet /api/files/* an den Dateidienst weiter (läuft als smbuser, Unix-Socket).
// Anmeldung und CSRF-Prüfung passieren hier im Webserver, bevor etwas weitergereicht wird.
func (s *Server) registerFiles() {
	if s.cfg.Files == "" {
		return
	}
	sock := s.cfg.Files
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "files"
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, "/api/files")
			pr.Out.Host = "files"
			pr.Out.Header.Del("Cookie") // Der Dateidienst braucht keine Sitzungsdaten
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
			MaxIdleConns:    4,
			IdleConnTimeout: 30 * time.Second,
		},
		FlushInterval: -1, // Downloads sofort durchreichen
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.cfg.Log.Error("dateidienst", "err", err)
			writeError(w, http.StatusBadGateway, "Der Dateidienst ist nicht eingerichtet oder antwortet nicht (Erweiterung Dateiablage).")
		},
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Große Up- und Downloads dürfen länger dauern als die üblichen Server-Timeouts
		rc := http.NewResponseController(w)
		rc.SetReadDeadline(time.Time{})
		rc.SetWriteDeadline(time.Time{})
		proxy.ServeHTTP(w, r)
	}
	for _, p := range []string{"GET /api/files/list", "GET /api/files/download", "POST /api/files/upload",
		"POST /api/files/mkdir", "POST /api/files/rename", "POST /api/files/delete"} {
		s.route(p, handler)
	}
}
