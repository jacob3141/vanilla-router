// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"vanilla-router/internal/apd"
	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
)

// The fixed actions for vr-apd, the process that talks to the access points (D-04). Only its user may call them, see
// serveConn, and each checks its parameters itself: vr-apd faces the network.

const maxPendingPairings = 8

// bundleOf renders the real bundle of one AP and names it after its content.
func (s *Server) bundleOf(c *config.Config, id string) (apd.Bundle, error) {
	caps := s.apCaps(c.Wireless)
	files, err := gen.APFiles(c, id, gen.PlanChannels(c.Wireless, caps), caps, s.secrets(), gen.APOptions{})
	if err != nil {
		return apd.Bundle{}, err
	}
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s %o %s %d\n", f.Path, f.Mode, f.Group, len(f.Content))
		h.Write(f.Content)
	}
	return apd.Bundle{Generation: hex.EncodeToString(h.Sum(nil))[:16], Files: files}, nil
}

func (s *Server) registerAPActions() {
	// The router's identity for the TLS listener. It lives only in the memory of vr-apd.
	s.handle("ap.identity", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		priv, err := s.controllerKey()
		if err != nil {
			return nil, err
		}
		return map[string]string{"seed": hex.EncodeToString(priv.Seed())}, nil
	})

	s.handle("ap.authorize", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p apd.KeyParam
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		for _, a := range apsOf(c.Wireless) {
			if a.PublicKey == p.PublicKey {
				return apd.Authorized{ID: a.ID}, nil
			}
		}
		return apd.Authorized{}, nil
	})

	s.handle("ap.pairing.add", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p apd.AddPairing
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		if c.Wireless == nil {
			return nil, Userf("The Wi-Fi networks are not set up yet.")
		}
		priv, err := s.controllerKey()
		if err != nil {
			return nil, err
		}
		if len(p.Radios) > config.MaxRadios || len(p.Hostname) > 64 || len(p.Model) > 64 {
			return nil, Userf("Invalid parameters.")
		}
		// The same AP asking again replaces its own request, so restarting an installation leaves no trace
		sum := sha256.Sum256([]byte(p.PublicKey))
		id := "r" + hex.EncodeToString(sum[:6])
		r := PairingRequest{ID: id, PublicKey: p.PublicKey, MAC: p.MAC, Hostname: p.Hostname, Model: p.Model, Radios: p.Radios,
			RouterNonce: p.RouterNonce, APNonce: p.APNonce, Commitment: p.Commitment, Received: time.Now()}
		if _, err := pairingCode(r, priv.Public().(ed25519.PublicKey)); err != nil {
			return nil, Userf("Invalid pairing request: %v", err)
		}
		reqs := s.pairingRequests(c, priv.Public().(ed25519.PublicKey))
		if len(reqs) >= maxPendingPairings && !slices.ContainsFunc(reqs, func(x PairingInfo) bool { return x.ID == id }) {
			return nil, Userf("Too many access points are waiting.")
		}
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return nil, err
		}
		return nil, config.WriteFileAtomic(filepath.Join(s.Path(apPairingDir), id+".json"), b, 0o644)
	})

	s.handle("ap.pairing.status", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p apd.KeyParam
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(apsOf(c.Wireless), func(a config.AccessPoint) bool { return a.PublicKey == p.PublicKey }) {
			return apd.PairStatus{State: apd.PairPaired}, nil
		}
		sum := sha256.Sum256([]byte(p.PublicKey))
		if _, err := readJSONFile[PairingRequest](filepath.Join(s.Path(apPairingDir), "r"+hex.EncodeToString(sum[:6])+".json")); err == nil {
			return apd.PairStatus{State: apd.PairWaiting}, nil
		}
		return apd.PairStatus{State: apd.PairUnknown}, nil
	})

	s.handle("ap.bundle", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		if c.Wireless == nil || c.Wireless.AccessPoint(p.ID) == nil {
			return nil, Userf("unknown access point")
		}
		// While a change waits for its confirmation the APs stay on the old one
		if s.pendingInfo() != nil {
			return apd.Bundle{}, nil
		}
		return s.bundleOf(c, p.ID)
	})

	s.handle("ap.report", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p apd.ReportParam
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !safeName.MatchString(p.ID) || len(p.Report.Version) > 64 || len(p.Report.Applied) > 32 || len(p.Report.Error) > 400 {
			return nil, Userf("Invalid parameters.")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		if c.Wireless == nil || c.Wireless.AccessPoint(p.ID) == nil {
			return nil, Userf("unknown access point")
		}
		// New cards or new abilities are kept for the plan
		if len(p.Report.Radios) > 0 && len(p.Report.Radios) <= config.MaxRadios {
			valid := !slices.ContainsFunc(p.Report.Radios, func(r gen.RadioCaps) bool { return !r.Valid() })
			if valid {
				if b, err := json.MarshalIndent(p.Report.Radios, "", "  "); err == nil {
					config.WriteFileAtomic(filepath.Join(s.Path(apCapsDir), p.ID+".json"), append(b, '\n'), 0o644)
				}
			}
		}
		rep := APReport{Seen: time.Now(), Version: p.Report.Version, Clients: p.Report.Clients, Applied: p.Report.Applied, Error: strings.TrimSpace(p.Report.Error)}
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return nil, err
		}
		return nil, config.WriteFileAtomic(filepath.Join(s.Path(apOnlineDir), p.ID+".json"), b, 0o644)
	})
}
