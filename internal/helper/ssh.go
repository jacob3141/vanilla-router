// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/json"
	"strings"
)

// SSH access for the user admin (F-SEC-04): off by default, keys only, from the home network only. The installed system
// has no password login at all, so this page is the only way to a shell.
func (s *Server) registerSSH() {
	s.handle("ssh.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		keys := c.Services.SSH.AuthorizedKeys
		if keys == nil {
			keys = []string{}
		}
		return map[string]any{"enabled": c.Services.SSH.Enabled, "keys": keys, "dry_run": s.cfg.DryRun}, nil
	})

	s.handle("ssh.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Enabled bool     `json:"enabled"`
			Keys    []string `json:"keys"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		keys := []string{}
		for _, k := range p.Keys {
			if k = strings.TrimSpace(k); k != "" {
				keys = append(keys, k)
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		nc := *c
		nc.Services.SSH.Enabled = p.Enabled
		nc.Services.SSH.AuthorizedKeys = keys
		if _, err := s.commitConfig(&nc, 0); err != nil {
			return nil, err
		}
		return nil, nil
	})
}
