// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vanilla-router/internal/apply"
	"vanilla-router/internal/conf"
	"vanilla-router/internal/config"
)

// ManualDNS ist ein Eintrag, den die Oberfläche anzeigt, aber nicht bearbeitet (der Router selbst).
type ManualDNS struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	Kind string `json:"kind"`
	File string `json:"file"`
}

// NetconfInfo ist die Antwort von netconf.get (Form wie im Prototyp).
type NetconfInfo struct {
	State     conf.State  `json:"state"`
	Manual    []ManualDNS `json:"manual"`
	Upstreams []string    `json:"upstreams"`
	Domain    string      `json:"domain"`
	LAN       string      `json:"lan"`
	Lease     string      `json:"lease"`
	DryRun    bool        `json:"dry_run"`
}

// loadConfig liest config.json. Im Probelauf zuerst die Kopie unter Root, sonst die echte Datei.
func (s *Server) loadConfig() (*config.Config, error) {
	p := s.Path(config.Path)
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) && s.cfg.DryRun {
		p = config.Path
	}
	c, err := config.Load(p)
	if err != nil {
		return nil, fmt.Errorf("config.json: %w", err)
	}
	conf.SetNetwork(c)
	return c, nil
}

func (s *Server) loadState() (conf.State, error) {
	c, err := s.loadConfig()
	if err != nil {
		return conf.State{}, err
	}
	return conf.FromConfig(c), nil
}

func (s *Server) applier() *apply.Applier {
	a := &apply.Applier{Root: "/", Run: apply.ExecRunner{}}
	if s.cfg.DryRun {
		a.Root = s.cfg.Root
		a.Run = apply.DryRunner{Log: func(cmd string) { s.cfg.Log.Info("probelauf: übersprungen", "cmd", cmd) }}
		a.SkipHealth = true
	}
	return a
}

type parts struct{ dhcp, dns, portfw bool }

// commit schreibt den neuen State in config.json und spielt ihn mit vrctl-apply-Logik ein:
// prüfen, sichern, ersetzen, neu laden, Gesundheitsprüfung, bei Fehler zurück (P-05).
// parts bleibt für die Lesbarkeit der Aufrufer, apply lädt ohnehin nur, was sich geändert hat.
func (s *Server) commit(ctx context.Context, prev, next conf.State, _ parts) error {
	cur, err := s.loadConfig()
	if err != nil {
		return err
	}
	nc := next.ApplyTo(cur)
	if err := nc.Validate(); err != nil {
		return userConfigError(err)
	}
	_, err = s.applier().Apply(nc, apply.DirSecrets{Dir: s.cfg.SecretsDir}, apply.Options{})
	if err != nil {
		return &UserError{Msg: "Die Änderung wurde nicht übernommen: " + firstLine(err.Error())}
	}
	return nil
}

// userConfigError macht aus den Feldfehlern der Konfiguration einen Text für die Oberfläche.
func userConfigError(err error) error {
	var es config.Errors
	if errors.As(err, &es) {
		var msgs []string
		for _, e := range es {
			msgs = append(msgs, e.Text)
		}
		return &UserError{Msg: strings.Join(msgs, " · ")}
	}
	return &UserError{Msg: err.Error()}
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// lastLines liefert die letzten n nicht leeren Zeilen, verbunden mit „ · “.
func lastLines(s string, n int) string {
	var keep []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			keep = append(keep, l)
		}
	}
	if len(keep) > n {
		keep = keep[len(keep)-n:]
	}
	return strings.Join(keep, " · ")
}

// restartUnit startet einen Dienst neu und prüft, dass er danach läuft.
func restartUnit(ctx context.Context, unit string) error {
	if _, err := run(ctx, "systemctl", "restart", unit); err != nil {
		return err
	}
	time.Sleep(time.Second)
	out, _ := run(ctx, "systemctl", "is-active", unit)
	if strings.TrimSpace(out) != "active" {
		return fmt.Errorf("%s ist %s", unit, strings.TrimSpace(out))
	}
	return nil
}

// writeFile schreibt atomar und legt fehlende Ordner an.
func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return config.WriteFileAtomic(path, data, mode)
}

// --- Aktionen ------------------------------------------------------------------------

func (s *Server) registerNetconf() {
	s.handle("netconf.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		_, router, _ := c.LANPrefix()
		manual := []ManualDNS{{Name: c.System.Hostname + "." + c.DNS.Domain, IP: router.String(), Kind: "router", File: "config.json"}}
		ups := c.DNS.Upstreams
		if len(ups) == 0 {
			ups = []string{"DNS-Server des Anbieters"}
		}
		return NetconfInfo{State: conf.FromConfig(c), Manual: manual, Upstreams: ups, Domain: c.DNS.Domain,
			LAN: c.LAN.IPv4, Lease: c.DHCP.Lease, DryRun: s.cfg.DryRun}, nil
	})

	s.handle("dhcp.reservation.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			OriginalMAC string `json:"original_mac"`
			Name        string `json:"name"`
			MAC         string `json:"mac"`
			IP          string `json:"ip"`
			Comment     string `json:"comment"`
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
		name, err := conf.NormalizeName(p.Name)
		if err != nil {
			return nil, &UserError{err.Error()}
		}
		mac, err := conf.NormalizeMAC(p.MAC)
		if err != nil {
			return nil, &UserError{err.Error()}
		}
		ip, err := conf.ParseLANIP(p.IP)
		if err != nil {
			return nil, &UserError{err.Error()}
		}
		r := conf.Reservation{Name: name, MAC: mac, IP: ip.String(), Comment: strings.TrimSpace(p.Comment)}
		next := prev.Clone()
		orig := p.OriginalMAC
		if orig != "" {
			if orig, err = conf.NormalizeMAC(orig); err != nil {
				return nil, &UserError{err.Error()}
			}
		}
		replaced := false
		for i, ex := range next.Reservations {
			if orig != "" && ex.MAC == orig {
				next.Reservations[i] = r
				replaced = true
				break
			}
		}
		if orig != "" && !replaced {
			return nil, Userf("Die Reservierung für %s gibt es nicht mehr.", orig)
		}
		if !replaced {
			next.Reservations = append(next.Reservations, r)
		}
		return r, s.commit(ctx, prev, next, parts{dhcp: true, dns: true})
	})

	s.handle("dhcp.reservation.delete", func(ctx context.Context, raw json.RawMessage) (any, error) {
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
		n := len(next.Reservations)
		next.Reservations = deleteWhere(next.Reservations, func(r conf.Reservation) bool { return r.MAC == mac })
		if len(next.Reservations) == n {
			return nil, Userf("Keine Reservierung für %s gefunden.", mac)
		}
		return nil, s.commit(ctx, prev, next, parts{dhcp: true, dns: true})
	})

	s.handle("dns.record.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			OriginalName string `json:"original_name"`
			Name         string `json:"name"`
			IP           string `json:"ip"`
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
		name, err := conf.NormalizeName(p.Name)
		if err != nil {
			return nil, &UserError{err.Error()}
		}
		rec := conf.Record{Name: name, IP: strings.TrimSpace(p.IP)}
		next := prev.Clone()
		replaced := false
		if p.OriginalName != "" {
			orig, _ := conf.NormalizeName(p.OriginalName)
			for i, ex := range next.Records {
				if ex.Name == orig {
					next.Records[i] = rec
					replaced = true
				}
			}
			if !replaced {
				return nil, Userf("Den Eintrag %s gibt es nicht mehr.", p.OriginalName)
			}
		} else {
			next.Records = append(next.Records, rec)
		}
		return rec, s.commit(ctx, prev, next, parts{dns: true})
	})

	s.handle("dns.record.delete", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name string `json:"name"`
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
		name, err := conf.NormalizeName(p.Name)
		if err != nil {
			return nil, &UserError{err.Error()}
		}
		next := prev.Clone()
		n := len(next.Records)
		next.Records = deleteWhere(next.Records, func(r conf.Record) bool { return r.Name == name })
		if len(next.Records) == n {
			return nil, Userf("Kein DNS-Eintrag %s gefunden.", name)
		}
		return nil, s.commit(ctx, prev, next, parts{dns: true})
	})

	s.handle("dns.flush", func(ctx context.Context, _ json.RawMessage) (any, error) {
		// dnsmasq leert den Cache bei SIGHUP, ohne Neustart und ohne DNS-Aussetzer
		return nil, s.apply(ctx, "systemctl", "kill", "--signal=HUP", "vr-dnsmasq.service")
	})
}

func deleteWhere[T any](s []T, f func(T) bool) []T {
	out := s[:0:0]
	for _, x := range s {
		if !f(x) {
			out = append(out, x)
		}
	}
	return out
}
