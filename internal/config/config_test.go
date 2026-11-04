// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"bytes"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	wanMAC = "02:00:00:00:00:01"
	lanMAC = []string{"02:00:00:00:00:02", "02:00:00:00:00:03"}
)

func testConfig(t *testing.T) *Config {
	t.Helper()
	c, err := Default(DefaultInput{WANMAC: wanMAC, LANMACs: lanMAC, Rand: bytes.NewReader([]byte{1, 2, 3, 4, 5})})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDefaultGueltig(t *testing.T) {
	c := testConfig(t)
	if c.LAN.IPv4 != "192.168.42.1/24" || c.DHCP.PoolStart != "192.168.42.100" || c.DHCP.PoolEnd != "192.168.42.249" {
		t.Errorf("Vorgaben: %+v %+v", c.LAN, c.DHCP)
	}
	if c.LAN.IPv6.ULA != "fd01:203:405::/48" {
		t.Errorf("ULA = %s", c.LAN.IPv6.ULA)
	}
	if c.Services.SSH.Enabled {
		t.Error("SSH muss ab Werk aus sein (F-SEC-04)")
	}
}

func TestChooseLANUeberschneidung(t *testing.T) {
	if got := ChooseLAN(netip.MustParsePrefix("192.168.42.0/24")); got.String() != "192.168.73.1/24" {
		t.Errorf("got %s", got)
	}
	if got := ChooseLAN(netip.MustParsePrefix("192.168.0.0/16")); got.String() != "10.42.0.1/24" {
		t.Errorf("got %s", got)
	}
	if got := ChooseLAN(netip.MustParsePrefix("0.0.0.0/0")); got.String() != "10.217.33.1/24" {
		t.Errorf("got %s", got)
	}
}

func TestNewULAZufaellig(t *testing.T) {
	a, _ := NewULA(nil)
	b, _ := NewULA(nil)
	if a == b || a.Bits() != 48 || a.Addr().As16()[0] != 0xfd {
		t.Errorf("%s %s", a, b)
	}
}

func TestRundreise(t *testing.T) {
	c := testConfig(t)
	path := filepath.Join(t.TempDir(), "etc", "config.json")
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Marshal()
	b, _ := c2.Marshal()
	if !bytes.Equal(a, b) {
		t.Errorf("Rundreise verändert die Datei:\n%s\n%s", a, b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o640 {
		t.Errorf("Rechte %v", fi.Mode().Perm())
	}
}

func TestParseStreng(t *testing.T) {
	for name, in := range map[string]string{
		"unbekanntes Feld": `{"schema":1,"foo":1}`,
		"ohne schema":      `{}`,
		"zu neu":           `{"schema":99}`,
		"Müll danach":      `{"schema":1} {}`,
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s: kein Fehler", name)
		}
	}
}

// expectErr prüft, dass Validate genau an field scheitert.
func expectErr(t *testing.T, name string, mod func(c *Config), field string) {
	t.Helper()
	c := testConfig(t)
	mod(c)
	err := c.Validate()
	var es Errors
	if !errors.As(err, &es) {
		t.Errorf("%s: kein Fehler, erwartet %s", name, field)
		return
	}
	for _, e := range es {
		if e.Field == field {
			return
		}
	}
	t.Errorf("%s: Fehler %v, erwartet Feld %s", name, err, field)
}

func TestValidateFehler(t *testing.T) {
	expectErr(t, "Hostname groß", func(c *Config) { c.System.Hostname = "Router" }, "system.hostname")
	expectErr(t, "Zeitzone mit ..", func(c *Config) { c.System.Timezone = "../etc" }, "system.timezone")
	expectErr(t, "WAN im LAN", func(c *Config) { c.LAN.Members = append(c.LAN.Members, wanMAC) }, "wan.match_mac")
	expectErr(t, "MAC Großbuchstaben", func(c *Config) { c.WAN.MatchMAC = "02:00:00:00:00:0A" }, "wan.match_mac")
	expectErr(t, "Multicast-MAC", func(c *Config) { c.LAN.Members[0] = "01:00:5e:00:00:01" }, "lan.members[0]")
	expectErr(t, "LAN öffentlich", func(c *Config) { c.LAN.IPv4 = "8.8.8.1/24" }, "lan.ipv4")
	expectErr(t, "LAN Netzadresse", func(c *Config) { c.LAN.IPv4 = "192.168.42.0/24" }, "lan.ipv4")
	expectErr(t, "ULA falsch", func(c *Config) { c.LAN.IPv6.ULA = "2001:db8::/48" }, "lan.ipv6.ula")
	expectErr(t, "Pool außerhalb", func(c *Config) { c.DHCP.PoolEnd = "192.168.43.10" }, "dhcp.pool_end")
	expectErr(t, "Pool rückwärts", func(c *Config) { c.DHCP.PoolStart, c.DHCP.PoolEnd = "192.168.42.200", "192.168.42.150" }, "dhcp.pool_end")
	expectErr(t, "Lease kurz", func(c *Config) { c.DHCP.Lease = "30s" }, "dhcp.lease")
	expectErr(t, "PPPoE ohne Daten", func(c *Config) { c.WAN.Type = WANPPPoE }, "wan.pppoe")
	expectErr(t, "PPPoE Anführungszeichen", func(c *Config) {
		c.WAN.Type, c.WAN.PPPoE = WANPPPoE, &PPPoE{User: `a"b`, PasswordRef: "pppoe"}
	}, "wan.pppoe.user")
	expectErr(t, "VLAN zu groß", func(c *Config) {
		c.WAN.Type, c.WAN.PPPoE = WANPPPoE, &PPPoE{User: "u", PasswordRef: "pppoe", VLAN: 4095}
	}, "wan.pppoe.vlan")
	expectErr(t, "Static Gateway fremd", func(c *Config) {
		c.WAN.Type, c.WAN.Static = WANStatic, &StaticWAN{Address: "203.0.113.10/29", Gateway: "198.51.100.1"}
	}, "wan.static.gateway")
	expectErr(t, "Static überschneidet LAN", func(c *Config) {
		c.WAN.Type, c.WAN.Static = WANStatic, &StaticWAN{Address: "192.168.42.10/24", Gateway: "192.168.42.254"}
	}, "wan.static.address")
	expectErr(t, "Prefix 40", func(c *Config) { c.WAN.IPv6.RequestPrefix = 40 }, "wan.ipv6.request_prefix")
	expectErr(t, "Reservierung doppelt", func(c *Config) {
		c.DHCP.Reservations = []Reservation{{"a", "02:00:00:00:00:10", "192.168.42.20"}, {"b", "02:00:00:00:00:11", "192.168.42.20"}}
	}, "dhcp.reservations[1].ip")
	expectErr(t, "Reservierung = Router", func(c *Config) {
		c.DHCP.Reservations = []Reservation{{"a", "02:00:00:00:00:10", "192.168.42.1"}}
	}, "dhcp.reservations[0].ip")
	expectErr(t, "Name = Hostname", func(c *Config) {
		c.DHCP.Reservations = []Reservation{{"router", "02:00:00:00:00:10", "192.168.42.20"}}
	}, "dhcp.reservations[0].name")
	expectErr(t, "DNS-Name doppelt", func(c *Config) {
		c.DHCP.Reservations = []Reservation{{"nas", "02:00:00:00:00:10", "192.168.42.20"}}
		c.DNS.Records = []Record{{"nas", "192.168.42.21"}}
	}, "dns.records[0].name")
	expectErr(t, "Upstream kein IP", func(c *Config) { c.DNS.Upstreams = []string{"dns.example.org"} }, "dns.upstreams[0]")
	expectErr(t, "Portweiterleitung überlappt", func(c *Config) {
		c.Firewall.PortForwards = []PortForward{
			{Proto: "both", ExtPort: 1000, ExtEnd: 1010, IntIP: "192.168.42.20", IntPort: 1000},
			{Proto: "udp", ExtPort: 1005, IntIP: "192.168.42.21", IntPort: 22},
		}
	}, "firewall.port_forwards[1].ext_port")
	expectErr(t, "Portweiterleitung auf Router", func(c *Config) {
		c.Firewall.PortForwards = []PortForward{{Proto: "tcp", ExtPort: 80, IntIP: "192.168.42.1", IntPort: 80}}
	}, "firewall.port_forwards[0].int_ip")
	expectErr(t, "Kommentar mit Anführungszeichen", func(c *Config) {
		c.Firewall.PortForwards = []PortForward{{Proto: "tcp", ExtPort: 80, IntIP: "192.168.42.2", IntPort: 80, Comment: `x" drop`}}
	}, "firewall.port_forwards[0].comment")
	expectErr(t, "Pinhole mit Präfix", func(c *Config) {
		c.Firewall.Pinholes = []Pinhole{{InterfaceID: "2001:db8::1", Proto: "tcp", Port: 443}}
	}, "firewall.ipv6_pinholes[0].interface_id")
	expectErr(t, "Sperre gleiche Zeit", func(c *Config) {
		c.Firewall.Blocks = []Block{{MAC: "02:00:00:00:00:10", Mode: "schedule", From: "20:00", To: "20:00"}}
	}, "firewall.blocks[0]")
	expectErr(t, "SSH ohne Schlüssel", func(c *Config) { c.Services.SSH.Enabled = true }, "services.ssh.authorized_keys")
	expectErr(t, "SSH-Schlüssel mehrzeilig", func(c *Config) {
		c.Services.SSH.AuthorizedKeys = []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA x\nssh-rsa AAAA"}
	}, "services.ssh.authorized_keys[0]")
}

func TestValidateGutfaelle(t *testing.T) {
	c := testConfig(t)
	c.WAN.Type, c.WAN.PPPoE = WANPPPoE, &PPPoE{User: "000123456789551234567890#0001@t-online.de", PasswordRef: "pppoe", VLAN: 7}
	c.DHCP.Reservations = []Reservation{{"drucker", "02:00:00:00:00:10", "192.168.42.20"}}
	c.DNS.Records = []Record{{"nas", "192.168.42.21"}, {"nas6", "fd01:203:405:1::21"}}
	c.DNS.Upstreams = []string{"9.9.9.9", "2620:fe::fe"}
	c.Firewall.PortForwards = []PortForward{{Proto: "both", ExtPort: 51820, IntIP: "192.168.42.20", IntPort: 51820, Comment: "WireGuard (Büro)"}}
	c.Firewall.Pinholes = []Pinhole{{InterfaceID: "::1a2b:3c4d:5e6f:7890", Proto: "tcp", Port: 443}}
	c.Firewall.Blocks = []Block{{MAC: "02:00:00:00:00:10", Mode: "schedule", From: "21:30", To: "07:00", Days: []int{0, 1, 2, 3, 4}}}
	c.Services.SSH = SSH{Enabled: true, AuthorizedKeys: []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyOnly1234567890 admin@pc"}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAlleFehlerAufEinmal(t *testing.T) {
	c := testConfig(t)
	c.System.Hostname = ""
	c.DHCP.Lease = "x"
	err := c.Validate()
	if es, ok := err.(Errors); !ok || len(es) != 2 {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(err.Error(), "system.hostname") {
		t.Error(err)
	}
}

func TestTelekomUser(t *testing.T) {
	u, err := TelekomUser("002912345678", "551234567890", "")
	if err != nil || u != "002912345678551234567890"+"0001@t-online.de" {
		t.Errorf("%q %v", u, err)
	}
	u, err = TelekomUser("1234567", "551234567890", "0002")
	if err != nil || u != "1234567551234567890#0002@t-online.de" {
		t.Errorf("%q %v", u, err)
	}
	if _, err := TelekomUser("12a", "1", "0001"); err == nil {
		t.Error("Buchstaben angenommen")
	}
}
