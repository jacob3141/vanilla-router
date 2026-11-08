// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"strings"
	"testing"
)

const testKey = "ed25519:AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

func testWireless() *Wireless {
	return &Wireless{
		Country: "DE",
		Segments: []Segment{
			{ID: "home", Name: "Home network", VLAN: 1, Access: AccessLAN},
			{ID: "guest", Name: "Guests", VLAN: 20, Access: AccessInternet, IPv4: "192.168.20.1/24"},
			{ID: "older", Name: "Older devices", VLAN: 30, Access: AccessInternet, IPv4: "192.168.30.1/24", ReachableFrom: []string{"home"}},
		},
		Networks: []WNetwork{
			{ID: "home", SSID: "Home", Segment: "home", PasswordRef: "wnet-1", Security: SecurityWPA3, Bands: []string{"5", "6"}, Enabled: true,
				Roaming: &Roaming{MobilityDomain: "a1b2", KeyRef: "ft-1"}},
			{ID: "older", SSID: "Home Older Devices", Segment: "older", PasswordRef: "wnet-2", Security: SecurityWPA2WPA3, Bands: []string{"2.4"}},
		},
		AccessPoints: []AccessPoint{
			{ID: "hall", Name: "Hall", PublicKey: testKey, MAC: "02:00:00:00:00:10", Power: PowerMedium,
				Radios: []APRadio{{ID: "pci-0000-03-00-0", Band: "6"}, {ID: "pci-0000-04-00-0", Band: "5", Channel: 100, Width: 80}}},
		},
	}
}

func withWireless(mod func(w *Wireless)) func(c *Config) {
	return func(c *Config) {
		c.Wireless = testWireless()
		mod(c.Wireless)
	}
}

func TestWirelessValid(t *testing.T) {
	c := testConfig(t)
	withWireless(func(*Wireless) {})(c)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	// Round trip through the strict parser
	b, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(b); err != nil {
		t.Fatal(err)
	}
}

func TestWirelessErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		mod   func(w *Wireless)
		field string
	}{
		"no country":           {func(w *Wireless) { w.Country = "" }, "wireless.country"},
		"no home segment":      {func(w *Wireless) { w.Segments = w.Segments[1:] }, "wireless.segments"},
		"home tagged":          {func(w *Wireless) { w.Segments[0].VLAN = 5 }, "wireless.segments[0].access"},
		"home with range":      {func(w *Wireless) { w.Segments[0].IPv4 = "192.168.50.1/24" }, "wireless.segments[0].ipv4"},
		"second lan segment":   {func(w *Wireless) { w.Segments[1].Access = AccessLAN }, "wireless.segments[1].access"},
		"vlan twice":           {func(w *Wireless) { w.Segments[2].VLAN = 20 }, "wireless.segments[2].vlan"},
		"vlan 4095":            {func(w *Wireless) { w.Segments[2].VLAN = 4095 }, "wireless.segments[2].vlan"},
		"segment id twice":     {func(w *Wireless) { w.Segments[2].ID = "guest" }, "wireless.segments[2].id"},
		"segment id injection": {func(w *Wireless) { w.Segments[1].ID = "a b" }, "wireless.segments[1].id"},
		"no segment name":      {func(w *Wireless) { w.Segments[1].Name = "" }, "wireless.segments[1].name"},
		"overlaps lan":         {func(w *Wireless) { w.Segments[1].IPv4 = "192.168.42.5/24" }, "wireless.segments[1].ipv4"},
		"overlaps segment":     {func(w *Wireless) { w.Segments[2].IPv4 = "192.168.20.129/25" }, "wireless.segments[2].ipv4"},
		"public range":         {func(w *Wireless) { w.Segments[1].IPv4 = "203.0.113.1/24" }, "wireless.segments[1].ipv4"},
		"network address":      {func(w *Wireless) { w.Segments[1].IPv4 = "192.168.20.0/24" }, "wireless.segments[1].ipv4"},
		"tiny range":           {func(w *Wireless) { w.Segments[1].IPv4 = "192.168.20.1/30" }, "wireless.segments[1].ipv4"},
		"unknown reachable":    {func(w *Wireless) { w.Segments[2].ReachableFrom = []string{"lab"} }, "wireless.segments[2].reachable_from"},
		"reachable self":       {func(w *Wireless) { w.Segments[2].ReachableFrom = []string{"older"} }, "wireless.segments[2].reachable_from"},
		"ssid twice":           {func(w *Wireless) { w.Networks[1].SSID = "Home" }, "wireless.networks[1].ssid"},
		"ssid injection":       {func(w *Wireless) { w.Networks[0].SSID = "a\nwpa=0" }, "wireless.networks[0].ssid"},
		"unknown segment":      {func(w *Wireless) { w.Networks[0].Segment = "lab" }, "wireless.networks[0].segment"},
		"no bands":             {func(w *Wireless) { w.Networks[0].Bands = nil }, "wireless.networks[0].bands"},
		"band twice":           {func(w *Wireless) { w.Networks[0].Bands = []string{"5", "5"} }, "wireless.networks[0].bands"},
		"60 GHz":               {func(w *Wireless) { w.Networks[0].Bands = []string{"60"} }, "wireless.networks[0].bands"},
		"wpa2 at home": {func(w *Wireless) {
			w.Networks[0].Security, w.Networks[0].Roaming, w.Networks[0].Bands = SecurityWPA2WPA3, nil, []string{"5"}
		}, "wireless.networks[0].security"},
		"wpa2 on 6 GHz":     {func(w *Wireless) { w.Networks[1].Bands = []string{"6"} }, "wireless.networks[1].bands"},
		"wpa2 with roaming": {func(w *Wireless) { w.Networks[1].Roaming = &Roaming{MobilityDomain: "c3d4", KeyRef: "ft-2"} }, "wireless.networks[1].roaming"},
		"wpa3 no roaming":   {func(w *Wireless) { w.Networks[0].Roaming = nil }, "wireless.networks[0].roaming"},
		"bad mobility":      {func(w *Wireless) { w.Networks[0].Roaming.MobilityDomain = "A1B2" }, "wireless.networks[0].roaming.mobility_domain"},
		"zero mobility":     {func(w *Wireless) { w.Networks[0].Roaming.MobilityDomain = "0000" }, "wireless.networks[0].roaming.mobility_domain"},
		"bad key ref":       {func(w *Wireless) { w.Networks[0].Roaming.KeyRef = "../x" }, "wireless.networks[0].roaming.key_ref"},
		"unknown security":  {func(w *Wireless) { w.Networks[1].Security = "wep" }, "wireless.networks[1].security"},
		"ap id local":       {func(w *Wireless) { w.AccessPoints[0].ID = "local" }, "wireless.access_points[0].id"},
		"ap no name":        {func(w *Wireless) { w.AccessPoints[0].Name = "" }, "wireless.access_points[0].name"},
		"ap long name":      {func(w *Wireless) { w.AccessPoints[0].Name = strings.Repeat("x", 33) }, "wireless.access_points[0].name"},
		"ap bad key":        {func(w *Wireless) { w.AccessPoints[0].PublicKey = "ed25519:AAEC" }, "wireless.access_points[0].public_key"},
		"ap rsa key":        {func(w *Wireless) { w.AccessPoints[0].PublicKey = "rsa:AAEC" }, "wireless.access_points[0].public_key"},
		"ap upper mac":      {func(w *Wireless) { w.AccessPoints[0].MAC = "02:00:00:00:00:AA" }, "wireless.access_points[0].mac"},
		"ap power":          {func(w *Wireless) { w.AccessPoints[0].Power = "max" }, "wireless.access_points[0].power"},
		"radio band":        {func(w *Wireless) { w.AccessPoints[0].Radios[0].Band = "7" }, "wireless.access_points[0].radios[0].band"},
		"radio twice":       {func(w *Wireless) { w.AccessPoints[0].Radios[1].ID = "pci-0000-03-00-0" }, "wireless.access_points[0].radios[1].id"},
		"pin channel only":  {func(w *Wireless) { w.AccessPoints[0].Radios[1].Width = 0 }, "wireless.access_points[0].radios[1].channel"},
		"pin wrong block":   {func(w *Wireless) { w.AccessPoints[0].Radios[1].Channel, w.AccessPoints[0].Radios[1].Width = 165, 40 }, "wireless.access_points[0].radios[1].channel"},
		"pin 2.4 wide":      {func(w *Wireless) { w.AccessPoints[0].Radios[1].Band = "2.4"; w.AccessPoints[0].Radios[1].Channel = 6 }, "wireless.access_points[0].radios[1].channel"},
		"ap twice": {func(w *Wireless) {
			w.AccessPoints = append(w.AccessPoints, w.AccessPoints[0])
			w.AccessPoints[1].ID, w.AccessPoints[1].Name, w.AccessPoints[1].MAC = "attic", "Attic", "02:00:00:00:00:11"
		}, "wireless.access_points[1].public_key"},
	} {
		expectErr(t, name, withWireless(tc.mod), tc.field)
	}
}

func TestWirelessSSIDOfLocalWiFi(t *testing.T) {
	expectErr(t, "local wifi", func(c *Config) {
		withWiFi(func(w *WiFi) { w.SSID = "Home" })(c)
		withWireless(func(*Wireless) {})(c)
	}, "wireless.networks[0].ssid")
}

func TestWirelessTooManyOnBand(t *testing.T) {
	expectErr(t, "five on 5 GHz", withWireless(func(w *Wireless) {
		for i := range 4 {
			n := w.Networks[0]
			n.ID, n.SSID = "n"+string(rune('a'+i)), "Net "+string(rune('A'+i))
			n.Roaming = &Roaming{MobilityDomain: "b00" + string(rune('1'+i)), KeyRef: "ft-x"}
			w.Networks = append(w.Networks, n)
		}
	}), "wireless.networks")
}

func TestChannelBlock(t *testing.T) {
	for _, tc := range []struct {
		band          string
		ch, width     int
		start, center int
		ok            bool
	}{
		{"5", 36, 80, 36, 42, true},
		{"5", 48, 80, 36, 42, true},
		{"5", 104, 160, 100, 114, true},
		{"5", 149, 80, 149, 155, true},
		{"5", 165, 40, 0, 0, false},
		{"5", 144, 160, 0, 0, false},
		{"5", 165, 20, 165, 165, true},
		{"6", 1, 80, 1, 7, true},
		{"6", 37, 160, 33, 47, true},
		{"6", 93, 80, 81, 87, true},
		{"6", 2, 20, 0, 0, false},
		{"2.4", 6, 20, 6, 6, true},
		{"2.4", 6, 40, 0, 0, false},
	} {
		s, c, ok := ChannelBlock(tc.band, tc.ch, tc.width)
		if s != tc.start || c != tc.center || ok != tc.ok {
			t.Errorf("%s/%d/%d: %d %d %v", tc.band, tc.ch, tc.width, s, c, ok)
		}
	}
	if !DFS("5", 100) || DFS("5", 36) || DFS("6", 100) {
		t.Error("DFS")
	}
}
