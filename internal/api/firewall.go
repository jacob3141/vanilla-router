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
	s.route("POST /api/portforwards", s.forward("portfw.set", "Port forward active. Please confirm within 60 seconds."))
	s.route("POST /api/portforwards/delete", s.forward("portfw.delete", "Port forward removed. Please confirm within 60 seconds."))
	s.route("POST /api/portforwards/confirm", s.forward("portfw.confirm", "Change confirmed."))
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
	devices := map[string]string{} // IP → name, for choosing the target
	for _, res := range info.State.Reservations {
		devices[res.IP] = res.Name
	}
	fw["port_forwards"] = info.State.PortForwards
	fw["devices"] = devices
	// Detect double NAT: a private address on the internet port means another router is in front.
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

// isCGNAT: 100.64.0.0/10, an address shared by the provider, port forwards are then impossible.
func isCGNAT(a netip.Addr) bool { return netip.MustParsePrefix("100.64.0.0/10").Contains(a) }
