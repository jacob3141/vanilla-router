// SPDX-License-Identifier: AGPL-3.0-or-later

package sys

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// Lease ist eine DHCP-Vergabe von dnsmasq. Die Form entspricht dem Prototyp (dort isc-dhcp).
type Lease struct {
	IP       string    `json:"ip"`
	MAC      string    `json:"mac"`
	Hostname string    `json:"hostname"`
	State    string    `json:"state"` // active oder expired
	Starts   time.Time `json:"starts"`
	Ends     time.Time `json:"ends"` // Null bei unendlicher Lease
}

// LeasesFile ist die Lease-Datei von vr-dnsmasq (gen.DNSmasq: dhcp-leasefile).
var LeasesFile = "/var/lib/vanilla-router/dnsmasq.leases"

// ReadLeases liest die aktuelle Lease-Datei.
func ReadLeases() ([]Lease, error) {
	f, err := os.Open(LeasesFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseLeases(f, time.Now())
}

// parseLeases liest das dnsmasq-Format: „<ablauf-unix> <mac> <ip> <name|*> <client-id|*>“.
// IPv6-Leases (Zeile „duid …“ und Zeilen danach) gibt es hier nicht, weil DHCPv6 aus ist (Q-04).
func parseLeases(r io.Reader, now time.Time) ([]Lease, error) {
	var out []Lease
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[0] == "duid" {
			continue
		}
		exp, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil || !strings.Contains(f[1], ":") || strings.Contains(f[2], ":") {
			continue // keine IPv4-Lease
		}
		l := Lease{MAC: strings.ToLower(f[1]), IP: f[2], State: "active"}
		if f[3] != "*" {
			l.Hostname = f[3]
		}
		if exp > 0 {
			l.Ends = time.Unix(exp, 0).UTC()
			if l.Ends.Before(now) {
				l.State = "expired"
			}
		}
		out = append(out, l)
	}
	return out, sc.Err()
}
