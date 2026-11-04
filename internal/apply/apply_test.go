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

// fakeRunner merkt sich Aufrufe und simuliert das System.
type fakeRunner struct {
	calls  []string
	fail   map[string]bool // Präfix eines Aufrufs → Fehler
	lanIP  string          // was `ip addr show lan0` meldet
	active bool            // vr-dnsmasq läuft
}

func (f *fakeRunner) Run(env []string, name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, call)
	for p := range f.fail {
		if strings.HasPrefix(call, p) {
			return []byte("simulierter Fehler"), errors.New("exit status 1")
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
	return "", errors.New("fehlt")
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

func TestErstesEinspielenUndKeineAenderung(t *testing.T) {
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
			t.Errorf("%q nicht aufgerufen: %v", want, r.calls)
		}
	}
	if !strings.Contains(read(t, a, config.Path), `"schema": 1`) {
		t.Error("config.json nicht geschrieben")
	}
	if fi, _ := os.Stat(a.p("/etc/ppp/chap-secrets")); fi.Mode().Perm() != 0o600 {
		t.Errorf("chap-secrets %v", fi.Mode().Perm())
	}
	r.calls = nil
	res, err = a.Apply(c, nil, Options{})
	if err != nil || len(res.Changes) != 0 || len(r.calls) != 0 {
		t.Errorf("zweiter Lauf: %+v %v %v", res, err, r.calls)
	}
}

func TestNurFirewallNeuGeladen(t *testing.T) {
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
	if len(res.Changes) != 2 { // vr.nft und config.json
		t.Errorf("%+v", res.Changes)
	}
	if r.called("networkctl") || r.called("systemctl restart vr-dnsmasq") || !r.called("nft -f") {
		t.Errorf("falsche Dienste neu geladen: %v", r.calls)
	}
	if !strings.Contains(read(t, a, "/etc/vanilla-router/nft/vr.nft"), "dport 8080 dnat to 192.168.42.20:80") {
		t.Error("Regel fehlt")
	}
}

func TestPruefungSchlaegtFehlNichtsGeaendert(t *testing.T) {
	a, r, c := setup(t)
	if _, err := a.Apply(c, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	before := read(t, a, "/etc/vanilla-router/nft/vr.nft")
	r.fail["nft -c"] = true
	c.Firewall.PortForwards = []config.PortForward{{Proto: "tcp", ExtPort: 8080, IntIP: "192.168.42.20", IntPort: 80}}
	if _, err := a.Apply(c, nil, Options{}); err == nil || !strings.Contains(err.Error(), "nft -c") {
		t.Fatalf("Fehler erwartet: %v", err)
	}
	if read(t, a, "/etc/vanilla-router/nft/vr.nft") != before {
		t.Error("Datei trotz abgelehnter Prüfung geändert")
	}
	if strings.Contains(read(t, a, config.Path), "8080") {
		t.Error("config.json trotz Fehler geändert")
	}
}

func TestGesundheitsfehlerNimmtZurueck(t *testing.T) {
	a, r, c := setup(t)
	if _, err := a.Apply(c, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	oldNet := read(t, a, "/etc/systemd/network/30-vr-lan0.network")
	oldCfg := read(t, a, config.Path)
	c.LAN.IPv4 = "192.168.42.2/24"
	r.calls = nil // lanIP bleibt .1/24: neue Adresse kommt „nicht an“
	_, err := a.Apply(c, nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "Heimnetz-Adresse") {
		t.Fatalf("Fehler erwartet: %v", err)
	}
	if read(t, a, "/etc/systemd/network/30-vr-lan0.network") != oldNet || read(t, a, config.Path) != oldCfg {
		t.Error("nicht zurückgenommen")
	}
	n := 0
	for _, call := range r.calls {
		if call == "networkctl reload" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("networkctl reload %d-mal, erwartet 2 (Einspielen und Rücknahme): %v", n, r.calls)
	}
}

func TestVeralteteDateienEntfernt(t *testing.T) {
	a, _, c := setup(t)
	c.WAN.Type = config.WANPPPoE
	c.WAN.PPPoE = &config.PPPoE{User: "nutzer@example.org", PasswordRef: "pppoe", VLAN: 7}
	if _, err := a.Apply(c, memSecrets{"pppoe": "pw"}, Options{}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/etc/systemd/network/20-vr-wan0.7.netdev", "/etc/ppp/peers/vr-wan"} {
		if read(t, a, p) == "" {
			t.Fatalf("%s fehlt", p)
		}
	}
	if !strings.Contains(read(t, a, "/etc/ppp/chap-secrets"), `"nutzer@example.org" * "pw" *`) {
		t.Error("chap-secrets")
	}
	// Eigene Datei des Nutzers im selben Ordner bleibt erhalten
	os.WriteFile(a.p("/etc/systemd/network/50-eigene.network"), []byte("x"), 0o644)

	c.WAN.Type, c.WAN.PPPoE = config.WANDHCP, nil
	res, err := a.Apply(c, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/etc/systemd/network/20-vr-wan0.7.netdev", "/etc/systemd/network/30-vr-ppp0.network", "/etc/ppp/peers/vr-wan"} {
		if read(t, a, p) != "" {
			t.Errorf("%s nicht entfernt", p)
		}
	}
	if read(t, a, "/etc/systemd/network/50-eigene.network") != "x" {
		t.Error("fremde Datei entfernt")
	}
	if strings.Contains(read(t, a, "/etc/ppp/chap-secrets"), "pw") {
		t.Error("altes Passwort liegt noch in chap-secrets")
	}
	removed := 0
	for _, ch := range res.Changes {
		if ch.Action == "remove" {
			removed++
		}
	}
	if removed != 4 { // netdev, vlan-network, ppp0-network, peers
		t.Errorf("%d entfernt: %+v", removed, res.Changes)
	}
}

func TestBestaetigungUndZeitablauf(t *testing.T) {
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
		t.Errorf("Timer nicht gestartet: %v", r.calls)
	}
	if _, err := a.Apply(c, nil, Options{}); err == nil {
		t.Error("zweite Änderung trotz offener Bestätigung angenommen")
	}
	// Timer läuft ab
	if err := a.RollbackPending(res.Generation); err != nil {
		t.Fatal(err)
	}
	if read(t, a, "/etc/vanilla-router/nft/vr.nft") != old || a.Pending() != nil {
		t.Error("nicht zurückgenommen")
	}
	// Erneut, diesmal bestätigt
	res, err = a.Apply(c, nil, Options{Confirm: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Confirm(); err != nil {
		t.Fatal(err)
	}
	if err := a.RollbackPending(res.Generation); err != nil { // Timer feuert trotzdem: nichts tun
		t.Fatal(err)
	}
	if !strings.Contains(read(t, a, "/etc/vanilla-router/nft/vr.nft"), "8080") {
		t.Error("bestätigte Änderung zurückgenommen")
	}
}

func TestDryRun(t *testing.T) {
	a, r, c := setup(t)
	res, err := a.Apply(c, nil, Options{DryRun: true})
	if err != nil || len(res.Changes) == 0 {
		t.Fatal(res, err)
	}
	if read(t, a, config.Path) != "" || r.called("nft -f") {
		t.Error("Probelauf hat etwas geändert")
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
		t.Error("nicht zurück")
	}
}

func TestDirSecrets(t *testing.T) {
	s := DirSecrets{Dir: filepath.Join(t.TempDir(), "secrets")}
	if err := s.Set("pppoe", "geheim"); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get("pppoe"); err != nil || v != "geheim" {
		t.Errorf("%q %v", v, err)
	}
	os.Chmod(filepath.Join(s.Dir, "pppoe"), 0o644)
	if _, err := s.Get("pppoe"); err == nil {
		t.Error("lesbare Datei angenommen")
	}
	if _, err := s.Get("../x"); err == nil {
		t.Error("Pfad mit .. angenommen")
	}
}
