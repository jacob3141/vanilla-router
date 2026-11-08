// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"vanilla-router/internal/apd"
	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
)

func newWirelessServer(t *testing.T) *Server {
	t.Helper()
	s := newDryServer(t, nil)
	return NewServer(Config{DryRun: true, Root: s.cfg.Root, FakeWiFi: true, Log: slog.New(slog.DiscardHandler)})
}

func homeSetup(password string) map[string]any {
	return map[string]any{
		"country":  "DE",
		"segments": []map[string]any{{"id": "home", "name": "Home network", "vlan": 1, "access": "lan"}},
		"networks": []map[string]any{{"id": "", "ssid": "Home", "segment": "home", "security": "wpa3",
			"bands": []string{"5", "6"}, "enabled": true, "password": password}},
		"access_points": []any{},
	}
}

// info reads wireless.get as the page sees it.
func info(t *testing.T, s *Server) WirelessInfo {
	t.Helper()
	m, err := call(t, s, "wireless.get", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m)
	var wi WirelessInfo
	if err := json.Unmarshal(b, &wi); err != nil {
		t.Fatal(err)
	}
	return wi
}

// setFrom turns the page's view back into a wireless.set request, as the page does.
func setFrom(wi WirelessInfo) map[string]any {
	var nets []map[string]any
	for _, n := range wi.Networks {
		nets = append(nets, map[string]any{"id": n.ID, "ssid": n.SSID, "segment": n.Segment, "security": n.Security,
			"bands": n.Bands, "enabled": n.Enabled, "password": ""})
	}
	aps := []map[string]any{}
	for _, a := range wi.AccessPoints {
		aps = append(aps, map[string]any{"id": a.ID, "name": a.Name, "power": a.Power, "radios": a.Radios})
	}
	return map[string]any{"country": wi.Country, "segments": wi.Segments, "networks": nets, "access_points": aps}
}

func TestWirelessSetupAndSecrets(t *testing.T) {
	s := newWirelessServer(t)
	wi := info(t, s)
	if wi.Configured || !wi.Simulated || len(wi.Pairing) != 2 || wi.Router == "" {
		t.Fatalf("before setup: %+v", wi)
	}
	if _, err := call(t, s, "wireless.set", homeSetup("")); err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("must ask for a password: %v", err)
	}
	if _, err := call(t, s, "wireless.set", homeSetup("short")); err == nil {
		t.Fatal("short password accepted")
	}
	if _, err := call(t, s, "wireless.set", homeSetup("a good password")); err != nil {
		t.Fatal(err)
	}
	call(t, s, "settings.confirm", nil) // the first apply of the test root also writes the network files
	wi = info(t, s)
	if !wi.Configured || len(wi.Networks) != 1 || !wi.Networks[0].HasPassword || wi.Networks[0].MobilityDomain == "" {
		t.Fatalf("after setup: %+v", wi)
	}
	raw, _ := os.ReadFile(s.Path(config.Path))
	if strings.Contains(string(raw), "a good password") {
		t.Error("password in config.json")
	}
	c := loadCfg(t, s)
	n := c.Wireless.Networks[0]
	key, err := s.secrets().Get(n.Roaming.KeyRef)
	if err != nil || len(key) != 64 {
		t.Fatalf("roaming key %q %v", key, err)
	}

	// Saving again without a password keeps password, mobility domain and key
	if _, err := call(t, s, "wireless.set", setFrom(wi)); err != nil {
		t.Fatal(err)
	}
	c2 := loadCfg(t, s)
	if *c2.Wireless.Networks[0].Roaming != *n.Roaming || c2.Wireless.Networks[0].PasswordRef != n.PasswordRef {
		t.Errorf("changed on save: %+v → %+v", n, c2.Wireless.Networks[0])
	}

	// WPA2 in the home segment is refused by the schema, the message names the field
	req := setFrom(wi)
	req["networks"].([]map[string]any)[0]["security"] = "wpa2-wpa3"
	req["networks"].([]map[string]any)[0]["bands"] = []string{"2.4"}
	if _, err := call(t, s, "wireless.set", req); err == nil || !strings.Contains(err.Error(), "WPA3 only") {
		t.Fatalf("WPA2 at home: %v", err)
	}
}

func TestWirelessPairPreviewRemove(t *testing.T) {
	s := newWirelessServer(t)
	if _, err := call(t, s, "wireless.pair", map[string]any{"id": "sim1", "code": "000 000"}); err == nil {
		t.Fatal("pairing before setup")
	}
	if _, err := call(t, s, "wireless.set", homeSetup("a good password")); err != nil {
		t.Fatal(err)
	}
	call(t, s, "settings.confirm", nil) // the first apply of the test root also writes the network files
	wi := info(t, s)
	p := wi.Pairing[0]
	if len(p.Code) != 7 || p.MAC != "02:00:00:00:00:10" {
		t.Fatalf("%+v", p)
	}
	if _, err := call(t, s, "wireless.pair", map[string]any{"id": p.ID, "code": "999 999"}); err == nil {
		t.Fatal("wrong code accepted")
	}
	res, err := call(t, s, "wireless.pair", map[string]any{"id": p.ID, "code": p.Code})
	if err != nil {
		t.Fatal(err)
	}
	if res["id"] != "ap1" || res["name"] != "hall" {
		t.Errorf("%v", res)
	}
	wi = info(t, s)
	if len(wi.Pairing) != 1 || len(wi.AccessPoints) != 1 {
		t.Fatalf("after pairing: %+v", wi)
	}
	a := wi.AccessPoints[0]
	if !a.Online || len(a.Radios) != 2 || a.Radios[0].Band != "6" || a.Radios[1].Band != "5" || len(a.Caps) != 2 {
		t.Errorf("AP: %+v", a)
	}
	if r, ok := wi.Plan.Radio("ap1", "pci-0000-03-00-0"); !ok || r.Channel != 1 || r.Width != 160 {
		t.Errorf("plan: %+v", wi.Plan)
	}

	// The preview hides every secret
	c := loadCfg(t, s)
	pw, _ := s.secrets().Get(c.Wireless.Networks[0].PasswordRef)
	ft, _ := s.secrets().Get(c.Wireless.Networks[0].Roaming.KeyRef)
	out, err := s.handlers["wireless.preview"](t.Context(), json.RawMessage(`{"id":"ap1"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), pw) || strings.Contains(string(b), ft) || !strings.Contains(string(b), "sae_password=(hidden)") {
		t.Errorf("preview: %s", b)
	}

	// Rename, pin a channel, a band the card cannot do
	req := setFrom(wi)
	ap := req["access_points"].([]map[string]any)[0]
	ap["name"] = "Hall"
	ap["radios"] = []config.APRadio{{ID: "pci-0000-03-00-0", Band: "6"}, {ID: "pci-0000-04-00-0", Band: "5", Channel: 100, Width: 80}}
	if _, err := call(t, s, "wireless.set", req); err != nil {
		t.Fatal(err)
	}
	ap["radios"] = []config.APRadio{{ID: "pci-0000-03-00-0", Band: "6"}, {ID: "pci-0000-04-00-0", Band: "6"}}
	if _, err := call(t, s, "wireless.set", req); err == nil || !strings.Contains(err.Error(), "cannot send") {
		t.Errorf("6 GHz on an MT7915: %v", err)
	}
	ap["radios"] = []config.APRadio{{ID: "pci-0000-99-00-0", Band: "6"}, {ID: "pci-0000-04-00-0", Band: "5"}}
	if _, err := call(t, s, "wireless.set", req); err == nil {
		t.Error("unknown card accepted")
	}
	if loadCfg(t, s).Wireless.AccessPoints[0].Name != "Hall" {
		t.Error("rename lost")
	}

	// Removing renews the roaming keys and deletes the old one
	oldRef := loadCfg(t, s).Wireless.Networks[0].Roaming.KeyRef
	if _, err := call(t, s, "wireless.remove", map[string]any{"id": "ap1"}); err != nil {
		t.Fatal(err)
	}
	c = loadCfg(t, s)
	if len(c.Wireless.AccessPoints) != 0 || c.Wireless.Networks[0].Roaming.KeyRef == oldRef {
		t.Errorf("after removing: %+v", c.Wireless)
	}
	if _, err := os.Stat(filepath.Join(s.secrets().Dir, oldRef)); !os.IsNotExist(err) {
		t.Error("old roaming key still there")
	}
	if _, err := s.secrets().Get(c.Wireless.Networks[0].PasswordRef); err != nil {
		t.Error("password lost on removing")
	}
}

func TestWirelessRejectAndForgedRequest(t *testing.T) {
	s := newWirelessServer(t)
	if _, err := call(t, s, "wireless.reject", map[string]any{"id": "sim2"}); err != nil {
		t.Fatal(err)
	}
	if n := len(info(t, s).Pairing); n != 1 {
		t.Errorf("%d requests after reject", n)
	}
	if _, err := call(t, s, "wireless.reject", map[string]any{"id": "../x"}); err == nil {
		t.Error("path accepted")
	}
	// A request whose random value does not match its commitment never shows a code
	p := filepath.Join(s.Path(apPairingDir), "sim1.json")
	r, err := readJSONFile[PairingRequest](p)
	if err != nil {
		t.Fatal(err)
	}
	r.APNonce = strings.Repeat("00", 32)
	b, _ := json.Marshal(r)
	os.WriteFile(p, b, 0o644)
	if n := len(info(t, s).Pairing); n != 0 {
		t.Errorf("forged request shown")
	}
	if _, err := call(t, s, "wireless.simulate", nil); err != nil {
		t.Fatal(err)
	}
	if n := len(info(t, s).Pairing); n != 2 {
		t.Errorf("%d requests after simulate", n)
	}
}

func TestChooseBands(t *testing.T) {
	for _, a := range fakeAPList {
		got := chooseBands(a.radios)
		if len(got) != len(a.radios) {
			t.Errorf("%s: %+v", a.host, got)
		}
	}
	if b := chooseBands(fakeAPList[1].radios); b[0].Band != "6" {
		t.Errorf("single card: %+v", b)
	}
}

// A preset (guest network) brings its own ID, which does not exist yet.
func TestWirelessPresetID(t *testing.T) {
	s := newWirelessServer(t)
	call(t, s, "wireless.set", homeSetup("a good password"))
	call(t, s, "settings.confirm", nil)
	req := setFrom(info(t, s))
	req["segments"] = append(req["segments"].([]config.Segment), config.Segment{ID: "guest", Name: "Guests", VLAN: 20, Access: config.AccessInternet, IPv4: "192.168.20.1/24"})
	req["networks"] = append(req["networks"].([]map[string]any), map[string]any{"id": "guest", "ssid": "Guests", "segment": "guest", "security": "wpa3",
		"bands": []string{"5"}, "enabled": true, "password": "a guest password"})
	res, err := call(t, s, "wireless.set", req)
	if err != nil {
		t.Fatal(err)
	}
	if res["pending"] == nil {
		t.Error("a new segment changes the network and must be confirmed")
	}
	if _, err := os.Stat(s.Path("/etc/systemd/network/20-vr-vl20.netdev")); err != nil {
		t.Error(err)
	}
	if loadCfg(t, s).Wireless.Networks[1].ID != "guest" {
		t.Error("ID lost")
	}
}

func TestAPActions(t *testing.T) {
	s := newWirelessServer(t)
	call(t, s, "wireless.set", homeSetup("a good password"))
	call(t, s, "settings.confirm", nil)
	wi := info(t, s)
	call(t, s, "wireless.pair", map[string]any{"id": wi.Pairing[0].ID, "code": wi.Pairing[0].Code})
	c := loadCfg(t, s)
	key := c.Wireless.AccessPoints[0].PublicKey

	if m, err := call(t, s, "ap.authorize", map[string]any{"public_key": key}); err != nil || m["id"] != "ap1" {
		t.Fatalf("%v %v", m, err)
	}
	if m, _ := call(t, s, "ap.authorize", map[string]any{"public_key": "ed25519:other"}); m["id"] != "" {
		t.Errorf("unknown key: %v", m)
	}
	if m, _ := call(t, s, "ap.pairing.status", map[string]any{"public_key": key}); m["state"] != "paired" {
		t.Errorf("%v", m)
	}
	b, err := call(t, s, "ap.bundle", map[string]any{"id": "ap1"})
	if err != nil || b["generation"] == "" || b["generation"] == nil {
		t.Fatalf("%v %v", b, err)
	}
	raw, _ := json.Marshal(b)
	var bundle apd.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(bundle.Files, func(f gen.File) bool { return strings.Contains(string(f.Content), "sae_password=a good password\n") }) {
		t.Error("the real bundle must carry the password")
	}
	b2, _ := call(t, s, "ap.bundle", map[string]any{"id": "ap1"})
	if b2["generation"] != b["generation"] {
		t.Error("generation is not stable")
	}
	if _, err := call(t, s, "ap.bundle", map[string]any{"id": "nope"}); err == nil {
		t.Error("unknown AP gets a bundle")
	}
	if _, err := call(t, s, "ap.report", map[string]any{"id": "ap1", "report": map[string]any{"applied": b["generation"], "version": "1", "clients": 2}}); err != nil {
		t.Fatal(err)
	}
	if a := info(t, s).AccessPoints[0]; a.Report == nil || a.Report.Applied != b["generation"] || a.Generation != b["generation"] {
		t.Errorf("%+v", a)
	}

	// A card report that would inject lines into generated files is dropped
	evil := map[string]any{"id": "x", "mac": "02:00:00:00:00:99", "chip": "a\nbridge=evil", "bands": []string{"5"}}
	call(t, s, "ap.report", map[string]any{"id": "ap1", "report": map[string]any{"applied": "x", "radios": []any{evil}}})
	if strings.Contains(fmtCaps(t, s), "evil") {
		t.Error("injected card stored")
	}

	// New request through vr-apd: refused with a commitment that does not match
	_, err = call(t, s, "ap.pairing.add", map[string]any{"public_key": key[:0] + "ed25519:AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=", "mac": "02:00:00:00:00:77",
		"radios": []any{}, "router_nonce": strings.Repeat("11", 32), "ap_nonce": strings.Repeat("22", 32), "commitment": strings.Repeat("33", 32)})
	if err == nil {
		t.Error("wrong commitment accepted")
	}
}

func fmtCaps(t *testing.T, s *Server) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(s.Path(apCapsDir), "ap1.json"))
	return string(b)
}

// vr-apd may only call ap.*, the web server only the others, and root everything.
func TestActionGate(t *testing.T) {
	s := NewServer(Config{DryRun: true, Root: t.TempDir(), AllowUID: 100, APDUID: 200, Log: slog.New(slog.DiscardHandler)})
	for _, tc := range []struct {
		uid    int
		action string
		want   bool
	}{{100, "wifi.get", true}, {100, "ap.identity", false}, {200, "ap.identity", true}, {200, "wifi.get", false}, {0, "ap.identity", true}, {0, "wifi.get", true}, {300, "wifi.get", false}} {
		if got := s.allowed(tc.uid, tc.action); got != tc.want {
			t.Errorf("uid %d %s: %v", tc.uid, tc.action, got)
		}
	}
	s = NewServer(Config{DryRun: true, Root: t.TempDir(), AllowUID: 100, APDUID: -1, Log: slog.New(slog.DiscardHandler)})
	if s.allowed(100, "ap.identity") || s.allowed(-1, "ap.identity") {
		t.Error("no vr-apd user: ap.* must stay closed")
	}
}
