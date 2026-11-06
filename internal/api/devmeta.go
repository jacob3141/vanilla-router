// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"vanilla-router/internal/conf"
	"vanilla-router/internal/sys"
)

// DevMeta holds details about a device that only concern the web interface (as on the FRITZ!Box):
// a custom name, a note, and when it was first and last seen.
type DevMeta struct {
	Alias     string    `json:"alias,omitempty"`
	Note      string    `json:"note,omitempty"`
	FirstSeen time.Time `json:"first_seen,omitzero"`
	LastSeen  time.Time `json:"last_seen,omitzero"`
	Baseline  bool      `json:"baseline,omitempty"` // was already there on the first pass, never counts as "new"
}

// NewWindow: how long a device counts as new after it first appears.
const NewWindow = 24 * time.Hour

type DeviceStore struct {
	mu    sync.Mutex
	m     map[string]*DevMeta
	file  string
	fresh bool // The file did not exist yet: the first pass only sets the baseline
}

func NewDeviceStore(dataDir string) *DeviceStore {
	s := &DeviceStore{m: map[string]*DevMeta{}}
	if dataDir == "" {
		return s
	}
	s.file = filepath.Join(dataDir, "devices.json")
	b, err := os.ReadFile(s.file)
	if errors.Is(err, fs.ErrNotExist) {
		s.fresh = true
	} else if err == nil {
		json.Unmarshal(b, &s.m)
	}
	return s
}

// Seen records devices that are currently online or have an active lease.
func (s *DeviceStore) Seen(macs []string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, mac := range macs {
		d, ok := s.m[mac]
		if !ok {
			d = &DevMeta{FirstSeen: now, Baseline: s.fresh}
			s.m[mac] = d
		}
		d.LastSeen = now
	}
	s.fresh = false
	s.saveLocked()
}

func (s *DeviceStore) Get(mac string) DevMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.m[mac]; ok {
		return *d
	}
	return DevMeta{}
}

func (s *DeviceStore) SetMeta(mac, alias, note string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[mac]
	if !ok {
		d = &DevMeta{FirstSeen: time.Now(), LastSeen: time.Now()}
		s.m[mac] = d
	}
	d.Alias, d.Note = alias, note
	s.saveLocked()
}

// NewSince counts devices that have appeared for the first time since the given moment.
func (s *DeviceStore) NewSince(t time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, d := range s.m {
		if !d.Baseline && d.FirstSeen.After(t) {
			n++
		}
	}
	return n
}

func (s *DeviceStore) saveLocked() {
	if s.file == "" {
		return
	}
	b, _ := json.MarshalIndent(s.m, "", " ")
	tmp := s.file + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, s.file)
	}
}

// track records every 2 minutes which devices are there, even if nobody has the web interface open.
func (s *Server) track(stop <-chan struct{}) {
	scan := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var macs []string
		if neigh, err := sys.Neighbors(ctx, "lan0"); err == nil {
			for _, n := range neigh {
				if n.Online() && strings.HasPrefix(n.IP, "10.") {
					macs = append(macs, n.MAC)
				}
			}
		}
		if leases, err := sys.ReadLeases(); err == nil {
			for _, l := range leases {
				if l.State == "active" && time.Until(l.Ends) > 0 {
					macs = append(macs, l.MAC)
				}
			}
		}
		if len(macs) > 0 {
			s.devices.Seen(macs, time.Now())
		}
	}
	scan()
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			scan()
		}
	}
}

// --- Endpoints -----------------------------------------------------------------------

func (s *Server) registerDeviceExtras() {
	s.route("POST /api/devices/meta", s.handleDeviceMeta)
	s.route("POST /api/devices/wake", s.handleWake)
	s.route("POST /api/blocks", s.forward("block.set", "Block saved."))
	s.route("POST /api/blocks/delete", s.forward("block.delete", "Block removed."))
}

func (s *Server) handleDeviceMeta(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MAC   string `json:"mac"`
		Alias string `json:"alias"`
		Note  string `json:"note"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	mac, err := conf.NormalizeMAC(req.MAC)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Alias, req.Note = strings.TrimSpace(req.Alias), strings.TrimSpace(req.Note)
	if len([]rune(req.Alias)) > 64 || len([]rune(req.Note)) > 500 {
		writeError(w, http.StatusBadRequest, "Name at most 64, note at most 500 characters.")
		return
	}
	s.devices.SetMeta(mac, req.Alias, req.Note)
	writeJSON(w, map[string]any{"ok": true, "message": "Saved."})
}

// handleWake sends a Wake-on-LAN packet (magic packet) as a broadcast into the home network.
func (s *Server) handleWake(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MAC string `json:"mac"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	mac, err := conf.NormalizeMAC(req.MAC)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := sendWOL(r.Context(), mac); err != nil {
		s.cfg.Log.Error("wake-on-lan", "err", err)
		writeError(w, http.StatusInternalServerError, "The wake packet could not be sent.")
		return
	}
	s.cfg.Log.Info("wake-on-lan sent", "mac", mac)
	writeJSON(w, map[string]any{"ok": true, "message": "Wake packet sent. The device must support Wake-on-LAN and be connected by cable."})
}

func magicPacket(mac string) ([]byte, error) {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return nil, errors.New("invalid MAC")
	}
	return append(bytes.Repeat([]byte{0xff}, 6), bytes.Repeat(hw, 16)...), nil
}

func sendWOL(ctx context.Context, mac string) error {
	pkt, err := magicPacket(mac)
	if err != nil {
		return err
	}
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		c.Control(func(fd uintptr) { serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1) })
		return serr
	}}
	conn, err := lc.ListenPacket(ctx, "udp4", net.JoinHostPort(conf.RouterIP.String(), "0"))
	if err != nil {
		return err
	}
	defer conn.Close()
	// Broadcast address of the home network, e.g. 192.168.42.255 for 192.168.42.0/24
	b := conf.LANPrefix.Masked().Addr().As4()
	host := uint32(1)<<(32-conf.LANPrefix.Bits()) - 1
	bcast := net.IPv4(b[0]|byte(host>>24), b[1]|byte(host>>16), b[2]|byte(host>>8), b[3]|byte(host))
	for _, port := range []int{9, 7} {
		if _, err := conn.WriteTo(pkt, &net.UDPAddr{IP: bcast, Port: port}); err != nil {
			return err
		}
	}
	return nil
}
