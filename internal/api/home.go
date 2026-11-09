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

// registerHome forwards /api/home/* to the Smart Home service (Unix socket). Login and CSRF checks happen here.
func (s *Server) registerHome() {
	if s.cfg.Home == "" {
		return
	}
	sock := s.cfg.Home
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "home"
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, "/api/home")
			pr.Out.Host = "home"
			pr.Out.Header.Del("Cookie")
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
			MaxIdleConns:    4,
			IdleConnTimeout: 30 * time.Second,
		},
		FlushInterval: -1, // live values go through at once
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if r.Context().Err() == nil {
				s.cfg.Log.Error("smart home service", "err", err)
				writeError(w, http.StatusBadGateway, "The Smart Home service is not responding.")
			}
		},
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		rc.SetWriteDeadline(time.Time{}) // the live stream stays open
		proxy.ServeHTTP(w, r)
	}
	for _, p := range []string{"GET /api/home/overview", "GET /api/home/events", "POST /api/home/device", "POST /api/home/room", "POST /api/home/room/delete", "POST /api/home/command"} {
		s.route(p, handler)
	}
}
