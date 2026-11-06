// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vanilla-router/internal/apply"
	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
)

// Settings for the internet port, home network and system (F-UI-03, F-UI-08, F-UI-09).
//
// Network changes go through apply's confirmation: without confirmation a
// systemd timer rolls them back, independently of this process. The deadline is longer for the home
// network, because devices first need a new address and you sign in again at the new address.
const (
	confirmWAN = 2 * time.Minute
	confirmLAN = 5 * time.Minute
	zoneinfo   = "/usr/share/zoneinfo"
)

// SettingsInfo is the response of settings.get.
type SettingsInfo struct {
	Hostname string           `json:"hostname"`
	Domain   string           `json:"domain"`
	Timezone string           `json:"timezone"`
	WAN      WANSettings      `json:"wan"`
	Detected *config.Detected `json:"detected"`
	Setup    bool             `json:"needs_setup"` // internet access still to be set up (F-UI-08)
	LAN      LANSettings      `json:"lan"`
	Pending  *PendingInfo     `json:"pending"`
	DryRun   bool             `json:"dry_run"`
}

// WANSettings are the fields of the "Internet" page. The PPPoE password is never returned.
type WANSettings struct {
	Type          string   `json:"type"`
	PPPoEUser     string   `json:"pppoe_user"`
	PPPoEVLAN     int      `json:"pppoe_vlan"`
	HasPassword   bool     `json:"has_password"`
	StaticAddress string   `json:"static_address"`
	StaticGateway string   `json:"static_gateway"`
	StaticDNS     []string `json:"static_dns"`
	IPv6          bool     `json:"ipv6"`
	MTU           int      `json:"mtu"`
}

// LANSettings are the fields for the home network.
type LANSettings struct {
	Address   string `json:"address"` // router address without prefix
	Prefix    int    `json:"prefix"`
	PoolStart string `json:"pool_start"`
	PoolEnd   string `json:"pool_end"`
	Lease     string `json:"lease"`
}

// PendingInfo describes a change waiting for confirmation.
type PendingInfo struct {
	Deadline time.Time `json:"deadline"`
}

func (s *Server) secrets() apply.DirSecrets { return apply.DirSecrets{Dir: s.Path(s.cfg.SecretsDir)} }

// commitConfig applies a whole configuration. confirm > 0: network changes must be confirmed.
// Returns the deadline or nil if there is nothing to confirm.
func (s *Server) commitConfig(nc *config.Config, confirm time.Duration) (*PendingInfo, error) {
	if err := nc.Validate(); err != nil {
		return nil, userConfigError(err)
	}
	a := s.applier()
	res, err := a.Apply(nc, s.secrets(), apply.Options{Confirm: confirm})
	if err != nil {
		return nil, &UserError{Msg: "The change was not applied: " + firstLine(err.Error())}
	}
	if !res.Pending {
		return nil, nil
	}
	if s.cfg.DryRun {
		// In a dry run no systemd timer starts, the helper does the rollback itself.
		genID := res.Generation
		time.AfterFunc(time.Until(res.Deadline), func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if err := s.applier().RollbackPending(genID); err != nil {
				s.cfg.Log.Error("dry run: rollback failed", "err", err)
			}
		})
	}
	return &PendingInfo{Deadline: res.Deadline}, nil
}

func (s *Server) pendingInfo() *PendingInfo {
	if p := s.applier().Pending(); p != nil {
		return &PendingInfo{Deadline: p.Deadline}
	}
	return nil
}

func settingsOf(c *config.Config) (WANSettings, LANSettings) {
	w := WANSettings{Type: c.WAN.Type, IPv6: c.WAN.IPv6.Mode == "auto", MTU: c.WAN.MTU, StaticDNS: []string{}}
	if p := c.WAN.PPPoE; p != nil {
		w.PPPoEUser, w.PPPoEVLAN = p.User, p.VLAN
	}
	if st := c.WAN.Static; st != nil {
		w.StaticAddress, w.StaticGateway = st.Address, st.Gateway
		w.StaticDNS = append(w.StaticDNS, st.DNS...)
	}
	l := LANSettings{PoolStart: c.DHCP.PoolStart, PoolEnd: c.DHCP.PoolEnd, Lease: c.DHCP.Lease}
	if p, err := netip.ParsePrefix(c.LAN.IPv4); err == nil {
		l.Address, l.Prefix = p.Addr().String(), p.Bits()
	}
	return w, l
}

// timezones reads the zones from zone1970.tab (one per country/region), plus UTC.
func timezones() []string {
	f, err := os.Open(filepath.Join(zoneinfo, "zone1970.tab"))
	if err != nil {
		return []string{"UTC"}
	}
	defer f.Close()
	out := []string{"UTC"}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "#") {
			continue
		}
		if f := strings.Split(l, "\t"); len(f) >= 3 {
			out = append(out, f[2])
		}
	}
	sort.Strings(out[1:])
	return out
}

// validZone checks that the time zone exists as a file (protection against paths also comes from Validate).
func validZone(tz string) bool {
	if tz == "UTC" {
		return true
	}
	if strings.Contains(tz, "..") || strings.HasPrefix(tz, "/") {
		return false
	}
	fi, err := os.Stat(filepath.Join(zoneinfo, filepath.FromSlash(tz)))
	return err == nil && fi.Mode().IsRegular()
}

// wanNets returns the IPv4 networks on the internet port (for the overlap check, F-NET-03).
func wanNets() []netip.Prefix {
	var out []netip.Prefix
	for _, name := range []string{gen.WANIf, "ppp0"} {
		ifc, err := net.InterfaceByName(name)
		if err != nil {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if p, err := netip.ParsePrefix(n.String()); err == nil && p.Addr().Is4() {
					out = append(out, p.Masked())
				}
			}
		}
	}
	return out
}

func (s *Server) registerSettings() {
	s.handle("settings.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		w, l := settingsOf(c)
		if c.WAN.Type == config.WANPPPoE && c.WAN.PPPoE != nil {
			_, err := s.secrets().Get(c.WAN.PPPoE.PasswordRef)
			w.HasPassword = err == nil
		}
		return SettingsInfo{Hostname: c.System.Hostname, Domain: c.DNS.Domain, Timezone: c.System.Timezone,
			WAN: w, Detected: c.WAN.Detected, Setup: c.WAN.NeedsSetup(), LAN: l,
			Pending: s.pendingInfo(), DryRun: s.cfg.DryRun}, nil
	})

	s.handle("settings.timezones.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"zones": timezones()}, nil
	})

	s.handle("settings.wan.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Type          string `json:"type"`
			PPPoEUser     string `json:"pppoe_user"`
			PPPoEPassword string `json:"pppoe_password"` // empty = keep the previous one
			PPPoEVLAN     int    `json:"pppoe_vlan"`
			Telekom       *struct {
				Anschlusskennung string `json:"line_id"`
				Zugangsnummer    string `json:"access_number"`
				Mitbenutzer      string `json:"co_user"`
			} `json:"telekom"`
			StaticAddress string   `json:"static_address"`
			StaticGateway string   `json:"static_gateway"`
			StaticDNS     []string `json:"static_dns"`
			IPv6          bool     `json:"ipv6"`
			MTU           int      `json:"mtu"`
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
		nc := *c
		w := c.WAN
		w.Type, w.MTU, w.PPPoE, w.Static, w.Detected = p.Type, p.MTU, nil, nil, nil
		w.IPv6.Mode = "off"
		if p.IPv6 {
			w.IPv6.Mode = "auto"
			if w.IPv6.RequestPrefix == 0 {
				w.IPv6.RequestPrefix = 56
			}
		}
		newRef := ""
		switch p.Type {
		case config.WANDHCP:
		case config.WANPPPoE:
			user := strings.TrimSpace(p.PPPoEUser)
			vlan := p.PPPoEVLAN
			if t := p.Telekom; t != nil {
				if user, err = config.TelekomUser(t.Anschlusskennung, t.Zugangsnummer, t.Mitbenutzer); err != nil {
					return nil, &UserError{err.Error()}
				}
				vlan = 7
			}
			if user == "" {
				return nil, Userf("Please enter the user name from your provider's letter.")
			}
			ref := ""
			if c.WAN.PPPoE != nil {
				ref = c.WAN.PPPoE.PasswordRef
			}
			if p.PPPoEPassword != "" {
				// New password under a new name: a rollback restores the old reference,
				// and the old password still matches it.
				newRef = "pppoe-" + newID()
				if err := s.secrets().Set(newRef, p.PPPoEPassword); err != nil {
					return nil, err
				}
				ref = newRef
			} else if _, err := s.secrets().Get(ref); ref == "" || err != nil {
				return nil, Userf("Please enter the password from your provider's letter.")
			}
			w.PPPoE = &config.PPPoE{User: user, PasswordRef: ref, VLAN: vlan}
		case config.WANStatic:
			dns := []string{}
			for _, d := range p.StaticDNS {
				if d = strings.TrimSpace(d); d != "" {
					dns = append(dns, d)
				}
			}
			w.Static = &config.StaticWAN{Address: strings.TrimSpace(p.StaticAddress), Gateway: strings.TrimSpace(p.StaticGateway), DNS: dns}
		default:
			return nil, Userf("Please choose a connection type.")
		}
		nc.WAN = w
		pend, err := s.commitConfig(&nc, confirmWAN)
		if err != nil {
			if newRef != "" {
				os.Remove(filepath.Join(s.secrets().Dir, newRef))
			}
			return nil, err
		}
		return map[string]any{"pending": pend}, nil
	})

	s.handle("settings.system.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Hostname string `json:"hostname"`
			Timezone string `json:"timezone"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		host := strings.ToLower(strings.TrimSpace(p.Hostname))
		if !config.ValidHostname(host) {
			return nil, Userf("The name \"%s\" does not work: only lowercase letters, digits and hyphens, at most 63 characters.", p.Hostname)
		}
		if !validZone(p.Timezone) {
			return nil, Userf("The time zone \"%s\" does not exist.", p.Timezone)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		nc := *c
		nc.System.Hostname, nc.System.Timezone = host, p.Timezone
		pend, err := s.commitConfig(&nc, 0)
		if err != nil {
			return nil, err
		}
		return map[string]any{"pending": pend, "new_url": "https://" + host + "." + c.DNS.Domain + "/",
			"restart": host != c.System.Hostname}, nil
	})

	s.handle("settings.lan.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Address string `json:"address"`
			Prefix  int    `json:"prefix"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		addr, err := netip.ParseAddr(strings.TrimSpace(p.Address))
		if err != nil || !addr.Is4() {
			return nil, Userf("\"%s\" is not an IPv4 address.", p.Address)
		}
		pfx := netip.PrefixFrom(addr, p.Prefix)
		if !pfx.IsValid() {
			return nil, Userf("The network size /%d does not work.", p.Prefix)
		}
		for _, w := range wanNets() {
			if w.Overlaps(pfx.Masked()) {
				return nil, Userf("The network %s overlaps with the network on the internet port (%s). Please choose another one, e.g. 192.168.50.1.", pfx.Masked(), w)
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		nc := *c
		if err := nc.MoveLAN(pfx.String()); err != nil {
			return nil, &UserError{err.Error()}
		}
		pend, err := s.commitConfig(&nc, confirmLAN)
		if err != nil {
			return nil, err
		}
		return map[string]any{"pending": pend, "new_url": "https://" + addr.String() + "/",
			"restart": nc.LAN.IPv4 != c.LAN.IPv4}, nil
	})

	s.handle("settings.dhcp.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			PoolStart string `json:"pool_start"`
			PoolEnd   string `json:"pool_end"`
			Lease     string `json:"lease"`
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
		nc := *c
		nc.DHCP.PoolStart, nc.DHCP.PoolEnd, nc.DHCP.Lease = strings.TrimSpace(p.PoolStart), strings.TrimSpace(p.PoolEnd), strings.TrimSpace(p.Lease)
		_, err = s.commitConfig(&nc, 0)
		return nil, err
	})

	s.handle("settings.confirm", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.applier().Confirm(); err != nil {
			return nil, Userf("No change is waiting for confirmation (deadline expired?).")
		}
		s.pruneSecrets()
		return nil, nil
	})

	s.handle("settings.revert", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		a := s.applier()
		if a.Pending() == nil {
			return nil, Userf("No change is waiting for confirmation.")
		}
		if err := a.RollbackLast(); err != nil {
			return nil, &UserError{Msg: "Rollback failed: " + firstLine(err.Error())}
		}
		return nil, nil
	})
}

// pruneSecrets deletes PPPoE and Wi-Fi passwords the confirmed configuration no longer refers to.
func (s *Server) pruneSecrets() {
	c, err := s.loadConfig()
	if err != nil {
		return
	}
	keep := map[string]bool{}
	if c.WAN.PPPoE != nil {
		keep[c.WAN.PPPoE.PasswordRef] = true
	}
	if c.WiFi != nil {
		keep[c.WiFi.PasswordRef] = true
	}
	dir := s.secrets().Dir
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if n := e.Name(); (n == "pppoe" || strings.HasPrefix(n, "pppoe-") || strings.HasPrefix(n, wifiRef)) && !keep[n] {
			os.Remove(filepath.Join(dir, n))
		}
	}
}
