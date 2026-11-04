// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"vanilla-router/internal/sys"
)

// Dienste, die auf der Übersicht als Kacheln erscheinen
var dashboardUnits = []string{
	"vr-dnsmasq", "systemd-networkd", "vr-firewall", "vr-pppoe", "ssh", "unattended-upgrades",
	"smbd", "docker",
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	gw, gwDev := sys.DefaultGatewayV4()
	temp, hasTemp := sys.Temperature()

	online := 0
	if neigh, err := sys.Neighbors(r.Context(), "lan0"); err == nil {
		for _, n := range neigh {
			if n.Online() {
				online++
			}
		}
	}

	helperOK := s.call(r, "health", nil, nil) == nil

	resp := map[string]any{
		"hostname":  hostname,
		"release":   sys.OSRelease(),
		"kernel":    sys.Kernel(),
		"uptime_s":  int64(sys.Uptime().Seconds()),
		"load":      sys.LoadAvg(),
		"wan":       sys.ReadInterface(cmp.Or(s.cfg.WANIf, "wan0")),
		"lan":       sys.ReadInterface("lan0"),
		"gateway":   map[string]string{"ip": gw, "dev": gwDev},
		"clients":   online,
		"services":  sys.UnitState(r.Context(), dashboardUnits...),
		"helper_ok": helperOK,
		"live":      s.cfg.Sampler.Live(),
		"sessions":  s.cfg.Sessions.Count(),
	}
	if hasTemp {
		resp["temperature"] = temp
	}
	writeJSON(w, resp)
}

// handleEvents sendet alle 2 Sekunden die Live-Werte als Server-Sent Events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming nicht möglich.")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	rc := http.NewResponseController(w)
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	send := func() bool {
		b, _ := json.Marshal(s.cfg.Sampler.Live())
		rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send() {
		return
	}
	// Sitzung läuft ab → Stream beenden, das Frontend meldet sich dann neu an
	cookie, _ := r.Cookie(cookieName)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
			if cookie == nil || !s.cfg.Sessions.Valid(cookie.Value) {
				return
			}
			if !send() {
				return
			}
		}
	}
}
