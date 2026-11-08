// SPDX-License-Identifier: AGPL-3.0-or-later

package api

// Access points and networks with the router as their center (docs/sdn-concept.md). The helper validates and keeps
// the secrets, the web server only passes things on. Passwords travel only toward the helper.
func (s *Server) registerWireless() {
	s.route("GET /api/wireless", s.get("wireless.get"))
	s.route("POST /api/wireless", s.forward("wireless.set", "Saved."))
	s.route("POST /api/wireless/pair", s.forward("wireless.pair", "The access point is paired."))
	s.route("POST /api/wireless/reject", s.forward("wireless.reject", "The request was rejected."))
	s.route("POST /api/wireless/remove", s.forward("wireless.remove", "The access point was removed."))
	s.route("POST /api/wireless/preview", s.forward("wireless.preview", ""))
	s.route("POST /api/wireless/simulate", s.forward("wireless.simulate", "The simulated access points are waiting again."))
}
