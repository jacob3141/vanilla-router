// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
	"vanilla-router/internal/sys"
)

func newWiFiServer(t *testing.T) *Server {
	t.Helper()
	s := newDryServer(t, nil)
	return NewServer(Config{DryRun: true, Root: s.cfg.Root, FakeWiFi: true, Log: slog.New(slog.DiscardHandler)})
}

func wifiParams(mod func(p map[string]any)) map[string]any {
	p := map[string]any{"enabled": true, "interface": "wlp3s0", "ssid": "Home", "password": "a good password",
		"country": "DE", "band": "2.4", "channel": 6, "width": 20}
	if mod != nil {
		mod(p)
	}
	return p
}

func TestWiFiNeedsSoftwareFirst(t *testing.T) {
	s := newWiFiServer(t)
	if _, err := call(t, s, "wifi.set", wifiParams(nil)); err == nil || !strings.Contains(err.Error(), "download") {
		t.Fatalf("must ask for the download first: %v", err)
	}
	if _, err := call(t, s, "wifi.install.start", nil); err != nil {
		t.Fatal(err)
	}
	st, err := call(t, s, "wifi.install.status", nil)
	if err != nil || st["installed"] != true {
		t.Fatalf("%v %v", st, err)
	}
}

func TestWiFiSetConfirmRollback(t *testing.T) {
	s := newWiFiServer(t)
	call(t, s, "wifi.install.start", nil)

	res, err := call(t, s, "wifi.set", wifiParams(nil))
	if err != nil {
		t.Fatal(err)
	}
	if res["pending"] == nil {
		t.Error("a Wi-Fi change must be confirmed (F-WIFI-06)")
	}
	c := loadCfg(t, s)
	if c.WiFi == nil || !c.WiFi.Enabled || !strings.HasPrefix(c.WiFi.PasswordRef, wifiRef) {
		t.Fatalf("%+v", c.WiFi)
	}
	raw, _ := os.ReadFile(s.Path(config.Path))
	if strings.Contains(string(raw), "a good password") {
		t.Error("password in config.json (F-WIFI-05)")
	}
	conf, err := os.ReadFile(s.Path(gen.WiFiConf))
	if err != nil || !strings.Contains(string(conf), "wpa_passphrase=a good password\n") {
		t.Fatalf("%s %v", conf, err)
	}
	fi, _ := os.Stat(s.Path(gen.WiFiConf))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("hostapd.conf has mode %o", fi.Mode().Perm())
	}
	if _, err := call(t, s, "settings.confirm", nil); err != nil {
		t.Fatal(err)
	}

	// The password never goes back to the browser, only the fact that there is one.
	out, err := s.handlers["wifi.get"](context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "a good password") || strings.Contains(string(b), c.WiFi.PasswordRef) {
		t.Errorf("secret in the response: %s", b)
	}
	info := out.(WiFiInfo)
	if !info.HasPassword || !info.Configured || !info.Status.Running {
		t.Errorf("%+v", info)
	}

	// A new password is rolled back together with its reference, the old one still applies.
	oldRef := c.WiFi.PasswordRef
	if _, err := call(t, s, "wifi.set", wifiParams(func(p map[string]any) { p["password"] = "another password" })); err != nil {
		t.Fatal(err)
	}
	newRef := loadCfg(t, s).WiFi.PasswordRef
	if newRef == oldRef {
		t.Fatal("new password under the old reference")
	}
	if _, err := call(t, s, "settings.revert", nil); err != nil {
		t.Fatal(err)
	}
	if got := loadCfg(t, s).WiFi.PasswordRef; got != oldRef {
		t.Errorf("after the rollback: %s", got)
	}
	if conf, _ := os.ReadFile(s.Path(gen.WiFiConf)); !strings.Contains(string(conf), "wpa_passphrase=a good password\n") {
		t.Errorf("old password not back: %s", conf)
	}

	// Keep the password, confirm: unused secrets disappear.
	if _, err := call(t, s, "wifi.set", wifiParams(func(p map[string]any) { p["password"] = ""; p["channel"] = 11 })); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, s, "settings.confirm", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.secrets().Dir, newRef)); !os.IsNotExist(err) {
		t.Errorf("unused secret stays: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.secrets().Dir, oldRef)); err != nil {
		t.Errorf("secret in use is gone: %v", err)
	}

	// Off: the file with the password goes away, the settings stay.
	if _, err := call(t, s, "wifi.set", wifiParams(func(p map[string]any) { p["password"] = ""; p["enabled"] = false })); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path(gen.WiFiConf)); !os.IsNotExist(err) {
		t.Errorf("hostapd.conf still there: %v", err)
	}
	if c := loadCfg(t, s); c.WiFi == nil || c.WiFi.Enabled || c.WiFi.SSID != "Home" {
		t.Errorf("%+v", c.WiFi)
	}
}

func TestWiFiRejected(t *testing.T) {
	s := newWiFiServer(t)
	call(t, s, "wifi.install.start", nil)
	entries := func() int {
		ents, _ := os.ReadDir(s.secrets().Dir)
		return len(ents)
	}
	for name, mod := range map[string]func(p map[string]any){
		"short password":  func(p map[string]any) { p["password"] = "short" },
		"no password":     func(p map[string]any) { p["password"] = "" },
		"line break":      func(p map[string]any) { p["ssid"] = "a\nwpa=0" },
		"no country":      func(p map[string]any) { p["country"] = "" },
		"unknown adapter": func(p map[string]any) { p["interface"] = "wlan9" },
		"bad adapter":     func(p map[string]any) { p["interface"] = "../../x" },
		"DFS channel":     func(p map[string]any) { p["band"] = "5"; p["channel"] = 100; p["width"] = 20 },
		"not on adapter":  func(p map[string]any) { p["band"] = "5"; p["channel"] = 149; p["width"] = 20 },
	} {
		if _, err := call(t, s, "wifi.set", wifiParams(mod)); err == nil {
			t.Errorf("%s accepted", name)
		}
		if entries() != 0 {
			t.Errorf("%s left a secret behind", name)
		}
	}
	if c := loadCfg(t, s); c.WiFi != nil {
		t.Errorf("%+v", c.WiFi)
	}
}

func TestCheckAdapter(t *testing.T) {
	a := sys.WiFiAdapter{Name: "wl0", WiFiCaps: sys.WiFiCaps{Known: true, AP: true, Bands: []sys.WiFiBand{
		{Band: "2.4", Channels: []sys.WiFiChannel{{Channel: 6}, {Channel: 13, Disabled: true}}},
		{Band: "5", HT40: true, Channels: []sys.WiFiChannel{{Channel: 36, NoIR: true}}},
	}}}
	w := func(band string, ch, width int) *config.WiFi {
		return &config.WiFi{Interface: "wl0", Band: band, Channel: ch, Width: width}
	}
	list := []sys.WiFiAdapter{a}
	for name, tc := range map[string]struct {
		w  *config.WiFi
		ok bool
	}{
		"fine":                  {w("2.4", 6, 20), true},
		"disabled":              {w("2.4", 13, 20), false},
		"missing":               {w("2.4", 1, 20), false},
		"no-IR may still start": {w("5", 36, 40), true},
		"no VHT":                {w("5", 36, 80), false},
	} {
		if err := checkAdapter(list, tc.w); (err == nil) != tc.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
	noAP := a
	noAP.AP = false
	if err := checkAdapter([]sys.WiFiAdapter{noAP}, w("2.4", 6, 20)); err == nil {
		t.Error("a client-only adapter was accepted")
	}
	// Without iw nothing is known, the start decides
	unknown := sys.WiFiAdapter{Name: "wl0"}
	if err := checkAdapter([]sys.WiFiAdapter{unknown}, w("5", 36, 80)); err != nil {
		t.Error(err)
	}
}

func TestCountries(t *testing.T) {
	dir := t.TempDir()
	iso := filepath.Join(dir, "iso")
	os.WriteFile(iso, []byte("# comment\nDE\tGermany\nAT\tAustria\n"), 0o644)
	if got := countries(iso); len(got) != 2 || got[0].Code != "AT" || got[1].Name != "Germany" {
		t.Errorf("%+v", got)
	}
	zone := filepath.Join(dir, "zone")
	os.WriteFile(zone, []byte("# comment\nDE,DK,NO\t+5230+01322\tEurope/Berlin\tcomment\nAT\t+4813+01620\tEurope/Vienna\n"), 0o644)
	if got := countryOfZone(zone, "Europe/Berlin"); got != "DE" {
		t.Error(got)
	}
	if got := countryOfZone(zone, "UTC"); got != "" {
		t.Error(got)
	}
}
