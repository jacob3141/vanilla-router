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

func TestMenuArrowKeys(t *testing.T) {
	p, out := prompt(down + down + down + up + "\n")
	n, err := p.Menu([]string{"a", "b", "c"}, 0)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if !strings.Contains(out.String(), "→ b") {
		t.Error(out.String())
	}
}

func TestMenuDefaultAndOtherKeys(t *testing.T) {
	// Letters and unknown sequences are ignored, Enter takes the preselection
	p, _ := prompt("x\x1b[Cj\n")
	if n, err := p.Menu([]string{"a", "b"}, 1); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	// application mode of the arrow keys (ESC O A)
	p, _ = prompt("\x1bOB\r")
	if n, _ := p.Menu([]string{"a", "b"}, 0); n != 1 {
		t.Error("ESC O B not recognized")
	}
}

func TestMenuAsyncCancel(t *testing.T) {
	r, w := io.Pipe()
	var out bytes.Buffer
	p := New(r, &out, true)
	ch, cancel := p.MenuAsync([]string{"a", "b"}, 0)
	cancel()
	cancel() // twice does no harm
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("cancelled menu returned a choice")
		}
	case <-time.After(time.Second):
		t.Fatal("menu not ended")
	}
	// After that the keys belong to the next menu
	go w.Write([]byte(down + "\n"))
	if n, err := p.Menu([]string{"x", "y"}, 0); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}

func TestBanner(t *testing.T) {
	p, out := prompt("")
	p.Banner("vanilla-router 1.0", "Welcome!")
	want := "\n" +
		"     .---.\n" +
		"    (     )   vanilla-router 1.0\n" +
		"     '---'    Welcome!\n" +
		"    \\     /\n" +
		"     \\   /\n" +
		"      \\ /\n" +
		"       V\n\n"
	if out.String() != want {
		t.Errorf("\n%s", out.String())
	}
	// With colors: same text once the control sequences are removed
	var col bytes.Buffer
	c := New(strings.NewReader(""), &col, false)
	c.Banner("vanilla-router 1.0", "Welcome!")
	c.Heading("Hard disk")
	plain := col.String()
	for _, s := range []string{brown, vanilla, bold, reset} {
		plain = strings.ReplaceAll(plain, s, "")
	}
	if !strings.HasPrefix(plain, want) || !strings.Contains(plain, "━━━ Hard disk ━━━") || !strings.Contains(col.String(), brown) {
		t.Errorf("%q", col.String())
	}
}

func TestEOF(t *testing.T) {
	p, _ := prompt("")
	if _, err := p.Menu([]string{"a"}, 0); err != ErrEOF {
		t.Error(err)
	}
	if err := p.WaitEnter("next"); err != ErrEOF {
		t.Error(err)
	}
}
