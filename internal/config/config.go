// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config ist das Schema von /etc/vanilla-router/config.json, der einzigen Wahrheitsquelle
// (D-02, PLAN.md 3.3). Laden ist streng (unbekannte Felder sind ein Fehler), Validate prüft alle
// Werte so, dass die Generatoren in internal/gen keine eigene Prüfung mehr brauchen.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SchemaVersion ist die aktuelle Version des Schemas. Ältere Versionen werden beim Laden migriert.
const SchemaVersion = 1

// Path ist der Ort der Konfiguration auf dem Zielsystem.
const Path = "/etc/vanilla-router/config.json"

// FileMode: lesbar für alle, damit die Weboberfläche ohne Root sie lesen kann. Geheimnisse stehen nie
// in config.json, sondern unter secrets/ (0600).
const FileMode = 0o644

// Config ist der gewünschte Zustand des Routers.
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

// System sind allgemeine Einstellungen.
type System struct {
	Hostname string `json:"hostname"`
	Timezone string `json:"timezone"`
	Locale   string `json:"locale,omitempty"`
	Keymap   string `json:"keymap,omitempty"`
}

// WAN-Arten.
const (
	WANDHCP   = "dhcp"
	WANPPPoE  = "pppoe"
	WANStatic = "static"
)

// WAN ist der Internet-Anschluss.
type WAN struct {
	// MatchMAC ordnet den physischen Port zu. Er heißt danach immer wan0 (per .link-Datei),
	// unabhängig davon, wie der Kernel ihn nennt.
	MatchMAC string     `json:"match_mac"`
	Type     string     `json:"type"`
	PPPoE    *PPPoE     `json:"pppoe,omitempty"`
	Static   *StaticWAN `json:"static,omitempty"`
	IPv6     WANIPv6    `json:"ipv6"`
	MTU      int        `json:"mtu,omitempty"` // 0 = Vorgabe (1500, bei PPPoE 1492)
	// Detected ist das Ergebnis der Erkennung im Assistenten, solange der Anschluss noch nicht in der
	// Oberfläche eingerichtet wurde (F-INST-15, F-UI-08). Der Assistent fragt keine Zugangsdaten
	// (P-11): Bei PPPoE läuft der Router bis dahin mit Type dhcp, das schadet nicht.
	Detected *Detected `json:"detected,omitempty"`
}

// Erkannte Anschlussarten. DetectedNone: Das Modem hat nicht geantwortet.
const DetectedNone = "none"

// Detected beschreibt, was der Assistent am Internet-Anschluss gefunden hat.
type Detected struct {
	Method string `json:"method"`         // dhcp, pppoe oder none
	VLAN   int    `json:"vlan,omitempty"` // bei PPPoE: VLAN der Antwort (Telekom: 7)
}

// NeedsSetup sagt, ob die Oberfläche nach dem Internetzugang fragen muss.
func (w WAN) NeedsSetup() bool {
	return w.Detected != nil && w.Detected.Method != WANDHCP
}

// PPPoE-Zugang. Das Passwort steht nicht hier, sondern unter secrets/<PasswordRef> (0600).
type PPPoE struct {
	User        string `json:"user"`
	PasswordRef string `json:"password_ref"`
	VLAN        int    `json:"vlan,omitempty"` // 0 = ohne VLAN, Telekom: 7
}

// StaticWAN ist eine feste Adresse am Internet-Anschluss.
type StaticWAN struct {
	Address string   `json:"address"` // CIDR, z. B. 203.0.113.10/29
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns,omitempty"`
}

// WANIPv6 steuert IPv6 am Internet-Anschluss.
type WANIPv6 struct {
	Mode          string `json:"mode"`           // auto (DHCPv6 + Prefix Delegation) oder off
	RequestPrefix int    `json:"request_prefix"` // gewünschte Präfixlänge, 48–64, typisch 56
}

// LAN ist das Heimnetz: eine Bridge lan0 über alle übrigen Ports.
type LAN struct {
	Members []string `json:"members"` // MAC-Adressen der Ports
	IPv4    string   `json:"ipv4"`    // Adresse des Routers mit Präfix, z. B. 192.168.42.1/24
	IPv6    LANIPv6  `json:"ipv6"`
}

// LANIPv6 steuert IPv6 im Heimnetz.
type LANIPv6 struct {
	ULA           string `json:"ula"`             // /48 nach RFC 4193, bei der Installation zufällig erzeugt
	FromWANPrefix bool   `json:"from_wan_prefix"` // ein /64 aus dem delegierten Präfix verteilen
}

// DHCP für IPv4 im Heimnetz.
type DHCP struct {
	PoolStart    string        `json:"pool_start"`
	PoolEnd      string        `json:"pool_end"`
	Lease        string        `json:"lease"` // Go-Dauer, z. B. 12h
	Reservations []Reservation `json:"reservations"`
}

// Reservation ist eine feste Adresse für ein Gerät. Der Name wird auch im DNS eingetragen.
type Reservation struct {
	Name    string `json:"name"`
	MAC     string `json:"mac"`
	IP      string `json:"ip"`
	Comment string `json:"comment,omitempty"`
}

// DNS im Heimnetz.
type DNS struct {
	Domain    string   `json:"domain"`    // home.arpa (RFC 8375)
	Upstreams []string `json:"upstreams"` // leer = DNS-Server des Anbieters
	Records   []Record `json:"records"`
}

// Record ist ein zusätzlicher lokaler Name.
type Record struct {
	Name string `json:"name"`
	IP   string `json:"ip"` // IPv4 oder IPv6
}

// Firewall-Einstellungen.
type Firewall struct {
	PortForwards []PortForward `json:"port_forwards"`
	Pinholes     []Pinhole     `json:"ipv6_pinholes"`
	Blocks       []Block       `json:"blocks"`
}

// PortForward leitet einen Port von außen an ein Gerät im Heimnetz weiter (IPv4).
type PortForward struct {
	ID       string `json:"id,omitempty"` // feste Kennung für die Oberfläche
	Proto    string `json:"proto"`        // tcp, udp, both
	ExtPort  int    `json:"ext_port"`
	ExtEnd   int    `json:"ext_end,omitempty"` // Bereich ext_port–ext_end, 0 = einzelner Port
	IntIP    string `json:"int_ip"`
	IntPort  int    `json:"int_port"` // Ziel-Port (bei Bereichen: Beginn)
	Comment  string `json:"comment,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

// Pinhole gibt einen Port eines Geräts für IPv6 von außen frei. Weil sich das Präfix des Anbieters
// ändern kann, wird das Gerät über den hinteren Teil der Adresse (Interface-ID) erkannt.
type Pinhole struct {
	InterfaceID string `json:"interface_id"` // z. B. ::1a2b:3c4d:5e6f:7890
	Proto       string `json:"proto"`
	Port        int    `json:"port"`
	PortEnd     int    `json:"port_end,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// Block sperrt einem Gerät den Weg ins Internet, immer oder zu Zeiten (Kindersicherung).
// Wochentage meinen den Beginn der Sperre, der Teil nach Mitternacht gehört zum Folgetag (L-16).
type Block struct {
	MAC      string `json:"mac"`
	Name     string `json:"name,omitempty"`
	Mode     string `json:"mode"`           // always, schedule
	From     string `json:"from,omitempty"` // HH:MM Ortszeit
	To       string `json:"to,omitempty"`
	Days     []int  `json:"days,omitempty"` // 0 = Sonntag … 6 = Samstag, leer = jeden Tag
	Disabled bool   `json:"disabled,omitempty"`
}

// Services sind Dienste auf dem Router.
type Services struct {
	SSH SSH `json:"ssh"`
}

// SSH ist ab Werk aus (F-SEC-04). An heißt: nur Schlüssel, nur aus dem Heimnetz.
type SSH struct {
	Enabled        bool     `json:"enabled"`
	AuthorizedKeys []string `json:"authorized_keys,omitempty"`
}

// Updates steuert unattended-upgrades und Snapshots.
type Updates struct {
	SecurityAuto bool   `json:"security_auto"`
	RebootWindow string `json:"reboot_window"` // HH:MM, Neustart nach Updates nur dann
	Snapshots    bool   `json:"snapshots"`
}

// Parse liest eine Konfiguration streng: unbekannte Felder und nachfolgende Daten sind Fehler.
func Parse(data []byte) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config.json: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("config.json: Daten nach dem Ende des Objekts")
	}
	if c.Schema == 0 {
		return nil, fmt.Errorf("config.json: Feld schema fehlt")
	}
	if c.Schema > SchemaVersion {
		return nil, fmt.Errorf("config.json: Schema %d ist neuer als diese Version (%d), bitte vanilla-router aktualisieren", c.Schema, SchemaVersion)
	}
	return &c, nil
}

// Load liest und prüft die Datei.
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

// Marshal liefert die kanonische Darstellung (eingerückt, mit Zeilenende).
func (c *Config) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Save prüft und schreibt atomar (temporäre Datei im selben Ordner, dann rename).
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

// WriteFileAtomic schreibt eine Datei so, dass Leser immer die alte oder die neue Fassung sehen.
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
