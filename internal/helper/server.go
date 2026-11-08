// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Handler executes an action. params are the raw JSON parameters.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Config of the helper.
type Config struct {
	Socket     string
	AllowUID   int    // besides root only this user may connect
	APDUID     int    // the user of vr-apd, which may call the actions ap.* and nothing else (-1 = none)
	SocketGID  int    // group of the socket
	DryRun     bool   // dry run: write files only under Root, apply nothing
	FakeWiFi   bool   // only with DryRun: simulate a Wi-Fi adapter for the test instance
	Root       string // root for files written in a dry run, otherwise "/"
	SecretsDir string // secrets for vrctl apply (PPPoE), default /etc/vanilla-router/secrets
	Log        *slog.Logger
}

type Server struct {
	cfg      Config
	handlers map[string]Handler
	mu       sync.Mutex // protects state.json and the managed files
	pending  *pendingChange
	wifi     wifiHW
}

func NewServer(cfg Config) *Server {
	if cfg.Root == "" {
		cfg.Root = "/"
	}
	if cfg.SecretsDir == "" {
		cfg.SecretsDir = "/etc/vanilla-router/secrets"
	}
	s := &Server{cfg: cfg, handlers: map[string]Handler{}, wifi: realWiFi{}}
	if cfg.DryRun && cfg.FakeWiFi {
		s.wifi = fakeWiFi{mu: &installFlag{}}
	}
	s.registerAll()
	if s.fakeAPs() {
		s.seedFakePairing()
		s.refreshFakeReports()
	}
	return s
}

func (s *Server) handle(name string, h Handler) { s.handlers[name] = h }

// Path translates a system path into the path that is really written.
// In a dry run everything lands under cfg.Root, reading stays from the real system.
func (s *Server) Path(p string) string {
	if !s.cfg.DryRun {
		return p
	}
	return filepath.Join(s.cfg.Root, p)
}

// Run accepts connections until ctx ends.
func (s *Server) Run(ctx context.Context) error {
	os.Remove(s.cfg.Socket)
	ln, err := net.Listen("unix", s.cfg.Socket)
	if err != nil {
		return err
	}
	defer ln.Close()
	if os.Geteuid() == 0 { // without root (dry run in development) the socket belongs to the caller
		if err := os.Chown(s.cfg.Socket, 0, s.cfg.SocketGID); err != nil {
			return err
		}
	}
	if err := os.Chmod(s.cfg.Socket, 0o660); err != nil {
		return err
	}
	s.cfg.Log.Info("helper ready", "socket", s.cfg.Socket, "dry_run", s.cfg.DryRun, "actions", len(s.handlers))

	go func() { <-ctx.Done(); ln.Close() }()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.cfg.Log.Error("accept", "err", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go s.serveConn(ctx, conn.(*net.UnixConn))
	}
}

func peerUID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return -1, err
	}
	var cred *syscall.Ucred
	var serr error
	if err := raw.Control(func(fd uintptr) {
		cred, serr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return -1, err
	}
	if serr != nil {
		return -1, serr
	}
	return int(cred.Uid), nil
}

func (s *Server) serveConn(ctx context.Context, conn *net.UnixConn) {
	defer conn.Close()
	uid, err := peerUID(conn)
	if err != nil || (uid != 0 && uid != s.cfg.AllowUID && (s.cfg.APDUID <= 0 || uid != s.cfg.APDUID)) {
		s.cfg.Log.Warn("connection rejected", "uid", uid, "err", err)
		return
	}
	conn.SetDeadline(time.Now().Add(30 * time.Minute)) // updates can take a while

	var req Request
	dec := json.NewDecoder(bufio.NewReader(&limitedReader{conn, maxMessage}))
	if err := dec.Decode(&req); err != nil {
		writeResp(conn, Response{Error: "request unreadable"})
		return
	}
	if !s.allowed(uid, req.Action) {
		s.cfg.Log.Warn("action not allowed for this user", "action", req.Action, "uid", uid)
		writeResp(conn, Response{Error: "Unknown action"})
		return
	}
	h, ok := s.handlers[req.Action]
	if !ok {
		s.cfg.Log.Warn("unknown action", "action", req.Action)
		writeResp(conn, Response{Error: "Unknown action"})
		return
	}

	start := time.Now()
	data, err := h(ctx, req.Params)
	log := s.cfg.Log.With("action", req.Action, "duration", time.Since(start).Round(time.Millisecond))
	if err != nil {
		var ue *UserError
		if errors.As(err, &ue) {
			log.Warn("action rejected", "reason", ue.Msg)
			writeResp(conn, Response{Error: ue.Msg})
		} else {
			log.Error("action failed", "err", err)
			writeResp(conn, Response{Error: "Internal error in the helper, details in the journal (vr-webui-helper)."})
		}
		return
	}
	if !isReadOnly(req.Action) {
		log.Info("action executed")
	}
	resp := Response{OK: true}
	if data != nil {
		if resp.Data, err = json.Marshal(data); err != nil {
			writeResp(conn, Response{Error: "response not serializable"})
			return
		}
	}
	writeResp(conn, resp)
}

// allowed decides per action. ap.* is only for vr-apd: it faces the network and may do nothing else, and the web
// server has no business with the router's key.
func (s *Server) allowed(uid int, action string) bool {
	if uid == 0 {
		return true
	}
	if strings.HasPrefix(action, "ap.") {
		return s.cfg.APDUID > 0 && uid == s.cfg.APDUID
	}
	return uid == s.cfg.AllowUID
}

func writeResp(c net.Conn, r Response) { json.NewEncoder(c).Encode(r) }

// isReadOnly: read-only actions are not logged individually, so as not to flood the journal.
func isReadOnly(action string) bool {
	return strings.HasPrefix(action, "ap.") || strings.HasSuffix(action, ".list") || strings.HasSuffix(action, ".get") ||
		strings.HasSuffix(action, ".status") || action == "health" || strings.HasPrefix(action, "tool.")
}

// run runs a program without a shell and returns stdout+stderr.
func run(ctx context.Context, name string, args ...string) (string, error) {
	return runTZ(ctx, "", name, args...)
}

// runTZ like run, with a set time zone (empty = system default).
func runTZ(ctx context.Context, tz, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C.UTF-8"}
	if tz != "" {
		cmd.Env = append(cmd.Env, "TZ="+tz)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// apply runs a program that changes something on the system. In a dry run it is only logged.
func (s *Server) apply(ctx context.Context, name string, args ...string) error {
	if s.cfg.DryRun {
		s.cfg.Log.Info("dry run: skipped", "cmd", name+" "+strings.Join(args, " "))
		return nil
	}
	_, err := run(ctx, name, args...)
	return err
}

func decode(params json.RawMessage, v any) error {
	if len(params) == 0 {
		params = []byte("{}")
	}
	dec := json.NewDecoder(strings.NewReader(string(params)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return Userf("Invalid parameters: %v", err)
	}
	return nil
}

func (s *Server) registerAll() {
	s.handle("health", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true, "dry_run": s.cfg.DryRun}, nil
	})
	s.registerNetconf()
	s.registerFirewall()
	s.registerServices()
	s.registerSystem()
	s.registerNetstat()
	s.registerSysinfo()
	s.registerExtras()
	s.registerSettings()
	s.registerSSH()
	s.registerWiFi()
	s.registerWireless()
	s.registerFakeWireless()
	s.registerAPActions()
	s.registerConsole()
}
