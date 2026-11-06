// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"vanilla-router/internal/apply"
	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
	"vanilla-router/internal/sys"
)

// Wi-Fi access point (F-WIFI, D-18). Like the home network, a change goes through apply's confirmation. The
// deadline is generous, because whoever is on Wi-Fi has to reconnect first, perhaps with a new password.
const (
	confirmWiFi = 5 * time.Minute
	wifiInstall = "vr-wifi-install"
	wifiRef     = "wifi-"
	// onlineComplete is the stamp of vrctl complete (apply.CompleteStamp, D-17): all firmware has been fetched.
	onlineComplete = "/var/lib/vanilla-router/online-complete"
)

// WiFiInfo is the response of wifi.get. The password is never returned (F-WIFI-05).
type WiFiInfo struct {
	Installed      bool              `json:"installed"`
	CardNoDriver   bool              `json:"card_without_driver"`
	FirmwareDone   bool              `json:"firmware_fetched"`
	Adapters       []sys.WiFiAdapter `json:"adapters"`
	Config         WiFiSettings      `json:"config"`
	Configured     bool              `json:"configured"`
	HasPassword    bool              `json:"has_password"`
	Countries      []WiFiCountry     `json:"countries"`
	Status         WiFiStatus        `json:"status"`
	Pending        *PendingInfo      `json:"pending"`
	DryRun         bool              `json:"dry_run"`
	InstallRunning bool              `json:"install_running"`
}

// WiFiSettings are the fields of the page.
type WiFiSettings struct {
	Enabled   bool   `json:"enabled"`
	Interface string `json:"interface"`
	SSID      string `json:"ssid"`
	Country   string `json:"country"`
	Band      string `json:"band"`
	Channel   int    `json:"channel"`
	Width     int    `json:"width"`
}

// WiFiCountry is an entry of the country list.
type WiFiCountry struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// WiFiStatus is the current state of the access point.
type WiFiStatus struct {
	Running bool              `json:"running"`
	SSID    string            `json:"ssid"`
	Channel int               `json:"channel"`
	Width   int               `json:"width"`
	Clients []sys.WiFiStation `json:"clients"`
}

// wifiHW is everything that depends on real hardware. The test instance replaces it (--fake-wifi).
type wifiHW interface {
	installed() bool
	adapters(ctx context.Context) []sys.WiFiAdapter
	cardWithoutDriver() bool
	status(ctx context.Context, c *config.Config) WiFiStatus
}

type realWiFi struct{}

func (realWiFi) installed() bool {
	return fileExists("/usr/sbin/hostapd") && fileExists("/usr/sbin/iw")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func (realWiFi) cardWithoutDriver() bool { return sys.WiFiCardWithoutDriver("/") }

func (r realWiFi) adapters(ctx context.Context) []sys.WiFiAdapter {
	out := sys.WiFiAdapters("/")
	if !r.installed() {
		return out
	}
	for i := range out {
		if !safeName.MatchString(out[i].Phy) {
			continue
		}
		if s, err := run(ctx, "iw", "phy", out[i].Phy, "info"); err == nil {
			out[i].WiFiCaps = sys.ParseIwPhy(s)
		}
	}
	return out
}

func (r realWiFi) status(ctx context.Context, c *config.Config) WiFiStatus {
	st := WiFiStatus{Clients: []sys.WiFiStation{}}
	if c.WiFi == nil || !r.installed() || !safeName.MatchString(c.WiFi.Interface) {
		return st
	}
	out, err := run(ctx, "iw", "dev", c.WiFi.Interface, "info")
	if err != nil {
		return st
	}
	l := sys.ParseIwLink(out)
	st.Running, st.SSID, st.Channel, st.Width = l.AP && l.SSID != "", l.SSID, l.Channel, l.Width
	if st.Running {
		if out, err := run(ctx, "iw", "dev", c.WiFi.Interface, "station", "dump"); err == nil {
			st.Clients = sys.ParseIwStations(out)
		}
	}
	return st
}

// safeName: names from sysfs and config.json go to iw as arguments, never through a shell, and still must
// not look like an option.
var safeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,14}$`)

// fakeWiFi simulates an adapter for the test instance. Nothing here touches a real radio.
type fakeWiFi struct{ mu *installFlag }

// installFlag lets the test instance show the page before and after the download.
type installFlag struct{ done bool }

func (f fakeWiFi) installed() bool         { return f.mu.done }
func (f fakeWiFi) cardWithoutDriver() bool { return false }
func (f fakeWiFi) adapters(context.Context) []sys.WiFiAdapter {
	ch := func(list ...int) []sys.WiFiChannel {
		var out []sys.WiFiChannel
		for _, c := range list {
			out = append(out, sys.WiFiChannel{Channel: c})
		}
		return out
	}
	a := sys.WiFiAdapter{Name: "wlp3s0", Phy: "phy0", MAC: "02:00:00:00:00:30", Driver: "simulated"}
	if f.mu.done {
		a.WiFiCaps = sys.WiFiCaps{Known: true, AP: true, Bands: []sys.WiFiBand{
			{Band: "2.4", HT40: true, Channels: ch(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13)},
			{Band: "5", HT40: true, VHT: true, Channels: ch(36, 40, 44, 48)},
		}}
	}
	return []sys.WiFiAdapter{a}
}
func (f fakeWiFi) status(_ context.Context, c *config.Config) WiFiStatus {
	st := WiFiStatus{Clients: []sys.WiFiStation{}}
	if gen.WiFiOn(c) {
		st.Running, st.SSID, st.Channel, st.Width = true, c.WiFi.SSID, c.WiFi.Channel, c.WiFi.Width
		st.Clients = []sys.WiFiStation{
			{MAC: "02:00:00:00:00:41", SignalDBm: -48, HasSignal: true, RxBitrate: 433.3, TxBitrate: 866.7, Connected: 5400},
			{MAC: "02:00:00:00:00:42", SignalDBm: -79, HasSignal: true, RxBitrate: 6, TxBitrate: 58.5, Connected: 42},
		}
	}
	return st
}

// countries reads the country list from the time zone data, a file every system has.
func countries(path string) []WiFiCountry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []WiFiCountry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "#") {
			continue
		}
		if code, name, ok := strings.Cut(l, "\t"); ok && len(code) == 2 {
			out = append(out, WiFiCountry{Code: code, Name: name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// countryOfZone suggests the country from the time zone. The owner confirms it, it only is a preselection:
// the router's actual location decides what is legal.
func countryOfZone(path, tz string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Split(sc.Text(), "\t")
		if len(p) >= 3 && p[2] == tz && !strings.HasPrefix(p[0], "#") {
			code, _, _ := strings.Cut(p[0], ",")
			return code
		}
	}
	return ""
}

// checkAdapter compares the choice with what the adapter reports. A channel only refused for the moment
// (NoIR) is allowed through: before the first start the kernel knows no country, so much of 5 GHz looks
// forbidden. If it really does not work, the start fails and the rollback restores the old state.
func checkAdapter(adapters []sys.WiFiAdapter, w *config.WiFi) error {
	i := slices.IndexFunc(adapters, func(a sys.WiFiAdapter) bool { return a.Name == w.Interface })
	if i < 0 {
		return Userf("The Wi-Fi adapter %s was not found.", w.Interface)
	}
	a := adapters[i]
	if !a.Known {
		return nil // iw could not say anything, the start will show it
	}
	if !a.AP {
		return Userf("The Wi-Fi adapter %s cannot run as an access point.", w.Interface)
	}
	b := slices.IndexFunc(a.Bands, func(x sys.WiFiBand) bool { return x.Band == w.Band })
	if b < 0 {
		return Userf("The Wi-Fi adapter %s does not support the %s GHz band.", w.Interface, w.Band)
	}
	band := a.Bands[b]
	c := slices.IndexFunc(band.Channels, func(x sys.WiFiChannel) bool { return x.Channel == w.Channel })
	if c < 0 || band.Channels[c].Disabled {
		return Userf("The Wi-Fi adapter %s cannot use channel %d.", w.Interface, w.Channel)
	}
	if w.Width >= 40 && !band.HT40 || w.Width == 80 && !band.VHT {
		return Userf("The Wi-Fi adapter %s does not support a channel width of %d MHz.", w.Interface, w.Width)
	}
	return nil
}

func (s *Server) wifiInfo(ctx context.Context, c *config.Config) WiFiInfo {
	info := WiFiInfo{
		Installed: s.wifi.installed(), CardNoDriver: s.wifi.cardWithoutDriver(), FirmwareDone: fileExists(onlineComplete),
		Adapters: s.wifi.adapters(ctx),
		Status:   s.wifi.status(ctx, c), Countries: countries(filepath.Join(zoneinfo, "iso3166.tab")),
		Pending: s.pendingInfo(), DryRun: s.cfg.DryRun, InstallRunning: s.wifiInstallRunning(ctx),
	}
	if info.Adapters == nil {
		info.Adapters = []sys.WiFiAdapter{}
	}
	if w := c.WiFi; w != nil {
		info.Configured = true
		info.Config = WiFiSettings{Enabled: w.Enabled, Interface: w.Interface, SSID: w.SSID, Country: w.Country,
			Band: w.Band, Channel: w.Channel, Width: w.Width}
		_, err := s.secrets().Get(w.PasswordRef)
		info.HasPassword = err == nil
	} else {
		info.Config = WiFiSettings{Country: countryOfZone(filepath.Join(zoneinfo, "zone1970.tab"), c.System.Timezone),
			Band: config.WiFiBand24, Channel: 6, Width: 20}
	}
	return info
}

func (s *Server) wifiInstallRunning(ctx context.Context) bool {
	if s.cfg.DryRun {
		return false
	}
	st, _ := run(ctx, "systemctl", "is-active", wifiInstall+".service")
	st = strings.TrimSpace(st)
	return st == "active" || st == "activating"
}

func (s *Server) registerWiFi() {
	s.handle("wifi.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		return s.wifiInfo(ctx, c), nil
	})

	s.handle("wifi.status", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		return s.wifi.status(ctx, c), nil
	})

	s.handle("wifi.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Enabled   bool   `json:"enabled"`
			Interface string `json:"interface"`
			SSID      string `json:"ssid"`
			Password  string `json:"password"` // empty = keep the previous one
			Country   string `json:"country"`
			Band      string `json:"band"`
			Channel   int    `json:"channel"`
			Width     int    `json:"width"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if p.Password != "" && !config.ValidWiFiPassword(p.Password) {
			return nil, Userf("The Wi-Fi password needs 8 to 63 characters, without umlauts or other special letters.")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		w := &config.WiFi{Enabled: p.Enabled, Interface: p.Interface, SSID: p.SSID, Country: p.Country,
			Band: p.Band, Channel: p.Channel, Width: p.Width}
		if c.WiFi != nil {
			w.PasswordRef = c.WiFi.PasswordRef
		}
		if p.Password == "" {
			if _, err := s.secrets().Get(w.PasswordRef); w.PasswordRef == "" || err != nil {
				return nil, Userf("Please enter a Wi-Fi password.")
			}
		}
		if w.Enabled {
			if !s.wifi.installed() {
				return nil, Userf("Please download the Wi-Fi software first.")
			}
			if err := checkAdapter(s.wifi.adapters(ctx), w); err != nil {
				return nil, err
			}
		}
		// New password under a new name: a rollback restores the old reference, and the old password
		// still matches it (as with PPPoE).
		newRef := ""
		if p.Password != "" {
			newRef = wifiRef + newID()
			w.PasswordRef = newRef
			// Validate before anything is written, so a typo leaves no file behind
			check := *c
			check.WiFi = w
			if err := check.Validate(); err != nil {
				return nil, userConfigError(err)
			}
			if err := s.secrets().Set(newRef, p.Password); err != nil {
				return nil, err
			}
		}
		nc := *c
		nc.WiFi = w
		pend, err := s.commitConfig(&nc, confirmWiFi)
		if err != nil {
			if newRef != "" {
				os.Remove(filepath.Join(s.secrets().Dir, newRef))
			}
			return nil, err
		}
		if pend == nil {
			s.pruneSecrets()
		}
		return map[string]any{"pending": pend}, nil
	})

	// The download runs as a unit of its own, like updates: it survives a restart of the helper and leaves its
	// log in the journal. Fixed command line, no input from the browser (D-04).
	s.handle("wifi.install.start", func(ctx context.Context, _ json.RawMessage) (any, error) {
		if len(s.wifi.adapters(ctx)) == 0 {
			return nil, Userf("There is no Wi-Fi adapter to download the software for.") // F-WIFI-07
		}
		if s.wifiInstallRunning(ctx) {
			return nil, Userf("The download is already running.")
		}
		if s.cfg.DryRun {
			s.cfg.Log.Info("dry run: Wi-Fi download skipped", "cmd", apply.WiFiInstallCommand())
			if f, ok := s.wifi.(fakeWiFi); ok {
				f.mu.done = true
			}
			return nil, nil
		}
		if err := s.applier().PrepareWiFiPackages(); err != nil {
			return nil, err
		}
		run(ctx, "systemctl", "reset-failed", wifiInstall+".service")
		_, err := run(ctx, "systemd-run", "--unit="+wifiInstall, "--description=Wi-Fi software from the web interface",
			"--setenv=DEBIAN_FRONTEND=noninteractive", "/bin/sh", "-c", apply.WiFiInstallCommand())
		return nil, err
	})

	s.handle("wifi.install.status", func(ctx context.Context, _ json.RawMessage) (any, error) {
		if s.cfg.DryRun {
			return map[string]any{"running": false, "failed": false, "log": "", "installed": s.wifi.installed()}, nil
		}
		st, _ := run(ctx, "systemctl", "show", "--property=Result", "--value", wifiInstall+".service")
		logs, _ := run(ctx, "journalctl", "-u", wifiInstall+".service", "-n", "12", "--no-pager", "-o", "cat")
		running := s.wifiInstallRunning(ctx)
		return map[string]any{"running": running, "installed": s.wifi.installed(),
			"failed": !running && strings.TrimSpace(st) != "success" && strings.TrimSpace(st) != "", "log": logs}, nil
	})
}
