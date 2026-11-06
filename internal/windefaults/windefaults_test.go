// SPDX-License-Identifier: AGPL-3.0-or-later

package windefaults

import "testing"

func TestTimezone(t *testing.T) {
	for win, want := range map[string]string{
		"W. Europe Standard Time":      "Europe/Berlin",
		"Romance Standard Time":        "Europe/Paris",
		"GMT Standard Time":            "Europe/London",
		"Pacific Standard Time":        "America/Los_Angeles",
		"UTC":                          "Etc/UTC",
		"unknown zone":                 "",
		"":                             "",
		"Central Europe Standard Time": "Europe/Budapest",
	} {
		if got := Timezone(win); got != want {
			t.Errorf("Timezone(%q) = %q, expected %q", win, got, want)
		}
	}
	if n := len(zones); n < 100 {
		t.Fatalf("only %d zones loaded", n)
	}
}

func TestLocale(t *testing.T) {
	for win, want := range map[string]string{
		"de-DE": "de_DE.UTF-8", "de-AT": "de_AT.UTF-8", "en-US": "en_US.UTF-8",
		"sr-Latn-RS": "sr_RS.UTF-8", "de": "", "x; rm": "", "": "", "de-de": "",
	} {
		if got := Locale(win); got != want {
			t.Errorf("Locale(%q) = %q, expected %q", win, got, want)
		}
	}
}

func TestKeymap(t *testing.T) {
	for klid, want := range map[string]string{
		"00000407": "de", "00000807": "ch", "00000409": "us", "00000c07": "at", "00000C07": "at",
		"00010409": "", "0407": "", "0000040g": "", "00000999": "",
	} {
		if got := Keymap(klid); got != want {
			t.Errorf("Keymap(%q) = %q, expected %q", klid, got, want)
		}
	}
}

func TestInfo(t *testing.T) {
	in := Info("de-AT", "00000407", "W. Europe Standard Time")
	if in.Schema != 1 || in.Locale != "de_AT.UTF-8" || in.Keymap != "de" || in.Timezone != "Europe/Berlin" {
		t.Fatalf("%+v", in)
	}
}
