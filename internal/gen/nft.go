// SPDX-License-Identifier: AGPL-3.0-or-later

package gen

import (
	"fmt"
	"slices"
	"strings"

	"vanilla-router/internal/config"
)

// Firewall erzeugt /etc/vanilla-router/nft/vr.nft mit den Tabellen
//
//	inet vr_filter  Filter für IPv4 und IPv6 gemeinsam (D-09)
//	ip   vr_nat     Masquerade
//	ip   vr_portfw  Portweiterleitungen (DNAT)
//	inet vr_block   Gerätesperren und Zeitsperren
//
// Jede Tabelle wird mit „table x; delete table x; table x { … }“ ersetzt, nie mit flush ruleset,
// damit Tabellen anderer Dienste (Docker) bestehen bleiben (D-03). `nft -f` lädt die Datei als
// eine Transaktion: Entweder gilt alles oder nichts.
func Firewall(c *config.Config) string {
	var b strings.Builder
	b.WriteString("#!/usr/sbin/nft -f\n")
	b.WriteString(header("#"))
	wan := UplinkIf(c)
	q := func(s string) string { return `"` + s + `"` }
	W, L := q(wan), q(LANIf)

	table(&b, "inet vr_filter", func(t *strings.Builder) {
		t.WriteString("\tset icmpv6_errors {\n\t\ttype icmpv6_type\n\t\telements = { destination-unreachable, packet-too-big, time-exceeded, parameter-problem }\n\t}\n\n")

		t.WriteString("\tchain input {\n\t\ttype filter hook input priority filter; policy drop;\n\n")
		t.WriteString("\t\tiif \"lo\" accept\n")
		t.WriteString("\t\tct state established,related accept\n")
		t.WriteString("\t\tct state invalid drop\n")
		fmt.Fprintf(t, "\t\tiifname %s accept comment \"Heimnetz darf alles\"\n\n", L)
		t.WriteString("\t\t# ICMPv6 nach RFC 4890: ohne ND, RA, MLD und PMTU funktioniert IPv6 nicht\n")
		t.WriteString("\t\ticmpv6 type @icmpv6_errors accept\n")
		t.WriteString("\t\ticmpv6 type { nd-router-advert, nd-neighbor-solicit, nd-neighbor-advert } ip6 hoplimit 255 accept\n")
		t.WriteString("\t\ticmpv6 type { mld-listener-query, mld-listener-report, mld-listener-done, mld2-listener-report } ip6 saddr fe80::/10 accept\n")
		t.WriteString("\t\ticmpv6 type echo-request limit rate 10/second accept\n")
		t.WriteString("\t\ticmp type echo-request limit rate 10/second accept\n")
		t.WriteString("\t\ticmp type { destination-unreachable, time-exceeded, parameter-problem } accept\n")
		if c.WAN.Type == config.WANDHCP {
			fmt.Fprintf(t, "\t\tiifname %s udp sport 67 udp dport 68 accept comment \"DHCPv4-Antworten des Modems\"\n", W)
		}
		if c.WAN.IPv6.Mode == "auto" {
			fmt.Fprintf(t, "\t\tiifname %s ip6 saddr fe80::/10 udp sport 547 udp dport 546 accept comment \"DHCPv6-Antworten des Anbieters\"\n", W)
		}
		t.WriteString("\t}\n\n")

		t.WriteString("\tchain forward {\n\t\ttype filter hook forward priority filter; policy drop;\n\n")
		if c.WAN.Type == config.WANPPPoE {
			fmt.Fprintf(t, "\t\toifname %s tcp flags syn tcp option maxseg size set rt mtu comment \"MSS an PPPoE-MTU anpassen\"\n", W)
			fmt.Fprintf(t, "\t\tiifname %s tcp flags syn tcp option maxseg size set rt mtu\n", W)
		}
		t.WriteString("\t\tct state established,related accept\n")
		t.WriteString("\t\tct state invalid drop\n")
		fmt.Fprintf(t, "\t\tiifname %s oifname %s accept comment \"Heimnetz ins Internet\"\n", L, W)
		fmt.Fprintf(t, "\t\tiifname %s oifname %s meta nfproto ipv4 ct status dnat accept comment \"Portweiterleitungen (DNAT in vr_portfw)\"\n", W, L)
		t.WriteString("\t\ticmpv6 type @icmpv6_errors accept\n")
		fmt.Fprintf(t, "\t\ticmpv6 type echo-request iifname %s oifname %s limit rate 10/second accept\n", W, L)
		for _, ph := range c.Firewall.Pinholes {
			if ph.Disabled {
				continue
			}
			id, _ := config.ParseInterfaceID(ph.InterfaceID)
			for _, proto := range protos(ph.Proto) {
				fmt.Fprintf(t, "\t\tiifname %s oifname %s ip6 daddr & ::ffff:ffff:ffff:ffff == %s %s dport %s accept comment %q\n",
					W, L, id, proto, portRange(ph.Port, ph.PortEnd), "freigabe "+ph.Comment)
			}
		}
		t.WriteString("\t}\n")
	})

	table(&b, "ip vr_nat", func(t *strings.Builder) {
		t.WriteString("\tchain postrouting {\n\t\ttype nat hook postrouting priority srcnat; policy accept;\n")
		fmt.Fprintf(t, "\t\toifname %s masquerade\n", W)
		t.WriteString("\t}\n")
	})

	table(&b, "ip vr_portfw", func(t *strings.Builder) {
		t.WriteString("\tchain prerouting {\n\t\ttype nat hook prerouting priority dstnat; policy accept;\n")
		for _, pf := range c.Firewall.PortForwards {
			if pf.Disabled {
				continue
			}
			for _, proto := range protos(pf.Proto) {
				target := fmt.Sprintf("%s:%d", pf.IntIP, pf.IntPort)
				if pf.ExtEnd != 0 {
					target = pf.IntIP // Bereich 1:1, Ports bleiben erhalten
				}
				fmt.Fprintf(t, "\t\tiifname %s %s dport %s dnat to %s comment %q\n",
					W, proto, portRange(pf.ExtPort, pf.ExtEnd), target, "weiterleitung "+pf.Comment)
			}
		}
		t.WriteString("\t}\n")
	})

	table(&b, "inet vr_block", func(t *strings.Builder) {
		t.WriteString("\tchain forward {\n\t\ttype filter hook forward priority filter - 10; policy accept;\n")
		blocks := slices.Clone(c.Firewall.Blocks)
		slices.SortFunc(blocks, func(a, b config.Block) int { return strings.Compare(a.MAC, b.MAC) })
		for _, bl := range blocks {
			if bl.Disabled {
				continue
			}
			base := fmt.Sprintf("iifname %s oifname %s ether saddr %s", L, W, bl.MAC)
			comment := fmt.Sprintf(" comment %q", "sperre "+bl.MAC)
			if bl.Mode == "always" {
				fmt.Fprintf(t, "\t\t%s drop%s\n", base, comment)
				continue
			}
			// Wochentage meinen den Beginn der Sperre. Über Mitternacht gehört der Morgen zum
			// Folgetag: „So–Do 21:30–07:00“ sperrt bis Freitag 7 Uhr, nicht Sonntagmorgen (L-16).
			// nft rechnet die Ortszeit beim Laden um, deshalb lädt apply mit TZ der Konfiguration.
			if bl.From < bl.To {
				fmt.Fprintf(t, "\t\t%s meta hour \"%s\"-\"%s\"%s drop%s\n", base, bl.From, bl.To, nftDays(bl.Days, 0), comment)
			} else {
				fmt.Fprintf(t, "\t\t%s meta hour \"%s\"-\"23:59:59\"%s drop%s\n", base, bl.From, nftDays(bl.Days, 0), comment)
				fmt.Fprintf(t, "\t\t%s meta hour \"00:00\"-\"%s\"%s drop%s\n", base, bl.To, nftDays(bl.Days, 1), comment)
			}
		}
		t.WriteString("\t}\n")
	})
	return b.String()
}

func table(b *strings.Builder, name string, body func(*strings.Builder)) {
	fmt.Fprintf(b, "\ntable %s\ndelete table %s\n\ntable %s {\n", name, name, name)
	body(b)
	b.WriteString("}\n")
}

func protos(p string) []string {
	if p == "both" {
		return []string{"tcp", "udp"}
	}
	return []string{p}
}

func portRange(start, end int) string {
	if end == 0 || end == start {
		return fmt.Sprint(start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

var dayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// nftDays liefert " meta day { … }" für die Tage, um shift Tage verschoben (leer = jeden Tag).
func nftDays(days []int, shift int) string {
	if len(days) == 0 || len(days) >= 7 {
		return ""
	}
	set := map[int]bool{}
	for _, d := range days {
		set[(d+shift)%7] = true
	}
	var names []string
	for d := 0; d < 7; d++ {
		if set[d] {
			names = append(names, fmt.Sprintf("%q", dayNames[d]))
		}
	}
	return " meta day { " + strings.Join(names, ", ") + " }"
}
