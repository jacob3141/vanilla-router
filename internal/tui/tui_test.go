// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func prompt(input string) (*Prompt, *bytes.Buffer) {
	var out bytes.Buffer
	return New(strings.NewReader(input), &out, -1), &out
}

func TestAskVorgabeUndPruefung(t *testing.T) {
	p, out := prompt("\nFALSCH\nrichtig\n")
	s, err := p.Ask("Name", "router", nil)
	if err != nil || s != "router" {
		t.Fatal(s, err)
	}
	s, err = p.Ask("Name", "", func(v string) error {
		if v != strings.ToLower(v) {
			return errors.New("nur Kleinbuchstaben")
		}
		return nil
	})
	if err != nil || s != "richtig" || !strings.Contains(out.String(), "nur Kleinbuchstaben") {
		t.Fatal(s, err, out.String())
	}
}

func TestChoose(t *testing.T) {
	p, out := prompt("7\n2\n\n")
	n, err := p.Choose("Welche?", []string{"a", "b"}, 0)
	if err != nil || n != 1 || !strings.Contains(out.String(), "Bitte eine Zahl von 1 bis 2") {
		t.Fatal(n, err)
	}
	if n, _ := p.Choose("Welche?", []string{"a", "b"}, 1); n != 1 {
		t.Error("Vorgabe")
	}
}

func TestConfirmUndYesNo(t *testing.T) {
	p, _ := prompt("ja\nLÖSCHEN\nvielleicht\nn\n")
	if ok, _ := p.Confirm("Wirklich?", "LÖSCHEN"); ok {
		t.Error("falsches Wort angenommen")
	}
	if ok, _ := p.Confirm("Wirklich?", "LÖSCHEN"); !ok {
		t.Error("richtiges Wort abgelehnt")
	}
	if ok, _ := p.YesNo("Weiter?", true); ok {
		t.Error("n nicht erkannt")
	}
}

func TestEOF(t *testing.T) {
	p, _ := prompt("")
	if _, err := p.Ask("x", "", nil); err != ErrEOF {
		t.Error(err)
	}
}
