// SPDX-License-Identifier: AGPL-3.0-or-later

package gen

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vanilla-router/internal/config"
)

// go test ./internal/gen -update schreibt die Golden Files neu. Danach den Unterschied mit
// git diff prüfen, bevor er committet wird (NF-04).
var update = flag.Bool("update", false, "Golden Files neu schreiben")

type secrets map[string]string

func (s secrets) Get(ref string) (string, error) {
	if v, ok := s[ref]; ok {
		return v, nil
	}
	return "", errors.New("nicht gesetzt")
}

func base(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.Default(config.DefaultInput{
		WANMAC:  "02:00:00:00:00:01",
		LANMACs: []string{"02:00:00:00:00:02", "02:00:00:00:00:03"},
		Rand:    bytes.NewReader([]byte{0x12, 0x34, 0x56, 0x78, 0x9a}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Fälle für die Golden Files. Nur Dokumentations-Adressen und -MACs.
func cases(t *testing.T) map[string]*config.Config {
	dhcp := base(t)

	pppoe := base(t)
	pppoe.WAN.Type = config.WANPPPoE
	pppoe.WAN.PPPoE = &config.PPPoE{User: "002912345678551234567890" + "0001@t-online.de", PasswordRef: "pppoe", VLAN: 7}
	pppoe.DNS.Upstreams = []string{"9.9.9.9", "2620:fe::fe"}

	full := base(t)
	full.WAN.Type = config.WANStatic
	full.WAN.Static = &config.StaticWAN{Address: "203.0.113.10/29", Gateway: "203.0.113.9", DNS: []string{"198.51.100.53"}}
	full.WAN.IPv6.Mode = "off"
	full.LAN.IPv6.ULA = ""
	full.DHCP.Reservations = []config.Reservation{
		{Name: "drucker", MAC: "02:00:00:00:00:10", IP: "192.168.42.20"},
		{Name: "nas", MAC: "02:00:00:00:00:11", IP: "192.168.42.21"},
	}
	full.DNS.Records = []config.Record{{Name: "kamera", IP: "192.168.42.30"}}
	full.Firewall.PortForwards = []config.PortForward{
		{Proto: "tcp", ExtPort: 8443, IntIP: "192.168.42.21", IntPort: 443, Comment: "NAS"},
		{Proto: "both", ExtPort: 27000, ExtEnd: 27010, IntIP: "192.168.42.20", IntPort: 27000, Comment: "Spiel"},
		{Proto: "udp", ExtPort: 51820, IntIP: "192.168.42.21", IntPort: 51820, Disabled: true},
	}
	full.Firewall.Blocks = []config.Block{
		{MAC: "02:00:00:00:00:21", Name: "Tablet", Mode: "schedule", From: "21:30", To: "07:00", Days: []int{0, 1, 2, 3, 4}},
		{MAC: "02:00:00:00:00:20", Name: "Konsole", Mode: "always"},
	}
	full.Services.SSH = config.SSH{Enabled: true, AuthorizedKeys: []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBeispielSchluesselNurFuerTests admin@pc"}}
	full.Updates.RebootWindow = ""

	pin := base(t)
	pin.Firewall.Pinholes = []config.Pinhole{{InterfaceID: "::1a2b:3c4d:5e6f:7890", Proto: "tcp", Port: 443, Comment: "Webserver"}}
	pin.LAN.IPv6.FromWANPrefix = true

	return map[string]*config.Config{"dhcp": dhcp, "pppoe-vlan7": pppoe, "static-voll": full, "ipv6-freigabe": pin}
}

func TestGolden(t *testing.T) {
	sec := secrets{"pppoe": "geheim-123"}
	for name, c := range cases(t) {
		t.Run(name, func(t *testing.T) {
			files, err := Render(c, sec)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join("testdata", name)
			if *update {
				os.RemoveAll(dir)
				cj, _ := c.Marshal()
				write(t, filepath.Join(dir, "config.json"), cj)
				for _, f := range files {
					write(t, filepath.Join(dir, "out", filepath.FromSlash(f.Path)), f.Content)
				}
				return
			}
			want := map[string]bool{}
			filepath.WalkDir(filepath.Join(dir, "out"), func(p string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					rel, _ := filepath.Rel(filepath.Join(dir, "out"), p)
					want["/"+filepath.ToSlash(rel)] = true
				}
				return nil
			})
			for _, f := range files {
				exp, err := os.ReadFile(filepath.Join(dir, "out", filepath.FromSlash(f.Path)))
				if err != nil {
					t.Errorf("%s: neue Datei ohne Golden File (go test -update)", f.Path)
					continue
				}
				delete(want, f.Path)
				if !bytes.Equal(exp, f.Content) {
					t.Errorf("%s weicht ab:\n--- erwartet\n%s\n--- erzeugt\n%s", f.Path, exp, f.Content)
				}
			}
			for p := range want {
				t.Errorf("%s wird nicht mehr erzeugt", p)
			}
		})
	}
}

func write(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEigenschaften(t *testing.T) {
	sec := secrets{"pppoe": "geheim-123"}
	for name, c := range cases(t) {
		files, err := Render(c, sec)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, f := range files {
			if !bytes.Contains(f.Content, []byte("Von vanilla-router erzeugt")) {
				t.Errorf("%s %s: Kopfzeile fehlt", name, f.Path)
			}
			if strings.Contains(f.Path, "secrets") && f.Mode != 0o600 {
				t.Errorf("%s %s: Rechte %o, Geheimnisse brauchen 0600", name, f.Path, f.Mode)
			}
			if bytes.Contains(f.Content, []byte("geheim-123")) && f.Mode != 0o600 {
				t.Errorf("%s %s: Passwort in lesbarer Datei", name, f.Path)
			}
			if bytes.Contains(f.Content, []byte("flush ruleset")) {
				t.Errorf("%s %s: flush ruleset ist verboten (D-03)", name, f.Path)
			}
			if f.Group == "" {
				t.Errorf("%s %s: keine Gruppe", name, f.Path)
			}
			if g, ok := IsManaged(f.Path); ok && g != f.Group {
				t.Errorf("%s %s: Gruppe %s, verwaltet als %s", name, f.Path, f.Group, g)
			}
		}
	}
}

func TestPPPoEPasswortPruefung(t *testing.T) {
	c := cases(t)["pppoe-vlan7"]
	for _, pw := range []string{"", `a"b`, `a\b`, "a\nb"} {
		if _, err := Render(c, secrets{"pppoe": pw}); err == nil {
			t.Errorf("Passwort %q angenommen", pw)
		}
	}
	if _, err := Render(c, secrets{}); err == nil {
		t.Error("fehlendes Passwort nicht bemerkt")
	}
	if _, err := Render(c, nil); err == nil {
		t.Error("ohne Secrets nicht bemerkt")
	}
}

func TestUngueltigeKonfigWirdAbgelehnt(t *testing.T) {
	c := base(t)
	c.LAN.IPv4 = "kaputt"
	if _, err := Render(c, nil); err == nil {
		t.Error("ungültige Konfiguration erzeugt")
	}
}

func TestIsManaged(t *testing.T) {
	for p, want := range map[string]bool{
		"/etc/systemd/network/30-vr-wan0.network":  true,
		"/etc/systemd/network/20-vr-wan0.7.netdev": true,
		"/etc/systemd/network/10-vr-lanp3.link":    true,
		"/etc/systemd/network/50-eigene.network":   false,
		"/etc/systemd/network/sub/30-vr-x.network": false,
		"/etc/vanilla-router/nft/vr.nft":           true,
		"/etc/vanilla-router/nft/eigene.conf":      false,
		"/etc/ppp/peers/vr-wan":                    true,
		"/etc/ppp/peers/provider":                  false,
	} {
		if _, got := IsManaged(p); got != want {
			t.Errorf("IsManaged(%s) = %v", p, got)
		}
	}
}

func TestNftDays(t *testing.T) {
	if got := nftDays([]int{5, 6}, 1); got != ` meta day { "Sunday", "Saturday" }` {
		t.Errorf("%q", got)
	}
	if got := nftDays(nil, 1); got != "" {
		t.Errorf("%q", got)
	}
}
