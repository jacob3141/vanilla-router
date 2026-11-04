// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tui ist eine schlichte zeilenbasierte Textoberfläche für den Installer: funktioniert auf
// jeder Konsole (tty1, serielle Konsole, SSH), braucht keine Bibliotheken (P-08).
package tui

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Prompt fragt den Nutzer. Alle Eingaben kommen über Lines, damit andere Teile (die WAN-Erkennung)
// dieselbe Eingabe mitlesen können.
type Prompt struct {
	Out   io.Writer
	Lines <-chan string
	Fd    int // Dateideskriptor des Terminals für verdeckte Eingaben, -1 = keiner
}

// New liest zeilenweise von r.
func New(r io.Reader, out io.Writer, fd int) *Prompt {
	ch := make(chan string)
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			ch <- sc.Text()
		}
		close(ch)
	}()
	return &Prompt{Out: out, Lines: ch, Fd: fd}
}

// ErrEOF: Eingabe beendet (z. B. Strg+D).
var ErrEOF = io.EOF

func (p *Prompt) line() (string, error) {
	s, ok := <-p.Lines
	if !ok {
		return "", ErrEOF
	}
	return strings.TrimSpace(s), nil
}

// Heading zeigt die Überschrift eines Schritts.
func (p *Prompt) Heading(n, total int, title string) {
	fmt.Fprintf(p.Out, "\n━━━ Schritt %d von %d: %s ━━━\n\n", n, total, title)
}

// Say zeigt Text.
func (p *Prompt) Say(format string, args ...any) { fmt.Fprintf(p.Out, format+"\n", args...) }

// Ask fragt mit Vorgabe. Leere Eingabe übernimmt die Vorgabe. valid darf nil sein.
func (p *Prompt) Ask(q, def string, valid func(string) error) (string, error) {
	for {
		if def != "" {
			fmt.Fprintf(p.Out, "%s [%s]: ", q, def)
		} else {
			fmt.Fprintf(p.Out, "%s: ", q)
		}
		s, err := p.line()
		if err != nil {
			return "", err
		}
		if s == "" {
			s = def
		}
		if valid != nil {
			if err := valid(s); err != nil {
				fmt.Fprintf(p.Out, "  %v\n", err)
				continue
			}
		} else if s == "" {
			continue
		}
		return s, nil
	}
}

// Choose lässt eine von mehreren Möglichkeiten wählen (Nummer ab 1). def ist die Vorgabe (Index).
func (p *Prompt) Choose(q string, options []string, def int) (int, error) {
	fmt.Fprintln(p.Out, q)
	for i, o := range options {
		mark := " "
		if i == def {
			mark = "*"
		}
		fmt.Fprintf(p.Out, "  %s %d) %s\n", mark, i+1, o)
	}
	for {
		fmt.Fprintf(p.Out, "Auswahl [%d]: ", def+1)
		s, err := p.line()
		if err != nil {
			return 0, err
		}
		if s == "" {
			return def, nil
		}
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		fmt.Fprintf(p.Out, "  Bitte eine Zahl von 1 bis %d eingeben.\n", len(options))
	}
}

// Confirm verlangt für gefährliche Schritte die Eingabe eines bestimmten Worts.
func (p *Prompt) Confirm(q, word string) (bool, error) {
	fmt.Fprintf(p.Out, "%s\nZum Bestätigen %q eingeben, sonst Enter: ", q, word)
	s, err := p.line()
	if err != nil {
		return false, err
	}
	return strings.EqualFold(s, word), nil
}

// YesNo fragt ja/nein.
func (p *Prompt) YesNo(q string, def bool) (bool, error) {
	d := "J/n"
	if !def {
		d = "j/N"
	}
	for {
		fmt.Fprintf(p.Out, "%s [%s]: ", q, d)
		s, err := p.line()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(s) {
		case "":
			return def, nil
		case "j", "ja", "y", "yes":
			return true, nil
		case "n", "nein", "no":
			return false, nil
		}
	}
}

// Password liest ohne Anzeige (Echo aus), wenn ein Terminal vorhanden ist.
func (p *Prompt) Password(q string) (string, error) {
	fmt.Fprintf(p.Out, "%s: ", q)
	restore := noEcho(p.Fd)
	s, err := p.line()
	restore()
	fmt.Fprintln(p.Out)
	return s, err
}

// noEcho schaltet die Anzeige der Eingabe aus und liefert die Funktion zum Zurücksetzen.
func noEcho(fd int) func() {
	if fd < 0 {
		return func() {}
	}
	var t syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&t))); e != 0 {
		return func() {}
	}
	old := t
	t.Lflag &^= syscall.ECHO
	t.Lflag |= syscall.ECHONL
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&t)))
	return func() { syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&old))) }
}

// StdinFd liefert 0, wenn stdin ein Terminal ist, sonst -1.
func StdinFd() int {
	var t syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, 0, syscall.TCGETS, uintptr(unsafe.Pointer(&t))); e != 0 {
		return -1
	}
	return 0
}
