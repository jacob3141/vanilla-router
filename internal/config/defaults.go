// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"crypto/rand"
	"fmt"
	"io"
	"net/netip"
	"strings"
)

// lanCandidates sind die Heimnetze, die der Installer der Reihe nach anbietet. Bewusst nicht
// 192.168.0.x, .1.x, .2.x und .178.x: Die nutzen Provider-Router (Vodafone, Telekom, FRITZ!Box)
// selbst, und hinter einem solchen Router (doppeltes NAT) gäbe es sonst eine Überschneidung (F-NET-03).
var lanCandidates = []string{"192.168.42.1/24", "192.168.73.1/24", "10.42.0.1/24", "172.20.42.1/24"}

// ChooseLAN liefert das erste Heimnetz, das sich nicht mit avoid (z. B. dem WAN-Netz) überschneidet.
func ChooseLAN(avoid ...netip.Prefix) netip.Prefix {
next:
	for _, s := range lanCandidates {
		p := netip.MustParsePrefix(s)
		for _, a := range avoid {
			if a.IsValid() && a.Overlaps(p.Masked()) {
				continue next
			}
		}
		return p
	}
	return netip.MustParsePrefix("10.217.33.1/24") // alle Kandidaten belegt: ungewöhnliches Netz
}

// NewULA erzeugt ein zufälliges ULA-Präfix /48 nach RFC 4193 (fd + 40 Bit Global ID, F-NET-12).
func NewULA(rnd io.Reader) (netip.Prefix, error) {
	if rnd == nil {
		rnd = rand.Reader
	}
	var b [16]byte
	b[0] = 0xfd
	if _, err := io.ReadFull(rnd, b[1:6]); err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(netip.AddrFrom16(b), 48), nil
}

// DefaultInput ist das, was der Installer über die Hardware weiß.
type DefaultInput struct {
	WANMAC  string
	LANMACs []string
	WANNet  netip.Prefix // vom Modem angebotenes Netz (DHCP), leer wenn unbekannt
	Rand    io.Reader    // für das ULA-Präfix, nil = crypto/rand
}

// Default erzeugt eine vollständige, gültige Konfiguration mit den Vorgaben aus PLAN.md 3.4.
func Default(in DefaultInput) (*Config, error) {
	lan := ChooseLAN(in.WANNet)
	net4 := lan.Masked().Addr().As4()
	ip := func(last byte) string { return netip.AddrFrom4([4]byte{net4[0], net4[1], net4[2], last}).String() }
	u, err := NewULA(in.Rand)
	if err != nil {
		return nil, err
	}
	c := &Config{
		Schema: SchemaVersion,
		System: System{Hostname: "router", Timezone: "Europe/Berlin", Locale: "de_DE.UTF-8", Keymap: "de"},
		WAN: WAN{
			MatchMAC: strings.ToLower(in.WANMAC),
			Type:     WANDHCP,
			IPv6:     WANIPv6{Mode: "auto", RequestPrefix: 56},
		},
		LAN: LAN{
			IPv4: lan.String(),
			IPv6: LANIPv6{ULA: u.String(), FromWANPrefix: true},
		},
		DHCP: DHCP{PoolStart: ip(100), PoolEnd: ip(249), Lease: "12h", Reservations: []Reservation{}},
		DNS:  DNS{Domain: "home.arpa", Upstreams: []string{}, Records: []Record{}},
		Firewall: Firewall{
			PortForwards: []PortForward{}, Pinholes: []Pinhole{}, Blocks: []Block{},
		},
		Updates: Updates{SecurityAuto: true, RebootWindow: "04:00", Snapshots: true},
	}
	for _, m := range in.LANMACs {
		c.LAN.Members = append(c.LAN.Members, strings.ToLower(m))
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("Vorgaben ungültig: %w", err)
	}
	return c, nil
}

// TelekomUser setzt den PPPoE-Benutzernamen der Telekom aus den Angaben im Zugangsbrief zusammen:
// Anschlusskennung + Zugangsnummer (T-Online-Nummer) + Mitbenutzernummer + "@t-online.de".
// Ist die Anschlusskennung kürzer als 12 Stellen, steht vor der Mitbenutzernummer ein „#“ [S-05].
func TelekomUser(anschlusskennung, zugangsnummer, mitbenutzer string) (string, error) {
	digits := func(s string) bool {
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		return s != ""
	}
	ak, zn, mb := strings.TrimSpace(anschlusskennung), strings.TrimSpace(zugangsnummer), strings.TrimSpace(mitbenutzer)
	if mb == "" {
		mb = "0001"
	}
	if !digits(ak) || !digits(zn) || !digits(mb) || len(ak) > 12 || len(zn) > 12 || len(mb) != 4 {
		return "", fmt.Errorf("Bitte nur die Ziffern aus dem Zugangsbrief eingeben (Anschlusskennung und Zugangsnummer je bis 12 Stellen, Mitbenutzernummer 4 Stellen)")
	}
	sep := ""
	if len(ak) < 12 {
		sep = "#"
	}
	return ak + zn + sep + mb + "@t-online.de", nil
}
