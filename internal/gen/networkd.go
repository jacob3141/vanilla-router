// SPDX-License-Identifier: AGPL-3.0-or-later

package gen

import (
	"fmt"
	"strconv"

	"vanilla-router/internal/config"
)

const networkDir = "/etc/systemd/network/"

// networkd erzeugt .link (feste Namen per MAC), .netdev (Bridge, VLAN) und .network-Dateien.
// IPv6 im Heimnetz (RA, SLAAC, Prefix Delegation) macht allein networkd, dnsmasq macht nur DHCPv4
// und DNS. So gibt es genau eine RA-Quelle im LAN (Q-04).
func networkd(c *config.Config) []File {
	var fs []File
	f := func(name, group string, w *ini) {
		fs = append(fs, File{Path: networkDir + name, Mode: 0o644, Group: group, Content: []byte(header("#") + w.String())})
	}
	ipv6 := c.WAN.IPv6.Mode == "auto"

	// Feste Namen: wan0, lanp1 … Die Reihenfolge der LAN-Ports folgt config.json.
	link := func(mac, name string) *ini {
		w := &ini{}
		w.section("Match")
		w.kv("MACAddress", mac)
		w.kv("Type", "ether")
		w.section("Link")
		w.kv("Name", name)
		return w
	}
	f("10-vr-"+WANIf+".link", GroupLink, link(c.WAN.MatchMAC, WANIf))
	for i, m := range c.LAN.Members {
		name := LANPort + strconv.Itoa(i+1)
		f("10-vr-"+name+".link", GroupLink, link(m, name))
	}

	// Bridge lan0, auch bei nur einem Port, damit weitere ohne Umbau dazukommen (PLAN.md 3.4).
	br := &ini{}
	br.section("NetDev")
	br.kv("Name", LANIf)
	br.kv("Kind", "bridge")
	br.section("Bridge")
	br.kv("STP", "no")
	f("20-vr-"+LANIf+".netdev", GroupNetworkd, br)

	ports := &ini{}
	ports.section("Match")
	ports.kv("Name", LANPort+"*")
	ports.section("Link")
	ports.kv("RequiredForOnline", "no")
	ports.section("Network")
	ports.kv("Bridge", LANIf)
	f("30-vr-"+LANPort+".network", GroupNetworkd, ports)

	// Internet-Anschluss
	wan := &ini{}
	wan.section("Match")
	wan.kv("Name", WANIf)
	wan.section("Link")
	wan.kv("RequiredForOnline", "no") // Router muss auch ohne Modem starten
	if c.WAN.MTU != 0 && c.WAN.Type != config.WANPPPoE {
		wan.kv("MTUBytes", strconv.Itoa(c.WAN.MTU))
	}
	wan.section("Network")
	switch c.WAN.Type {
	case config.WANDHCP:
		wan.kv("DHCP", map[bool]string{true: "yes", false: "ipv4"}[ipv6])
	case config.WANStatic:
		wan.kv("DHCP", map[bool]string{true: "ipv6", false: "no"}[ipv6])
		wan.kv("Address", c.WAN.Static.Address)
		wan.kv("Gateway", c.WAN.Static.Gateway)
		for _, d := range c.WAN.Static.DNS {
			wan.kv("DNS", d)
		}
	case config.WANPPPoE:
		// wan0 trägt nur PPPoE (bzw. das VLAN), keine eigenen Adressen.
		wan.kv("DHCP", "no")
		wan.kv("LinkLocalAddressing", "no")
		wan.kv("IPv6AcceptRA", "no")
		if c.WAN.PPPoE.VLAN > 0 {
			wan.kv("VLAN", pppLowerIf(c))
		}
	}
	if c.WAN.Type != config.WANPPPoE {
		wan.kv("IPv6AcceptRA", yesno(ipv6))
		wan.kv("LinkLocalAddressing", map[bool]string{true: "ipv6", false: "no"}[ipv6])
		if c.WAN.Type == config.WANDHCP {
			wan.section("DHCPv4")
			wan.kv("UseDNS", "yes")
			wan.kv("UseDomains", "no")
			wan.kv("SendHostname", "no") // nichts über das Heimnetz verraten (P-09)
			wan.kv("UseHostname", "no")
		}
		if ipv6 {
			dhcp6(wan, c)
		}
	}
	f("30-vr-"+WANIf+".network", GroupNetworkd, wan)

	if c.WAN.Type == config.WANPPPoE {
		if c.WAN.PPPoE.VLAN > 0 {
			vl := &ini{}
			vl.section("NetDev")
			vl.kv("Name", pppLowerIf(c))
			vl.kv("Kind", "vlan")
			vl.section("VLAN")
			vl.kv("Id", strconv.Itoa(c.WAN.PPPoE.VLAN))
			f("20-vr-"+pppLowerIf(c)+".netdev", GroupNetworkd, vl)

			vn := &ini{}
			vn.section("Match")
			vn.kv("Name", pppLowerIf(c))
			vn.section("Link")
			vn.kv("RequiredForOnline", "no")
			vn.section("Network")
			vn.kv("DHCP", "no")
			vn.kv("LinkLocalAddressing", "no")
			vn.kv("IPv6AcceptRA", "no")
			f("30-vr-"+pppLowerIf(c)+".network", GroupNetworkd, vn)
		}
		// ppp0 legt pppd an und setzt IPv4 selbst. networkd übernimmt nur IPv6 (DHCPv6-PD über PPP).
		pp := &ini{}
		pp.section("Match")
		pp.kv("Name", PPPIf)
		pp.section("Link")
		pp.kv("RequiredForOnline", "no")
		pp.section("Network")
		pp.kv("KeepConfiguration", "yes")
		if ipv6 {
			pp.kv("DHCP", "ipv6")
			pp.kv("IPv6AcceptRA", "yes")
			pp.kv("LinkLocalAddressing", "ipv6")
			dhcp6(pp, c)
		} else {
			pp.kv("DHCP", "no")
			pp.kv("IPv6AcceptRA", "no")
			pp.kv("LinkLocalAddressing", "no")
		}
		f("30-vr-"+PPPIf+".network", GroupNetworkd, pp)
	}

	// Heimnetz
	lan := &ini{}
	lan.section("Match")
	lan.kv("Name", LANIf)
	lan.section("Link")
	lan.kv("RequiredForOnline", "no")
	lan.section("Network")
	lan.kv("Address", c.LAN.IPv4)
	ula, ulaAddr, hasULA := lanULA(c)
	if hasULA {
		lan.kv("Address", ulaAddr.String()+"/64")
	}
	lan.kv("ConfigureWithoutCarrier", "yes")
	lan.kv("IPv6AcceptRA", "no")
	lan.kv("LinkLocalAddressing", "ipv6")
	pd := ipv6 && c.LAN.IPv6.FromWANPrefix
	sendRA := hasULA || pd
	lan.kv("IPv6SendRA", yesno(sendRA))
	if pd {
		lan.kv("DHCPPrefixDelegation", "yes")
		lan.section("DHCPPrefixDelegation")
		lan.kv("UplinkInterface", UplinkIf(c))
		lan.kv("SubnetId", "1")
		lan.kv("Announce", "yes")
	}
	if sendRA {
		lan.section("IPv6SendRA")
		lan.kv("EmitDNS", "yes")
		if hasULA {
			lan.kv("DNS", ulaAddr.String()) // feste Adresse, ändert sich nicht mit dem Anbieter-Präfix
		} else {
			lan.kv("DNS", "_link_local")
		}
		lan.kv("EmitDomains", "yes")
		lan.kv("Domains", c.DNS.Domain)
	}
	if hasULA {
		lan.section("IPv6Prefix")
		lan.kv("Prefix", ula.String())
	}
	f("30-vr-"+LANIf+".network", GroupNetworkd, lan)
	return fs
}

func dhcp6(w *ini, c *config.Config) {
	w.section("DHCPv6")
	w.kv("PrefixDelegationHint", fmt.Sprintf("::/%d", c.WAN.IPv6.RequestPrefix))
	w.kv("UseDNS", "yes")
	w.kv("WithoutRA", "solicit") // manche Anbieter senden kein RA mit gesetztem M-Bit
	w.section("IPv6AcceptRA")
	w.kv("UseDNS", "yes")
}
