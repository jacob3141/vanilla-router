// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
	"net/netip"

	"vanilla-router/internal/helper"
	"vanilla-router/internal/sys"
)

func (s *Server) registerFirewall() {
	s.route("GET /api/firewall", s.handleFirewall)
	s.route("POST /api/portforwards", s.forward("portfw.set", "Weiterleitung aktiv. Bitte innerhalb von 60 Sekunden bestätigen."))
	s.route("POST /api/portforwards/delete", s.forward("portfw.delete", "Weiterleitung entfernt. Bitte innerhalb von 60 Sekunden bestätigen."))
	s.route("POST /api/portforwards/confirm", s.forward("portfw.confirm", "Änderung bestätigt."))
}

func (s *Server) handleFirewall(w http.ResponseWriter, r *http.Request) {
	var info helper.NetconfInfo
	if err := s.call(r, "netconf.get", nil, &info); err != nil {
		s.helperError(w, err)
		return
	}
	var fw map[string]any
	if err := s.call(r, "firewall.get", nil, &fw); err != nil {
		s.helperError(w, err)
		return
	}
	devices := map[string]string{} // IP → Name, für die Auswahl des Ziels
	for _, res := range info.State.Reservations {
		devices[res.IP] = res.Name
	}
	fw["port_forwards"] = info.State.PortForwards
	fw["devices"] = devices
	// Doppeltes NAT erkennen: private Adresse am Internet-Anschluss heißt, davor steht noch ein Router.
	gw, dev := sys.DefaultGatewayV4()
	if dev != "" {
		for _, a := range sys.ReadInterface(dev).IPv4 {
			if p, err := netip.ParsePrefix(a); err == nil && (p.Addr().IsPrivate() || isCGNAT(p.Addr())) {
				fw["double_nat"] = map[string]any{"wan_ip": p.Addr().String(), "gateway": gw, "cgnat": isCGNAT(p.Addr())}
			}
		}
	}
	writeJSON(w, fw)
}

// isCGNAT: 100.64.0.0/10, Adresse vom Anbieter geteilt, Weiterleitungen sind dann unmöglich.
func isCGNAT(a netip.Addr) bool { return netip.MustParsePrefix("100.64.0.0/10").Contains(a) }
