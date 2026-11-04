// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vanilla-router/internal/conf"
	"vanilla-router/internal/helper"
)

func (s *Server) registerAdmin() {
	s.route("GET /api/system/disks", s.get("disk.status.get"))
	s.route("GET /api/system/hardware", s.get("hw.get"))
	s.route("GET /api/system/update-history", s.get("updates.history.get"))
	s.route("GET /api/fail2ban", s.get("fail2ban.status.get"))
	s.route("POST /api/fail2ban/unban", s.forward("fail2ban.unban", "Sperre aufgehoben."))
	s.route("GET /api/samba", s.get("samba.status.get"))
	s.route("GET /api/logs/system", s.handleJournal)
	s.route("GET /api/logs/changes", s.get("uilog.get"))
	s.route("GET /api/sessions", s.handleSessions)
	s.route("POST /api/sessions/delete", s.handleSessionDelete)
	s.route("GET /api/dhcp/settings", s.handleDHCPSettings)
	s.route("GET /api/config/export", s.handleExport)
	s.route("POST /api/config/import", s.handleImport)
	s.route("POST /api/tools/port", s.forward("tool.port", ""))
}

// Export: alles, was die Oberfläche verwaltet (keine Passwörter, keine Systemdateien).
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	var info helper.NetconfInfo
	if err := s.call(r, "netconf.get", nil, &info); err != nil {
		s.helperError(w, err)
		return
	}
	s.devices.mu.Lock()
	meta := map[string]DevMeta{}
	for k, v := range s.devices.m {
		if v.Alias != "" || v.Note != "" {
			meta[k] = DevMeta{Alias: v.Alias, Note: v.Note}
		}
	}
	s.devices.mu.Unlock()
	w.Header().Set("Content-Disposition", `attachment; filename="vanilla-router-konfiguration-`+time.Now().Format("2006-01-02")+`.json"`)
	b, _ := json.MarshalIndent(map[string]any{
		"routerui_export": 1, "exported": time.Now().UTC(), "state": info.State, "device_names": meta,
	}, "", "  ")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(b)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Export      int                `json:"routerui_export"`
		Exported    time.Time          `json:"exported"`
		State       json.RawMessage    `json:"state"`
		DeviceNames map[string]DevMeta `json:"device_names"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Export != 1 || len(req.State) == 0 {
		writeError(w, http.StatusBadRequest, "Das ist keine Export-Datei der Oberfläche.")
		return
	}
	var out map[string]any
	if err := s.call(r, "state.import", map[string]any{"state": req.State}, &out); err != nil {
		s.helperError(w, err)
		return
	}
	for mac, m := range req.DeviceNames {
		if n, err := conf.NormalizeMAC(mac); err == nil && len([]rune(m.Alias)) <= 64 && len([]rune(m.Note)) <= 500 {
			s.devices.SetMeta(n, m.Alias, m.Note)
		}
	}
	msg := "Konfiguration eingespielt."
	if fw, _ := out["firewall_changed"].(bool); fw {
		msg += " Weiterleitungen oder Sperren haben sich geändert: Bitte auf der Firewall-Seite innerhalb von 60 Sekunden bestätigen."
	}
	writeJSON(w, map[string]any{"ok": true, "message": msg, "data": out})
}

func (s *Server) handleJournal(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	atoi := func(k string, def int) int {
		if v, err := strconv.Atoi(q.Get(k)); err == nil {
			return v
		}
		return def
	}
	var out any
	err := s.call(r, "journal.get", map[string]any{
		"unit": q.Get("unit"), "priority": atoi("priority", 6), "query": q.Get("q"),
		"lines": atoi("lines", 300), "hours": atoi("hours", 24),
	}, &out)
	if err != nil {
		s.helperError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	tok := ""
	if c, err := r.Cookie(cookieName); err == nil {
		tok = c.Value
	}
	writeJSON(w, map[string]any{"sessions": s.cfg.Sessions.List(tok)})
}

func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.cfg.Sessions.DeleteID(req.ID) {
		writeError(w, http.StatusNotFound, "Diese Sitzung gibt es nicht mehr.")
		return
	}
	s.cfg.Log.Info("auth: session revoked", "ip", clientIP(r))
	writeJSON(w, map[string]any{"ok": true, "message": "Sitzung abgemeldet."})
}

// handleDHCPSettings zeigt die DHCP-Einstellungen aus config.json (nur lesen). Die Schlüssel heißen
// wie im Prototyp (dhcpd), damit das Frontend unverändert bleibt.
func (s *Server) handleDHCPSettings(w http.ResponseWriter, r *http.Request) {
	var info helper.NetconfInfo
	if err := s.call(r, "netconf.get", nil, &info); err != nil {
		s.helperError(w, err)
		return
	}
	router, _, _ := strings.Cut(info.LAN, "/")
	lease := ""
	if d, err := time.ParseDuration(info.Lease); err == nil {
		lease = strconv.Itoa(int(d.Seconds()))
	}
	settings := map[string]string{
		"subnet": conf.LANPrefix.String(), "option routers": router, "option domain-name-servers": router,
		"option domain-name": info.Domain, "default-lease-time": lease, "max-lease-time": lease, "authoritative": "",
	}
	writeJSON(w, map[string]any{"settings": settings, "pool": info.State.Pool, "reservations": len(info.State.Reservations)})
}
