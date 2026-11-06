// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"fmt"
	"net/netip"
)

// MoveLAN sets a new address for the home network (e.g. 192.168.50.1/24) and moves all
// addresses in the old network (DHCP range, reservations, DNS records, port forwards)
// into the new network. The rear part of the address stays: 192.168.42.20 becomes 192.168.50.20.
// If an address does not fit into the new network (smaller network), there is an error and nothing changes.
func (c *Config) MoveLAN(ipv4 string) error {
	np, err := netip.ParsePrefix(ipv4)
	if err != nil || !np.Addr().Is4() {
		return fmt.Errorf("address with prefix expected, e.g. 192.168.42.1/24")
	}
	oldNet, _, err := c.LANPrefix()
	if err != nil {
		return err
	}
	newNet := np.Masked()
	move := func(s string) (string, error) {
		a, err := netip.ParseAddr(s)
		if err != nil || !a.Is4() || !oldNet.Contains(a) {
			return s, nil // not from the home network (e.g. a public DNS record): unchanged
		}
		host := u32(a) - u32(oldNet.Addr())
		if host >= uint32(1)<<(32-newNet.Bits()) {
			return "", fmt.Errorf("%s does not fit into the smaller network %s", a, newNet)
		}
		return addrFrom(u32(newNet.Addr()) + host).String(), nil
	}
	// First convert everything on a copy, so that an error leaves nothing half changed.
	n := *c
	n.DHCP.Reservations = append([]Reservation(nil), c.DHCP.Reservations...)
	n.DNS.Records = append([]Record(nil), c.DNS.Records...)
	n.Firewall.PortForwards = append([]PortForward(nil), c.Firewall.PortForwards...)
	var errs []error
	set := func(p *string) {
		v, err := move(*p)
		if err != nil {
			errs = append(errs, err)
			return
		}
		*p = v
	}
	set(&n.DHCP.PoolStart)
	set(&n.DHCP.PoolEnd)
	for i := range n.DHCP.Reservations {
		set(&n.DHCP.Reservations[i].IP)
	}
	for i := range n.DNS.Records {
		set(&n.DNS.Records[i].IP)
	}
	for i := range n.Firewall.PortForwards {
		set(&n.Firewall.PortForwards[i].IntIP)
	}
	if len(errs) > 0 {
		return errs[0]
	}
	n.LAN.IPv4 = np.String()
	*c = n
	return nil
}

func u32(a netip.Addr) uint32 {
	b := a.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func addrFrom(n uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)})
}
