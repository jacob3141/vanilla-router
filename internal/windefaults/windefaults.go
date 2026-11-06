// SPDX-License-Identifier: AGPL-3.0-or-later

// Package windefaults translates settings from Windows into defaults for vr/stick.json
// (F-STICK-07, PLAN.md 3.12): language, keyboard layout and time zone. The package is platform-independent,
// so it can be tested on Linux. cmd/vr-stick reads the values from Windows.
//
// The time zone table windowsZones.xml comes from Unicode CLDR
// (github.com/unicode-org/cldr, common/supplemental/windowsZones.xml), for the terms of use see
// https://www.unicode.org/copyright.html (Unicode License v3).
package windefaults

import (
	_ "embed"
	"encoding/xml"
	"regexp"
	"strings"
	"sync"

	"vanilla-router/internal/stick"
)

//go:embed windowsZones.xml
var zonesXML []byte

var (
	zonesOnce sync.Once
	zones     map[string]string
)

func loadZones() {
	var doc struct {
		Zones []struct {
			Other     string `xml:"other,attr"`
			Territory string `xml:"territory,attr"`
			Type      string `xml:"type,attr"`
		} `xml:"windowsZones>mapTimezones>mapZone"`
	}
	zones = map[string]string{}
	if err := xml.Unmarshal(zonesXML, &doc); err != nil {
		return
	}
	for _, z := range doc.Zones {
		// Territory 001 is the main zone for a Windows name, Type can contain several names.
		if z.Territory == "001" {
			if f := strings.Fields(z.Type); len(f) > 0 {
				zones[z.Other] = f[0]
			}
		}
	}
}

// Timezone returns the IANA zone for the Windows name (e.g. "W. Europe Standard Time" → Europe/Berlin)
// or "" if the name is unknown.
func Timezone(windows string) string {
	zonesOnce.Do(loadZones)
	return zones[windows]
}

var bcp47Re = regexp.MustCompile(`^([a-z]{2,3})(?:-[A-Z][a-z]{3})?-([A-Z]{2})$`)

// Locale turns a Windows locale (BCP 47, e.g. de-AT) into a Linux locale (de_AT.UTF-8).
// Unknown forms result in "".
func Locale(windows string) string {
	m := bcp47Re.FindStringSubmatch(windows)
	if m == nil {
		return ""
	}
	return m[1] + "_" + m[2] + ".UTF-8"
}

// keyboards assigns Windows keyboard layouts (the lower 16 bits of the KLID, i.e. the language ID) to an
// XKB layout (/etc/default/keyboard, XKBLAYOUT). Only common layouts, otherwise no default.
var keyboards = map[uint16]string{
	0x0407: "de", 0x0c07: "at", 0x1007: "de", 0x1407: "de", 0x0807: "ch", 0x100c: "ch",
	0x0409: "us", 0x0809: "gb", 0x1809: "ie", 0x040c: "fr", 0x080c: "be", 0x0813: "be",
	0x0410: "it", 0x040a: "es", 0x0c0a: "es", 0x0413: "nl", 0x0415: "pl", 0x0405: "cz",
	0x041b: "sk", 0x040e: "hu", 0x041d: "se", 0x0406: "dk", 0x0414: "no", 0x040b: "fi",
	0x0816: "pt", 0x0416: "br", 0x0419: "ru", 0x0422: "ua", 0x041f: "tr", 0x0408: "gr",
}

// Keymap returns the XKB layout for a Windows KLID like "00000407" or "" (unknown).
// Variants (upper 16 bits, e.g. Dvorak) are not taken over, because they cannot be mapped reliably.
func Keymap(klid string) string {
	if len(klid) != 8 {
		return ""
	}
	var v uint32
	for _, c := range strings.ToLower(klid) {
		switch {
		case c >= '0' && c <= '9':
			v = v<<4 | uint32(c-'0')
		case c >= 'a' && c <= 'f':
			v = v<<4 | uint32(c-'a'+10)
		default:
			return ""
		}
	}
	if v>>16 != 0 {
		return ""
	}
	return keyboards[uint16(v)]
}

// Info builds the defaults for vr/stick.json. Missing values stay empty, the assistant then takes
// its own default.
func Info(locale, klid, windowsZone string) stick.Info {
	return stick.Info{Schema: 1, Locale: Locale(locale), Keymap: Keymap(klid), Timezone: Timezone(windowsZone)}
}
