// SPDX-License-Identifier: AGPL-3.0-or-later

package gen

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"vanilla-router/internal/config"
)

// DNSmasq erzeugt /etc/vanilla-router/dnsmasq.conf für DNS und DHCPv4 im Heimnetz.
// Gestartet wird das Programm aus dnsmasq-base (main) von vr-dnsmasq.service, nicht das Paket
// dnsmasq aus universe (D-10). Router Advertisements macht networkd (Q-04), hier also kein enable-ra.
func DNSmasq(c *config.Config) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	b.WriteString(header("#"))
	lanNet, router, _ := c.LANPrefix()
	dom := c.DNS.Domain
	_, ulaAddr, hasULA := lanULA(c)

	w("\n# Nur im Heimnetz, nie am Internet-Anschluss")
	w("interface=%s", LANIf)
	w("bind-dynamic")
	w("no-hosts") // /etc/hosts enthält 127.0.1.1 für den Hostnamen, das soll nicht ins LAN

	w("\n# DNS")
	w("domain-needed")
	w("bogus-priv")
	w("stop-dns-rebind")
	w("rebind-localhost-ok")
	w("rebind-domain-ok=/%s/", dom)
	w("local=/%s/", dom)
	w("domain=%s,%s,local", dom, lanNet)
	w("cache-size=10000")
	if len(c.DNS.Upstreams) > 0 {
		w("no-resolv")
		for _, u := range c.DNS.Upstreams {
			w("server=%s", u)
		}
	} else {
		// DNS-Server des Anbieters: networkd gibt sie an resolved weiter (Stub-Listener aus, D-12),
		// pppd schreibt sie bei PPPoE nach /etc/ppp/resolv.conf.
		w("resolv-file=/run/systemd/resolve/resolv.conf")
		if c.WAN.Type == config.WANPPPoE {
			w("resolv-file=/etc/ppp/resolv.conf")
		}
	}

	w("\n# Namen im Heimnetz")
	if hasULA {
		w("host-record=%s.%s,%s,%s,%s", c.System.Hostname, dom, c.System.Hostname, router, ulaAddr)
	} else {
		w("host-record=%s.%s,%s,%s", c.System.Hostname, dom, c.System.Hostname, router)
	}
	for _, r := range c.DHCP.Reservations {
		// Name auch dann auflösbar, wenn das Gerät gerade keine Lease hat
		w("host-record=%s.%s,%s,%s", r.Name, dom, r.Name, r.IP)
	}
	for _, r := range c.DNS.Records {
		w("host-record=%s.%s,%s,%s", r.Name, dom, r.Name, r.IP)
	}

	w("\n# DHCPv4")
	lease, _ := time.ParseDuration(c.DHCP.Lease)
	w("dhcp-range=%s,%s,%s,%s,%s", LANIf, c.DHCP.PoolStart, c.DHCP.PoolEnd, netmask(lanNet), dnsmasqDuration(lease))
	w("dhcp-option=%s,option:router,%s", LANIf, router)
	w("dhcp-option=%s,option:dns-server,%s", LANIf, router)
	w("dhcp-option=%s,option:domain-search,%s", LANIf, dom)
	w("dhcp-authoritative")
	w("dhcp-rapid-commit")
	w("dhcp-leasefile=/var/lib/vanilla-router/dnsmasq.leases")
	for _, r := range c.DHCP.Reservations {
		w("dhcp-host=%s,%s,%s", r.MAC, r.IP, r.Name)
	}
	return b.String()
}

func netmask(p netip.Prefix) string {
	m := ^uint32(0) << (32 - p.Bits())
	return fmt.Sprintf("%d.%d.%d.%d", m>>24, m>>16&0xff, m>>8&0xff, m&0xff)
}

// dnsmasqDuration schreibt eine Dauer in dnsmasq-Form (12h, 30m, 90s).
func dnsmasqDuration(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return fmt.Sprintf("%d", int(d.Seconds()))
}
