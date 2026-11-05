// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tui ist die Textoberfläche des Installers. Bedient wird sie nur mit Pfeiltasten und Enter
// (P-11, F-INST-16): Es gibt keine Texteingabe, deshalb spielt die Tastaturbelegung keine Rolle.
// Funktioniert auf jeder Konsole (tty1, serielle Konsole), braucht keine Bibliotheken (P-08).
package tui

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// Key ist ein Tastendruck.
type Key int

const (
	KeyOther Key = iota
	KeyUp
	KeyDown
	KeyEnter
)

// Prompt zeigt Text und Menüs. Alle Tasten kommen über einen Kanal, damit ein Menü neben einer
// laufenden Erkennung warten kann (MenuAsync).
type Prompt struct {
	Out  io.Writer
	keys chan Key
	mu   sync.Mutex
	back []Key // zurückgelegte Tasten (ein abgebrochenes Menü hat eine Taste zu viel gelesen)
	// Plain: keine Steuerzeichen zum Neuzeichnen (Tests, Protokolle)
	Plain bool
}

// ErrEOF: Eingabe beendet.
var ErrEOF = io.EOF

// New liest Tasten aus r. Pfeiltasten kommen als Escape-Folgen (ESC [ A, ESC O A).
func New(r io.Reader, out io.Writer, plain bool) *Prompt {
	p := &Prompt{Out: out, keys: make(chan Key), Plain: plain}
	go func() {
		br := bufio.NewReader(r)
		for {
			b, err := br.ReadByte()
			if err != nil {
				close(p.keys)
				return
			}
			switch b {
			case '\r', '\n':
				p.keys <- KeyEnter
			case 0x1b:
				b1, err1 := br.ReadByte()
				b2, err2 := br.ReadByte()
				if err1 != nil || err2 != nil || (b1 != '[' && b1 != 'O') {
					p.keys <- KeyOther
					continue
				}
				switch b2 {
				case 'A':
					p.keys <- KeyUp
				case 'B':
					p.keys <- KeyDown
				default:
					p.keys <- KeyOther
				}
			default:
				p.keys <- KeyOther
			}
		}
	}()
	return p
}

// Heading zeigt die Überschrift eines Abschnitts. Laien sehen keine Schrittnummern, weil
// Schritte ohne Handlung nicht erscheinen (F-INST-18).
func (p *Prompt) Heading(title string) {
	if p.Plain {
		fmt.Fprintf(p.Out, "\n━━━ %s ━━━\n\n", title)
		return
	}
	fmt.Fprintf(p.Out, "\n%s━━━%s %s%s%s %s━━━%s\n\n", brown, reset, bold, title, reset, brown, reset)
}

// Farben der Konsole in den zwei Farben des Logos (internal/logo). Die Linux-Konsole kennt nur 16
// Farben: Farbe 3 ist dort braun, hell (fett) gelb wie Vanille.
const (
	brown   = "\x1b[33m"
	vanilla = "\x1b[1;33m"
	bold    = "\x1b[1m"
	reset   = "\x1b[0m"
)

// banner ist das Logo als Textbild: Eiskugel und Hörnchen.
var banner = [][2]string{
	{vanilla, `     .---.`}, {"", ""},
	{vanilla, `    (     )`}, {"", ""},
	{vanilla, `     '---' `}, {"", ""},
	{brown, `    \     /`}, {"", ""},
	{brown, `     \   /`}, {"", ""},
	{brown, `      \ /`}, {"", ""},
	{brown, `       V`}, {"", ""},
}

// Banner zeigt das Logo mit Titel und Untertitel daneben.
func (p *Prompt) Banner(title, sub string) {
	var b strings.Builder
	b.WriteString("\n")
	for i := 0; i < len(banner); i += 2 {
		row := banner[i : i+2]
		text := map[int]string{1: title, 2: sub}[i/2]
		if text == "" && row[1][1] == "" { // keine Leerzeichen am Zeilenende
			row = [][2]string{{row[0][0], strings.TrimRight(row[0][1], " ")}}
		}
		for _, part := range row {
			if !p.Plain && part[0] != "" {
				b.WriteString(part[0] + part[1] + reset)
			} else {
				b.WriteString(part[1])
			}
		}
		if text != "" {
			if !p.Plain && i/2 == 1 {
				text = bold + text + reset
			}
			b.WriteString("   " + text)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	io.WriteString(p.Out, b.String())
}

// Say zeigt Text.
func (p *Prompt) Say(format string, args ...any) { fmt.Fprintf(p.Out, format+"\n", args...) }

// next liefert die nächste Taste. ok = false: Eingabe zu Ende oder done geschlossen.
func (p *Prompt) next(done <-chan struct{}) (Key, bool) {
	p.mu.Lock()
	if len(p.back) > 0 {
		k := p.back[0]
		p.back = p.back[1:]
		p.mu.Unlock()
		return k, true
	}
	p.mu.Unlock()
	select {
	case <-done:
		return 0, false
	case k, ok := <-p.keys:
		if !ok {
			return 0, false
		}
		select {
		case <-done: // gleichzeitig abgebrochen: Taste gehört dem nächsten Leser
			p.mu.Lock()
			p.back = append(p.back, k)
			p.mu.Unlock()
			return 0, false
		default:
			return k, true
		}
	}
}

// WaitEnter wartet auf Enter.
func (p *Prompt) WaitEnter(text string) error {
	fmt.Fprintf(p.Out, "%s\n", text)
	for {
		k, ok := p.next(nil)
		if !ok {
			return ErrEOF
		}
		if k == KeyEnter {
			return nil
		}
	}
}

// Menu zeigt eine Auswahl, die mit ↑ ↓ und Enter bedient wird. def ist die Vorauswahl.
func (p *Prompt) Menu(options []string, def int) (int, error) {
	ch, _ := p.MenuAsync(options, def)
	n, ok := <-ch
	if !ok {
		return 0, ErrEOF
	}
	return n, nil
}

// MenuAsync zeigt ein Menü und liefert die Wahl über den Kanal, ohne zu blockieren. cancel beendet
// das Menü (der Kanal wird dann geschlossen). Es darf immer nur ein Menü offen sein.
func (p *Prompt) MenuAsync(options []string, def int) (<-chan int, func()) {
	out := make(chan int, 1)
	done := make(chan struct{})
	sel := max(0, min(def, len(options)-1))
	p.draw(options, sel, false)
	go func() {
		defer close(out)
		for {
			k, ok := p.next(done)
			if !ok {
				return
			}
			{
				switch k {
				case KeyUp:
					if sel > 0 {
						sel--
						p.draw(options, sel, true)
					}
				case KeyDown:
					if sel < len(options)-1 {
						sel++
						p.draw(options, sel, true)
					}
				case KeyEnter:
					fmt.Fprintf(p.Out, "  → %s\n", options[sel])
					out <- sel
					return
				}
			}
		}
	}()
	var once sync.Once
	return out, func() { once.Do(func() { close(done) }) }
}

func (p *Prompt) draw(options []string, sel int, redraw bool) {
	var b strings.Builder
	if redraw && !p.Plain {
		fmt.Fprintf(&b, "\x1b[%dA", len(options)+1) // zurück an den Anfang des Menüs
	}
	if redraw && p.Plain {
		return // ohne Steuerzeichen nur die Wahl am Ende melden
	}
	for i, o := range options {
		mark := "   "
		if i == sel {
			mark = " ▸ "
		}
		if !p.Plain {
			b.WriteString("\r\x1b[K")
			if i == sel {
				b.WriteString("\x1b[7m") // invertiert
			}
		}
		b.WriteString(mark + o)
		if !p.Plain && i == sel {
			b.WriteString("\x1b[0m")
		}
		b.WriteString("\n")
	}
	if !p.Plain {
		b.WriteString("\r\x1b[K")
	}
	b.WriteString("  (Pfeiltasten ↑ ↓, Enter)\n")
	io.WriteString(p.Out, b.String())
}

// RawMode schaltet das Terminal auf Tastendrücke ohne Echo um (für Pfeiltasten). Liefert die
// Funktion zum Zurücksetzen. Ohne Terminal passiert nichts.
func RawMode(fd int) func() {
	var t syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&t))); e != 0 {
		return func() {}
	}
	old := t
	t.Lflag &^= syscall.ICANON | syscall.ECHO
	t.Cc[syscall.VMIN], t.Cc[syscall.VTIME] = 1, 0
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&t)))
	fmt.Print("\x1b[?25l") // Cursor ausblenden
	return func() {
		fmt.Print("\x1b[?25h")
		syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&old)))
	}
}

// IsTerminal sagt, ob fd ein Terminal ist.
func IsTerminal(fd int) bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return e == 0
}
