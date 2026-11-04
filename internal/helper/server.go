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

// Handler führt eine Aktion aus. params sind die rohen JSON-Parameter.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Config des Helpers.
type Config struct {
	Socket     string
	AllowUID   int    // neben root darf nur dieser Benutzer verbinden
	SocketGID  int    // Gruppe des Sockets
	DryRun     bool   // Probelauf: Dateien nur unter Root schreiben, nichts anwenden
	Root       string // Wurzel für geschriebene Dateien im Probelauf, sonst "/"
	SecretsDir string // Geheimnisse für vrctl apply (PPPoE), Vorgabe /etc/vanilla-router/secrets
	Log        *slog.Logger
}

type Server struct {
	cfg      Config
	handlers map[string]Handler
	mu       sync.Mutex // schützt state.json und die verwalteten Dateien
	pending  *pendingChange
}

func NewServer(cfg Config) *Server {
	if cfg.Root == "" {
		cfg.Root = "/"
	}
	if cfg.SecretsDir == "" {
		cfg.SecretsDir = "/etc/vanilla-router/secrets"
	}
	s := &Server{cfg: cfg, handlers: map[string]Handler{}}
	s.registerAll()
	return s
}

func (s *Server) handle(name string, h Handler) { s.handlers[name] = h }

// Path übersetzt einen Systempfad in den Pfad, der wirklich geschrieben wird.
// Im Probelauf landet alles unter cfg.Root, gelesen wird weiter vom echten System.
func (s *Server) Path(p string) string {
	if !s.cfg.DryRun {
		return p
	}
	return filepath.Join(s.cfg.Root, p)
}

// Run nimmt Verbindungen an, bis ctx endet.
func (s *Server) Run(ctx context.Context) error {
	os.Remove(s.cfg.Socket)
	ln, err := net.Listen("unix", s.cfg.Socket)
	if err != nil {
		return err
	}
	defer ln.Close()
	if os.Geteuid() == 0 { // ohne root (Probelauf in der Entwicklung) gehört der Socket dem Aufrufer
		if err := os.Chown(s.cfg.Socket, 0, s.cfg.SocketGID); err != nil {
			return err
		}
	}
	if err := os.Chmod(s.cfg.Socket, 0o660); err != nil {
		return err
	}
	s.cfg.Log.Info("helper bereit", "socket", s.cfg.Socket, "dry_run", s.cfg.DryRun, "aktionen", len(s.handlers))

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
	if err != nil || (uid != 0 && uid != s.cfg.AllowUID) {
		s.cfg.Log.Warn("verbindung abgelehnt", "uid", uid, "err", err)
		return
	}
	conn.SetDeadline(time.Now().Add(30 * time.Minute)) // Updates können dauern

	var req Request
	dec := json.NewDecoder(bufio.NewReader(&limitedReader{conn, maxMessage}))
	if err := dec.Decode(&req); err != nil {
		writeResp(conn, Response{Error: "Anfrage unlesbar"})
		return
	}
	h, ok := s.handlers[req.Action]
	if !ok {
		s.cfg.Log.Warn("unbekannte aktion", "action", req.Action)
		writeResp(conn, Response{Error: "Unbekannte Aktion"})
		return
	}

	start := time.Now()
	data, err := h(ctx, req.Params)
	log := s.cfg.Log.With("action", req.Action, "dauer", time.Since(start).Round(time.Millisecond))
	if err != nil {
		var ue *UserError
		if errors.As(err, &ue) {
			log.Warn("aktion abgelehnt", "grund", ue.Msg)
			writeResp(conn, Response{Error: ue.Msg})
		} else {
			log.Error("aktion fehlgeschlagen", "err", err)
			writeResp(conn, Response{Error: "Interner Fehler im Helper, Details im Journal (vr-webui-helper)."})
		}
		return
	}
	if !isReadOnly(req.Action) {
		log.Info("aktion ausgeführt")
	}
	resp := Response{OK: true}
	if data != nil {
		if resp.Data, err = json.Marshal(data); err != nil {
			writeResp(conn, Response{Error: "Antwort nicht serialisierbar"})
			return
		}
	}
	writeResp(conn, resp)
}

func writeResp(c net.Conn, r Response) { json.NewEncoder(c).Encode(r) }

// isReadOnly: lesende Aktionen werden nicht einzeln protokolliert, um das Journal nicht zu fluten.
func isReadOnly(action string) bool {
	return strings.HasSuffix(action, ".list") || strings.HasSuffix(action, ".get") ||
		strings.HasSuffix(action, ".status") || action == "health" || strings.HasPrefix(action, "tool.")
}

// run führt ein Programm ohne Shell aus und liefert stdout+stderr.
func run(ctx context.Context, name string, args ...string) (string, error) {
	return runTZ(ctx, "", name, args...)
}

// runTZ wie run, mit gesetzter Zeitzone (leer = Systemvorgabe).
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

// apply führt ein Programm aus, das etwas am System ändert. Im Probelauf wird es nur protokolliert.
func (s *Server) apply(ctx context.Context, name string, args ...string) error {
	if s.cfg.DryRun {
		s.cfg.Log.Info("probelauf: übersprungen", "cmd", name+" "+strings.Join(args, " "))
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
		return Userf("Ungültige Parameter: %v", err)
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
}
