// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"vanilla-router/internal/config"
)

// Every physical port has a role. The Linux names (wan0, lanp1 …) are an internal detail. A role is the
// only thing the web interface knows about, so another use of a port, such as a second internet connection
// for failover (F-NET-11), becomes one more role here.
const (
	roleWAN    = "wan"
	roleLAN    = "lan"
	roleUnused = "unused"

	// Only for Docker networks: what else a network can hang off
	roleSegment = "segment"
	roleFree    = "free"

	identifySeconds = 10
	sysNet          = "/sys/class/net"
)

// PortInfo is one physical port in the web interface.
type PortInfo struct {
	MAC       string `json:"mac"`
	Role      string `json:"role"`
	Label     string `json:"label"` // WAN, LAN 1 …, empty for a free port
	Present   bool   `json:"present"`
	Carrier   bool   `json:"carrier"`
	SpeedMbit int    `json:"speed_mbit"`
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
	Errors    uint64 `json:"errors"`
}

// sysPort is what the kernel knows about a port.
type sysPort struct {
	name string
	info PortInfo
}

func readSys(dir, name string) string {
	b, _ := os.ReadFile(filepath.Join(dir, name))
	return strings.TrimSpace(string(b))
}

func readCount(dir, name string) uint64 {
	n, _ := strconv.ParseUint(readSys(dir, "statistics/"+name), 10, 64)
	return n
}

// physicalPorts lists the wired ports. Bridges, VLANs and the like have no device behind them, and Wi-Fi
// adapters are handled by the Wi-Fi pages.
func physicalPorts() []sysPort {
	ents, err := os.ReadDir(sysNet)
	if err != nil {
		return nil
	}
	var out []sysPort
	for _, e := range ents {
		dir := filepath.Join(sysNet, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "device")); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "wireless")); err == nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "phy80211")); err == nil {
			continue
		}
		if readSys(dir, "type") != "1" {
			continue
		}
		speed, _ := strconv.Atoi(readSys(dir, "speed")) // -1 or an error without a link
		out = append(out, sysPort{name: e.Name(), info: PortInfo{
			MAC: strings.ToLower(readSys(dir, "address")), Present: true, Carrier: readSys(dir, "carrier") == "1",
			SpeedMbit: max(speed, 0), RxBytes: readCount(dir, "rx_bytes"), TxBytes: readCount(dir, "tx_bytes"),
			Errors: readCount(dir, "rx_errors") + readCount(dir, "tx_errors"),
		}})
	}
	return out
}

// sysPorts is physicalPorts, or simulated ports for the test instance.
func (s *Server) sysPorts(c *config.Config) []sysPort {
	if !s.cfg.DryRun {
		return physicalPorts()
	}
	var out []sysPort
	for i, m := range append([]string{c.WAN.MatchMAC, "02:00:00:00:00:0a"}, c.LAN.Members...) {
		out = append(out, sysPort{name: "sim" + strconv.Itoa(i), info: PortInfo{MAC: strings.ToLower(m), Present: true,
			Carrier: m != "02:00:00:00:00:0a", SpeedMbit: 1000, RxBytes: uint64(i+1) * 7 << 30, TxBytes: uint64(i+1) << 30}})
	}
	return out
}

// portList merges what the kernel sees with the configuration. A configured port that is missing, for
// example after a hardware change, stays visible, so that it can be taken out of use.
func (s *Server) portList(c *config.Config) []PortInfo {
	byMAC := map[string]PortInfo{}
	for _, p := range s.sysPorts(c) {
		p.info.Role = roleUnused
		byMAC[p.info.MAC] = p.info
	}
	var out []PortInfo
	take := func(mac, role, label string) {
		mac = strings.ToLower(mac)
		p, ok := byMAC[mac]
		if !ok {
			p = PortInfo{MAC: mac}
		}
		p.Role, p.Label = role, label
		delete(byMAC, mac)
		out = append(out, p)
	}
	take(c.WAN.MatchMAC, roleWAN, "WAN")
	for i, m := range c.LAN.Members {
		take(m, roleLAN, "LAN "+strconv.Itoa(i+1))
	}
	var free []PortInfo
	for _, p := range byMAC {
		free = append(free, p)
	}
	slices.SortFunc(free, func(a, b PortInfo) int { return strings.Compare(a.MAC, b.MAC) })
	return append(out, free...)
}

func (s *Server) registerInterfaces() {
	s.handle("interfaces.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		return map[string]any{"ports": s.portList(c), "pending": s.pendingInfo()}, nil
	})

	s.handle("interfaces.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Ports []struct {
				MAC  string `json:"mac"`
				Role string `json:"role"`
			} `json:"ports"`
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
		known := map[string]bool{}
		for _, k := range s.portList(c) {
			known[k.MAC] = true
		}
		var wan string
		wans := 0
		lan := map[string]bool{}
		for _, x := range p.Ports {
			mac := strings.ToLower(x.MAC)
			if !known[mac] {
				return nil, Userf("The port %s does not exist on this router.", x.MAC)
			}
			switch x.Role {
			case roleWAN:
				wan = mac
				wans++
			case roleLAN:
				lan[mac] = true
			case roleUnused:
			default:
				return nil, Userf("Unknown use for a port.")
			}
		}
		if wans != 1 {
			return nil, Userf("Exactly one port must be the internet port (WAN).")
		}
		if len(lan) == 0 {
			return nil, Userf("At least one port must be in the home network (LAN), or the router could no longer be reached.")
		}
		nc := *c
		nc.WAN.MatchMAC = wan
		nc.LAN.Members = lanOrder(c.LAN.Members, lan)
		pend, err := s.commitConfig(&nc, confirmLAN)
		if err != nil {
			return nil, err
		}
		return map[string]any{"pending": pend}, nil
	})

	s.handle("interfaces.identify", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			MAC string `json:"mac"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		c, err := s.loadConfig()
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		for _, x := range s.sysPorts(c) {
			if x.info.MAC != strings.ToLower(p.MAC) {
				continue
			}
			if s.cfg.DryRun {
				return nil, nil
			}
			return nil, blink(x.name)
		}
		return nil, Userf("This port is not present on the router.")
	})
}

// lanOrder keeps every port that stays in its place, because a port whose number changes is reconnected for
// nothing. A new port takes the place of one that left.
func lanOrder(old []string, lan map[string]bool) []string {
	stays := map[string]bool{}
	for _, m := range old {
		stays[strings.ToLower(m)] = true
	}
	var fresh []string
	for m := range lan {
		if !stays[m] {
			fresh = append(fresh, m)
		}
	}
	slices.Sort(fresh)
	var out []string
	for _, m := range old {
		m = strings.ToLower(m)
		if lan[m] {
			out = append(out, m)
		} else if len(fresh) > 0 {
			out, fresh = append(out, fresh[0]), fresh[1:]
		}
	}
	return append(out, fresh...)
}

// blink lets the port's LED flash. ethtool blocks while it blinks, so only an immediate failure, typically
// a driver without this function, can be reported.
func blink(name string) error {
	cmd := exec.Command("ethtool", "--identify", name, strconv.Itoa(identifySeconds))
	if err := cmd.Start(); err != nil {
		return Userf("The LED cannot be controlled on this router.")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return Userf("The driver of this port cannot make its LED blink.")
		}
	case <-time.After(time.Second):
	}
	return nil
}
