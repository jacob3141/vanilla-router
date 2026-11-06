// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strconv"
	"syscall"
	"time"

	"vanilla-router/internal/conf"
)

func (s *Server) registerExtras() {
	// Import of an exported configuration: fully validated and applied like any other
	// change. Firewall parts need confirmation within 60 s.
	s.handle("state.import", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			State conf.State `json:"state"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		next := p.State
		next.Version = 1
		// treat missing lists as empty, so the comparison below works
		next.Reservations = nonNil(next.Reservations)
		next.Records = nonNil(next.Records)
		next.PortForwards = nonNil(next.PortForwards)
		next.Blocks = nonNil(next.Blocks)
		s.mu.Lock()
		defer s.mu.Unlock()
		prev, err := s.loadState()
		if err != nil {
			return nil, err
		}
		fw := !reflect.DeepEqual(prev.PortForwards, next.PortForwards) || !reflect.DeepEqual(prev.Blocks, next.Blocks)
		if err := s.commit(ctx, prev, next, parts{dhcp: true, dns: true, portfw: fw}); err != nil {
			return nil, err
		}
		res := map[string]any{"firewall_changed": fw}
		if fw {
			res["pending_until"] = s.startPending(prev)
		}
		return res, nil
	})

	// Port check: can the router open a TCP connection to target:port?
	s.handle("tool.port", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Target string `json:"target"`
			Port   int    `json:"port"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		t, err := validTarget(p.Target)
		if err != nil {
			return nil, err
		}
		if p.Port < 1 || p.Port > 65535 {
			return nil, Userf("The port must be between 1 and 65535.")
		}
		start := time.Now()
		d := net.Dialer{Timeout: 5 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(t, strconv.Itoa(p.Port)))
		ms := float64(time.Since(start).Microseconds()) / 1000
		res := map[string]any{"target": t, "port": p.Port, "ms": ms}
		switch {
		case err == nil:
			res["state"] = "open"
			res["remote"] = conn.RemoteAddr().String()
			conn.Close()
		case errors.Is(err, syscall.ECONNREFUSED):
			res["state"] = "closed"
		case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
			res["state"] = "timeout"
		default:
			res["state"] = "error"
			res["error"] = err.Error()
		}
		return res, nil
	})
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
