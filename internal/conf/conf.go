// SPDX-License-Identifier: AGPL-3.0-or-later

// Package conf is the web interface's view of the configuration (taken over from the prototype,
// PLAN.md 3.11). The source of truth is config.json (D-02): FromConfig reads the parts the web interface
// can edit, ApplyTo writes them back. Validation is done only with config.Validate,
// generation only with internal/gen. The JSON forms stay those of the prototype, so the API and
// frontend stay unchanged.
package conf

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	"vanilla-router/internal/config"
)

// Domain and LAN are the values of the loaded configuration (SetNetwork). The defaults only apply
// until the first configuration is loaded.
var (
	Domain    = "home.arpa"
	LANPrefix = netip.MustParsePrefix("192.168.42.0/24")
	RouterIP  = netip.MustParseAddr("192.168.42.1")
)

// SetNetwork takes over the domain and home network from the configuration.
func SetNetwork(c *config.Config) {
	if p, a, err := c.LANPrefix(); err == nil {
		LANPrefix, RouterIP = p, a
	}
	if c.DNS.Domain != "" {
		Domain = c.DNS.Domain
	}
}

// Reservation is a fixed DHCP address. The name is also published as a DNS name.
type Reservation struct {
	Name    string `json:"name"`
	MAC     string `json:"mac"`
	IP      string `json:"ip"`
	Comment string `json:"comment,omitempty"`
}

// Record is an additional local DNS name without a DHCP reservation.
type Record struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// Pool is the dynamic DHCP range.
type Pool struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// PortForward as in the prototype: IntPort 0 = same as external, range with ExtPortEnd.
type PortForward struct {
	ID         string `json:"id"`
	Proto      string `json:"proto"`
	ExtPort    int    `json:"ext_port"`
	ExtPortEnd int    `json:"ext_port_end,omitempty"`
	IntIP      string `json:"int_ip"`
	IntPort    int    `json:"int_port,omitempty"`
	Comment    string `json:"comment,omitempty"`
	Enabled    bool   `json:"enabled"`
}

// Block blocks a device from the internet, always or at certain times.
type Block struct {
	MAC     string `json:"mac"`
	Name    string `json:"name,omitempty"`
	Mode    string `json:"mode"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Days    []int  `json:"days,omitempty"`
	Enabled bool   `json:"enabled"`
}

// State is the part of the configuration the web interface can edit.
type State struct {
	Version      int           `json:"version"`
	Pool         Pool          `json:"pool"`
	Reservations []Reservation `json:"reservations"`
	Records      []Record      `json:"records"`
	PortForwards []PortForward `json:"port_forwards"`
	Blocks       []Block       `json:"blocks"`
}

// Clone returns a deep copy.
func (s State) Clone() State {
	c := s
	c.Reservations = slices.Clone(s.Reservations)
	c.Records = slices.Clone(s.Records)
	c.PortForwards = slices.Clone(s.PortForwards)
	c.Blocks = slices.Clone(s.Blocks)
	return c
}

// FromConfig reads the State from the configuration.
func FromConfig(c *config.Config) State {
	s := State{Version: 1, Pool: Pool{c.DHCP.PoolStart, c.DHCP.PoolEnd},
		Reservations: []Reservation{}, Records: []Record{}, PortForwards: []PortForward{}, Blocks: []Block{}}
	for _, r := range c.DHCP.Reservations {
		s.Reservations = append(s.Reservations, Reservation{r.Name, r.MAC, r.IP, r.Comment})
	}
	for _, r := range c.DNS.Records {
		s.Records = append(s.Records, Record{r.Name, r.IP})
	}
	for _, p := range c.Firewall.PortForwards {
		pf := PortForward{ID: p.ID, Proto: p.Proto, ExtPort: p.ExtPort, ExtPortEnd: p.ExtEnd, IntIP: p.IntIP,
			Comment: p.Comment, Enabled: !p.Disabled}
		if p.IntPort != p.ExtPort {
			pf.IntPort = p.IntPort
		}
		s.PortForwards = append(s.PortForwards, pf)
	}
	for _, b := range c.Firewall.Blocks {
		s.Blocks = append(s.Blocks, Block{b.MAC, b.Name, b.Mode, b.From, b.To, slices.Clone(b.Days), !b.Disabled})
	}
	return s
}

// ApplyTo writes the State to a copy of the configuration. The caller must validate (config.Validate).
func (s State) ApplyTo(c *config.Config) *config.Config {
	n := *c
	n.DHCP.PoolStart, n.DHCP.PoolEnd = s.Pool.Start, s.Pool.End
	n.DHCP.Reservations = []config.Reservation{}
	for _, r := range s.Reservations {
		n.DHCP.Reservations = append(n.DHCP.Reservations, config.Reservation{Name: r.Name, MAC: r.MAC, IP: r.IP, Comment: r.Comment})
	}
	n.DNS.Records = []config.Record{}
	for _, r := range s.Records {
		n.DNS.Records = append(n.DNS.Records, config.Record{Name: r.Name, IP: r.IP})
	}
	n.Firewall.PortForwards = []config.PortForward{}
	for _, p := range s.PortForwards {
		ip := p.IntPort
		if ip == 0 {
			ip = p.ExtPort
		}
		n.Firewall.PortForwards = append(n.Firewall.PortForwards, config.PortForward{ID: p.ID, Proto: p.Proto,
			ExtPort: p.ExtPort, ExtEnd: p.ExtPortEnd, IntIP: p.IntIP, IntPort: ip, Comment: p.Comment, Disabled: !p.Enabled})
	}
	n.Firewall.Blocks = []config.Block{}
	for _, b := range s.Blocks {
		n.Firewall.Blocks = append(n.Firewall.Blocks, config.Block{MAC: b.MAC, Name: b.Name, Mode: b.Mode, From: b.From,
			To: b.To, Days: slices.Clone(b.Days), Disabled: !b.Enabled})
	}
	return &n
}

// --- Normalize input (config.Validate checks the overall configuration) ---------

var (
	labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	macRe   = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)
)

// NormalizeName turns "Printer-1" or "printer-1.home.arpa" into the name "printer-1".
func NormalizeName(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".")
	s = strings.TrimSuffix(s, "."+Domain)
	if !labelRe.MatchString(s) {
		return "", fmt.Errorf("\"%s\" is not a valid name: allowed are a–z, 0–9 and hyphens, at most 63 characters, no hyphen at the start or end", s)
	}
	return s, nil
}

// NormalizeMAC accepts aa:bb:…, AA-BB-… and aabb.ccdd.eeff.
func NormalizeMAC(s string) (string, error) {
	r := strings.NewReplacer("-", ":", ".", "")
	m := strings.ToLower(strings.TrimSpace(r.Replace(s)))
	if len(m) == 12 && !strings.Contains(m, ":") {
		m = m[0:2] + ":" + m[2:4] + ":" + m[4:6] + ":" + m[6:8] + ":" + m[8:10] + ":" + m[10:12]
	}
	if !macRe.MatchString(m) {
		return "", fmt.Errorf("\"%s\" is not a valid MAC address", s)
	}
	if m == "00:00:00:00:00:00" || m == "ff:ff:ff:ff:ff:ff" {
		return "", fmt.Errorf("The MAC address %s is not allowed", m)
	}
	if b := m[1]; strings.ContainsRune("13579bdf", rune(b)) {
		return "", fmt.Errorf("%s is a multicast address and cannot be a device", m)
	}
	return m, nil
}

// LocallyAdministered reports MACs that are often randomly generated (second bit set).
func LocallyAdministered(mac string) bool {
	return len(mac) >= 2 && strings.ContainsRune("2367abef", rune(mac[1]))
}

// ParseLANIP checks an IPv4 address in the home network that may belong to a device.
func ParseLANIP(s string) (netip.Addr, error) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil || !a.Is4() {
		return netip.Addr{}, fmt.Errorf("\"%s\" is not a valid IPv4 address", s)
	}
	if !LANPrefix.Contains(a) {
		return netip.Addr{}, fmt.Errorf("%s is not in the home network %s", a, LANPrefix)
	}
	if a == RouterIP {
		return netip.Addr{}, fmt.Errorf("%s is the router's address", a)
	}
	return a, nil
}
