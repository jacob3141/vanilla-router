// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"strings"
	"testing"
)

func TestMoveLAN(t *testing.T) {
	c := testConfig(t)
	c.DHCP.Reservations = []Reservation{res("printer", "02:00:00:00:00:10", "192.168.42.20")}
	c.DNS.Records = []Record{{Name: "nas", IP: "192.168.42.30"}, {Name: "external", IP: "198.51.100.7"}}
	c.Firewall.PortForwards = []PortForward{{Proto: "tcp", ExtPort: 443, IntIP: "192.168.42.30", IntPort: 443}}
	if err := c.MoveLAN("10.20.30.1/24"); err != nil {
		t.Fatal(err)
	}
	if c.LAN.IPv4 != "10.20.30.1/24" || c.DHCP.PoolStart != "10.20.30.100" || c.DHCP.PoolEnd != "10.20.30.249" {
		t.Errorf("LAN/Pool: %s %s–%s", c.LAN.IPv4, c.DHCP.PoolStart, c.DHCP.PoolEnd)
	}
	if c.DHCP.Reservations[0].IP != "10.20.30.20" || c.DNS.Records[0].IP != "10.20.30.30" || c.Firewall.PortForwards[0].IntIP != "10.20.30.30" {
		t.Errorf("moved: %+v %+v %+v", c.DHCP.Reservations, c.DNS.Records, c.Firewall.PortForwards)
	}
	if c.DNS.Records[1].IP != "198.51.100.7" {
		t.Error("an address outside the home network must not change")
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMoveLANSmallerNetwork(t *testing.T) {
	c := testConfig(t)
	before := c.DHCP.PoolEnd
	err := c.MoveLAN("192.168.50.1/25")
	if err == nil || !strings.Contains(err.Error(), "192.168.42.249") {
		t.Fatalf("error expected, got %v", err)
	}
	if c.LAN.IPv4 != "192.168.42.1/24" || c.DHCP.PoolEnd != before {
		t.Error("nothing may change on an error")
	}
}

func TestMoveLANCopiesLists(t *testing.T) {
	c := testConfig(t)
	c.DHCP.Reservations = []Reservation{res("printer", "02:00:00:00:00:10", "192.168.42.20")}
	old := c.DHCP.Reservations
	if err := c.MoveLAN("192.168.50.1/24"); err != nil {
		t.Fatal(err)
	}
	if old[0].IP != "192.168.42.20" {
		t.Error("MoveLAN changed the old list (rollback needs it unchanged)")
	}
}

func TestDetectedValidation(t *testing.T) {
	expectErr(t, "detection unknown", func(c *Config) { c.WAN.Detected = &Detected{Method: "dsl"} }, "wan.detected.method")
	expectErr(t, "VLAN without PPPoE", func(c *Config) { c.WAN.Detected = &Detected{Method: "dhcp", VLAN: 7} }, "wan.detected.vlan")
	c := testConfig(t)
	c.WAN.Detected = &Detected{Method: WANPPPoE, VLAN: 7}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if !c.WAN.NeedsSetup() {
		t.Error("PPPoE detected, but no setup required")
	}
	c.WAN.Detected.Method, c.WAN.Detected.VLAN = WANDHCP, 0
	if c.WAN.NeedsSetup() {
		t.Error("DHCP needs no setup")
	}
}
