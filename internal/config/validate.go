// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

// FieldError is an error at a certain field. Text is meant for the web interface.
type FieldError struct {
	Field, Text string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Text }

// Errors collects all errors of a check, so the web interface can show them at once.
type Errors []FieldError

func (es Errors) Error() string {
	var s []string
	for _, e := range es {
		s = append(s, e.Error())
	}
	return strings.Join(s, "; ")
}

var (
	labelRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	refRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	tzRe      = regexp.MustCompile(`^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+){0,2}$`)
	localeRe  = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,32}$`)
	commentRe = regexp.MustCompile(`^[\p{L}\p{N} ._:/()+,-]{0,64}$`)
	hhmmRe    = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	pppUserRe = regexp.MustCompile(`^[\x21-\x7e]{1,128}$`)
	sshKeyRe  = regexp.MustCompile(`^(ssh-ed25519|ecdsa-sha2-nistp(256|384|521)|ssh-rsa|sk-ssh-ed25519@openssh\.com) [A-Za-z0-9+/=]{16,} ?[\x20-\x7e]{0,128}$`)
	idRe      = regexp.MustCompile(`^[a-z0-9]{8,32}$`)
	ula       = netip.MustParsePrefix("fd00::/8")
)

type validator struct {
	errs Errors
}

func (v *validator) add(field, format string, args ...any) {
	v.errs = append(v.errs, FieldError{field, fmt.Sprintf(format, args...)})
}

// NormalizeMAC returns the MAC in lowercase with colons. Only 48-bit unicast addresses.
func NormalizeMAC(s string) (string, error) {
	hw, err := net.ParseMAC(s)
	if err != nil || len(hw) != 6 {
		return "", fmt.Errorf("invalid MAC address %q", s)
	}
	if hw[0]&1 != 0 {
		return "", fmt.Errorf("%q is a group address, not a device address", s)
	}
	return hw.String(), nil
}

// ValidHostname checks a single DNS name part (RFC 1123, lowercase only).
func ValidHostname(s string) bool { return labelRe.MatchString(s) }

func validDomain(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, l := range strings.Split(s, ".") {
		if !labelRe.MatchString(l) {
			return false
		}
	}
	return true
}

func (v *validator) mac(field, s string) string {
	m, err := NormalizeMAC(s)
	if err != nil {
		v.add(field, "%v", err)
		return ""
	}
	if m != s {
		v.add(field, "please write the MAC in the form %s", m)
	}
	return m
}

func (v *validator) port(field string, p int) {
	if p < 1 || p > 65535 {
		v.add(field, "port %d is not between 1 and 65535", p)
	}
}

func (v *validator) proto(field, p string, both bool) {
	if p == "tcp" || p == "udp" || (both && p == "both") {
		return
	}
	v.add(field, "protocol %q unknown (tcp, udp%s)", p, map[bool]string{true: " or both"}[both])
}

func (v *validator) comment(field, s string) {
	if !commentRe.MatchString(s) {
		v.add(field, "Comment is too long or contains characters that are not allowed (allowed: letters, digits, spaces, . _ : / ( ) + , -)")
	}
}

// LANPrefix returns the home network (e.g. 192.168.42.0/24) and the router's address.
func (c *Config) LANPrefix() (netip.Prefix, netip.Addr, error) {
	p, err := netip.ParsePrefix(c.LAN.IPv4)
	if err != nil {
		return netip.Prefix{}, netip.Addr{}, err
	}
	return p.Masked(), p.Addr(), nil
}

// Validate checks the whole configuration and returns all errors at once (type Errors).
func (c *Config) Validate() error {
	v := &validator{}
	if c.Schema != SchemaVersion {
		v.add("schema", "version %d is not supported", c.Schema)
	}
	c.validateSystem(v)
	lanNet, routerIP := c.validateLAN(v)
	c.validateWAN(v, lanNet)
	c.validateDHCP(v, lanNet, routerIP)
	c.validateDNS(v)
	c.validateFirewall(v, lanNet, routerIP)
	c.validateServices(v)
	c.validateWiFi(v)
	if c.Updates.RebootWindow != "" && !hhmmRe.MatchString(c.Updates.RebootWindow) {
		v.add("updates.reboot_window", "please give the time as HH:MM")
	}
	if len(v.errs) > 0 {
		return v.errs
	}
	return nil
}

func (c *Config) validateSystem(v *validator) {
	if !ValidHostname(c.System.Hostname) {
		v.add("system.hostname", "name %q is invalid (lowercase letters, digits and hyphen, at most 63 characters)", c.System.Hostname)
	}
	if !tzRe.MatchString(c.System.Timezone) || strings.Contains(c.System.Timezone, "..") {
		v.add("system.timezone", "time zone %q is invalid", c.System.Timezone)
	}
	if c.System.Locale != "" && !localeRe.MatchString(c.System.Locale) {
		v.add("system.locale", "invalid")
	}
	if c.System.Keymap != "" && !localeRe.MatchString(c.System.Keymap) {
		v.add("system.keymap", "invalid")
	}
}

func (c *Config) validateLAN(v *validator) (netip.Prefix, netip.Addr) {
	if len(c.LAN.Members) == 0 {
		v.add("lan.members", "The home network needs at least one port")
	}
	seen := map[string]bool{}
	for i, m := range c.LAN.Members {
		mm := v.mac(fmt.Sprintf("lan.members[%d]", i), m)
		if mm != "" && seen[mm] {
			v.add(fmt.Sprintf("lan.members[%d]", i), "port %s listed twice", mm)
		}
		seen[mm] = true
	}
	p, err := netip.ParsePrefix(c.LAN.IPv4)
	if err != nil || !p.Addr().Is4() {
		v.add("lan.ipv4", "address with prefix expected, e.g. 192.168.42.1/24")
		return netip.Prefix{}, netip.Addr{}
	}
	net4, addr := p.Masked(), p.Addr()
	switch {
	case !addr.IsPrivate():
		v.add("lan.ipv4", "The home network must use a private address range (10.x, 172.16–31.x, 192.168.x)")
	case p.Bits() < 8 || p.Bits() > 30:
		v.add("lan.ipv4", "prefix length %d makes no sense (8 to 30)", p.Bits())
	case addr == net4.Addr() || addr == lastAddr(net4):
		v.add("lan.ipv4", "%s is the network or broadcast address", addr)
	}
	if c.LAN.IPv6.ULA != "" {
		u, err := netip.ParsePrefix(c.LAN.IPv6.ULA)
		if err != nil || u.Bits() != 48 || !ula.Contains(u.Addr()) || u.Masked() != u {
			v.add("lan.ipv6.ula", "ULA prefix /48 from fd00::/8 expected, e.g. fd12:3456:789a::/48")
		}
	}
	return net4, addr
}

func (c *Config) validateWAN(v *validator, lanNet netip.Prefix) {
	w := c.WAN
	if m := v.mac("wan.match_mac", w.MatchMAC); m != "" {
		for _, lm := range c.LAN.Members {
			if strings.EqualFold(lm, m) {
				v.add("wan.match_mac", "The internet port cannot also be in the home network")
			}
		}
	}
	switch w.Type {
	case WANDHCP:
	case WANPPPoE:
		if w.PPPoE == nil {
			v.add("wan.pppoe", "credentials are missing")
			break
		}
		if !pppUserRe.MatchString(w.PPPoE.User) || strings.ContainsAny(w.PPPoE.User, `"\`) {
			v.add("wan.pppoe.user", "user name is missing or contains characters that are not allowed")
		}
		if !refRe.MatchString(w.PPPoE.PasswordRef) {
			v.add("wan.pppoe.password_ref", "password reference is invalid")
		}
		if w.PPPoE.VLAN < 0 || w.PPPoE.VLAN > 4094 {
			v.add("wan.pppoe.vlan", "VLAN %d is not between 1 and 4094", w.PPPoE.VLAN)
		}
	case WANStatic:
		s := w.Static
		if s == nil {
			v.add("wan.static", "address is missing")
			break
		}
		p, err := netip.ParsePrefix(s.Address)
		if err != nil || !p.Addr().Is4() || p.Bits() < 8 || p.Bits() > 32 {
			v.add("wan.static.address", "address with prefix expected, e.g. 203.0.113.10/29")
			break
		}
		gw, err := netip.ParseAddr(s.Gateway)
		if err != nil || !gw.Is4() || (p.Bits() < 32 && !p.Masked().Contains(gw)) || gw == p.Addr() {
			v.add("wan.static.gateway", "gateway %q is not in the network %s", s.Gateway, p.Masked())
		}
		if lanNet.IsValid() && p.Masked().Overlaps(lanNet) {
			v.add("wan.static.address", "The network overlaps with the home network %s", lanNet)
		}
		for i, d := range s.DNS {
			if _, err := netip.ParseAddr(d); err != nil {
				v.add(fmt.Sprintf("wan.static.dns[%d]", i), "not an IP address: %q", d)
			}
		}
	default:
		v.add("wan.type", "connection type %q unknown (dhcp, pppoe, static)", w.Type)
	}
	if w.Type != WANPPPoE && w.PPPoE != nil {
		v.add("wan.pppoe", "only allowed with connection type pppoe")
	}
	if w.Type != WANStatic && w.Static != nil {
		v.add("wan.static", "only allowed with connection type static")
	}
	switch w.IPv6.Mode {
	case "off":
	case "auto":
		if w.IPv6.RequestPrefix < 48 || w.IPv6.RequestPrefix > 64 {
			v.add("wan.ipv6.request_prefix", "prefix length %d not between 48 and 64", w.IPv6.RequestPrefix)
		}
	default:
		v.add("wan.ipv6.mode", "auto or off expected")
	}
	if w.MTU != 0 && (w.MTU < 1280 || w.MTU > 9000) {
		v.add("wan.mtu", "MTU %d not between 1280 and 9000", w.MTU)
	}
	if d := w.Detected; d != nil {
		if d.Method != WANDHCP && d.Method != WANPPPoE && d.Method != DetectedNone {
			v.add("wan.detected.method", "dhcp, pppoe or none expected")
		}
		if d.VLAN < 0 || d.VLAN > 4094 || (d.VLAN != 0 && d.Method != WANPPPoE) {
			v.add("wan.detected.vlan", "VLAN only with pppoe, 1 to 4094")
		}
	}
}

func (c *Config) validateDHCP(v *validator, lanNet netip.Prefix, router netip.Addr) {
	d := c.DHCP
	if !lanNet.IsValid() {
		return
	}
	inLAN := func(field, s string) netip.Addr {
		a, err := netip.ParseAddr(s)
		switch {
		case err != nil || !a.Is4():
			v.add(field, "not an IPv4 address: %q", s)
		case !lanNet.Contains(a):
			v.add(field, "%s is not in the home network %s", a, lanNet)
		case a == router:
			v.add(field, "%s is the router's address", a)
		case a == lanNet.Addr() || a == lastAddr(lanNet):
			v.add(field, "%s is the network or broadcast address", a)
		default:
			return a
		}
		return netip.Addr{}
	}
	start, end := inLAN("dhcp.pool_start", d.PoolStart), inLAN("dhcp.pool_end", d.PoolEnd)
	if start.IsValid() && end.IsValid() && end.Less(start) {
		v.add("dhcp.pool_end", "end %s is before the start %s", end, start)
	}
	if l, err := time.ParseDuration(d.Lease); err != nil || l < 2*time.Minute || l > 30*24*time.Hour {
		v.add("dhcp.lease", "duration between 2m and 720h expected, e.g. 12h")
	}
	names, macs, ips := map[string]bool{c.System.Hostname: true}, map[string]bool{}, map[netip.Addr]bool{}
	for i, r := range d.Reservations {
		f := fmt.Sprintf("dhcp.reservations[%d]", i)
		if !ValidHostname(r.Name) {
			v.add(f+".name", "name %q is invalid (lowercase letters, digits, hyphen)", r.Name)
		} else if names[r.Name] {
			v.add(f+".name", "name %q is already taken", r.Name)
		}
		names[r.Name] = true
		if m := v.mac(f+".mac", r.MAC); m != "" {
			if macs[m] {
				v.add(f+".mac", "there is already a reservation for %s", m)
			}
			macs[m] = true
		}
		if len(r.Comment) > 200 || strings.ContainsAny(r.Comment, "\n\r\"\\#;{}") {
			v.add(f+".comment", "comment too long or with characters that are not allowed")
		}
		if a := inLAN(f+".ip", r.IP); a.IsValid() {
			if ips[a] {
				v.add(f+".ip", "%s is already reserved", a)
			}
			ips[a] = true
		}
	}
}

func (c *Config) validateDNS(v *validator) {
	if !validDomain(c.DNS.Domain) {
		v.add("dns.domain", "domain %q is invalid (recommended: home.arpa)", c.DNS.Domain)
	}
	for i, u := range c.DNS.Upstreams {
		if _, err := netip.ParseAddr(u); err != nil {
			v.add(fmt.Sprintf("dns.upstreams[%d]", i), "not an IP address: %q", u)
		}
	}
	taken := map[string]bool{c.System.Hostname: true}
	for _, r := range c.DHCP.Reservations {
		taken[r.Name] = true
	}
	for i, r := range c.DNS.Records {
		f := fmt.Sprintf("dns.records[%d]", i)
		if !ValidHostname(r.Name) {
			v.add(f+".name", "name %q is invalid", r.Name)
		} else if taken[r.Name] {
			v.add(f+".name", "name %q is already taken", r.Name)
		}
		taken[r.Name] = true
		if _, err := netip.ParseAddr(r.IP); err != nil {
			v.add(f+".ip", "not an IP address: %q", r.IP)
		}
	}
}

func (c *Config) validateFirewall(v *validator, lanNet netip.Prefix, router netip.Addr) {
	type used struct {
		proto      string
		start, end int
	}
	var taken []used
	overlap := func(proto string, s, e int) bool {
		for _, t := range taken {
			if (t.proto == proto || t.proto == "both" || proto == "both") && s <= t.end && t.start <= e {
				return true
			}
		}
		return false
	}
	ids := map[string]bool{}
	for i, pf := range c.Firewall.PortForwards {
		f := fmt.Sprintf("firewall.port_forwards[%d]", i)
		if pf.ID != "" {
			if !idRe.MatchString(pf.ID) || ids[pf.ID] {
				v.add(f+".id", "ID invalid or duplicate")
			}
			ids[pf.ID] = true
		}
		v.proto(f+".proto", pf.Proto, true)
		v.port(f+".ext_port", pf.ExtPort)
		v.port(f+".int_port", pf.IntPort)
		end := pf.ExtPort
		if pf.ExtEnd != 0 {
			v.port(f+".ext_end", pf.ExtEnd)
			if pf.ExtEnd < pf.ExtPort {
				v.add(f+".ext_end", "the end of the range is before the start")
			}
			end = pf.ExtEnd
			if pf.IntPort != pf.ExtPort {
				// nft cannot map ranges with an offset (1000–1010 → 2000–2010), only 1:1.
				v.add(f+".int_port", "With a port range the target port must equal the external port")
			}
		}
		a, err := netip.ParseAddr(pf.IntIP)
		switch {
		case err != nil || !a.Is4():
			v.add(f+".int_ip", "not an IPv4 address: %q", pf.IntIP)
		case lanNet.IsValid() && !lanNet.Contains(a):
			v.add(f+".int_ip", "%s is not in the home network", a)
		case a == router:
			v.add(f+".int_ip", "Forwarding to the router itself is not allowed")
		}
		v.comment(f+".comment", pf.Comment)
		if !pf.Disabled {
			if overlap(pf.Proto, pf.ExtPort, end) {
				v.add(f+".ext_port", "port %d is already forwarded", pf.ExtPort)
			}
			taken = append(taken, used{pf.Proto, pf.ExtPort, end})
		}
	}
	for i, ph := range c.Firewall.Pinholes {
		f := fmt.Sprintf("firewall.ipv6_pinholes[%d]", i)
		if _, err := ParseInterfaceID(ph.InterfaceID); err != nil {
			v.add(f+".interface_id", "%v", err)
		}
		v.proto(f+".proto", ph.Proto, true)
		v.port(f+".port", ph.Port)
		if ph.PortEnd != 0 && ph.PortEnd < ph.Port {
			v.add(f+".port_end", "the end of the range is before the start")
		}
		v.comment(f+".comment", ph.Comment)
	}
	seen := map[string]bool{}
	for i, b := range c.Firewall.Blocks {
		f := fmt.Sprintf("firewall.blocks[%d]", i)
		if m := v.mac(f+".mac", b.MAC); m != "" {
			if seen[m] {
				v.add(f+".mac", "there is already a block for %s", m)
			}
			seen[m] = true
		}
		v.comment(f+".name", b.Name)
		switch b.Mode {
		case "always":
		case "schedule":
			if !hhmmRe.MatchString(b.From) || !hhmmRe.MatchString(b.To) || b.From == b.To {
				v.add(f, "block time \"%s–%s\" is invalid (HH:MM, start and end must differ)", b.From, b.To)
			}
			for _, d := range b.Days {
				if d < 0 || d > 6 {
					v.add(f+".days", "weekday %d invalid (0 = Sunday … 6 = Saturday)", d)
				}
			}
		default:
			v.add(f+".mode", "always or schedule expected")
		}
	}
}

func (c *Config) validateServices(v *validator) {
	for i, k := range c.Services.SSH.AuthorizedKeys {
		if !sshKeyRe.MatchString(k) {
			v.add(fmt.Sprintf("services.ssh.authorized_keys[%d]", i), "not a valid public SSH key (one line, e.g. ssh-ed25519 AAAA…)")
		}
	}
	if c.Services.SSH.Enabled && len(c.Services.SSH.AuthorizedKeys) == 0 {
		v.add("services.ssh.authorized_keys", "SSH only works with a key, please enter at least one")
	}
}

// ParseInterfaceID checks an interface ID like ::1a2b:3c4d:5e6f:7890 (only the lower 64 bits).
func ParseInterfaceID(s string) (netip.Addr, error) {
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is6() || a.Is4In6() {
		return netip.Addr{}, fmt.Errorf("interface ID %q invalid, form ::1a2b:3c4d:5e6f:7890 expected", s)
	}
	b := a.As16()
	for _, x := range b[:8] {
		if x != 0 {
			return netip.Addr{}, fmt.Errorf("interface ID %q may only contain the rear 64 bits", s)
		}
	}
	if a == netip.IPv6Unspecified() {
		return netip.Addr{}, fmt.Errorf("interface ID must not be ::")
	}
	return a, nil
}

// lastAddr returns the highest address of an IPv4 network (broadcast).
func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Masked().Addr().As4()
	host := uint32(1)<<(32-p.Bits()) - 1
	n := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	n |= host
	return netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)})
}
