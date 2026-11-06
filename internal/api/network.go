// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"vanilla-router/internal/conf"
	"vanilla-router/internal/helper"
	"vanilla-router/internal/sys"
)

func (s *Server) registerNetwork() {
	s.route("GET /api/devices", s.handleDevices)
	s.route("POST /api/reservations", s.handleReservationSet)
	s.route("POST /api/reservations/delete", s.forward("dhcp.reservation.delete", "Reservation deleted."))
	s.route("GET /api/dns", s.handleDNS)
	s.route("POST /api/dns", s.forward("dns.record.set", "DNS record saved."))
	s.route("POST /api/dns/delete", s.forward("dns.record.delete", "DNS record deleted."))
	s.route("POST /api/dns/flush", s.forward("dns.flush", "DNS cache cleared."))
}

// forward passes the request body unchanged to a helper action.
// The helper validates the parameters.
func (s *Server) forward(action, okMsg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any
		if !readJSON(w, r, &params) {
			return
		}
		var out any
		if err := s.call(r, action, params, &out); err != nil {
			s.helperError(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "message": okMsg, "data": out})
	}
}

func (s *Server) handleReservationSet(w http.ResponseWriter, r *http.Request) {
	s.forward("dhcp.reservation.set", "Reservation saved. The device gets the address the next time it renews its lease.")(w, r)
}

// Device combines the lease, neighbor table and reservation of a MAC address.
type Device struct {
	MAC         string            `json:"mac"`
	IP          string            `json:"ip"`
	Hostname    string            `json:"hostname"` // reported by the device
	Online      bool              `json:"online"`
	Neighbor    string            `json:"neighbor_state,omitempty"`
	LeaseState  string            `json:"lease_state,omitempty"`
	LeaseEnds   *time.Time        `json:"lease_ends,omitempty"`
	RandomMAC   bool              `json:"random_mac"`
	Reservation *conf.Reservation `json:"reservation,omitempty"`
	DNSName     string            `json:"dns_name,omitempty"`
	Alias       string            `json:"alias,omitempty"`
	Note        string            `json:"note,omitempty"`
	FirstSeen   *time.Time        `json:"first_seen,omitempty"`
	LastSeen    *time.Time        `json:"last_seen,omitempty"`
	New         bool              `json:"new"`
	Block       *conf.Block       `json:"block,omitempty"`
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	var info helper.NetconfInfo
	if err := s.call(r, "netconf.get", nil, &info); err != nil {
		s.helperError(w, err)
		return
	}
	leases, _ := sys.ReadLeases()
	neigh, _ := sys.Neighbors(r.Context(), "lan0")

	byMAC := map[string]*Device{}
	get := func(mac string) *Device {
		d, ok := byMAC[mac]
		if !ok {
			d = &Device{MAC: mac, RandomMAC: conf.LocallyAdministered(mac)}
			byMAC[mac] = d
		}
		return d
	}
	now := time.Now()
	for _, l := range leases {
		// We hide expired leases released long ago that have no other reference
		if l.State != "active" && now.Sub(l.Ends) > 7*24*time.Hour {
			continue
		}
		d := get(l.MAC)
		d.IP, d.Hostname, d.LeaseState = l.IP, l.Hostname, l.State
		if !l.Ends.IsZero() {
			ends := l.Ends
			d.LeaseEnds = &ends
		}
	}
	for _, n := range neigh {
		if n.MAC == "" || !strings.HasPrefix(n.IP, "10.") {
			continue
		}
		d := get(n.MAC)
		d.Neighbor = n.State
		if n.Online() {
			d.Online = true
			d.IP = n.IP
		}
	}
	for i := range info.State.Reservations {
		res := info.State.Reservations[i]
		d := get(res.MAC)
		d.Reservation = &res
		d.DNSName = res.Name + "." + conf.Domain
		if d.IP == "" {
			d.IP = res.IP
		}
	}
	// assign hand-written DNS names to devices (e.g. a printer)
	for _, m := range info.Manual {
		for _, d := range byMAC {
			if d.DNSName == "" && d.IP == m.IP && strings.HasSuffix(m.Name, "."+conf.Domain) {
				d.DNSName = m.Name
			}
		}
	}

	for i := range info.State.Blocks {
		b := info.State.Blocks[i]
		get(b.MAC).Block = &b
	}

	out := make([]*Device, 0, len(byMAC))
	for _, d := range byMAC {
		// Docker containers (ipvlan) and the router itself do not belong in the device list
		if d.MAC == "" {
			continue
		}
		meta := s.devices.Get(d.MAC)
		d.Alias, d.Note = meta.Alias, meta.Note
		if !meta.FirstSeen.IsZero() {
			fs, ls := meta.FirstSeen, meta.LastSeen
			d.FirstSeen, d.LastSeen = &fs, &ls
			d.New = !meta.Baseline && now.Sub(meta.FirstSeen) < NewWindow
		}
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b *Device) int {
		if a.Online != b.Online {
			if a.Online {
				return -1
			}
			return 1
		}
		return compareIPStr(a.IP, b.IP)
	})
	writeJSON(w, map[string]any{"devices": out, "pool": info.State.Pool, "dry_run": info.DryRun})
}

func (s *Server) handleDNS(w http.ResponseWriter, r *http.Request) {
	var info helper.NetconfInfo
	if err := s.call(r, "netconf.get", nil, &info); err != nil {
		s.helperError(w, err)
		return
	}
	type entry struct {
		Name   string `json:"name"`
		IP     string `json:"ip"`
		Source string `json:"source"` // record, reservation, manual
		Detail string `json:"detail,omitempty"`
	}
	var out []entry
	for _, rec := range info.State.Records {
		out = append(out, entry{rec.Name + "." + conf.Domain, rec.IP, "record", ""})
	}
	for _, res := range info.State.Reservations {
		out = append(out, entry{res.Name + "." + conf.Domain, res.IP, "reservation", res.MAC})
	}
	for _, m := range info.Manual {
		out = append(out, entry{m.Name, m.IP, "manual", m.File})
	}
	slices.SortFunc(out, func(a, b entry) int { return strings.Compare(a.Name, b.Name) })
	writeJSON(w, map[string]any{"entries": out, "upstreams": info.Upstreams, "domain": conf.Domain, "dry_run": info.DryRun})
}

func compareIPStr(a, b string) int {
	pa, pb := ipKey(a), ipKey(b)
	return strings.Compare(pa, pb)
}

// ipKey makes IPv4 addresses sortable ("10.0.10.5" → "010.000.010.005").
func ipKey(ip string) string {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return ip
	}
	for i, p := range parts {
		parts[i] = strings.Repeat("0", 3-min(3, len(p))) + p
	}
	return strings.Join(parts, ".")
}
