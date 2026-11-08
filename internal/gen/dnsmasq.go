// SPDX-License-Identifier: AGPL-3.0-or-later

package gen

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"vanilla-router/internal/config"
)

// DNSmasq generates /etc/vanilla-router/dnsmasq.conf for DNS and DHCPv4 in the home network.
// The program from dnsmasq-base (main) is started by vr-dnsmasq.service, not the package
// dnsmasq from universe (D-10). networkd does router advertisements (Q-04), hence no enable-ra here.
func DNSmasq(c *config.Config) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	b.WriteString(header("#"))
	lanNet, router, _ := c.LANPrefix()
	dom := c.DNS.Domain
	_, ulaAddr, hasULA := lanULA(c)

	w("\n# Only in the home network, never on the internet port")
	w("interface=%s", LANIf)
	w("bind-dynamic")
	w("no-hosts") // /etc/hosts contains 127.0.1.1 for the host name, that should not go into the LAN

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
		// The provider's DNS servers: networkd passes them to resolved (stub listener off, D-12),
		// pppd writes them to /etc/ppp/resolv.conf with PPPoE.
		w("resolv-file=/run/systemd/resolve/resolv.conf")
		if c.WAN.Type == config.WANPPPoE {
			w("resolv-file=/etc/ppp/resolv.conf")
		}
	}

	w("\n# Names in the home network")
	if hasULA {
		w("host-record=%s.%s,%s,%s,%s", c.System.Hostname, dom, c.System.Hostname, router, ulaAddr)
	} else {
		w("host-record=%s.%s,%s,%s", c.System.Hostname, dom, c.System.Hostname, router)
	}
	for _, r := range c.DHCP.Reservations {
		// Name resolvable even if the device has no lease right now
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
	if segs := routedSegments(c); len(segs) > 0 {
		segmentDNSmasq(w, segs, lease, dom)
	}
	return b.String()
}

func netmask(p netip.Prefix) string {
	m := ^uint32(0) << (32 - p.Bits())
	return fmt.Sprintf("%d.%d.%d.%d", m>>24, m>>16&0xff, m>>8&0xff, m&0xff)
}

// dnsmasqDuration writes a duration in dnsmasq form (12h, 30m, 90s).
func dnsmasqDuration(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return fmt.Sprintf("%d", int(d.Seconds()))
}
