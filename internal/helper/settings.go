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

// Einstellungen für Internet-Anschluss, Heimnetz und System (F-UI-03, F-UI-08, F-UI-09).
//
// Netzwerk-Änderungen laufen über die Bestätigung von apply: Ohne Bestätigung nimmt ein
// systemd-Timer sie zurück, unabhängig von diesem Prozess. Beim Heimnetz ist die Frist länger, weil
// die Geräte erst eine neue Adresse brauchen und man sich unter der neuen Adresse neu anmeldet.
const (
	confirmWAN = 2 * time.Minute
	confirmLAN = 5 * time.Minute
	zoneinfo   = "/usr/share/zoneinfo"
)

// SettingsInfo ist die Antwort von settings.get.
type SettingsInfo struct {
	Hostname string           `json:"hostname"`
	Domain   string           `json:"domain"`
	Timezone string           `json:"timezone"`
	WAN      WANSettings      `json:"wan"`
	Detected *config.Detected `json:"detected"`
	Setup    bool             `json:"needs_setup"` // Internetzugang noch einrichten (F-UI-08)
	LAN      LANSettings      `json:"lan"`
	Pending  *PendingInfo     `json:"pending"`
	DryRun   bool             `json:"dry_run"`
}

// WANSettings sind die Felder der Seite „Internet“. Das PPPoE-Passwort wird nie zurückgegeben.
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

// LANSettings sind die Felder für das Heimnetz.
type LANSettings struct {
	Address   string `json:"address"` // Adresse des Routers ohne Präfix
	Prefix    int    `json:"prefix"`
	PoolStart string `json:"pool_start"`
	PoolEnd   string `json:"pool_end"`
	Lease     string `json:"lease"`
}

// PendingInfo beschreibt eine Änderung, die auf Bestätigung wartet.
type PendingInfo struct {
	Deadline time.Time `json:"deadline"`
}

func (s *Server) secrets() apply.DirSecrets { return apply.DirSecrets{Dir: s.Path(s.cfg.SecretsDir)} }

// commitConfig spielt eine ganze Konfiguration ein. confirm > 0: Netzwerk-Änderungen müssen bestätigt
// werden. Liefert die Frist oder nil, wenn nichts zu bestätigen ist.
func (s *Server) commitConfig(nc *config.Config, confirm time.Duration) (*PendingInfo, error) {
	if err := nc.Validate(); err != nil {
		return nil, userConfigError(err)
	}
	a := s.applier()
	res, err := a.Apply(nc, s.secrets(), apply.Options{Confirm: confirm})
	if err != nil {
		return nil, &UserError{Msg: "Die Änderung wurde nicht übernommen: " + firstLine(err.Error())}
	}
	if !res.Pending {
		return nil, nil
	}
	if s.cfg.DryRun {
		// Im Probelauf startet kein systemd-Timer, die Rücknahme übernimmt der Helper selbst.
		genID := res.Generation
		time.AfterFunc(time.Until(res.Deadline), func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if err := s.applier().RollbackPending(genID); err != nil {
				s.cfg.Log.Error("probelauf: rücknahme fehlgeschlagen", "err", err)
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

// timezones liest die Zonen aus zone1970.tab (eine pro Land/Region), dazu UTC.
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

// validZone prüft, dass es die Zeitzone als Datei gibt (Schutz vor Pfaden kommt zusätzlich aus Validate).
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

// wanNets liefert die IPv4-Netze am Internet-Anschluss (für die Prüfung auf Überschneidung, F-NET-03).
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
			PPPoEPassword string `json:"pppoe_password"` // leer = bisheriges behalten
			PPPoEVLAN     int    `json:"pppoe_vlan"`
			Telekom       *struct {
				Anschlusskennung string `json:"anschlusskennung"`
				Zugangsnummer    string `json:"zugangsnummer"`
				Mitbenutzer      string `json:"mitbenutzer"`
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
				return nil, Userf("Bitte den Benutzernamen aus dem Brief des Anbieters eingeben.")
			}
			ref := ""
			if c.WAN.PPPoE != nil {
				ref = c.WAN.PPPoE.PasswordRef
			}
			if p.PPPoEPassword != "" {
				// Neues Passwort unter neuem Namen: Eine Rücknahme stellt den alten Verweis wieder her,
				// das alte Passwort passt dann noch dazu.
				newRef = "pppoe-" + newID()
				if err := s.secrets().Set(newRef, p.PPPoEPassword); err != nil {
					return nil, err
				}
				ref = newRef
			} else if _, err := s.secrets().Get(ref); ref == "" || err != nil {
				return nil, Userf("Bitte das Passwort aus dem Brief des Anbieters eingeben.")
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
			return nil, Userf("Bitte eine Anschlussart wählen.")
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
			return nil, Userf("Der Name „%s“ geht nicht: nur Kleinbuchstaben, Ziffern und Bindestrich, höchstens 63 Zeichen.", p.Hostname)
		}
		if !validZone(p.Timezone) {
			return nil, Userf("Die Zeitzone „%s“ gibt es nicht.", p.Timezone)
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
			return nil, Userf("„%s“ ist keine IPv4-Adresse.", p.Address)
		}
		pfx := netip.PrefixFrom(addr, p.Prefix)
		if !pfx.IsValid() {
			return nil, Userf("Die Netzgröße /%d geht nicht.", p.Prefix)
		}
		for _, w := range wanNets() {
			if w.Overlaps(pfx.Masked()) {
				return nil, Userf("Das Netz %s überschneidet sich mit dem Netz am Internet-Anschluss (%s). Bitte ein anderes wählen, z. B. 192.168.50.1.", pfx.Masked(), w)
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
			return nil, Userf("Es wartet keine Änderung auf Bestätigung (Frist abgelaufen?).")
		}
		s.pruneSecrets()
		return nil, nil
	})

	s.handle("settings.revert", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		a := s.applier()
		if a.Pending() == nil {
			return nil, Userf("Es wartet keine Änderung auf Bestätigung.")
		}
		if err := a.RollbackLast(); err != nil {
			return nil, &UserError{Msg: "Zurücknehmen fehlgeschlagen: " + firstLine(err.Error())}
		}
		return nil, nil
	})
}

// pruneSecrets löscht PPPoE-Passwörter, auf die die bestätigte Konfiguration nicht mehr verweist.
func (s *Server) pruneSecrets() {
	c, err := s.loadConfig()
	if err != nil {
		return
	}
	keep := ""
	if c.WAN.PPPoE != nil {
		keep = c.WAN.PPPoE.PasswordRef
	}
	dir := s.secrets().Dir
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if n := e.Name(); (n == "pppoe" || strings.HasPrefix(n, "pppoe-")) && n != keep {
			os.Remove(filepath.Join(dir, n))
		}
	}
}
