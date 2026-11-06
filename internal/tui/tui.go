// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tui is the installer's text interface. It is operated with arrow keys and Enter only
// (P-11, F-INST-16): there is no text input, so the keyboard layout does not matter.
// Works on any console (tty1, serial console), needs no libraries (P-08).
package tui

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
	"vanilla-router/internal/i18n"
)

// Key is a keypress.
type Key int

const (
	KeyOther Key = iota
	KeyUp
	KeyDown
	KeyEnter
)

// Prompt shows text and menus. All keys come through a channel, so that a menu can wait next to a
// running detection (MenuAsync).
type Prompt struct {
	Out  io.Writer
	keys chan Key
	mu   sync.Mutex
	back []Key // keys put back (a cancelled menu read one key too many)
	// Plain: no control characters for redrawing (tests, logs)
	Plain bool
}

// ErrEOF: input ended.
var ErrEOF = io.EOF

// New reads keys from r. Arrow keys come as escape sequences (ESC [ A, ESC O A).
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

// Heading shows the heading of a section. Non-experts see no step numbers, because
// steps without an action do not appear (F-INST-18).
func (p *Prompt) Heading(title string) {
	title = i18n.T(title)
	if p.Plain {
		fmt.Fprintf(p.Out, "\n━━━ %s ━━━\n\n", title)
		return
	}
	fmt.Fprintf(p.Out, "\n%s━━━%s %s%s%s %s━━━%s\n\n", brown, reset, bold, title, reset, brown, reset)
}

// Console colors in the logo's two colors (internal/logo). The Linux console only knows 16
// colors: color 3 is brown there, bright (bold) is yellow like vanilla.
const (
	brown   = "\x1b[33m"
	vanilla = "\x1b[1;33m"
	bold    = "\x1b[1m"
	reset   = "\x1b[0m"
)

// banner is the logo as a text image: scoop and cone.
var banner = [][2]string{
	{vanilla, `     .---.`}, {"", ""},
	{vanilla, `    (     )`}, {"", ""},
	{vanilla, `     '---' `}, {"", ""},
	{brown, `    \     /`}, {"", ""},
	{brown, `     \   /`}, {"", ""},
	{brown, `      \ /`}, {"", ""},
	{brown, `       V`}, {"", ""},
}

// Banner shows the logo with the title and subtitle next to it.
func (p *Prompt) Banner(title, sub string) {
	title, sub = i18n.T(title), i18n.T(sub)
	var b strings.Builder
	b.WriteString("\n")
	for i := 0; i < len(banner); i += 2 {
		row := banner[i : i+2]
		text := map[int]string{1: title, 2: sub}[i/2]
		if text == "" && row[1][1] == "" { // no spaces at the end of a line
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

// StartSpinner shows that the program is alive while it waits: an animation, a text and the seconds
// passed, on the current line. The returned function stops it and clears the line, and must be called
// before anything else is printed. Without control characters (Plain) it does nothing.
func (p *Prompt) StartSpinner(text string) (stop func()) {
	if p.Plain {
		return func() {}
	}
	text = i18n.T(text)
	done, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		start := time.Now()
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for i := 0; ; i++ {
			fmt.Fprintf(p.Out, "\r\x1b[K  %c %s (%d s)", `|/-\`[i%4], text, int(time.Since(start).Seconds()))
			select {
			case <-done:
				fmt.Fprint(p.Out, "\r\x1b[K")
				return
			case <-tick.C:
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			<-finished
		})
	}
}

// Clear empties the screen, so that boot messages do not stand above the first question.
func (p *Prompt) Clear() {
	if !p.Plain {
		fmt.Fprint(p.Out, "\x1b[2J\x1b[H")
	}
}

// Say shows text.
func (p *Prompt) Say(format string, args ...any) {
	io.WriteString(p.Out, i18n.T(fmt.Sprintf(format, args...))+"\n")
}

// next returns the next key. ok = false: end of input or done closed.
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
		case <-done: // cancelled at the same time: the key belongs to the next reader
			p.mu.Lock()
			p.back = append(p.back, k)
			p.mu.Unlock()
			return 0, false
		default:
			return k, true
		}
	}
}

// WaitEnter waits for Enter.
func (p *Prompt) WaitEnter(text string) error {
	fmt.Fprintf(p.Out, "%s\n", i18n.T(text))
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

// Menu shows a choice operated with ↑ ↓ and Enter. def is the preselection.
func (p *Prompt) Menu(options []string, def int) (int, error) {
	ch, _ := p.MenuAsync(options, def)
	n, ok := <-ch
	if !ok {
		return 0, ErrEOF
	}
	return n, nil
}

// MenuAsync shows a menu and returns the choice through the channel without blocking. cancel ends
// the menu (the channel is then closed). Only one menu may be open at a time.
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
					fmt.Fprintf(p.Out, "  → %s\n", i18n.T(options[sel]))
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
		fmt.Fprintf(&b, "\x1b[%dA", len(options)+1) // back to the start of the menu
	}
	if redraw && p.Plain {
		return // without control characters only report the choice at the end
	}
	for i, o := range options {
		o = i18n.T(o)
		mark := "   "
		if i == sel {
			mark = " ▸ "
		}
		if !p.Plain {
			b.WriteString("\r\x1b[K")
			if i == sel {
				b.WriteString("\x1b[7m") // inverted
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
	b.WriteString(i18n.T("  (arrow keys ↑ ↓, Enter)\n"))
	io.WriteString(p.Out, b.String())
}

// RawMode switches the terminal to keypresses without echo (for arrow keys). Returns the
// function for resetting it. Without a terminal nothing happens.
func RawMode(fd int) func() {
	var t syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&t))); e != 0 {
		return func() {}
	}
	old := t
	t.Lflag &^= syscall.ICANON | syscall.ECHO
	t.Cc[syscall.VMIN], t.Cc[syscall.VTIME] = 1, 0
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&t)))
	fmt.Print("\x1b[?25l") // hide the cursor
	return func() {
		fmt.Print("\x1b[?25h")
		syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&old)))
	}
}

// IsTerminal says whether fd is a terminal.
func IsTerminal(fd int) bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return e == 0
}
