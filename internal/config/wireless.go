// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"encoding/base64"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Wireless is the network of several access points with the router as their center (docs/sdn-concept.md).
// The block is optional. The router plans channels from it and renders one bundle per paired access point (gen.APFiles),
// which vr-apd hands out. Segments also generate VLANs, DHCP and firewall rules on the router (gen/segments.go).
// The local adapter stays in the WiFi block until the generators move over (concept, stage 2).
type Wireless struct {
	// Country of all access points (ISO 3166-1 alpha-2), decides which channels are legal.
	Country      string        `json:"country"`
	Segments     []Segment     `json:"segments"`
	Networks     []WNetwork    `json:"networks"`
	AccessPoints []AccessPoint `json:"access_points"`
}

// Segment is a part of the home network with its own VLAN (concept 5.7). The home segment is untagged and uses
// the addresses of the LAN block, every other segment has its own range and reaches the internet only.
type Segment struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	VLAN   int    `json:"vlan"`
	Access string `json:"access"`         // lan (the home segment) or internet
	IPv4   string `json:"ipv4,omitempty"` // router address with prefix, empty for the home segment
	// ReachableFrom lists segments whose devices may open connections into this one (older devices: from home).
	ReachableFrom []string `json:"reachable_from,omitempty"`
}

// WNetwork is a Wi-Fi network name offered by all access points on the listed bands.
type WNetwork struct {
	ID          string   `json:"id"`
	SSID        string   `json:"ssid"`
	Segment     string   `json:"segment"`
	PasswordRef string   `json:"password_ref"`
	Security    string   `json:"security"` // wpa3 or wpa2-wpa3
	Bands       []string `json:"bands"`
	Enabled     bool     `json:"enabled"`
	// Roaming is required with WPA3 and absent otherwise: FT only with SAE (concept 5.4, 5.5).
	Roaming *Roaming `json:"roaming,omitempty"`
}

// Roaming holds what fast transition (802.11r) needs. The key itself is under secrets/<KeyRef>.
type Roaming struct {
	MobilityDomain string `json:"mobility_domain"` // 4 hex digits, random once, stable across renames
	KeyRef         string `json:"key_ref"`
}

// AccessPoint is a paired access point: a PC with Ubuntu Server and vanilla-router in the role ap (concept 3.1).
type AccessPoint struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	PublicKey string    `json:"public_key"` // ed25519:<base64>, pinned for TLS (concept 4.2)
	MAC       string    `json:"mac"`        // port on the cable, shown to tell APs apart
	Power     string    `json:"power"`      // low, medium or high
	Radios    []APRadio `json:"radios"`
}

// APRadio is one Wi-Fi card of an access point. Channel and width 0 mean "planned by the router" (concept 5.6).
type APRadio struct {
	ID      string `json:"id"` // stable name the AP reports, e.g. pci-0000-03-00-0
	Band    string `json:"band"`
	Channel int    `json:"channel,omitempty"`
	Width   int    `json:"width,omitempty"`
}

// Constants of the wireless block.
const (
	WiFiBand6        = "6"
	SecurityWPA3     = "wpa3"
	SecurityWPA2WPA3 = "wpa2-wpa3"
	AccessLAN        = "lan"
	AccessInternet   = "internet"
	SegmentHome      = "home"
	PowerLow         = "low"
	PowerMedium      = "medium"
	PowerHigh        = "high"

	// Limits keep the page and the radios manageable: hostapd needs one BSS per network and band on every radio.
	MaxSegments       = 8
	MaxNetworks       = 8
	MaxNetworksOnBand = 4
	MaxAccessPoints   = 16
	MaxRadios         = 4
)

var (
	wIDRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,15}$`)
	radioIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,31}$`)
	mdRe      = regexp.MustCompile(`^[0-9a-f]{4}$`)
)

// WirelessBands are all bands in display order.
var WirelessBands = []string{WiFiBand24, WiFiBand5, WiFiBand6}

// BandChannels lists the 20 MHz channels a band has worldwide. Which ones a country allows decides the plan
// (gen.PlanChannels), not the schema: a pinned channel is checked against the country by the AP's start.
func BandChannels(band string) []int {
	var out []int
	switch band {
	case WiFiBand24:
		for c := 1; c <= 13; c++ {
			out = append(out, c)
		}
	case WiFiBand5:
		for c := 36; c <= 64; c += 4 {
			out = append(out, c)
		}
		for c := 100; c <= 144; c += 4 {
			out = append(out, c)
		}
		for c := 149; c <= 165; c += 4 {
			out = append(out, c)
		}
	case WiFiBand6:
		for c := 1; c <= 233; c += 4 {
			out = append(out, c)
		}
	}
	return out
}

// DFS reports whether a 5 GHz channel needs radar detection.
func DFS(band string, ch int) bool { return band == WiFiBand5 && ch >= 52 && ch <= 144 }

// BandWidths lists the channel widths of a band. 2.4 GHz stays at 20 MHz (as F-WIFI).
func BandWidths(band string) []int {
	switch band {
	case WiFiBand24:
		return []int{20}
	case WiFiBand5, WiFiBand6:
		return []int{20, 40, 80, 160}
	}
	return nil
}

// blockStarts are the first 20 MHz channels of the wider channels.
func blockStarts(band string, width int) []int {
	if band == WiFiBand5 {
		switch width {
		case 40:
			return []int{36, 44, 52, 60, 100, 108, 116, 124, 132, 140, 149, 157}
		case 80:
			return []int{36, 52, 100, 116, 132, 149}
		case 160:
			return []int{36, 100}
		}
		return nil
	}
	if band == WiFiBand6 && width >= 40 {
		n := width / 20
		var out []int
		for s := 1; s+4*(n-1) <= 233; s += 4 * n {
			out = append(out, s)
		}
		return out
	}
	return nil
}

// ChannelBlock returns the first channel and the center channel of the channel of width MHz that contains ch.
// ok is false if the combination does not exist (e.g. channel 165 with 40 MHz).
func ChannelBlock(band string, ch, width int) (start, center int, ok bool) {
	if !slices.Contains(BandChannels(band), ch) || !slices.Contains(BandWidths(band), width) {
		return 0, 0, false
	}
	if width == 20 {
		return ch, ch, true
	}
	n := width / 20
	for _, s := range blockStarts(band, width) {
		if ch >= s && ch <= s+4*(n-1) {
			return s, s + 2*(n-1), true
		}
	}
	return 0, 0, false
}

// ParsePublicKey decodes "ed25519:<base64>" into 32 bytes.
func ParsePublicKey(s string) ([]byte, bool) {
	b64, ok := strings.CutPrefix(s, "ed25519:")
	if !ok {
		return nil, false
	}
	k, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(k) != 32 {
		return nil, false
	}
	return k, true
}

// ValidDisplayName checks a name for the web interface: 1 to 32 characters, no control characters.
func ValidDisplayName(s string) bool {
	if s == "" || len([]rune(s)) > 32 || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Segment returns the segment with the given ID.
func (w *Wireless) Segment(id string) *Segment {
	for i := range w.Segments {
		if w.Segments[i].ID == id {
			return &w.Segments[i]
		}
	}
	return nil
}

// AccessPoint returns the paired access point with the given ID.
func (w *Wireless) AccessPoint(id string) *AccessPoint {
	for i := range w.AccessPoints {
		if w.AccessPoints[i].ID == id {
			return &w.AccessPoints[i]
		}
	}
	return nil
}

// NetworksOn lists the enabled networks of a band, in configuration order.
func (w *Wireless) NetworksOn(band string) []WNetwork {
	var out []WNetwork
	for _, n := range w.Networks {
		if n.Enabled && slices.Contains(n.Bands, band) {
			out = append(out, n)
		}
	}
	return out
}

func (c *Config) validateWireless(v *validator, lanNet netip.Prefix) {
	w := c.Wireless
	if w == nil {
		return
	}
	if !countryRe.MatchString(w.Country) {
		v.add("wireless.country", "Please choose the country the access points are in")
	}
	c.validateSegments(v, lanNet)
	c.validateWNetworks(v)
	c.validateAccessPoints(v)
}

func (c *Config) validateSegments(v *validator, lanNet netip.Prefix) {
	w := c.Wireless
	if len(w.Segments) > MaxSegments {
		v.add("wireless.segments", "At most %d segments", MaxSegments)
	}
	ids, vlans := map[string]bool{}, map[int]bool{}
	var nets []netip.Prefix
	if lanNet.IsValid() {
		nets = append(nets, lanNet)
	}
	home := 0
	for i, s := range w.Segments {
		f := fieldf("wireless.segments[%d]", i)
		if !wIDRe.MatchString(s.ID) {
			v.add(f+".id", "invalid ID %q", s.ID)
		} else if ids[s.ID] {
			v.add(f+".id", "The segment %s exists twice", s.ID)
		}
		ids[s.ID] = true
		if !ValidDisplayName(s.Name) {
			v.add(f+".name", "The name needs 1 to 32 characters")
		}
		if s.VLAN < 1 || s.VLAN > 4094 {
			v.add(f+".vlan", "The VLAN needs a number from 1 to 4094")
		} else if vlans[s.VLAN] {
			v.add(f+".vlan", "VLAN %d is used twice", s.VLAN)
		}
		vlans[s.VLAN] = true
		switch s.Access {
		case AccessLAN:
			home++
			if s.ID != SegmentHome || s.VLAN != 1 {
				v.add(f+".access", "Only the home segment (ID home, VLAN 1) reaches the home network")
			}
			if s.IPv4 != "" {
				v.add(f+".ipv4", "The home segment uses the addresses of the home network")
			}
		case AccessInternet:
			if s.ID == SegmentHome {
				v.add(f+".access", "The home segment must reach the home network")
			}
			p, err := netip.ParsePrefix(s.IPv4)
			switch {
			case err != nil || !p.Addr().Is4():
				v.add(f+".ipv4", "Please give an address with prefix, e.g. 192.168.20.1/24")
			case !p.Addr().IsPrivate() || p.Bits() < 16 || p.Bits() > 28:
				v.add(f+".ipv4", "Please use a private range from /16 to /28")
			case p.Addr() == p.Masked().Addr() || p.Addr() == lastAddr(p):
				v.add(f+".ipv4", "%s is not usable as the router address", p.Addr())
			default:
				for _, o := range nets {
					if o.Overlaps(p.Masked()) {
						v.add(f+".ipv4", "%s overlaps with %s", p.Masked(), o)
					}
				}
				nets = append(nets, p.Masked())
			}
		default:
			v.add(f+".access", "invalid access %q", s.Access)
		}
	}
	if home != 1 {
		v.add("wireless.segments", "There must be exactly one home segment")
	}
	for i, s := range w.Segments {
		for _, r := range s.ReachableFrom {
			if r == s.ID || !ids[r] {
				v.add(fieldf("wireless.segments[%d].reachable_from", i), "unknown segment %q", r)
			}
		}
	}
}

func (c *Config) validateWNetworks(v *validator) {
	w := c.Wireless
	if len(w.Networks) > MaxNetworks {
		v.add("wireless.networks", "At most %d networks", MaxNetworks)
	}
	ids, ssids, mds := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i, n := range w.Networks {
		f := fieldf("wireless.networks[%d]", i)
		if !wIDRe.MatchString(n.ID) {
			v.add(f+".id", "invalid ID %q", n.ID)
		} else if ids[n.ID] {
			v.add(f+".id", "The network %s exists twice", n.ID)
		}
		ids[n.ID] = true
		if !ValidSSID(n.SSID) {
			v.add(f+".ssid", "The network name needs 1 to 32 characters, without control characters and without spaces at the start or end")
		} else if ssids[n.SSID] {
			v.add(f+".ssid", "The network name %s is used twice", n.SSID)
		}
		ssids[n.SSID] = true
		if c.WiFi != nil && c.WiFi.SSID == n.SSID {
			v.add(f+".ssid", "The router's own Wi-Fi already uses the name %s", n.SSID)
		}
		seg := w.Segment(n.Segment)
		if seg == nil {
			v.add(f+".segment", "unknown segment %q", n.Segment)
		}
		if !refRe.MatchString(n.PasswordRef) {
			v.add(f+".password_ref", "invalid reference %q", n.PasswordRef)
		}
		if len(n.Bands) == 0 {
			v.add(f+".bands", "Please choose at least one band")
		}
		seen := map[string]bool{}
		for _, b := range n.Bands {
			if !slices.Contains(WirelessBands, b) || seen[b] {
				v.add(f+".bands", "invalid band %q", b)
			}
			seen[b] = true
		}
		switch n.Security {
		case SecurityWPA3:
			if n.Roaming == nil {
				v.add(f+".roaming", "WPA3 networks always roam (802.11r)")
			} else {
				if !mdRe.MatchString(n.Roaming.MobilityDomain) || n.Roaming.MobilityDomain == "0000" {
					v.add(f+".roaming.mobility_domain", "invalid mobility domain %q", n.Roaming.MobilityDomain)
				} else if mds[n.Roaming.MobilityDomain] {
					v.add(f+".roaming.mobility_domain", "The mobility domain %s is used twice", n.Roaming.MobilityDomain)
				}
				mds[n.Roaming.MobilityDomain] = true
				if !refRe.MatchString(n.Roaming.KeyRef) {
					v.add(f+".roaming.key_ref", "invalid reference %q", n.Roaming.KeyRef)
				}
			}
		case SecurityWPA2WPA3:
			// Older devices (concept 5.4): never in the home segment, never on 6 GHz, no FT
			if seg != nil && seg.Access == AccessLAN {
				v.add(f+".security", "The home network uses WPA3 only. Older devices get a network of their own")
			}
			if seen[WiFiBand6] {
				v.add(f+".bands", "6 GHz allows WPA3 only")
			}
			if n.Roaming != nil {
				v.add(f+".roaming", "Fast roaming needs WPA3")
			}
		default:
			v.add(f+".security", "invalid security %q", n.Security)
		}
	}
	for _, b := range WirelessBands {
		if len(w.NetworksOn(b)) > MaxNetworksOnBand {
			v.add("wireless.networks", "At most %d networks can be on at the same time on %s GHz", MaxNetworksOnBand, b)
		}
	}
}

func (c *Config) validateAccessPoints(v *validator) {
	w := c.Wireless
	if len(w.AccessPoints) > MaxAccessPoints {
		v.add("wireless.access_points", "At most %d access points", MaxAccessPoints)
	}
	ids, keys, macs, names := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i, a := range w.AccessPoints {
		f := fieldf("wireless.access_points[%d]", i)
		if !wIDRe.MatchString(a.ID) || a.ID == "local" {
			v.add(f+".id", "invalid ID %q", a.ID)
		} else if ids[a.ID] {
			v.add(f+".id", "The access point %s exists twice", a.ID)
		}
		ids[a.ID] = true
		if !ValidDisplayName(a.Name) {
			v.add(f+".name", "The name needs 1 to 32 characters")
		} else if names[strings.ToLower(a.Name)] {
			v.add(f+".name", "The name %s is used twice", a.Name)
		}
		names[strings.ToLower(a.Name)] = true
		if _, ok := ParsePublicKey(a.PublicKey); !ok {
			v.add(f+".public_key", "invalid public key")
		} else if keys[a.PublicKey] {
			v.add(f+".public_key", "This access point is paired twice")
		}
		keys[a.PublicKey] = true
		if m := v.mac(f+".mac", a.MAC); m != "" {
			if macs[m] {
				v.add(f+".mac", "The MAC address %s is used twice", m)
			}
			macs[m] = true
		}
		switch a.Power {
		case PowerLow, PowerMedium, PowerHigh:
		default:
			v.add(f+".power", "invalid transmit power %q", a.Power)
		}
		if len(a.Radios) > MaxRadios {
			v.add(f+".radios", "At most %d Wi-Fi cards per access point", MaxRadios)
		}
		rids := map[string]bool{}
		for j, r := range a.Radios {
			rf := fieldf("%s.radios[%d]", f, j)
			if !radioIDRe.MatchString(r.ID) || rids[r.ID] {
				v.add(rf+".id", "invalid card %q", r.ID)
			}
			rids[r.ID] = true
			if !slices.Contains(WirelessBands, r.Band) {
				v.add(rf+".band", "invalid band %q", r.Band)
				continue
			}
			if r.Channel == 0 && r.Width == 0 {
				continue
			}
			if r.Channel == 0 || r.Width == 0 {
				v.add(rf+".channel", "Please pin channel and width together, or neither")
			} else if _, _, ok := ChannelBlock(r.Band, r.Channel, r.Width); !ok {
				v.add(rf+".channel", "Channel %d with %d MHz does not exist on %s GHz", r.Channel, r.Width, r.Band)
			}
		}
	}
}

func fieldf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// ValidRadioID checks the name of a card as an AP reports it.
func ValidRadioID(s string) bool { return radioIDRe.MatchString(s) }
