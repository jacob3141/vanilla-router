// SPDX-License-Identifier: AGPL-3.0-or-later

package apply

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vanilla-router/internal/config"
)

// fakeRunner records calls and simulates the system.
type fakeRunner struct {
	calls  []string
	fail   map[string]bool // prefix of a call → error
	lanIP  string          // what `ip addr show lan0` reports
	active bool            // vr-dnsmasq is running
}

func (f *fakeRunner) Run(env []string, name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, call)
	for p := range f.fail {
		if strings.HasPrefix(call, p) {
			return []byte("simulated error"), errors.New("exit status 1")
		}
	}
	switch {
	case strings.HasPrefix(call, "ip -4 -o addr show dev lan0"):
		return []byte("5: lan0    inet " + f.lanIP + " brd x scope global lan0"), nil
	case strings.HasPrefix(call, "systemctl is-active"):
		if !f.active {
			return nil, errors.New("inactive")
		}
	}
	return nil, nil
}

func (f *fakeRunner) called(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

type memSecrets map[string]string

func (m memSecrets) Get(ref string) (string, error) {
	if v, ok := m[ref]; ok {
		return v, nil
	}
	return "", errors.New("missing")
}

func setup(t *testing.T) (*Applier, *fakeRunner, *config.Config) {
	t.Helper()
	c, err := config.Default(config.DefaultInput{WANMAC: "02:00:00:00:00:01", LANMACs: []string{"02:00:00:00:00:02"},
		Rand: bytes.NewReader([]byte{1, 2, 3, 4, 5})})
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{lanIP: c.LAN.IPv4, active: true, fail: map[string]bool{}}
	clock := time.Date(2026, 11, 4, 20, 0, 0, 0, time.UTC)
	a := &Applier{Root: t.TempDir(), Run: r, HealthTimeout: 2 * time.Second,
		Now:   func() time.Time { clock = clock.Add(time.Second); return clock },
		Sleep: func(time.Duration) {}}
	return a, r, c
}

func read(t *testing.T, a *Applier, p string) string {
	t.Helper()
	b, err := os.ReadFile(a.p(p))
	if err != nil {
		return ""
	}
	return string(b)
}

func TestFirstApplyAndNoChange(t *testing.T) {
	a, r, c := setup(t)
	res, err := a.Apply(c, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) < 10 || res.Generation == "" || !res.RebootNeeded {
		t.Errorf("%+v", res)
	}
	for _, want := range []string{"nft -c -f", "dnsmasq --test", "sysctl", "nft -f", "networkctl reload", "systemctl restart vr-dnsmasq", "systemctl disable --now vr-pppoe", "systemctl disable --now ssh.socket"} {
		if !r.called(want) {
			t.Errorf("%q not called: %v", want, r.calls)
		}
	}
	if !strings.Contains(read(t, a, config.Path), `"schema": 1`) {
		t.Error("config.json not written")
	}
	if fi, _ := os.Stat(a.p("/etc/ppp/chap-secrets")); fi.Mode().Perm() != 0o600 {
		t.Errorf("chap-secrets %v", fi.Mode().Perm())
	}
	r.calls = nil
	res, err = a.Apply(c, nil, Options{})
	if err != nil || len(res.Changes) != 0 || len(r.calls) != 0 {
		t.Errorf("second run: %+v %v %v", res, err, r.calls)
	}
}

func TestOnlyFirewallReloaded(t *testing.T) {
	a, r, c := setup(t)
	if _, err := a.Apply(c, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	r.calls = nil
	c.Firewall.PortForwards = []config.PortForward{{Proto: "tcp", ExtPort: 8080, IntIP: "192.168.42.20", IntPort: 80}}
	res, err := a.Apply(c, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 2 { // vr.nft and config.json
		t.Errorf("%+v", res.Changes)
	}
	if r.called("networkctl") || r.called("systemctl restart vr-dnsmasq") || !r.called("nft -f") {
		t.Errorf("wrong services reloaded: %v", r.calls)
	}
	if !strings.Contains(read(t, a, "/etc/vanilla-router/nft/vr.nft"), "dport 8080 dnat to 192.168.42.20:80") {
		t.Error("rule is missing")
	}
}

func TestCheckFailsNothingChanged(t *testing.T) {
	a, r, c := setup(t)
	if _, err := a.Apply(c, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	before := read(t, a, "/etc/vanilla-router/nft/vr.nft")
	r.fail["nft -c"] = true
	c.Firewall.PortForwards = []config.PortForward{{Proto: "tcp", ExtPort: 8080, IntIP: "192.168.42.20", IntPort: 80}}
	if _, err := a.Apply(c, nil, Options{}); err == nil || !strings.Contains(err.Error(), "nft -c") {
		t.Fatalf("error expected: %v", err)
	}
	if read(t, a, "/etc/vanilla-router/nft/vr.nft") != before {
		t.Error("file changed despite the rejected check")
	}
	if strings.Contains(read(t, a, config.Path), "8080") {
		t.Error("config.json changed despite the error")
	}
}

func TestHealthFailureRollsBack(t *testing.T) {
	a, r, c := setup(t)
	if _, err := a.Apply(c, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	oldNet := read(t, a, "/etc/systemd/network/30-vr-lan0.network")
	oldCfg := read(t, a, config.Path)
	c.LAN.IPv4 = "192.168.42.2/24"
	r.calls = nil // lanIP stays .1/24: the new address "does not arrive"
	_, err := a.Apply(c, nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "home network address") {
		t.Fatalf("error expected: %v", err)
	}
	if read(t, a, "/etc/systemd/network/30-vr-lan0.network") != oldNet || read(t, a, config.Path) != oldCfg {
		t.Error("not rolled back")
	}
	n := 0
	for _, call := range r.calls {
		if call == "networkctl reload" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("networkctl reload %d times, expected 2 (apply and rollback): %v", n, r.calls)
	}
}

func TestOutdatedFilesRemoved(t *testing.T) {
	a, _, c := setup(t)
	c.WAN.Type = config.WANPPPoE
	c.WAN.PPPoE = &config.PPPoE{User: "user@example.org", PasswordRef: "pppoe", VLAN: 7}
	if _, err := a.Apply(c, memSecrets{"pppoe": "pw"}, Options{}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/etc/systemd/network/20-vr-wan0.7.netdev", "/etc/ppp/peers/vr-wan"} {
		if read(t, a, p) == "" {
			t.Fatalf("%s is missing", p)
		}
	}
	if !strings.Contains(read(t, a, "/etc/ppp/chap-secrets"), `"user@example.org" * "pw" *`) {
		t.Error("chap-secrets")
	}
	// The user's own file in the same folder stays
	os.WriteFile(a.p("/etc/systemd/network/50-own.network"), []byte("x"), 0o644)

	c.WAN.Type, c.WAN.PPPoE = config.WANDHCP, nil
	res, err := a.Apply(c, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/etc/systemd/network/20-vr-wan0.7.netdev", "/etc/systemd/network/30-vr-ppp0.network", "/etc/ppp/peers/vr-wan"} {
		if read(t, a, p) != "" {
			t.Errorf("%s not removed", p)
		}
	}
	if read(t, a, "/etc/systemd/network/50-own.network") != "x" {
		t.Error("foreign file removed")
	}
	if strings.Contains(read(t, a, "/etc/ppp/chap-secrets"), "pw") {
		t.Error("old password is still in chap-secrets")
	}
	removed := 0
	for _, ch := range res.Changes {
		if ch.Action == "remove" {
			removed++
		}
	}
	if removed != 4 { // netdev, vlan-network, ppp0-network, peers
		t.Errorf("%d removed: %+v", removed, res.Changes)
	}
}

func TestConfirmationAndTimeout(t *testing.T) {
	a, r, c := setup(t)
	if _, err := a.Apply(c, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	old := read(t, a, "/etc/vanilla-router/nft/vr.nft")
	c.Firewall.PortForwards = []config.PortForward{{Proto: "tcp", ExtPort: 8080, IntIP: "192.168.42.20", IntPort: 80}}
	res, err := a.Apply(c, nil, Options{Confirm: 60 * time.Second})
	if err != nil || !res.Pending {
		t.Fatalf("%+v %v", res, err)
	}
	if !r.called("systemd-run --unit=vr-rollback-") || !r.called("systemd-run --unit=vr-rollback-"+strings.NewReplacer(".", "-").Replace(res.Generation)) {
		t.Errorf("timer not started: %v", r.calls)
	}
	if _, err := a.Apply(c, nil, Options{}); err == nil {
		t.Error("second change accepted despite a pending confirmation")
	}
	// timer expires
	if err := a.RollbackPending(res.Generation); err != nil {
		t.Fatal(err)
	}
	if read(t, a, "/etc/vanilla-router/nft/vr.nft") != old || a.Pending() != nil {
		t.Error("not rolled back")
	}
	// Again, this time confirmed
	res, err = a.Apply(c, nil, Options{Confirm: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Confirm(); err != nil {
		t.Fatal(err)
	}
	if err := a.RollbackPending(res.Generation); err != nil { // timer fires anyway: do nothing
		t.Fatal(err)
	}
	if !strings.Contains(read(t, a, "/etc/vanilla-router/nft/vr.nft"), "8080") {
		t.Error("confirmed change rolled back")
	}
}

func TestDryRun(t *testing.T) {
	a, r, c := setup(t)
	res, err := a.Apply(c, nil, Options{DryRun: true})
	if err != nil || len(res.Changes) == 0 {
		t.Fatal(res, err)
	}
	if read(t, a, config.Path) != "" || r.called("nft -f") {
		t.Error("dry run changed something")
	}
}

func TestRollbackLast(t *testing.T) {
	a, _, c := setup(t)
	a.Apply(c, nil, Options{})
	first := read(t, a, config.Path)
	c.System.Hostname = "gateway"
	if _, err := a.Apply(c, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := a.RollbackLast(); err != nil {
		t.Fatal(err)
	}
	if read(t, a, config.Path) != first {
		t.Error("not restored")
	}
}

func TestDirSecrets(t *testing.T) {
	s := DirSecrets{Dir: filepath.Join(t.TempDir(), "secrets")}
	if err := s.Set("pppoe", "secret"); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get("pppoe"); err != nil || v != "secret" {
		t.Errorf("%q %v", v, err)
	}
	os.Chmod(filepath.Join(s.Dir, "pppoe"), 0o644)
	if _, err := s.Get("pppoe"); err == nil {
		t.Error("readable file accepted")
	}
	if _, err := s.Get("../x"); err == nil {
		t.Error("path with .. accepted")
	}
}

func wifiConfig(c *config.Config) {
	c.WiFi = &config.WiFi{Enabled: true, Interface: "wlp3s0", SSID: "Home", PasswordRef: "wifi", Country: "DE",
		Band: config.WiFiBand24, Channel: 6, Width: 20}
}

// runnerWiFi reports what iw says about the adapter.
type runnerWiFi struct {
	*fakeRunner
	iw string
}

func (r *runnerWiFi) Run(env []string, name string, args ...string) ([]byte, error) {
	out, err := r.fakeRunner.Run(env, name, args...)
	if name == "iw" {
		return []byte(r.iw), err
	}
	if name == "journalctl" {
		return []byte("Could not set channel for kernel driver\n"), err
	}
	return out, err
}

func TestWiFiApply(t *testing.T) {
	a, r, c := setup(t)
	a.Run = &runnerWiFi{r, "Interface wlp3s0\n\tssid Home\n\ttype AP\n"}
	wifiConfig(c)
	sec := memSecrets{"wifi": "a good password"}
	res, err := a.Apply(c, sec, Options{Confirm: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if !r.called("systemctl enable vr-hostapd.service") || !r.called("systemctl restart vr-hostapd.service") {
		t.Errorf("access point not started: %v", r.calls)
	}
	if !res.Pending {
		t.Error("a Wi-Fi change must wait for confirmation (F-WIFI-06)")
	}
	fi, err := os.Stat(filepath.Join(a.Root, "etc/vanilla-router/wifi/hostapd.conf"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("hostapd.conf: %v %v", err, fi)
	}
	if err := a.Confirm(); err != nil {
		t.Fatal(err)
	}

	// Off: the file with the password goes away, the service stops.
	r.calls = nil
	c.WiFi.Enabled = false
	if _, err := a.Apply(c, sec, Options{}); err != nil {
		t.Fatal(err)
	}
	if !r.called("systemctl disable --now vr-hostapd.service") {
		t.Errorf("access point not stopped: %v", r.calls)
	}
	if _, err := os.Stat(filepath.Join(a.Root, "etc/vanilla-router/wifi/hostapd.conf")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("hostapd.conf still there: %v", err)
	}
}

// hostapd has no check mode (D-18): what it rejects shows up after the start, and that must roll back.
func TestWiFiFailedStartRollsBack(t *testing.T) {
	a, r, c := setup(t)
	a.Run = &runnerWiFi{r, "Interface wlp3s0\n\ttype managed\n"}
	if _, err := a.Apply(c, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	old, _ := os.ReadFile(filepath.Join(a.Root, config.Path))
	wifiConfig(c)
	_, err := a.Apply(c, memSecrets{"wifi": "a good password"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "Could not set channel") {
		t.Fatalf("expected the message from hostapd, got %v", err)
	}
	now, _ := os.ReadFile(filepath.Join(a.Root, config.Path))
	if !bytes.Equal(old, now) {
		t.Error("config.json not restored")
	}
	if _, err := os.Stat(filepath.Join(a.Root, "etc/vanilla-router/wifi/hostapd.conf")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("hostapd.conf still there: %v", err)
	}
	if !r.called("systemctl disable --now vr-hostapd.service") {
		t.Errorf("access point not stopped again: %v", r.calls)
	}
}

func TestWiFiPackageSource(t *testing.T) {
	a, _, _ := setup(t)
	if err := a.PrepareWiFiPackages(); err == nil {
		t.Error("a missing os-release must be an error (L-17)")
	}
	rel := filepath.Join(a.Root, "etc/os-release")
	os.MkdirAll(filepath.Dir(rel), 0o755)
	os.WriteFile(rel, []byte("NAME=\"Ubuntu\"\nVERSION_CODENAME=resolute\n"), 0o644)
	if err := a.PrepareWiFiPackages(); err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile(filepath.Join(a.Root, "etc/apt/sources.list.d/vr-wifi.sources"))
	if !strings.Contains(string(src), "Suites: resolute resolute-updates resolute-security\n") || !strings.Contains(string(src), "Components: universe\n") {
		t.Errorf("%s", src)
	}
	pin, _ := os.ReadFile(filepath.Join(a.Root, "etc/apt/preferences.d/vr-wifi"))
	if !strings.Contains(string(pin), "Package: *\nPin: release c=universe\nPin-Priority: -1\n") ||
		!strings.Contains(string(pin), "Package: hostapd\nPin: release c=universe\nPin-Priority: 500\n") {
		t.Errorf("%s", pin)
	}
}

// The list in the code and the list test/check-main.sh checks must agree (NF-09).
func TestWiFiPackagesMatchList(t *testing.T) {
	b, err := os.ReadFile("../../image/packages-wifi.txt")
	if err != nil {
		t.Fatal(err)
	}
	var list []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			list = append(list, l)
		}
	}
	if strings.Join(list, " ") != strings.Join(WiFiPackages, " ") {
		t.Errorf("image/packages-wifi.txt: %v, code: %v", list, WiFiPackages)
	}
	if cmd := WiFiInstallCommand(); !strings.Contains(cmd, strings.Join(WiFiPackages, " ")) {
		t.Error(cmd)
	}
}

// ipLinks makes the fake system report ports, and records what relink does with them.
type linkRunner struct {
	fakeRunner
	links string
}

func (f *linkRunner) Run(env []string, name string, args ...string) ([]byte, error) {
	if name == "ip" && len(args) == 3 && args[0] == "-j" {
		f.calls = append(f.calls, "ip -j link show")
		return []byte(f.links), nil
	}
	return f.fakeRunner.Run(env, name, args...)
}

func indexOf(calls []string, want string) int {
	for i, c := range calls {
		if c == want {
			return i
		}
	}
	return -1
}

func TestSwapPortsLiveAndRollBack(t *testing.T) {
	a, fr, c := setup(t)
	r := &linkRunner{fakeRunner: *fr, links: `[{"ifname":"wan0","address":"02:00:00:00:00:01","link_type":"ether"},
		{"ifname":"lanp1","address":"02:00:00:00:00:02","link_type":"ether"},
		{"ifname":"wan0.7","address":"02:00:00:00:00:01","link_type":"ether","link":"wan0"},
		{"ifname":"lan0","address":"02:00:00:00:00:02","link_type":"ether"},
		{"ifname":"docker0","address":"02:00:00:00:00:09","link_type":"ether"}]`}
	a.Run = r
	if _, err := a.Apply(c, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if r.called("ip link set") {
		t.Fatalf("a first setup must not rename anything: %v", r.calls)
	}
	nc := *c
	nc.WAN.MatchMAC, nc.LAN.Members = "02:00:00:00:00:02", []string{"02:00:00:00:00:01"}
	r.calls = nil
	res, err := a.Apply(&nc, nil, Options{Confirm: 5 * time.Minute, Self: "/usr/sbin/vrctl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pending {
		t.Error("swapping ports must wait for confirmation")
	}
	if !strings.Contains(read(t, a, "/etc/systemd/network/10-vr-wan0.link"), "02:00:00:00:00:02") {
		t.Error(".link file not rewritten")
	}
	for _, want := range []string{"ip link set dev wan0 nomaster", "ip addr flush dev wan0", "ip link set dev wan0 name vrtmp0",
		"ip link set dev lanp1 name vrtmp1", "ip link set dev vrtmp0 name lanp1", "ip link set dev vrtmp1 name wan0", "networkctl reconfigure lanp1 wan0"} {
		if indexOf(r.calls, want) < 0 {
			t.Errorf("%q not called: %v", want, r.calls)
		}
	}
	if indexOf(r.calls, "ip link set dev vrtmp0 name lanp1") < indexOf(r.calls, "ip link set dev lanp1 name vrtmp1") {
		t.Error("a name was used before it was free")
	}
	for _, c := range r.calls {
		if strings.Contains(c, "docker0") || strings.Contains(c, "wan0.7") || strings.HasPrefix(c, "ip link set dev lan0 ") {
			t.Errorf("touched a port that is not ours: %s", c)
		}
	}
	// the system now has the new names
	r.links = `[{"ifname":"wan0","address":"02:00:00:00:00:02","link_type":"ether"},{"ifname":"lanp1","address":"02:00:00:00:00:01","link_type":"ether"}]`
	r.calls = nil
	if err := a.RollbackPending(res.Generation); err != nil {
		t.Fatal(err)
	}
	if !r.called("ip link set dev lanp1 name vrtmp") || !strings.Contains(read(t, a, "/etc/systemd/network/10-vr-wan0.link"), "02:00:00:00:00:01") {
		t.Errorf("rollback did not restore the ports: %v", r.calls)
	}
}

func TestUnusedPortLeavesTheBridgeNames(t *testing.T) {
	a, fr, c := setup(t)
	c.LAN.Members = []string{"02:00:00:00:00:02", "02:00:00:00:00:03"}
	r := &linkRunner{fakeRunner: *fr, links: `[{"ifname":"wan0","address":"02:00:00:00:00:01","link_type":"ether"},
		{"ifname":"lanp1","address":"02:00:00:00:00:02","link_type":"ether"},
		{"ifname":"lanp2","address":"02:00:00:00:00:03","link_type":"ether"}]`}
	a.Run = r
	if _, err := a.Apply(c, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	nc := *c
	nc.LAN.Members = []string{"02:00:00:00:00:02"}
	r.calls = nil
	if _, err := a.Apply(&nc, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if indexOf(r.calls, "ip link set dev vrtmp0 name vroff-000003") < 0 {
		t.Errorf("a port without a use must not keep a name the .network files match: %v", r.calls)
	}
	if read(t, a, "/etc/systemd/network/10-vr-lanp2.link") != "" {
		t.Error(".link file of the removed port is still there")
	}
}
