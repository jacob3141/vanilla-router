// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func prompt(input string) (*Prompt, *bytes.Buffer) {
	var out bytes.Buffer
	return New(strings.NewReader(input), &out, true), &out
}

const (
	up   = "\x1b[A"
	down = "\x1b[B"
)

func TestMenuPfeiltasten(t *testing.T) {
	p, out := prompt(down + down + down + up + "\n")
	n, err := p.Menu([]string{"a", "b", "c"}, 0)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if !strings.Contains(out.String(), "→ b") {
		t.Error(out.String())
	}
}

func TestMenuVorgabeUndAndereTasten(t *testing.T) {
	// Buchstaben und unbekannte Folgen werden ignoriert, Enter nimmt die Vorauswahl
	p, _ := prompt("x\x1b[Cj\n")
	if n, err := p.Menu([]string{"a", "b"}, 1); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	// Anwendungsmodus der Pfeiltasten (ESC O A)
	p, _ = prompt("\x1bOB\r")
	if n, _ := p.Menu([]string{"a", "b"}, 0); n != 1 {
		t.Error("ESC O B nicht erkannt")
	}
}

func TestMenuAsyncAbbrechen(t *testing.T) {
	r, w := io.Pipe()
	var out bytes.Buffer
	p := New(r, &out, true)
	ch, cancel := p.MenuAsync([]string{"a", "b"}, 0)
	cancel()
	cancel() // zweimal ist harmlos
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("abgebrochenes Menü lieferte eine Wahl")
		}
	case <-time.After(time.Second):
		t.Fatal("Menü nicht beendet")
	}
	// Danach gehören die Tasten dem nächsten Menü
	go w.Write([]byte(down + "\n"))
	if n, err := p.Menu([]string{"x", "y"}, 0); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}

func TestBanner(t *testing.T) {
	p, out := prompt("")
	p.Banner("vanilla-router 1.0", "Willkommen!")
	want := "\n" +
		"      .-~~~-.\n" +
		"     (       )    vanilla-router 1.0\n" +
		"    (_._._._._)   Willkommen!\n" +
		"     \\/\\/\\/\\/\n" +
		"      \\/\\/\\/\n" +
		"       \\/\\/\n" +
		"        \\/\n\n"
	if out.String() != want {
		t.Errorf("\n%s", out.String())
	}
	// Mit Farben: gleicher Text, sobald die Steuerfolgen entfernt sind
	var col bytes.Buffer
	c := New(strings.NewReader(""), &col, false)
	c.Banner("vanilla-router 1.0", "Willkommen!")
	c.Heading("Festplatte")
	plain := col.String()
	for _, s := range []string{brown, vanilla, bold, reset} {
		plain = strings.ReplaceAll(plain, s, "")
	}
	if !strings.HasPrefix(plain, want) || !strings.Contains(plain, "━━━ Festplatte ━━━") || !strings.Contains(col.String(), brown) {
		t.Errorf("%q", col.String())
	}
}

func TestEOF(t *testing.T) {
	p, _ := prompt("")
	if _, err := p.Menu([]string{"a"}, 0); err != ErrEOF {
		t.Error(err)
	}
	if err := p.WaitEnter("weiter"); err != ErrEOF {
		t.Error(err)
	}
}
