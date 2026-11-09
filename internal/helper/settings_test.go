// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vanilla-router/internal/config"
)

// newDryServer creates a helper in dry-run mode with a default configuration under a test folder.
func newDryServer(t *testing.T, mod func(*config.Config)) *Server {
	t.Helper()
	root := t.TempDir()
	c, err := config.Default(config.DefaultInput{WANMAC: "02:00:00:00:00:01", LANMACs: []string{"02:00:00:00:00:02"},
		Rand: bytes.NewReader([]byte{1, 2, 3, 4, 5})})
	if err != nil {
		t.Fatal(err)
	}
	if mod != nil {
		mod(c)
	}
	if err := c.Save(filepath.Join(root, config.Path)); err != nil {
		t.Fatal(err)
	}
	return NewServer(Config{DryRun: true, Root: root, Log: slog.New(slog.DiscardHandler)})
}

func call(t *testing.T, s *Server, action string, params any) (map[string]any, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := s.handlers[action](context.Background(), raw)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	b, _ := json.Marshal(out)
	json.Unmarshal(b, &m)
	return m, nil
}

func loadCfg(t *testing.T, s *Server) *config.Config {
	t.Helper()
	c, err := s.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSettingsTelekomWithConfirmation(t *testing.T) {
	s := newDryServer(t, func(c *config.Config) { c.WAN.Detected = &config.Detected{Method: config.WANPPPoE, VLAN: 7} })
	info, _ := call(t, s, "settings.get", nil)
	if info["needs_setup"] != true {
		t.Fatal("detected PPPoE must require setup")
	}
	if _, err := call(t, s, "settings.wan.set", map[string]any{"type": "pppoe", "ipv6": true,
		"telekom": map[string]string{"line_id": "001234567890", "access_number": "551234567890", "co_user": "0001"}}); err == nil ||
		!strings.Contains(err.Error(), "password") {
		t.Fatalf("must fail without a password: %v", err)
	}
	res, err := call(t, s, "settings.wan.set", map[string]any{"type": "pppoe", "ipv6": true, "pppoe_password": "secret",
		"telekom": map[string]string{"line_id": "001234567890", "access_number": "551234567890", "co_user": "0001"}})
	if err != nil {
		t.Fatal(err)
	}
	if res["pending"] == nil {
		t.Error("WAN change must be confirmed")
	}
	c := loadCfg(t, s)
	if c.WAN.Type != "pppoe" || c.WAN.PPPoE.User != "0012345678905512345678900001@t-online.de" || c.WAN.PPPoE.VLAN != 7 || c.WAN.Detected != nil {
		t.Fatalf("WAN: %+v %+v", c.WAN, c.WAN.PPPoE)
	}
	fi, err := os.Stat(filepath.Join(s.secrets().Dir, c.WAN.PPPoE.PasswordRef))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("password file: %v %v", fi, err)
	}
	info, _ = call(t, s, "settings.get", nil)
	if info["needs_setup"] != false || info["wan"].(map[string]any)["has_password"] != true {
		t.Errorf("after saving: %v", info)
	}
	if _, err := call(t, s, "settings.confirm", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, s, "settings.confirm", nil); err == nil {
		t.Error("second confirmation without a waiting change must fail")
	}
	// Without a new password the old one stays
	if _, err := call(t, s, "settings.wan.set", map[string]any{"type": "pppoe", "pppoe_user": "customer@example.org", "pppoe_vlan": 0}); err != nil {
		t.Fatal(err)
	}
	if c := loadCfg(t, s); c.WAN.PPPoE.User != "customer@example.org" || c.WAN.PPPoE.VLAN != 0 {
		t.Errorf("second change: %+v", c.WAN.PPPoE)
	}
}

func TestSettingsLANMoveAndRollback(t *testing.T) {
	s := newDryServer(t, func(c *config.Config) {
		c.DHCP.Reservations = []config.Reservation{{Name: "printer", MAC: "02:00:00:00:00:10", IP: "192.168.42.20"}}
	})
	res, err := call(t, s, "settings.lan.set", map[string]any{"address": "10.20.30.1", "prefix": 24})
	if err != nil {
		t.Fatal(err)
	}
	if res["new_url"] != "https://10.20.30.1/" || res["restart"] != true || res["pending"] == nil {
		t.Errorf("response: %v", res)
	}
	if c := loadCfg(t, s); c.LAN.IPv4 != "10.20.30.1/24" || c.DHCP.Reservations[0].IP != "10.20.30.20" {
		t.Errorf("moved: %s %v", c.LAN.IPv4, c.DHCP.Reservations)
	}
	// As long as the change is waiting, no other gets through
	if _, err := call(t, s, "settings.dhcp.set", map[string]any{"pool_start": "10.20.30.50", "pool_end": "10.20.30.60", "lease": "1h"}); err == nil ||
		!strings.Contains(err.Error(), "confirmation") {
		t.Errorf("change during a pending confirmation: %v", err)
	}
	if _, err := call(t, s, "settings.revert", nil); err != nil {
		t.Fatal(err)
	}
	if c := loadCfg(t, s); c.LAN.IPv4 != "192.168.42.1/24" || c.DHCP.Reservations[0].IP != "192.168.42.20" {
		t.Errorf("rolled back: %s %v", c.LAN.IPv4, c.DHCP.Reservations)
	}
	if _, err := call(t, s, "settings.lan.set", map[string]any{"address": "8.8.8.1", "prefix": 24}); err == nil {
		t.Error("public network accepted")
	}
}

func TestSettingsSystemAndDHCP(t *testing.T) {
	s := newDryServer(t, nil)
	if _, err := call(t, s, "settings.system.set", map[string]any{"hostname": "gateway", "timezone": "../../etc/passwd"}); err == nil {
		t.Error("path accepted as time zone")
	}
	if _, err := call(t, s, "settings.system.set", map[string]any{"hostname": "Gate Way", "timezone": "UTC"}); err == nil {
		t.Error("invalid name accepted")
	}
	res, err := call(t, s, "settings.system.set", map[string]any{"hostname": "Gateway", "timezone": "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if res["pending"] != nil || res["new_url"] != "https://gateway.home.arpa/" {
		t.Errorf("response: %v", res)
	}
	if c := loadCfg(t, s); c.System.Hostname != "gateway" || c.System.Timezone != "UTC" {
		t.Errorf("System: %+v", c.System)
	}
	if _, err := call(t, s, "settings.dhcp.set", map[string]any{"pool_start": "192.168.42.50", "pool_end": "192.168.42.40", "lease": "1h"}); err == nil {
		t.Error("end before start accepted")
	}
	if _, err := call(t, s, "settings.dhcp.set", map[string]any{"pool_start": "192.168.42.50", "pool_end": "192.168.42.90", "lease": "1h"}); err != nil {
		t.Fatal(err)
	}
}

func TestInterfacesRoles(t *testing.T) {
	s := newDryServer(t, nil)
	info, err := call(t, s, "interfaces.get", nil)
	if err != nil {
		t.Fatal(err)
	}
	ports := info["ports"].([]any)
	if len(ports) != 3 {
		t.Fatalf("%v", ports)
	}
	first := ports[0].(map[string]any)
	if first["role"] != "wan" || first["label"] != "WAN" || ports[2].(map[string]any)["role"] != "unused" {
		t.Errorf("%v", ports)
	}
	set := func(wan, lan, free string) error {
		_, err := call(t, s, "interfaces.set", map[string]any{"ports": []map[string]string{{"mac": wan, "role": "wan"}, {"mac": lan, "role": "lan"}, {"mac": free, "role": "unused"}}})
		return err
	}
	const a, b, c = "02:00:00:00:00:01", "02:00:00:00:00:02", "02:00:00:00:00:0a"
	if err := set(a, a, c); err == nil {
		// the same port twice: the last role would win without the check on the whole configuration
		if got := loadCfg(t, s); got.WAN.MatchMAC == got.LAN.Members[0] {
			t.Fatal("one port with two roles accepted")
		}
	}
	for _, bad := range []map[string]any{
		{"ports": []map[string]string{{"mac": a, "role": "lan"}, {"mac": b, "role": "lan"}}},
		{"ports": []map[string]string{{"mac": a, "role": "wan"}, {"mac": b, "role": "unused"}}},
		{"ports": []map[string]string{{"mac": a, "role": "wan"}, {"mac": b, "role": "wan"}}},
		{"ports": []map[string]string{{"mac": a, "role": "wan"}, {"mac": b, "role": "router"}}},
		{"ports": []map[string]string{{"mac": a, "role": "wan"}, {"mac": "02:00:00:00:00:99", "role": "lan"}}},
	} {
		if _, err := call(t, s, "interfaces.set", bad); err == nil {
			t.Errorf("accepted: %v", bad)
		}
	}
	if err := set(c, a, b); err != nil {
		t.Fatal(err)
	}
	got := loadCfg(t, s)
	if got.WAN.MatchMAC != c || len(got.LAN.Members) != 1 || got.LAN.Members[0] != a {
		t.Errorf("%s %v", got.WAN.MatchMAC, got.LAN.Members)
	}
	if _, err := call(t, s, "interfaces.identify", map[string]string{"mac": "02:00:00:00:00:99"}); err == nil {
		t.Error("identify of an unknown port accepted")
	}
}

func TestLANOrderKeepsPorts(t *testing.T) {
	const a, b, c, d = "02:00:00:00:00:01", "02:00:00:00:00:02", "02:00:00:00:00:03", "02:00:00:00:00:04"
	for _, x := range []struct {
		old  []string
		lan  []string
		want string
	}{
		{[]string{b, c}, []string{a, c}, a + " " + c}, // the old internet port takes the place of LAN 1, LAN 2 stays
		{[]string{b, c}, []string{b, c, d}, b + " " + c + " " + d},
		{[]string{b, c}, []string{c}, c},
	} {
		lan := map[string]bool{}
		for _, m := range x.lan {
			lan[m] = true
		}
		if got := strings.Join(lanOrder(x.old, lan), " "); got != x.want {
			t.Errorf("%v %v: %s", x.old, x.lan, got)
		}
	}
}

func TestDockerRunArgs(t *testing.T) {
	ok := dockerCreate{Name: "media", Image: "jellyfin/jellyfin:10", Network: "lan", IP: "192.168.42.60", Restart: "unless-stopped",
		Mounts: []string{"data:/data", "/srv/movies:/movies:ro"}, Env: []string{"TZ=Europe/Berlin", "A=b=c d"}}
	args, mounts, err := ok.runArgs()
	if err != nil {
		t.Fatal(err)
	}
	want := "run -d --name media --restart unless-stopped --network lan --ip 192.168.42.60 --mount type=volume,src=data,dst=/data --mount type=bind,src=/srv/movies,dst=/movies,readonly -e TZ=Europe/Berlin -e A=b=c d jellyfin/jellyfin:10"
	if got := strings.Join(args, " "); got != want {
		t.Errorf("%s", got)
	}
	if len(mounts) != 2 || !mounts[0].volume || mounts[1].volume {
		t.Errorf("%+v", mounts)
	}
	for _, f := range []func(*dockerCreate){
		func(p *dockerCreate) { p.Name = "--privileged" },
		func(p *dockerCreate) { p.Name = "a b" },
		func(p *dockerCreate) { p.Image = "--privileged" },
		func(p *dockerCreate) { p.Image = "x y" },
		func(p *dockerCreate) { p.Restart = "maybe" },
		func(p *dockerCreate) { p.Network = "--net=host" },
		func(p *dockerCreate) { p.Env = []string{"novalue"} },
		func(p *dockerCreate) { p.Env = []string{"BAD KEY=x"} },
		func(p *dockerCreate) { p.Env = []string{"A=x\ny"} },
		func(p *dockerCreate) { p.Mounts = []string{"/:/host"} },
		func(p *dockerCreate) { p.Mounts = []string{"/etc:/etc"} },
		func(p *dockerCreate) { p.Mounts = []string{"/var/run/docker.sock:/var/run/docker.sock"} },
		func(p *dockerCreate) { p.Mounts = []string{"/srv/../etc:/x"} },
		func(p *dockerCreate) { p.Mounts = []string{"/srv/a,b:/x"} },
		func(p *dockerCreate) { p.Mounts = []string{"data:relative"} },
		func(p *dockerCreate) { p.Mounts = []string{"data:/x,dst=/y"} },
		func(p *dockerCreate) { p.Mounts = []string{"da,ta:/x"} },
		func(p *dockerCreate) { p.Mounts = []string{"data"} },
	} {
		p := ok
		f(&p)
		if a, _, err := p.runArgs(); err == nil {
			t.Errorf("accepted: %v", a)
		}
	}
}

func TestContainerAddresses(t *testing.T) {
	c, err := config.Default(config.DefaultInput{WANMAC: "02:00:00:00:00:01", LANMACs: []string{"02:00:00:00:00:02"}, Rand: bytes.NewReader([]byte{1, 2, 3, 4, 5})})
	if err != nil {
		t.Fatal(err)
	}
	c.LAN.IPv4, c.DHCP.PoolStart, c.DHCP.PoolEnd = "192.168.42.1/24", "192.168.42.100", "192.168.42.249"
	c.DHCP.Reservations = []config.Reservation{{Name: "printer", MAC: "02:00:00:00:00:44", IP: "192.168.42.20"}}
	lan := DockerNetwork{Name: "lan", Driver: "ipvlan", Subnet: "192.168.42.0/24", Gateway: "192.168.42.1"}
	if err := checkContainerIP(c, lan, "192.168.42.60"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"", "x", "198.51.100.5", "192.168.42.1", "192.168.42.150", "192.168.42.20"} {
		if err := checkContainerIP(c, lan, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	wan := DockerNetwork{Name: "wan", Driver: "ipvlan", Subnet: "203.0.113.0/24", Gateway: "203.0.113.1"}
	if err := checkContainerIP(c, wan, "203.0.113.9"); err != nil {
		t.Error(err)
	}
	host := DockerNetwork{Name: "host", Driver: "host"}
	if checkContainerIP(c, host, "") != nil || checkContainerIP(c, host, "192.168.42.60") == nil {
		t.Error("host network")
	}
}

func TestDockerNetworkCreate(t *testing.T) {
	s := newDryServer(t, nil)
	good := map[string]any{"name": "wan-net", "parent": "wan0", "subnet": "203.0.113.0/24", "gateway": "203.0.113.1", "internal": false}
	if _, err := call(t, s, "docker.network.create", good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []map[string]any{{"name": "--x"}, {"parent": "docker0"}, {"parent": "wan0; rm"}, {"subnet": "203.0.113.5/24"}, {"subnet": "nope"}, {"gateway": "198.51.100.1"}, {"gateway": ""}} {
		p := map[string]any{}
		for k, v := range good {
			p[k] = v
		}
		for k, v := range change {
			p[k] = v
		}
		if _, err := call(t, s, "docker.network.create", p); err == nil {
			t.Errorf("accepted: %v", change)
		}
	}
	if _, err := call(t, s, "docker.network.remove", map[string]string{"name": "host"}); err == nil {
		t.Error("removing host accepted")
	}
}

func TestDockerErr(t *testing.T) {
	for in, want := range map[string]string{
		"docker network rm -- x: exit status 1: Error response from daemon: network x has active endpoints\nexit status 1":                      "network x has active endpoints",
		"docker run -e SECRET=hunter2 img: exit status 125: docker: invalid reference format":                                                   "invalid reference format",
		"docker run x: exit status 125: docker: Error response from daemon: pull access denied\n\nRun 'docker run --help' for more information": "pull access denied",
		"docker pull x: exit status 1: ": "unknown error",
	} {
		if got := dockerErr(errors.New(in)); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
