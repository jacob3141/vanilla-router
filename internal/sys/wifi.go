// SPDX-License-Identifier: AGPL-3.0-or-later

package sys

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Wi-Fi hardware from sysfs and the output of iw (F-WIFI). root is "/" in production and a test folder in tests.
// Reading sysfs works without privileges, so the web server can decide whether to show the page at all.

// WiFiAdapter is a Wi-Fi interface. What it can do comes from `iw phy` (ParseIwPhy), which needs the package iw.
type WiFiAdapter struct {
	Name   string `json:"name"`
	Phy    string `json:"phy"`
	MAC    string `json:"mac"`
	Driver string `json:"driver"`
	// USB adapters can be unplugged, so the page says which kind it is.
	USB bool `json:"usb"`
	WiFiCaps
}

// WiFiCaps is what `iw phy` reports.
type WiFiCaps struct {
	Known bool       `json:"known"` // false: iw was not available, nothing is known about the abilities
	AP    bool       `json:"ap"`    // can be an access point
	Bands []WiFiBand `json:"bands"`
}

// WiFiBand is a frequency band of an adapter.
type WiFiBand struct {
	Band     string        `json:"band"` // 2.4 or 5
	HT40     bool          `json:"ht40"`
	VHT      bool          `json:"vht"`
	Channels []WiFiChannel `json:"channels"`
}

// WiFiChannel is a channel of an adapter under the current country setting.
type WiFiChannel struct {
	Channel  int  `json:"channel"`
	Disabled bool `json:"disabled"` // not allowed at all
	// NoIR: the adapter may not start a network here. With the "world" setting before the first start
	// that applies to much of 5 GHz and can go away with the right country. Some cards always refuse.
	NoIR  bool `json:"no_ir"`
	Radar bool `json:"radar"` // DFS channel
}

// WiFiStation is a device connected to the access point.
type WiFiStation struct {
	MAC        string  `json:"mac"`
	SignalDBm  int     `json:"signal_dbm"`
	HasSignal  bool    `json:"has_signal"`
	RxBitrate  float64 `json:"rx_mbit"`
	TxBitrate  float64 `json:"tx_mbit"`
	Connected  int64   `json:"connected_seconds"`
	InactiveMS int64   `json:"inactive_ms"`
}

// WiFiLink is what `iw dev <name> info` reports about the running interface.
type WiFiLink struct {
	AP      bool   `json:"ap"`
	SSID    string `json:"ssid"`
	Channel int    `json:"channel"`
	Width   int    `json:"width"`
}

// WiFiAdapters lists the Wi-Fi interfaces, sorted by name.
func WiFiAdapters(root string) []WiFiAdapter {
	dir := filepath.Join(root, "sys/class/net")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []WiFiAdapter
	for _, e := range ents {
		d := filepath.Join(dir, e.Name())
		phy, err := os.ReadFile(filepath.Join(d, "phy80211", "name"))
		if err != nil {
			continue // no wireless device, or a virtual one
		}
		a := WiFiAdapter{Name: e.Name(), Phy: strings.TrimSpace(string(phy))}
		if b, err := os.ReadFile(filepath.Join(d, "address")); err == nil {
			a.MAC = strings.TrimSpace(string(b))
		}
		if l, err := os.Readlink(filepath.Join(d, "device", "driver")); err == nil {
			a.Driver = filepath.Base(l)
		}
		if l, err := filepath.EvalSymlinks(filepath.Join(d, "device")); err == nil {
			a.USB = strings.Contains(filepath.ToSlash(l), "/usb")
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// WiFiCardWithoutDriver reports a PCI Wi-Fi card (class 0x0280) that has no wireless device. That is the normal
// case on a freshly installed router: the image only keeps the firmware of network cards, and vrctl complete
// brings back the rest (D-17). USB adapters cannot be told apart that simply, they stay invisible until they work.
func WiFiCardWithoutDriver(root string) bool {
	if phys, err := os.ReadDir(filepath.Join(root, "sys/class/ieee80211")); err == nil && len(phys) > 0 {
		return false
	}
	cards, err := filepath.Glob(filepath.Join(root, "sys/bus/pci/devices/*/class"))
	if err != nil {
		return false
	}
	for _, c := range cards {
		if b, err := os.ReadFile(c); err == nil && strings.HasPrefix(string(b), "0x0280") {
			return true
		}
	}
	return false
}

// WiFiPresent says whether the page makes sense: an adapter works or a card only lacks its firmware.
func WiFiPresent(root string) bool {
	if phys, err := os.ReadDir(filepath.Join(root, "sys/class/ieee80211")); err == nil && len(phys) > 0 {
		return true
	}
	return WiFiCardWithoutDriver(root)
}

var (
	iwFreqRe  = regexp.MustCompile(`^\s*\*\s+(\d+)(?:\.\d+)?\s+MHz\s+\[(\d+)\](.*)$`)
	iwWidthRe = regexp.MustCompile(`width:\s*(\d+)`)
	iwChanRe  = regexp.MustCompile(`^\s*channel\s+(\d+)\b`)
)

func bandOf(freq int) string {
	switch {
	case freq >= 2400 && freq < 2500:
		return "2.4"
	case freq >= 5000 && freq < 5900:
		return "5"
	}
	return "" // 6 GHz and others are not offered
}

// ParseIwPhy reads the output of `iw phy <name> info`. Evaluate with LC_ALL=C (L-17): iw has no translations
// today, but the check below fails on missing data instead of claiming an ability.
func ParseIwPhy(out string) WiFiCaps {
	caps := WiFiCaps{Known: strings.Contains(out, "Wiphy ")}
	var cur *WiFiBand
	flush := func() {
		if cur != nil && cur.Band != "" {
			caps.Bands = append(caps.Bands, *cur)
		}
		cur = nil
	}
	modes := false
	for _, line := range strings.Split(out, "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "Band ") && strings.HasSuffix(trim, ":"):
			flush()
			cur = &WiFiBand{}
			modes = false
			continue
		case trim == "Supported interface modes:":
			flush()
			modes = true
			continue
		}
		if modes {
			if !strings.HasPrefix(trim, "*") {
				modes = false
			} else if strings.TrimSpace(strings.TrimPrefix(trim, "*")) == "AP" {
				caps.AP = true
			}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.Contains(trim, "HT20/HT40") {
			cur.HT40 = true
		}
		if strings.HasPrefix(trim, "VHT Capabilities") {
			cur.VHT = true
		}
		if m := iwFreqRe.FindStringSubmatch(line); m != nil {
			freq, _ := strconv.Atoi(m[1])
			b := bandOf(freq)
			if b == "" {
				continue
			}
			cur.Band = b
			ch, _ := strconv.Atoi(m[2])
			cur.Channels = append(cur.Channels, WiFiChannel{Channel: ch,
				Disabled: strings.Contains(m[3], "disabled"),
				NoIR:     strings.Contains(m[3], "no IR") || strings.Contains(m[3], "passive scan"),
				Radar:    strings.Contains(m[3], "radar")})
		}
	}
	flush()
	return caps
}

// ParseIwLink reads the output of `iw dev <name> info`.
func ParseIwLink(out string) WiFiLink {
	var l WiFiLink
	for _, line := range strings.Split(out, "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case trim == "type AP":
			l.AP = true
		case strings.HasPrefix(trim, "ssid "):
			l.SSID = strings.TrimPrefix(trim, "ssid ")
		}
		if m := iwChanRe.FindStringSubmatch(line); m != nil {
			l.Channel, _ = strconv.Atoi(m[1])
			if w := iwWidthRe.FindStringSubmatch(line); w != nil {
				l.Width, _ = strconv.Atoi(w[1])
			}
		}
	}
	return l
}

// ParseIwStations reads the output of `iw dev <name> station dump`.
func ParseIwStations(out string) []WiFiStation {
	stations := []WiFiStation{}
	var cur *WiFiStation
	num := func(s string) float64 {
		f := strings.Fields(s)
		if len(f) == 0 {
			return 0
		}
		v, _ := strconv.ParseFloat(f[0], 64)
		return v
	}
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "Station "); ok {
			if f := strings.Fields(rest); len(f) > 0 {
				stations = append(stations, WiFiStation{MAC: strings.ToLower(f[0])})
				cur = &stations[len(stations)-1]
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok || cur == nil {
			continue
		}
		switch strings.TrimSpace(k) {
		case "signal":
			cur.SignalDBm, cur.HasSignal = int(num(v)), true
		case "rx bitrate":
			cur.RxBitrate = num(v)
		case "tx bitrate":
			cur.TxBitrate = num(v)
		case "connected time":
			cur.Connected = int64(num(v))
		case "inactive time":
			cur.InactiveMS = int64(num(v))
		}
	}
	return stations
}
