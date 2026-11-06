// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"crypto/rand"
	"fmt"
	"io"
	"net/netip"
	"strings"
)

// lanCandidates are the home networks the installer offers in turn. Deliberately not
// 192.168.0.x, .1.x, .2.x and .178.x: provider routers use those themselves (Vodafone, Telekom, FRITZ!Box),
// and behind such a router (double NAT) there would otherwise be an overlap (F-NET-03).
var lanCandidates = []string{"192.168.42.1/24", "192.168.73.1/24", "10.42.0.1/24", "172.20.42.1/24"}

// ChooseLAN returns the first home network that does not overlap with avoid (e.g. the WAN network).
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
	return netip.MustParsePrefix("10.217.33.1/24") // all candidates taken: an unusual network
}

// NewULA generates a random ULA prefix /48 per RFC 4193 (fd + 40-bit global ID, F-NET-12).
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

// DefaultInput is what the installer knows about the hardware.
type DefaultInput struct {
	WANMAC  string
	LANMACs []string
	WANNet  netip.Prefix // network offered by the modem (DHCP), empty if unknown
	Rand    io.Reader    // for the ULA prefix, nil = crypto/rand
}

// Default generates a complete, valid configuration with the defaults from PLAN.md 3.4.
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
		System: System{Hostname: "router", Timezone: "UTC", Locale: "en_US.UTF-8", Keymap: "us"},
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
		return nil, fmt.Errorf("defaults invalid: %w", err)
	}
	return c, nil
}

// TelekomUser builds Telekom's PPPoE user name from the details in the access letter:
// line ID + access number (T-Online number) + co-user number + "@t-online.de".
// if the line ID is shorter than 12 digits, a "#" goes before the co-user number [S-05].
func TelekomUser(line_id, access_number, co_user string) (string, error) {
	digits := func(s string) bool {
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		return s != ""
	}
	ak, zn, mb := strings.TrimSpace(line_id), strings.TrimSpace(access_number), strings.TrimSpace(co_user)
	if mb == "" {
		mb = "0001"
	}
	if !digits(ak) || !digits(zn) || !digits(mb) || len(ak) > 12 || len(zn) > 12 || len(mb) != 4 {
		return "", fmt.Errorf("Please enter only the digits from the access letter (line ID and access number up to 12 digits each, co-user number 4 digits)")
	}
	sep := ""
	if len(ak) < 12 {
		sep = "#"
	}
	return ak + zn + sep + mb + "@t-online.de", nil
}
