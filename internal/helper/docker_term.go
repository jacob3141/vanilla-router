// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// An interactive terminal in a container. The standard library has no WebSocket, so the browser reads and
// writes in small requests. A session is a real pseudo terminal with docker exec or attach on it.

const (
	termMaxSessions = 8
	termIdle        = 30 * time.Minute
	termWait        = 8 * time.Second
	termBuffer      = 1 << 20
)

var termShellRe = regexp.MustCompile(`^/[a-zA-Z0-9/_.-]{1,100}$`)

type termSession struct {
	cmd  *exec.Cmd
	pty  *os.File
	wake chan struct{}

	mu    sync.Mutex
	buf   []byte
	alive bool
	last  time.Time
}

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

func openPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	var unlock int32
	var n uint32
	if err = ioctl(master.Fd(), syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err == nil {
		err = ioctl(master.Fd(), syscall.TIOCGPTN, unsafe.Pointer(&n))
	}
	if err == nil {
		slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	}
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

func setPTYSize(f *os.File, rows, cols int) error {
	ws := struct{ row, col, x, y uint16 }{uint16(rows), uint16(cols), 0, 0}
	return ioctl(f.Fd(), syscall.TIOCSWINSZ, unsafe.Pointer(&ws))
}

func (t *termSession) signal() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func (t *termSession) pump() {
	b := make([]byte, 32*1024)
	for {
		n, err := t.pty.Read(b)
		t.mu.Lock()
		// A client that stopped reading must not grow this without limit
		if n > 0 && len(t.buf) < termBuffer {
			t.buf = append(t.buf, b[:n]...)
		}
		if err != nil {
			t.alive = false
		}
		t.mu.Unlock()
		t.signal()
		if err != nil {
			return
		}
	}
}

func (t *termSession) stop() {
	if t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}
	t.pty.Close()
}

func (s *Server) registerTerminal() {
	var mu sync.Mutex
	sessions := map[string]*termSession{}
	find := func(id string) (*termSession, error) {
		mu.Lock()
		defer mu.Unlock()
		t := sessions[id]
		if t == nil {
			return nil, Userf("This terminal has ended.")
		}
		t.mu.Lock()
		t.last = time.Now()
		t.mu.Unlock()
		return t, nil
	}
	remove := func(id string) {
		mu.Lock()
		defer mu.Unlock()
		if t := sessions[id]; t != nil {
			t.stop()
			delete(sessions, id)
		}
	}

	s.handle("docker.term.open", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name  string `json:"name"`
			Mode  string `json:"mode"`
			Shell string `json:"shell"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !containerNameRe.MatchString(p.Name) {
			return nil, Userf("Invalid container name.")
		}
		var args []string
		switch p.Mode {
		case "exec":
			if !termShellRe.MatchString(p.Shell) {
				return nil, Userf("Please enter the path of a shell, for example /bin/sh.")
			}
			args = []string{"exec", "-it", "-e", "TERM=xterm-256color", p.Name, p.Shell}
		case "attach":
			args = []string{"attach", p.Name}
		default:
			return nil, Userf("Unknown terminal mode.")
		}
		mu.Lock()
		defer mu.Unlock()
		for id, t := range sessions {
			t.mu.Lock()
			old := time.Since(t.last) > termIdle
			t.mu.Unlock()
			if old {
				t.stop()
				delete(sessions, id)
			}
		}
		if len(sessions) >= termMaxSessions {
			return nil, Userf("Too many open terminals, please close one.")
		}
		master, slave, err := openPTY()
		if err != nil {
			return nil, Userf("No terminal could be opened.")
		}
		defer slave.Close()
		setPTYSize(master, 24, 80)
		name, cargs := "docker", args
		if s.cfg.DryRun {
			name, cargs = "sh", nil // the test instance has no Docker, a local shell exercises the same path
		}
		cmd := exec.Command(name, cargs...)
		cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "TERM=xterm-256color"}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
		if err := cmd.Start(); err != nil {
			master.Close()
			return nil, Userf("The terminal could not be started.")
		}
		var idb [16]byte
		rand.Read(idb[:])
		id := hex.EncodeToString(idb[:])
		t := &termSession{cmd: cmd, pty: master, wake: make(chan struct{}, 1), alive: true, last: time.Now()}
		sessions[id] = t
		go t.pump()
		go cmd.Wait()
		return map[string]string{"id": id}, nil
	})

	type ref struct {
		ID   string `json:"id"`
		Data []byte `json:"data"`
		Rows int    `json:"rows"`
		Cols int    `json:"cols"`
	}
	session := func(raw json.RawMessage) (*termSession, ref, error) {
		var p ref
		if err := decode(raw, &p); err != nil {
			return nil, p, err
		}
		t, err := find(p.ID)
		return t, p, err
	}

	s.handle("docker.term.read", func(ctx context.Context, raw json.RawMessage) (any, error) {
		t, p, err := session(raw)
		if err != nil {
			return nil, err
		}
		take := func() ([]byte, bool) {
			t.mu.Lock()
			defer t.mu.Unlock()
			b := t.buf
			t.buf = nil
			return b, t.alive
		}
		b, alive := take()
		if len(b) == 0 && alive {
			select {
			case <-t.wake:
			case <-time.After(termWait):
			case <-ctx.Done():
			}
			b, alive = take()
		}
		if !alive && len(b) == 0 {
			remove(p.ID)
		}
		return map[string]any{"data": b, "alive": alive}, nil
	})

	s.handle("docker.term.write", func(ctx context.Context, raw json.RawMessage) (any, error) {
		t, p, err := session(raw)
		if err != nil {
			return nil, err
		}
		if len(p.Data) > 65536 {
			return nil, Userf("Too much text at once.")
		}
		_, err = t.pty.Write(p.Data)
		if err != nil {
			return nil, Userf("This terminal has ended.")
		}
		return nil, nil
	})

	s.handle("docker.term.resize", func(ctx context.Context, raw json.RawMessage) (any, error) {
		t, p, err := session(raw)
		if err != nil {
			return nil, err
		}
		if p.Rows < 2 || p.Rows > 300 || p.Cols < 2 || p.Cols > 500 {
			return nil, Userf("Invalid size.")
		}
		return nil, setPTYSize(t.pty, p.Rows, p.Cols)
	})

	s.handle("docker.term.close", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p ref
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		remove(p.ID)
		return nil, nil
	})
}
