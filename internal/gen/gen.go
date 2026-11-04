// SPDX-License-Identifier: AGPL-3.0-or-later

// Package gen erzeugt aus einer geprüften Konfiguration alle Dateien der Dienste (P-04, M2):
// systemd-networkd, nftables, dnsmasq, pppd, sysctl, sshd, resolved und unattended-upgrades.
// Erzeugen ist eine reine Funktion ohne Zugriff auf das System, damit sie mit Golden Files
// getestet werden kann (NF-04). Einspielen übernimmt internal/apply.
package gen

import (
	"fmt"
	"net/netip"
	"path"
	"sort"
	"strings"

	"vanilla-router/internal/config"
)

// Gruppen bestimmen, was nach einer Änderung neu geladen wird.
const (
	GroupNetworkd = "networkd"
	GroupLink     = "link" // Umbenennung der Ports, wirkt erst nach Neustart
	GroupFirewall = "firewall"
	GroupDNSmasq  = "dnsmasq"
	GroupPPP      = "ppp"
	GroupSysctl   = "sysctl"
	GroupResolved = "resolved"
	GroupSSH      = "ssh"
	GroupAPT      = "apt"
)

// File ist eine erzeugte Datei.
type File struct {
	Path    string
	Mode    uint32
	Group   string
	Content []byte
}

// Managed beschreibt, welche Dateien vanilla-router gehören. Was dort liegt und nicht mehr erzeugt
// wird, entfernt apply (z. B. die VLAN-Datei nach einem Wechsel von PPPoE auf DHCP).
var Managed = []struct{ Dir, Glob, Group string }{
	{"/etc/systemd/network", "*-vr-*.link", GroupLink},
	{"/etc/systemd/network", "*-vr-*.netdev", GroupNetworkd},
	{"/etc/systemd/network", "*-vr-*.network", GroupNetworkd},
	{"/etc/vanilla-router/nft", "*.nft", GroupFirewall},
	{"/etc/ppp/peers", "vr-*", GroupPPP},
}

// Header steht in jeder erzeugten Textdatei.
func header(comment string) string {
	return comment + " Von vanilla-router erzeugt, nicht von Hand ändern.\n" +
		comment + " Änderungen über die Weboberfläche oder /etc/vanilla-router/config.json und `vrctl apply`.\n"
}

// Secrets liefert Geheimnisse, auf die config.json nur verweist (z. B. das PPPoE-Passwort).
type Secrets interface {
	Get(ref string) (string, error)
}

// Names sind die festen Interface-Namen (PLAN.md 3.4).
const (
	WANIf    = "wan0"
	LANIf    = "lan0"
	LANPort  = "lanp" // lanp1, lanp2 … sind die Ports in der Bridge lan0
	PPPIf    = "ppp0"
	PPPPeers = "vr-wan"
)

// UplinkIf ist das Interface Richtung Internet für Firewall und Prefix Delegation.
func UplinkIf(c *config.Config) string {
	if c.WAN.Type == config.WANPPPoE {
		return PPPIf
	}
	return WANIf
}

// pppLowerIf ist das Interface, auf dem pppd PPPoE spricht (mit VLAN: wan0.7).
func pppLowerIf(c *config.Config) string {
	if c.WAN.PPPoE != nil && c.WAN.PPPoE.VLAN > 0 {
		return fmt.Sprintf("%s.%d", WANIf, c.WAN.PPPoE.VLAN)
	}
	return WANIf
}

// lanULA liefert Präfix /64 und Router-Adresse im Heimnetz aus dem ULA-/48 (Subnetz 1, Adresse ::1).
func lanULA(c *config.Config) (netip.Prefix, netip.Addr, bool) {
	if c.LAN.IPv6.ULA == "" {
		return netip.Prefix{}, netip.Addr{}, false
	}
	p := netip.MustParsePrefix(c.LAN.IPv6.ULA)
	b := p.Addr().As16()
	b[7] = 1 // Subnetz-ID 1
	net64 := netip.PrefixFrom(netip.AddrFrom16(b), 64)
	b[15] = 1
	return net64, netip.AddrFrom16(b), true
}

// Render erzeugt alle Dateien. Die Konfiguration muss geprüft sein (config.Validate).
func Render(c *config.Config, sec Secrets) ([]File, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var files []File
	add := func(fs ...File) { files = append(files, fs...) }

	add(networkd(c)...)
	add(File{Path: "/etc/vanilla-router/nft/vr.nft", Mode: 0o644, Group: GroupFirewall, Content: []byte(Firewall(c))})
	add(File{Path: "/etc/vanilla-router/dnsmasq.conf", Mode: 0o644, Group: GroupDNSmasq, Content: []byte(DNSmasq(c))})
	ppp, err := pppFiles(c, sec)
	if err != nil {
		return nil, err
	}
	add(ppp...)
	add(misc(c)...)

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for i := 1; i < len(files); i++ {
		if files[i].Path == files[i-1].Path {
			return nil, fmt.Errorf("interner Fehler: %s doppelt erzeugt", files[i].Path)
		}
	}
	return files, nil
}

// IsManaged meldet, ob ein Pfad zu den von vanilla-router verwalteten Dateien gehört.
func IsManaged(p string) (string, bool) {
	for _, m := range Managed {
		if path.Dir(p) != m.Dir {
			continue
		}
		if ok, _ := path.Match(m.Glob, path.Base(p)); ok {
			return m.Group, true
		}
	}
	return "", false
}

// ini schreibt Abschnitte einer systemd-Datei. Leere Werte werden weggelassen.
type ini struct{ b strings.Builder }

func (w *ini) section(name string) { fmt.Fprintf(&w.b, "\n[%s]\n", name) }
func (w *ini) kv(k, v string) {
	if v != "" {
		fmt.Fprintf(&w.b, "%s=%s\n", k, v)
	}
}
func (w *ini) String() string { return w.b.String() }

func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
