// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config is the schema of /etc/vanilla-router/config.json, the single source of truth
// (D-02, PLAN.md 3.3). Loading is strict (unknown fields are an error), Validate checks all
// values so that the generators in internal/gen need no checks of their own.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SchemaVersion is the current version of the schema. Older versions are migrated on load.
const SchemaVersion = 1

// Path is the location of the configuration on the target system.
const Path = "/etc/vanilla-router/config.json"

// FileMode: readable by all, so the web interface can read it without root. Secrets are never
// in config.json, but under secrets/ (0600).
const FileMode = 0o644

// Config is the desired state of the router.
type Config struct {
	Schema   int      `json:"schema"`
	System   System   `json:"system"`
	WAN      WAN      `json:"wan"`
	LAN      LAN      `json:"lan"`
	DHCP     DHCP     `json:"dhcp"`
	DNS      DNS      `json:"dns"`
	Firewall Firewall `json:"firewall"`
	Services Services `json:"services"`
	Updates  Updates  `json:"updates"`
}

// System holds general settings.
type System struct {
	Hostname string `json:"hostname"`
	Timezone string `json:"timezone"`
	Locale   string `json:"locale,omitempty"`
	Keymap   string `json:"keymap,omitempty"`
}

// WAN types.
const (
	WANDHCP   = "dhcp"
	WANPPPoE  = "pppoe"
	WANStatic = "static"
)

// WAN is the internet port.
type WAN struct {
	// MatchMAC assigns the physical port. It is then always called wan0 (through a .link file),
	// regardless of what the kernel calls it.
	MatchMAC string     `json:"match_mac"`
	Type     string     `json:"type"`
	PPPoE    *PPPoE     `json:"pppoe,omitempty"`
	Static   *StaticWAN `json:"static,omitempty"`
	IPv6     WANIPv6    `json:"ipv6"`
	MTU      int        `json:"mtu,omitempty"` // 0 = default (1500, with PPPoE 1492)
	// Detected is the result of detection in the assistant, as long as the connection has not been set
	// up in the web interface (F-INST-15, F-UI-08). The assistant does not ask for credentials
	// (P-11): with PPPoE the router runs with Type dhcp until then, which does no harm.
	Detected *Detected `json:"detected,omitempty"`
}

// Detected connection types. DetectedNone: the modem did not answer.
const DetectedNone = "none"

// Detected describes what the assistant found on the internet port.
type Detected struct {
	Method string `json:"method"`         // dhcp, pppoe or none
	VLAN   int    `json:"vlan,omitempty"` // with PPPoE: VLAN of the answer (Telekom: 7)
}

// NeedsSetup says whether the web interface has to ask about internet access.
func (w WAN) NeedsSetup() bool {
	return w.Detected != nil && w.Detected.Method != WANDHCP
}

// PPPoE access. The password is not here, but under secrets/<PasswordRef> (0600).
type PPPoE struct {
	User        string `json:"user"`
	PasswordRef string `json:"password_ref"`
	VLAN        int    `json:"vlan,omitempty"` // 0 = no VLAN, Telekom: 7
}

// StaticWAN is a fixed address on the internet port.
type StaticWAN struct {
	Address string   `json:"address"` // CIDR, z. B. 203.0.113.10/29
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns,omitempty"`
}

// WANIPv6 controls IPv6 on the internet port.
type WANIPv6 struct {
	Mode          string `json:"mode"`           // auto (DHCPv6 + prefix delegation) or off
	RequestPrefix int    `json:"request_prefix"` // desired prefix length, 48–64, typically 56
}

// LAN is the home network: a bridge lan0 over all remaining ports.
type LAN struct {
	Members []string `json:"members"` // MAC addresses of the ports
	IPv4    string   `json:"ipv4"`    // router address with prefix, e.g. 192.168.42.1/24
	IPv6    LANIPv6  `json:"ipv6"`
}

// LANIPv6 controls IPv6 in the home network.
type LANIPv6 struct {
	ULA           string `json:"ula"`             // /48 per RFC 4193, generated randomly at installation
	FromWANPrefix bool   `json:"from_wan_prefix"` // hand out a /64 from the delegated prefix
}

// DHCP for IPv4 in the home network.
type DHCP struct {
	PoolStart    string        `json:"pool_start"`
	PoolEnd      string        `json:"pool_end"`
	Lease        string        `json:"lease"` // Go duration, e.g. 12h
	Reservations []Reservation `json:"reservations"`
}

// Reservation is a fixed address for a device. The name also goes into DNS.
type Reservation struct {
	Name    string `json:"name"`
	MAC     string `json:"mac"`
	IP      string `json:"ip"`
	Comment string `json:"comment,omitempty"`
}

// DNS in the home network.
type DNS struct {
	Domain    string   `json:"domain"`    // home.arpa (RFC 8375)
	Upstreams []string `json:"upstreams"` // empty = the provider's DNS servers
	Records   []Record `json:"records"`
}

// Record is an additional local name.
type Record struct {
	Name string `json:"name"`
	IP   string `json:"ip"` // IPv4 or IPv6
}

// Firewall settings.
type Firewall struct {
	PortForwards []PortForward `json:"port_forwards"`
	Pinholes     []Pinhole     `json:"ipv6_pinholes"`
	Blocks       []Block       `json:"blocks"`
}

// PortForward forwards a port from outside to a device in the home network (IPv4).
type PortForward struct {
	ID       string `json:"id,omitempty"` // fixed ID for the web interface
	Proto    string `json:"proto"`        // tcp, udp, both
	ExtPort  int    `json:"ext_port"`
	ExtEnd   int    `json:"ext_end,omitempty"` // range ext_port–ext_end, 0 = single port
	IntIP    string `json:"int_ip"`
	IntPort  int    `json:"int_port"` // target port (for ranges: start)
	Comment  string `json:"comment,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

// Pinhole opens a port of a device for IPv6 from outside. Because the provider's prefix can
// change, the device is identified by the rear part of the address (interface ID).
type Pinhole struct {
	InterfaceID string `json:"interface_id"` // z. B. ::1a2b:3c4d:5e6f:7890
	Proto       string `json:"proto"`
	Port        int    `json:"port"`
	PortEnd     int    `json:"port_end,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// Block blocks a device's way to the internet, always or at certain times (parental controls).
// Weekdays mean the start of the block, the part after midnight belongs to the next day (L-16).
type Block struct {
	MAC      string `json:"mac"`
	Name     string `json:"name,omitempty"`
	Mode     string `json:"mode"`           // always, schedule
	From     string `json:"from,omitempty"` // HH:MM local time
	To       string `json:"to,omitempty"`
	Days     []int  `json:"days,omitempty"` // 0 = Sunday … 6 = Saturday, empty = every day
	Disabled bool   `json:"disabled,omitempty"`
}

// Services are services on the router.
type Services struct {
	SSH SSH `json:"ssh"`
}

// SSH is off by default (F-SEC-04). On means: keys only, only from the home network.
type SSH struct {
	Enabled        bool     `json:"enabled"`
	AuthorizedKeys []string `json:"authorized_keys,omitempty"`
}

// Updates controls unattended-upgrades and snapshots.
type Updates struct {
	SecurityAuto bool   `json:"security_auto"`
	RebootWindow string `json:"reboot_window"` // HH:MM, restart after updates only then
	Snapshots    bool   `json:"snapshots"`
}

// Parse reads a configuration strictly: unknown fields and trailing data are errors.
func Parse(data []byte) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config.json: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("config.json: data after the end of the object")
	}
	if c.Schema == 0 {
		return nil, fmt.Errorf("config.json: field schema is missing")
	}
	if c.Schema > SchemaVersion {
		return nil, fmt.Errorf("config.json: schema %d is newer than this version (%d), please update vanilla-router", c.Schema, SchemaVersion)
	}
	return &c, nil
}

// Load reads and validates the file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Marshal returns the canonical form (indented, with a trailing newline).
func (c *Config) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Save validates and writes atomically (temporary file in the same folder, then rename).
func (c *Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := c.Marshal()
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, b, FileMode)
}

// WriteFileAtomic writes a file so that readers always see the old or the new version.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
