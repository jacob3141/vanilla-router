// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
)

// File share (Samba and the file browser): off by default. There is one account, smbuser, its password lives only in
// Samba's own database and never in config.json.
const shareUser = "smbuser"

func validSharePassword(s string) bool {
	if len(s) < 8 || len(s) > 127 {
		return false
	}
	return !strings.ContainsAny(s, "\r\n\x00")
}

// setSharePassword writes the password into Samba's database.
func (s *Server) setSharePassword(ctx context.Context, password string) error {
	if s.cfg.DryRun {
		s.cfg.Log.Info("dry run: Samba password not set")
		return nil
	}
	cmd := exec.CommandContext(ctx, "smbpasswd", "-s", "-a", shareUser)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C.UTF-8"}
	cmd.Stdin = strings.NewReader(password + "\n" + password + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		s.cfg.Log.Error("smbpasswd", "err", err, "out", strings.TrimSpace(string(out)))
		return Userf("The password could not be set.")
	}
	return nil
}

func (s *Server) sharePasswordSet(ctx context.Context) bool {
	if s.cfg.DryRun {
		return false
	}
	_, err := run(ctx, "pdbedit", "-L", "-u", shareUser)
	return err == nil
}

func (s *Server) registerFileShare() {
	s.handle("fileshare.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		return map[string]any{"enabled": gen.FileShareOn(c), "has_password": s.sharePasswordSet(ctx), "user": shareUser,
			"share": "guest", "address": c.System.Hostname + "." + c.DNS.Domain, "dry_run": s.cfg.DryRun}, nil
	})

	s.handle("fileshare.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Enabled  bool   `json:"enabled"`
			Password string `json:"password"` // empty = keep
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if p.Password != "" && !validSharePassword(p.Password) {
			return nil, Userf("The password needs 8 to 127 characters.")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		if p.Enabled && p.Password == "" && !s.cfg.DryRun && !s.sharePasswordSet(ctx) {
			return nil, Userf("Please enter a password for the file share.")
		}
		if p.Password != "" {
			if err := s.setSharePassword(ctx, p.Password); err != nil {
				return nil, err
			}
		}
		nc := *c
		nc.Services.Files = &config.FileShare{Enabled: p.Enabled}
		if _, err := s.commitConfig(&nc, 0); err != nil {
			return nil, err
		}
		return nil, nil
	})
}
