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

// registerFiles forwards /api/files/* to the file service (runs as smbuser, Unix socket).
// Login and CSRF checks happen here in the web server, before anything is passed on.
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
			pr.Out.Header.Del("Cookie") // The file service does not need session data
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
			MaxIdleConns:    4,
			IdleConnTimeout: 30 * time.Second,
		},
		FlushInterval: -1, // pass downloads through immediately
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.cfg.Log.Error("file service", "err", err)
			writeError(w, http.StatusBadGateway, "The file service is not set up or not responding (file share extension).")
		},
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Big uploads and downloads may take longer than the usual server timeouts
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
