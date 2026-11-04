// SPDX-License-Identifier: AGPL-3.0-or-later

// Package conf ist die Sicht der Weboberfläche auf die Konfiguration (übernommen aus dem Prototyp,
// PLAN.md 3.11). Wahrheitsquelle ist config.json (D-02): FromConfig liest die für die Oberfläche
// bearbeitbaren Teile, ApplyTo schreibt sie zurück. Geprüft wird ausschließlich mit config.Validate,
// erzeugt ausschließlich mit internal/gen. Die JSON-Formen bleiben die des Prototyps, damit API und
// Frontend unverändert bleiben.
package conf

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	"vanilla-router/internal/config"
)

// Domain und LAN sind die Werte der geladenen Konfiguration (SetNetwork). Die Vorgaben gelten nur,
// bis die erste Konfiguration geladen ist.
var (
	Domain    = "home.arpa"
	LANPrefix = netip.MustParsePrefix("192.168.42.0/24")
	RouterIP  = netip.MustParseAddr("192.168.42.1")
)

// SetNetwork übernimmt Domain und Heimnetz aus der Konfiguration.
func SetNetwork(c *config.Config) {
	if p, a, err := c.LANPrefix(); err == nil {
		LANPrefix, RouterIP = p, a
	}
	if c.DNS.Domain != "" {
		Domain = c.DNS.Domain
	}
}

// Reservation ist eine feste DHCP-Adresse. Der Name wird zugleich als DNS-Name veröffentlicht.
type Reservation struct {
	Name    string `json:"name"`
	MAC     string `json:"mac"`
	IP      string `json:"ip"`
	Comment string `json:"comment,omitempty"`
}

// Record ist ein zusätzlicher lokaler DNS-Name ohne DHCP-Reservierung.
type Record struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// Pool ist der dynamische DHCP-Bereich.
type Pool struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// PortForward wie im Prototyp: IntPort 0 = wie extern, Bereich mit ExtPortEnd.
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

// Block sperrt einem Gerät das Internet, immer oder zu Zeiten.
type Block struct {
	MAC     string `json:"mac"`
	Name    string `json:"name,omitempty"`
	Mode    string `json:"mode"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Days    []int  `json:"days,omitempty"`
	Enabled bool   `json:"enabled"`
}

// State ist der von der Oberfläche bearbeitbare Teil der Konfiguration.
type State struct {
	Version      int           `json:"version"`
	Pool         Pool          `json:"pool"`
	Reservations []Reservation `json:"reservations"`
	Records      []Record      `json:"records"`
	PortForwards []PortForward `json:"port_forwards"`
	Blocks       []Block       `json:"blocks"`
}

// Clone liefert eine tiefe Kopie.
func (s State) Clone() State {
	c := s
	c.Reservations = slices.Clone(s.Reservations)
	c.Records = slices.Clone(s.Records)
	c.PortForwards = slices.Clone(s.PortForwards)
	c.Blocks = slices.Clone(s.Blocks)
	return c
}

// FromConfig liest den State aus der Konfiguration.
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

// ApplyTo schreibt den State in eine Kopie der Konfiguration. Prüfen muss der Aufrufer (config.Validate).
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

// --- Eingaben normalisieren (Prüfung der Gesamtkonfiguration macht config.Validate) ---------

var (
	labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	macRe   = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)
)

// NormalizeName macht aus "Drucker-1" oder "drucker-1.home.arpa" den Namen "drucker-1".
func NormalizeName(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".")
	s = strings.TrimSuffix(s, "."+Domain)
	if !labelRe.MatchString(s) {
		return "", fmt.Errorf("„%s“ ist kein gültiger Name: erlaubt sind a–z, 0–9 und Bindestriche, höchstens 63 Zeichen, nicht am Anfang oder Ende ein Bindestrich", s)
	}
	return s, nil
}

// NormalizeMAC akzeptiert aa:bb:…, AA-BB-… und aabb.ccdd.eeff.
func NormalizeMAC(s string) (string, error) {
	r := strings.NewReplacer("-", ":", ".", "")
	m := strings.ToLower(strings.TrimSpace(r.Replace(s)))
	if len(m) == 12 && !strings.Contains(m, ":") {
		m = m[0:2] + ":" + m[2:4] + ":" + m[4:6] + ":" + m[6:8] + ":" + m[8:10] + ":" + m[10:12]
	}
	if !macRe.MatchString(m) {
		return "", fmt.Errorf("„%s“ ist keine gültige MAC-Adresse", s)
	}
	if m == "00:00:00:00:00:00" || m == "ff:ff:ff:ff:ff:ff" {
		return "", fmt.Errorf("Die MAC-Adresse %s ist nicht erlaubt", m)
	}
	if b := m[1]; strings.ContainsRune("13579bdf", rune(b)) {
		return "", fmt.Errorf("%s ist eine Multicast-Adresse und kann kein Gerät sein", m)
	}
	return m, nil
}

// LocallyAdministered meldet MACs, die oft zufällig erzeugt sind (zweites Bit gesetzt).
func LocallyAdministered(mac string) bool {
	return len(mac) >= 2 && strings.ContainsRune("2367abef", rune(mac[1]))
}

// ParseLANIP prüft eine IPv4-Adresse im Heimnetz, die einem Gerät gehören darf.
func ParseLANIP(s string) (netip.Addr, error) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil || !a.Is4() {
		return netip.Addr{}, fmt.Errorf("„%s“ ist keine gültige IPv4-Adresse", s)
	}
	if !LANPrefix.Contains(a) {
		return netip.Addr{}, fmt.Errorf("%s liegt nicht im Heimnetz %s", a, LANPrefix)
	}
	if a == RouterIP {
		return netip.Addr{}, fmt.Errorf("%s ist die Adresse des Routers", a)
	}
	return a, nil
}
