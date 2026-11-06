// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"strings"
	"testing"
)

func withWiFi(mod func(w *WiFi)) func(c *Config) {
	return func(c *Config) {
		c.WiFi = &WiFi{Enabled: true, Interface: "wlp3s0", SSID: "Home", PasswordRef: "wifi", Country: "DE", Band: WiFiBand5, Channel: 36, Width: 80}
		mod(c.WiFi)
	}
}

func TestWiFiValid(t *testing.T) {
	c := testConfig(t)
	withWiFi(func(*WiFi) {})(c)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	withWiFi(func(w *WiFi) { w.SSID = "Küche WLAN"; w.Band, w.Channel, w.Width = WiFiBand24, 13, 20 })(c)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWiFiErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		mod   func(w *WiFi)
		field string
	}{
		"no adapter":       {func(w *WiFi) { w.Interface = "" }, "wifi.interface"},
		"path as adapter":  {func(w *WiFi) { w.Interface = "../x" }, "wifi.interface"},
		"line break":       {func(w *WiFi) { w.Interface = "wl\nbridge=x" }, "wifi.interface"},
		"internet port":    {func(w *WiFi) { w.Interface = "wan0" }, "wifi.interface"},
		"bridge":           {func(w *WiFi) { w.Interface = "lan0" }, "wifi.interface"},
		"home network":     {func(w *WiFi) { w.Interface = "lanp1" }, "wifi.interface"},
		"empty name":       {func(w *WiFi) { w.SSID = "" }, "wifi.ssid"},
		"long name":        {func(w *WiFi) { w.SSID = strings.Repeat("x", 33) }, "wifi.ssid"},
		"long in bytes":    {func(w *WiFi) { w.SSID = strings.Repeat("ü", 17) }, "wifi.ssid"},
		"injection":        {func(w *WiFi) { w.SSID = "a\nwpa=0" }, "wifi.ssid"},
		"spaces around":    {func(w *WiFi) { w.SSID = " a " }, "wifi.ssid"},
		"bad reference":    {func(w *WiFi) { w.PasswordRef = "../x" }, "wifi.password_ref"},
		"lowercase land":   {func(w *WiFi) { w.Country = "de" }, "wifi.country"},
		"no country":       {func(w *WiFi) { w.Country = "" }, "wifi.country"},
		"unknown band":     {func(w *WiFi) { w.Band = "6" }, "wifi.band"},
		"channel of 5 GHz": {func(w *WiFi) { w.Band, w.Channel, w.Width = WiFiBand24, 36, 20 }, "wifi.channel"},
		"DFS channel":      {func(w *WiFi) { w.Channel = 100 }, "wifi.channel"},
		"channel 14":       {func(w *WiFi) { w.Band, w.Channel, w.Width = WiFiBand24, 14, 20 }, "wifi.channel"},
		"wide on 2.4 GHz":  {func(w *WiFi) { w.Band, w.Channel, w.Width = WiFiBand24, 6, 40 }, "wifi.width"},
		"wide on 165":      {func(w *WiFi) { w.Channel, w.Width = 165, 40 }, "wifi.width"},
		"unknown width":    {func(w *WiFi) { w.Width = 160 }, "wifi.width"},
	} {
		expectErr(t, name, withWiFi(tc.mod), tc.field)
	}
}

func TestWiFiPassword(t *testing.T) {
	for pw, want := range map[string]bool{
		"12345678":              true,
		"with spaces and #=\"":  true,
		strings.Repeat("a", 63): true,
		"1234567":               false,
		strings.Repeat("a", 64): false,
		"passwört1":             false,
		"line\nbreak":           false,
	} {
		if got := ValidWiFiPassword(pw); got != want {
			t.Errorf("%q: %v", pw, got)
		}
	}
}
