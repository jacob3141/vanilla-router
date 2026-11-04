// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"strings"
	"time"

	"vanilla-router/internal/conf"
)

// Firewall-Änderungen werden sofort wirksam, aber ohne Bestätigung innerhalb dieser Frist
// automatisch zurückgenommen (wie bei Consumer-Routern).
const confirmWindow = 60 * time.Second

type pendingChange struct {
	prev     conf.State
	deadline time.Time
	timer    *time.Timer
}

// Das tägliche Neuladen der Zeitsperren (Sommerzeit) macht vr-firewall-reload.timer, die Zeitzone
// kommt aus config.json (vrctl firewall-load).

// startPending merkt sich den alten Zustand und stellt ihn nach Ablauf der Frist wieder her.
// Aufrufer hält s.mu.
func (s *Server) startPending(prev conf.State) time.Time {
	if s.pending != nil {
		s.pending.timer.Stop()
		prev = s.pending.prev // Ausgangspunkt bleibt der letzte bestätigte Zustand
	}
	p := &pendingChange{prev: prev, deadline: time.Now().Add(confirmWindow)}
	p.timer = time.AfterFunc(confirmWindow, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.pending != p {
			return
		}
		s.pending = nil
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.cfg.Log.Warn("firewall: keine bestätigung, nehme portweiterleitungen zurück")
		// Nur die Weiterleitungen zurücksetzen; Sperren, die inzwischen geändert wurden, bleiben
		cur, err := s.loadState()
		if err != nil {
			s.cfg.Log.Error("firewall: zustand unlesbar", "err", err)
			return
		}
		next := cur.Clone()
		next.PortForwards = p.prev.PortForwards
		if err := s.commit(ctx, cur, next, parts{portfw: true}); err != nil {
			s.cfg.Log.Error("firewall: zurücknehmen fehlgeschlagen", "err", err)
		}
	})
	s.pending = p
	return p.deadline
}

func newID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	rand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

func (s *Server) registerFirewall() {
	s.handle("firewall.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		var pending *time.Time
		if s.pending != nil {
			d := s.pending.deadline
			pending = &d
		}
		s.mu.Unlock()
		tables := map[string]string{}
		for _, t := range []string{"inet vr_filter", "ip vr_nat", "ip vr_portfw", "inet vr_block"} {
			out, err := run(ctx, "nft", append([]string{"list", "table"}, strings.Fields(t)...)...)
			if err != nil {
				out = "(Tabelle nicht geladen)"
			}
			tables[t] = out
		}
		names, _ := run(ctx, "nft", "list", "tables")
		return map[string]any{"pending_until": pending, "tables": tables, "all_tables": strings.Fields(strings.ReplaceAll(names, "table ", "")), "dry_run": s.cfg.DryRun}, nil
	})

	s.handle("portfw.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p conf.PortForward
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		prev, err := s.loadState()
		if err != nil {
			return nil, err
		}
		p.Comment = strings.TrimSpace(p.Comment)
		if ip, err := conf.ParseLANIP(p.IntIP); err != nil {
			return nil, &UserError{err.Error()}
		} else {
			p.IntIP = ip.String()
		}
		next := prev.Clone()
		if p.ID == "" {
			p.ID = newID()
			next.PortForwards = append(next.PortForwards, p)
		} else {
			found := false
			for i, ex := range next.PortForwards {
				if ex.ID == p.ID {
					next.PortForwards[i] = p
					found = true
				}
			}
			if !found {
				return nil, Userf("Diese Weiterleitung gibt es nicht mehr.")
			}
		}
		if err := s.commit(ctx, prev, next, parts{portfw: true}); err != nil {
			return nil, err
		}
		return map[string]any{"id": p.ID, "pending_until": s.startPending(prev)}, nil
	})

	s.handle("portfw.delete", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		prev, err := s.loadState()
		if err != nil {
			return nil, err
		}
		next := prev.Clone()
		n := len(next.PortForwards)
		next.PortForwards = deleteWhere(next.PortForwards, func(f conf.PortForward) bool { return f.ID == p.ID })
		if len(next.PortForwards) == n {
			return nil, Userf("Diese Weiterleitung gibt es nicht mehr.")
		}
		if err := s.commit(ctx, prev, next, parts{portfw: true}); err != nil {
			return nil, err
		}
		return map[string]any{"pending_until": s.startPending(prev)}, nil
	})

	s.handle("block.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var b conf.Block
		if err := decode(raw, &b); err != nil {
			return nil, err
		}
		mac, err := conf.NormalizeMAC(b.MAC)
		if err != nil {
			return nil, &UserError{err.Error()}
		}
		b.MAC = mac
		b.Name = strings.TrimSpace(b.Name)
		s.mu.Lock()
		defer s.mu.Unlock()
		prev, err := s.loadState()
		if err != nil {
			return nil, err
		}
		next := prev.Clone()
		next.Blocks = deleteWhere(next.Blocks, func(x conf.Block) bool { return x.MAC == mac })
		next.Blocks = append(next.Blocks, b)
		return b, s.commit(ctx, prev, next, parts{portfw: true})
	})

	s.handle("block.delete", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			MAC string `json:"mac"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		mac, err := conf.NormalizeMAC(p.MAC)
		if err != nil {
			return nil, &UserError{err.Error()}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		prev, err := s.loadState()
		if err != nil {
			return nil, err
		}
		next := prev.Clone()
		n := len(next.Blocks)
		next.Blocks = deleteWhere(next.Blocks, func(x conf.Block) bool { return x.MAC == mac })
		if len(next.Blocks) == n {
			return nil, Userf("Für %s gibt es keine Sperre.", mac)
		}
		return nil, s.commit(ctx, prev, next, parts{portfw: true})
	})

	s.handle("portfw.confirm", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.pending == nil {
			return nil, Userf("Es gibt keine offene Änderung (Frist abgelaufen?).")
		}
		s.pending.timer.Stop()
		s.pending = nil
		return nil, nil
	})
}
