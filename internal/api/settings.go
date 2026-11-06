// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import "net/http"

// Settings for the internet port, home network and system (F-UI-03, F-UI-08, F-UI-09). The helper
// validates, the web server only passes things on. The PPPoE password only travels toward the helper and
// is never logged.
func (s *Server) registerSettings() {
	s.route("GET /api/settings", s.get("settings.get"))
	s.route("GET /api/settings/timezones", s.get("settings.timezones.get"))
	s.route("POST /api/settings/wan", s.forward("settings.wan.set",
		"Saved. Check that the internet works, then confirm the change."))
	s.route("POST /api/settings/system", s.forward("settings.system.set", "Saved."))
	s.route("POST /api/settings/lan", s.forward("settings.lan.set",
		"Saved. The router now has the new address."))
	s.route("POST /api/settings/dhcp", s.forward("settings.dhcp.set",
		"Saved. Devices get the new values the next time they renew their address."))
	s.route("POST /api/settings/confirm", s.forward("settings.confirm", "Change confirmed."))
	s.route("POST /api/settings/revert", s.forward("settings.revert", "Change rolled back."))
	s.route("GET /api/settings/check", s.handleInternetCheck)
}

// handleInternetCheck says whether the internet is reachable right now.
func (s *Server) handleInternetCheck(w http.ResponseWriter, r *http.Request) {
	ok := false
	if s.cfg.Monitor != nil {
		ok = s.cfg.Monitor.InternetOK(r.Context())
	}
	writeJSON(w, map[string]any{"internet": ok})
}
