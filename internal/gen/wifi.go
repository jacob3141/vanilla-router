// SPDX-License-Identifier: AGPL-3.0-or-later

package gen

import (
	"fmt"
	"strings"

	"vanilla-router/internal/config"
)

// WiFiConf is the configuration of hostapd, it contains the password (0600).
const WiFiConf = "/etc/vanilla-router/wifi/hostapd.conf"

// WiFiOn says whether an access point is to run.
func WiFiOn(c *config.Config) bool { return c != nil && c.WiFi != nil && c.WiFi.Enabled }

// wifiFiles generates hostapd.conf and leaves the adapter alone in networkd (F-WIFI-01). A switched-off
// access point has no file at all, so no password stays in a generated file.
//
// hostapd itself puts the adapter into the bridge: the kernel only allows that once the adapter is in
// access point mode, which a .network file with Bridge= could not wait for. Clients then appear on lan0
// like any device on a cable, so DHCP, DNS and firewall need no change.
func wifiFiles(c *config.Config, sec Secrets) ([]File, error) {
	if !WiFiOn(c) {
		return nil, nil
	}
	w := c.WiFi
	if sec == nil {
		return nil, fmt.Errorf("Wi-Fi password %q: no secrets available", w.PasswordRef)
	}
	pw, err := sec.Get(w.PasswordRef)
	if err != nil {
		return nil, fmt.Errorf("Wi-Fi password %q: %w", w.PasswordRef, err)
	}
	if !config.ValidWiFiPassword(pw) {
		return nil, fmt.Errorf("The Wi-Fi password needs 8 to 63 characters, without umlauts or other special letters")
	}

	var b strings.Builder
	b.WriteString(header("#"))
	kv := func(k, v string) { fmt.Fprintf(&b, "%s=%s\n", k, v) }
	kv("interface", w.Interface)
	kv("bridge", LANIf)
	kv("driver", "nl80211")
	kv("ssid", w.SSID)
	kv("utf8_ssid", "1")
	kv("country_code", w.Country)
	kv("ieee80211d", "1")
	kv("hw_mode", map[string]string{config.WiFiBand24: "g", config.WiFiBand5: "a"}[w.Band])
	kv("channel", fmt.Sprint(w.Channel))
	kv("wmm_enabled", "1")
	kv("ieee80211n", "1")
	if w.Width >= 40 {
		kv("ht_capab", ht40(w.Channel))
	}
	if w.Width == 80 {
		kv("ieee80211ac", "1")
		kv("vht_oper_chwidth", "1")
		kv("vht_oper_centr_freq_seg0_idx", fmt.Sprint(vht80Center(w.Channel)))
	}
	// WPA2 and WPA3 at once (F-WIFI-08). Protected management frames are optional for WPA2 devices and
	// mandatory for WPA3, as the standard requires. Old TKIP is deliberately not offered.
	kv("auth_algs", "1")
	kv("wpa", "2")
	kv("wpa_key_mgmt", "WPA-PSK SAE")
	kv("rsn_pairwise", "CCMP")
	kv("ieee80211w", "1")
	kv("sae_require_mfp", "1")
	kv("wpa_passphrase", pw)

	un := &ini{}
	un.section("Match")
	un.kv("Name", w.Interface)
	un.section("Link")
	un.kv("Unmanaged", "yes")
	return []File{
		{Path: WiFiConf, Mode: 0o600, Group: GroupWiFi, Content: []byte(b.String())},
		{Path: networkDir + "30-vr-wifi.network", Mode: 0o644, Group: GroupNetworkd, Content: []byte(header("#") + un.String())},
	}, nil
}

// ht40 says on which side of a 5 GHz channel its 40 MHz partner lies: the channels form the pairs
// 36/40, 44/48, 149/153 and 157/161.
func ht40(channel int) string {
	first := 36
	if channel > 100 {
		first = 149
	}
	if (channel-first)/4%2 == 0 {
		return "[HT40+]"
	}
	return "[HT40-]"
}

// vht80Center is the center channel of the 80 MHz block a channel belongs to.
func vht80Center(channel int) int {
	if channel < 100 {
		return 42
	}
	return 155
}
