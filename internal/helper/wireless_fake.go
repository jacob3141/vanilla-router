// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
	"vanilla-router/internal/pairing"
)

// Simulated access points for the test instance (--dry-run --fake-wifi). They play the part of vr-apd: two APs wait
// for pairing with real keys and real commitments, so the page computes real codes. Nothing touches a radio or the
// network. Only documentation MACs (02:00:…).

func (s *Server) fakeAPs() bool { return s.cfg.DryRun && s.cfg.FakeWiFi }

type fakeAP struct {
	host, model, mac string
	radios           []gen.RadioCaps
}

var fakeAPList = []fakeAP{
	{"hall", "Mini PC N100, 2.5 GbE", "02:00:00:00:00:10", []gen.RadioCaps{
		{ID: "pci-0000-03-00-0", Path: "pci-0000:03:00.0", MAC: "02:00:00:00:01:00", Chip: "MediaTek MT7916", Bands: []string{"5", "6"}, DFS: true, Width160: true},
		{ID: "pci-0000-04-00-0", Path: "pci-0000:04:00.0", MAC: "02:00:00:00:02:00", Chip: "MediaTek MT7915", Bands: []string{"2.4", "5"}, DFS: true},
	}},
	{"attic", "Mini PC N150, 2.5 GbE", "02:00:00:00:00:11", []gen.RadioCaps{
		{ID: "pci-0000-02-00-0", Path: "pci-0000:02:00.0", MAC: "02:00:00:00:03:00", Chip: "MediaTek MT7922", Bands: []string{"2.4", "5", "6"}},
	}},
}

// seedFakePairing writes the requests of the simulated APs, unless they are paired already.
func (s *Server) seedFakePairing() {
	dir := s.Path(apPairingDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	c, _ := s.loadConfig()
	for i, f := range fakeAPList {
		if c != nil && c.Wireless != nil && func() bool {
			for _, a := range c.Wireless.AccessPoints {
				if a.MAC == f.mac {
					return true
				}
			}
			return false
		}() {
			continue
		}
		pub, _, err := pairing.NewKey()
		if err != nil {
			return
		}
		nr, _ := pairing.Nonce()
		na, _ := pairing.Nonce()
		r := PairingRequest{ID: "sim" + string(rune('1'+i)), PublicKey: pairing.FormatKey(pub), MAC: f.mac, Hostname: f.host,
			Model: f.model, Radios: f.radios, RouterNonce: hex.EncodeToString(nr), APNonce: hex.EncodeToString(na),
			Commitment: hex.EncodeToString(pairing.Commit(na)), Received: time.Now().Add(-time.Duration(len(fakeAPList)-i) * time.Minute)}
		b, _ := json.MarshalIndent(r, "", "  ")
		config.WriteFileAtomic(filepath.Join(dir, r.ID+".json"), b, 0o644)
	}
}

// simulatePaired lets a freshly paired simulated AP report in, as vr-apd will.
func (s *Server) simulatePaired(id string) {
	if !s.fakeAPs() {
		return
	}
	rep := APReport{Seen: time.Now(), Version: "simulated", Clients: 3}
	if c, err := s.loadConfig(); err == nil {
		if b, err := s.bundleOf(c, id); err == nil {
			rep.Applied = b.Generation
		}
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	config.WriteFileAtomic(filepath.Join(s.Path(apOnlineDir), id+".json"), b, 0o644)
}

// refreshFakeReports keeps the simulated APs online while the test instance runs.
func (s *Server) refreshFakeReports() {
	if !s.fakeAPs() {
		return
	}
	go func() {
		for range time.Tick(30 * time.Second) {
			s.mu.Lock()
			c, err := s.loadConfig()
			if err == nil && c.Wireless != nil {
				for _, a := range c.Wireless.AccessPoints {
					if slices.ContainsFunc(fakeAPList, func(f fakeAP) bool { return f.mac == a.MAC }) {
						s.simulatePaired(a.ID)
					}
				}
			}
			s.mu.Unlock()
		}
	}()
}

func (s *Server) registerFakeWireless() {
	// Test instance only: the simulated APs that are not paired ask again, with new keys and codes.
	s.handle("wireless.simulate", func(_ context.Context, _ json.RawMessage) (any, error) {
		if !s.fakeAPs() {
			return nil, Userf("Only in the test instance.")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		os.RemoveAll(s.Path(apPairingDir))
		s.seedFakePairing()
		return nil, nil
	})
}
