// SPDX-License-Identifier: AGPL-3.0-or-later

package api

// Wi-Fi access point (F-WIFI, D-18). The helper validates, the web server only passes things on. The password
// only travels toward the helper and is never logged. A pending change is confirmed through /api/settings/confirm.
func (s *Server) registerWiFi() {
	s.route("GET /api/wifi", s.get("wifi.get"))
	s.route("GET /api/wifi/status", s.get("wifi.status"))
	s.route("POST /api/wifi", s.forward("wifi.set",
		"Saved. Check that your devices can connect, then confirm the change."))
	s.route("GET /api/wifi/install", s.get("wifi.install.status"))
	s.route("POST /api/wifi/install", s.forward("wifi.install.start", "Download started."))
}
