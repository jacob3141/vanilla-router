// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import "net/http"

// Einstellungen für Internet-Anschluss, Heimnetz und System (F-UI-03, F-UI-08, F-UI-09). Die Prüfung
// macht der Helper, der Webserver reicht nur weiter. Das PPPoE-Passwort geht nur in Richtung Helper und
// wird nirgends protokolliert.
func (s *Server) registerSettings() {
	s.route("GET /api/settings", s.get("settings.get"))
	s.route("GET /api/settings/timezones", s.get("settings.timezones.get"))
	s.route("POST /api/settings/wan", s.forward("settings.wan.set",
		"Gespeichert. Prüfen Sie, ob das Internet geht, und bestätigen Sie die Änderung."))
	s.route("POST /api/settings/system", s.forward("settings.system.set", "Gespeichert."))
	s.route("POST /api/settings/lan", s.forward("settings.lan.set",
		"Gespeichert. Der Router hat jetzt die neue Adresse."))
	s.route("POST /api/settings/dhcp", s.forward("settings.dhcp.set",
		"Gespeichert. Geräte bekommen die neuen Werte bei der nächsten Erneuerung ihrer Adresse."))
	s.route("POST /api/settings/confirm", s.forward("settings.confirm", "Änderung bestätigt."))
	s.route("POST /api/settings/revert", s.forward("settings.revert", "Änderung zurückgenommen."))
	s.route("GET /api/settings/check", s.handleInternetCheck)
}

// handleInternetCheck sagt, ob das Internet gerade erreichbar ist.
func (s *Server) handleInternetCheck(w http.ResponseWriter, r *http.Request) {
	ok := false
	if s.cfg.Monitor != nil {
		ok = s.cfg.Monitor.InternetOK(r.Context())
	}
	writeJSON(w, map[string]any{"internet": ok})
}
