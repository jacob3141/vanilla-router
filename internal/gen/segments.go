// SPDX-License-Identifier: AGPL-3.0-or-later

package gen

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"vanilla-router/internal/config"
)

// Segments on the router (docs/sdn-concept.md 5.7). Every segment apart from the home network is a VLAN on lan0 with
// its own addresses, DHCP, DNS and firewall rules. A router without such segments generates exactly what it did before.
// Segments are IPv4 only for now: without a stable second prefix IPv6 in guest networks would need its own design.

// SegmentIf is the interface of a segment on the router.
func SegmentIf(vlan int) string { return "vl" + strconv.Itoa(vlan) }

// routedSegments are the segments that need an interface of their own, sorted by VLAN.
func routedSegments(c *config.Config) []config.Segment {
	if c.Wireless == nil {
		return nil
	}
	var out []config.Segment
	for _, s := range c.Wireless.Segments {
		if s.Access == config.AccessInternet {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b config.Segment) int { return a.VLAN - b.VLAN })
	return out
}

// segmentNetworkd writes the VLAN interfaces and adds the VLANs to bridge and ports. Called only with segments.
func segmentNetworkd(f func(name, group string, w *ini), segs []config.Segment) {
	for _, s := range segs {
		name := SegmentIf(s.VLAN)
		nd := &ini{}
		nd.section("NetDev")
		nd.kv("Name", name)
		nd.kv("Kind", "vlan")
		nd.section("VLAN")
		nd.kv("Id", strconv.Itoa(s.VLAN))
		f("20-vr-"+name+".netdev", GroupNetworkd, nd)

		n := &ini{}
		n.section("Match")
		n.kv("Name", name)
		n.section("Link")
		n.kv("RequiredForOnline", "no")
		n.section("Network")
		n.kv("Address", s.IPv4)
		n.kv("ConfigureWithoutCarrier", "yes")
		n.kv("LinkLocalAddressing", "no")
		n.kv("IPv6AcceptRA", "no")
		n.kv("IPv6SendRA", "no")
		f("30-vr-"+name+".network", GroupNetworkd, n)
	}
}

// bridgeVLANs adds the [BridgeVLAN] sections of a tagged port: the home network untagged, every segment tagged.
func bridgeVLANs(w *ini, segs []config.Segment) {
	w.section("BridgeVLAN")
	w.kv("PVID", "1")
	w.kv("EgressUntagged", "1")
	for _, s := range segs {
		w.section("BridgeVLAN")
		w.kv("VLAN", strconv.Itoa(s.VLAN))
	}
}

// segmentPool is the DHCP range of a segment: from .100 where the range is large enough, without the router's address.
func segmentPool(s config.Segment) (start, end netip.Addr, mask string) {
	p, _ := netip.ParsePrefix(s.IPv4)
	net := p.Masked()
	last := lastAddr4(net)
	end = last.Prev()
	start = net.Addr().Next()
	if n := 32 - net.Bits(); n >= 8 {
		for range 99 {
			start = start.Next()
		}
	}
	if !p.Addr().Less(start) && !end.Less(p.Addr()) {
		if p.Addr() == end {
			end = end.Prev()
		} else {
			start = p.Addr().Next()
		}
	}
	return start, end, netmask(net)
}

func lastAddr4(p netip.Prefix) netip.Addr {
	b := p.Masked().Addr().As4()
	host := uint32(1)<<(32-p.Bits()) - 1
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]) | host
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

// segmentDNSmasq adds DHCP and DNS for the segments. Called only with segments.
func segmentDNSmasq(w func(string, ...any), segs []config.Segment, lease time.Duration, dom string) {
	w("\n# Segments")
	for _, s := range segs {
		name := SegmentIf(s.VLAN)
		start, end, mask := segmentPool(s)
		router, _, _ := strings.Cut(s.IPv4, "/")
		w("interface=%s", name)
		w("dhcp-range=%s,%s,%s,%s,%s", name, start, end, mask, dnsmasqDuration(lease))
		w("dhcp-option=%s,option:router,%s", name, router)
		w("dhcp-option=%s,option:dns-server,%s", name, router)
		w("dhcp-option=%s,option:domain-search,%s", name, dom)
	}
}

// segmentInput lets devices in a segment use the router for DHCP and DNS, and nothing else: no web interface, no SSH.
func segmentInput(t *strings.Builder, segs []config.Segment) {
	for _, s := range segs {
		n := SegmentIf(s.VLAN)
		fmt.Fprintf(t, "\t\tiifname %q udp sport 68 udp dport 67 accept comment \"DHCP in segment %s\"\n", n, s.ID)
		fmt.Fprintf(t, "\t\tiifname %q meta l4proto { tcp, udp } th dport 53 accept comment \"DNS in segment %s\"\n", n, s.ID)
	}
	if len(segs) > 0 {
		t.WriteString("\n")
	}
}

// segmentForward: a segment reaches the internet and nothing else. reachable_from opens the way in, answers go
// through "ct state established". Everything else falls to the chain's drop.
func segmentForward(t *strings.Builder, segs []config.Segment, w *config.Wireless, wan string) {
	ifOf := func(id string) string {
		if s := w.Segment(id); s != nil && s.Access == config.AccessInternet {
			return SegmentIf(s.VLAN)
		}
		return LANIf
	}
	for _, s := range segs {
		n := SegmentIf(s.VLAN)
		fmt.Fprintf(t, "\t\tiifname %q oifname %q meta nfproto ipv4 accept comment \"segment %s to internet\"\n", n, wan, s.ID)
		for _, r := range s.ReachableFrom {
			fmt.Fprintf(t, "\t\tiifname %q oifname %q meta nfproto ipv4 accept comment \"segment %s reachable from %s\"\n", ifOf(r), n, s.ID, r)
		}
	}
}
