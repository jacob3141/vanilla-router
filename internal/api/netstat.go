// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"vanilla-router/internal/conf"
	"vanilla-router/internal/helper"
	"vanilla-router/internal/sys"
)

func (s *Server) registerNetstat() {
	s.route("GET /api/network", s.handleNetwork)
	s.route("GET /api/ports", s.get("ports.list"))
	s.route("GET /api/interfaces", s.get("interfaces.get"))
	s.route("POST /api/interfaces", s.forward("interfaces.set", "Saved. Move the cables if needed, then confirm the change."))
	s.route("POST /api/interfaces/identify", s.forward("interfaces.identify", "The LED of this port is blinking for 10 seconds."))
	s.route("GET /api/connections", s.handleConnections)
	s.route("GET /api/dns/stats", s.handleDNSStats)
}

// ipJSON runs `ip -j …` and passes on the JSON unchanged.
func ipJSON(ctx context.Context, args ...string) any {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ip", append([]string{"-j"}, args...)...).Output()
	if err != nil {
		return []any{}
	}
	var v any
	if json.Unmarshal(out, &v) != nil {
		return []any{}
	}
	return v
}

func readSysctl(path string) string {
	b, err := os.ReadFile("/proc/sys/" + strings.ReplaceAll(path, ".", "/"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sysctls := map[string]string{}
	for _, k := range []string{
		"net.ipv4.ip_forward", "net.ipv6.conf.all.forwarding",
		"net.ipv6.conf.wan0.accept_ra", "net.ipv6.conf.lan0.accept_ra", "net.ipv6.conf.wan0.autoconf",
		"net.ipv4.conf.all.rp_filter", "net.ipv4.conf.wan0.rp_filter",
	} {
		sysctls[k] = readSysctl(k)
	}
	writeJSON(w, map[string]any{
		"links":     ipJSON(ctx, "-s", "link"),
		"addrs":     ipJSON(ctx, "addr"),
		"routes4":   ipJSON(ctx, "-4", "route", "show", "table", "main"),
		"routes6":   ipJSON(ctx, "-6", "route", "show", "table", "main"),
		"neighbors": ipJSON(ctx, "neigh"),
		"sysctl":    sysctls,
	})
}

// names assigns a name to IP addresses in the LAN (custom name, reservation, DHCP name).
func (s *Server) names(r *http.Request) map[string]string {
	out := map[string]string{s.cfg.RouterIP: "router"}
	var info helper.NetconfInfo
	if s.call(r, "netconf.get", nil, &info) == nil {
		for _, res := range info.State.Reservations {
			out[res.IP] = res.Name
		}
		for _, rec := range info.State.Records {
			out[rec.IP] = rec.Name
		}
		for _, m := range info.Manual {
			if _, ok := out[m.IP]; !ok {
				out[m.IP] = strings.TrimSuffix(m.Name, "."+conf.Domain)
			}
		}
	}
	leases, _ := sys.ReadLeases()
	for _, l := range leases {
		meta := s.devices.Get(l.MAC)
		switch {
		case meta.Alias != "":
			out[l.IP] = meta.Alias
		case out[l.IP] == "" && l.Hostname != "":
			out[l.IP] = l.Hostname
		}
	}
	return out
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	var conns []helper.Conn
	if err := s.call(r, "conn.list", nil, &conns); err != nil {
		s.helperError(w, err)
		return
	}
	names := s.names(r)
	type perDevice struct {
		IP    string `json:"ip"`
		Name  string `json:"name,omitempty"`
		Total int    `json:"total"`
		TCP   int    `json:"tcp"`
		UDP   int    `json:"udp"`
	}
	byDev := map[string]*perDevice{}
	for _, c := range conns {
		if !strings.HasPrefix(c.Src, "10.") {
			continue
		}
		d, ok := byDev[c.Src]
		if !ok {
			d = &perDevice{IP: c.Src, Name: names[c.Src]}
			byDev[c.Src] = d
		}
		d.Total++
		switch c.Proto {
		case "tcp":
			d.TCP++
		case "udp":
			d.UDP++
		}
	}
	devices := make([]*perDevice, 0, len(byDev))
	for _, d := range byDev {
		devices = append(devices, d)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Total > devices[j].Total })

	// For the table: filtered by ?ip= and at most 500 entries
	filter := r.URL.Query().Get("ip")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	list := make([]helper.Conn, 0, limit)
	for _, c := range conns {
		if filter != "" && c.Src != filter && c.Dst != filter && c.ReplySrc != filter {
			continue
		}
		if len(list) < limit {
			list = append(list, c)
		}
	}
	writeJSON(w, map[string]any{"total": len(conns), "devices": devices, "connections": list, "names": names})
}

func (s *Server) handleDNSStats(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	var out map[string]any
	if err := s.call(r, "dns.stats.get", map[string]any{"hours": hours, "client": r.URL.Query().Get("client")}, &out); err != nil {
		s.helperError(w, err)
		return
	}
	out["names"] = s.names(r)
	writeJSON(w, out)
}
