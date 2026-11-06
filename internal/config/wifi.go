// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Wi-Fi bands. The values are also the labels in the web interface.
const (
	WiFiBand24 = "2.4"
	WiFiBand5  = "5"
)

// WiFi is an access point on an adapter in the router (F-WIFI, D-18). The block is optional, without it
// nothing Wi-Fi related is generated. The password is under secrets/<PasswordRef> (0600).
type WiFi struct {
	Enabled bool `json:"enabled"`
	// Interface is the adapter's kernel name, e.g. wlp3s0. It is stable (bus position or MAC in the
	// name) and, unlike a name from a .link file, needs no restart (D-18).
	Interface   string `json:"interface"`
	SSID        string `json:"ssid"`
	PasswordRef string `json:"password_ref"`
	Country     string `json:"country"` // ISO 3166-1 alpha-2: decides which channels are legal
	Band        string `json:"band"`    // 2.4 or 5
	Channel     int    `json:"channel"`
	Width       int    `json:"width"` // channel width in MHz: 20, 40 or 80
}

var (
	ifaceRe   = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,15}$`)
	countryRe = regexp.MustCompile(`^[A-Z]{2}$`)
)

// WiFiChannels lists the channels of a band. Deliberately without DFS channels (5 GHz, 52–144): they need
// radar detection and cannot be tested without hardware (F-WIFI-09). Channel 14 is only legal in Japan.
func WiFiChannels(band string) []int {
	switch band {
	case WiFiBand24:
		return []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	case WiFiBand5:
		return []int{36, 40, 44, 48, 149, 153, 157, 161, 165}
	}
	return nil
}

// WiFiWidths lists the channel widths possible on a channel. 2.4 GHz stays at 20 MHz, wider channels
// disturb the neighbors there and barely help. Channel 165 has no partner for a wider channel.
func WiFiWidths(band string, channel int) []int {
	if band != WiFiBand5 || channel == 165 {
		return []int{20}
	}
	return []int{20, 40, 80}
}

// ValidWiFiPassword checks a WPA passphrase: 8 to 63 printable ASCII characters.
func ValidWiFiPassword(s string) bool {
	if len(s) < 8 || len(s) > 63 {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

// ValidSSID checks a network name: 1 to 32 bytes of UTF-8 without control characters and without spaces at
// the start or end, which are invisible in every device list.
func ValidSSID(s string) bool {
	if s == "" || len(s) > 32 || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (c *Config) validateWiFi(v *validator) {
	w := c.WiFi
	if w == nil {
		return
	}
	// The name ends up in a generated file and must not collide with the fixed names (PLAN.md 3.4).
	if !ifaceRe.MatchString(w.Interface) {
		v.add("wifi.interface", "Please choose a Wi-Fi adapter")
	} else if w.Interface == "wan0" || w.Interface == "lan0" || strings.HasPrefix(w.Interface, "lanp") || strings.HasPrefix(w.Interface, "ppp") {
		v.add("wifi.interface", "%s is not a Wi-Fi adapter", w.Interface)
	}
	if !ValidSSID(w.SSID) {
		v.add("wifi.ssid", "The network name needs 1 to 32 characters, without control characters and without spaces at the start or end")
	}
	if !refRe.MatchString(w.PasswordRef) {
		v.add("wifi.password_ref", "invalid reference %q", w.PasswordRef)
	}
	if !countryRe.MatchString(w.Country) {
		v.add("wifi.country", "Please choose the country the router is in")
	}
	channels := WiFiChannels(w.Band)
	if channels == nil {
		v.add("wifi.band", "Please choose 2.4 or 5 GHz")
		return
	}
	if !slices.Contains(channels, w.Channel) {
		v.add("wifi.channel", "Channel %d does not exist in the %s GHz band", w.Channel, w.Band)
		return
	}
	if !slices.Contains(WiFiWidths(w.Band, w.Channel), w.Width) {
		v.add("wifi.width", "Channel %d cannot use a width of %d MHz", w.Channel, w.Width)
	}
}
