// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vanilla-router/internal/config"
)

// newDryServer legt einen Helper im Probelauf mit einer Vorgabe-Konfiguration unter einem Testordner an.
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

func TestSettingsTelekomMitBestaetigung(t *testing.T) {
	s := newDryServer(t, func(c *config.Config) { c.WAN.Detected = &config.Detected{Method: config.WANPPPoE, VLAN: 7} })
	info, _ := call(t, s, "settings.get", nil)
	if info["needs_setup"] != true {
		t.Fatal("erkanntes PPPoE muss die Einrichtung verlangen")
	}
	if _, err := call(t, s, "settings.wan.set", map[string]any{"type": "pppoe", "ipv6": true,
		"telekom": map[string]string{"anschlusskennung": "001234567890", "zugangsnummer": "551234567890", "mitbenutzer": "0001"}}); err == nil ||
		!strings.Contains(err.Error(), "Passwort") {
		t.Fatalf("ohne Passwort muss es scheitern: %v", err)
	}
	res, err := call(t, s, "settings.wan.set", map[string]any{"type": "pppoe", "ipv6": true, "pppoe_password": "geheim",
		"telekom": map[string]string{"anschlusskennung": "001234567890", "zugangsnummer": "551234567890", "mitbenutzer": "0001"}})
	if err != nil {
		t.Fatal(err)
	}
	if res["pending"] == nil {
		t.Error("WAN-Änderung muss bestätigt werden")
	}
	c := loadCfg(t, s)
	if c.WAN.Type != "pppoe" || c.WAN.PPPoE.User != "0012345678905512345678900001@t-online.de" || c.WAN.PPPoE.VLAN != 7 || c.WAN.Detected != nil {
		t.Fatalf("WAN: %+v %+v", c.WAN, c.WAN.PPPoE)
	}
	fi, err := os.Stat(filepath.Join(s.secrets().Dir, c.WAN.PPPoE.PasswordRef))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("Passwortdatei: %v %v", fi, err)
	}
	info, _ = call(t, s, "settings.get", nil)
	if info["needs_setup"] != false || info["wan"].(map[string]any)["has_password"] != true {
		t.Errorf("nach dem Speichern: %v", info)
	}
	if _, err := call(t, s, "settings.confirm", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, s, "settings.confirm", nil); err == nil {
		t.Error("zweite Bestätigung ohne wartende Änderung muss scheitern")
	}
	// Ohne neues Passwort bleibt das alte
	if _, err := call(t, s, "settings.wan.set", map[string]any{"type": "pppoe", "pppoe_user": "kunde@example.org", "pppoe_vlan": 0}); err != nil {
		t.Fatal(err)
	}
	if c := loadCfg(t, s); c.WAN.PPPoE.User != "kunde@example.org" || c.WAN.PPPoE.VLAN != 0 {
		t.Errorf("zweite Änderung: %+v", c.WAN.PPPoE)
	}
}

func TestSettingsLANUmzugUndRuecknahme(t *testing.T) {
	s := newDryServer(t, func(c *config.Config) {
		c.DHCP.Reservations = []config.Reservation{{Name: "drucker", MAC: "02:00:00:00:00:10", IP: "192.168.42.20"}}
	})
	res, err := call(t, s, "settings.lan.set", map[string]any{"address": "10.20.30.1", "prefix": 24})
	if err != nil {
		t.Fatal(err)
	}
	if res["new_url"] != "https://10.20.30.1/" || res["restart"] != true || res["pending"] == nil {
		t.Errorf("Antwort: %v", res)
	}
	if c := loadCfg(t, s); c.LAN.IPv4 != "10.20.30.1/24" || c.DHCP.Reservations[0].IP != "10.20.30.20" {
		t.Errorf("umgezogen: %s %v", c.LAN.IPv4, c.DHCP.Reservations)
	}
	// Solange die Änderung wartet, geht keine andere durch
	if _, err := call(t, s, "settings.dhcp.set", map[string]any{"pool_start": "10.20.30.50", "pool_end": "10.20.30.60", "lease": "1h"}); err == nil ||
		!strings.Contains(err.Error(), "Bestätigung") {
		t.Errorf("Änderung während offener Bestätigung: %v", err)
	}
	if _, err := call(t, s, "settings.revert", nil); err != nil {
		t.Fatal(err)
	}
	if c := loadCfg(t, s); c.LAN.IPv4 != "192.168.42.1/24" || c.DHCP.Reservations[0].IP != "192.168.42.20" {
		t.Errorf("zurückgenommen: %s %v", c.LAN.IPv4, c.DHCP.Reservations)
	}
	if _, err := call(t, s, "settings.lan.set", map[string]any{"address": "8.8.8.1", "prefix": 24}); err == nil {
		t.Error("öffentliches Netz akzeptiert")
	}
}

func TestSettingsSystemUndDHCP(t *testing.T) {
	s := newDryServer(t, nil)
	if _, err := call(t, s, "settings.system.set", map[string]any{"hostname": "gateway", "timezone": "../../etc/passwd"}); err == nil {
		t.Error("Pfad als Zeitzone akzeptiert")
	}
	if _, err := call(t, s, "settings.system.set", map[string]any{"hostname": "Gate Way", "timezone": "UTC"}); err == nil {
		t.Error("ungültiger Name akzeptiert")
	}
	res, err := call(t, s, "settings.system.set", map[string]any{"hostname": "Gateway", "timezone": "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if res["pending"] != nil || res["new_url"] != "https://gateway.home.arpa/" {
		t.Errorf("Antwort: %v", res)
	}
	if c := loadCfg(t, s); c.System.Hostname != "gateway" || c.System.Timezone != "UTC" {
		t.Errorf("System: %+v", c.System)
	}
	if _, err := call(t, s, "settings.dhcp.set", map[string]any{"pool_start": "192.168.42.50", "pool_end": "192.168.42.40", "lease": "1h"}); err == nil {
		t.Error("Ende vor Beginn akzeptiert")
	}
	if _, err := call(t, s, "settings.dhcp.set", map[string]any{"pool_start": "192.168.42.50", "pool_end": "192.168.42.90", "lease": "1h"}); err != nil {
		t.Fatal(err)
	}
}
