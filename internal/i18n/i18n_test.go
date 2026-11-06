// SPDX-License-Identifier: AGPL-3.0-or-later

package i18n

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEnglishIsSource(t *testing.T) {
	SetLocale("en_US.UTF-8")
	if got := T("Welcome!"); got != "Welcome!" {
		t.Error(got)
	}
}

func TestGerman(t *testing.T) {
	SetLocale("de_AT.UTF-8")
	defer SetLocale("")
	for in, want := range map[string]string{
		"Welcome!":                          "Willkommen!",
		"  Press Enter to start over.\n":    "  Drücken Sie Enter, um von vorn zu beginnen.\n",
		"Port 2":                            "Anschluss 2",
		"Port 1, Port 2":                    "Anschluss 1, Anschluss 2",
		"Port 2 is now your internet port.": "Anschluss 2 ist jetzt Ihr Internet-Anschluss.",
		"Hard disk VBOX (34 GB)":            "Festplatte VBOX (34 GB)",
		"Internet port: Port 1\nHome network: Port 2\n": "Internet-Anschluss: Anschluss 1\nHeimnetz: Anschluss 2\n",
		"not in the catalog":                            "not in the catalog",
		"lan.ipv4: address is missing":                  "lan.ipv4: Adresse fehlt",
		"The change was not applied: invalid":           "Die Änderung wurde nicht übernommen: ungültig",
	} {
		if got := T(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// Every value must have as many placeholders as its key, otherwise values get lost.
func TestPlaceholders(t *testing.T) {
	raw := map[string]string{}
	if err := json.Unmarshal(deJSON, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) < 500 {
		t.Fatalf("only %d entries", len(raw))
	}
	for k, v := range raw {
		if strings.Count(k, "{}") != strings.Count(v, "{}") {
			t.Errorf("%q → %q", k, v)
		}
	}
}
