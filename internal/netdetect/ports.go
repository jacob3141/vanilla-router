// SPDX-License-Identifier: AGPL-3.0-or-later

// Package netdetect finds the Ethernet ports of a computer and watches their link state.
// Basis for detecting the internet port in the installer (PLAN.md 3.9.3, T-M1-02, T-M1-03).
package netdetect

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Tri is a truth value with the state "unknown". For ports that are off, sysfs returns an
// error when reading carrier instead of 0.
type Tri int8

const (
	Unknown Tri = iota
	No
	Yes
)

func (t Tri) String() string {
	switch t {
	case Yes:
		return "yes"
	case No:
		return "no"
	}
	return "unknown"
}

// Port describes a physical Ethernet port.
type Port struct {
	Name      string // Kernel-Name, z. B. enp1s0
	Index     int    // interface index (for netlink and AF_PACKET)
	MAC       string
	Driver    string // z. B. r8169, e1000e, igc, virtio_net
	Bus       string // pci, usb, virtio …
	DevPath   string // resolved device path under /sys/devices, determines the order
	AdminUp   bool   // IFF_UP set
	Carrier   Tri    // cable is plugged in and the other end is active
	OperState string // up, down, dormant …
	SpeedMbps int    // current speed, 0 if unknown or no link
	Number    int    // running number from 1 for texts ("Port 2")
}

// Label is the name for non-experts.
func (p Port) Label() string {
	s := fmt.Sprintf("Port %d", p.Number)
	if p.Bus == "usb" {
		s += " (USB adapter)"
	}
	return s
}

// Reasons for exclusion, only for diagnostics and tests.
const (
	skipLoopback = "loopback"
	skipNotEther = "not Ethernet"
	skipVirtual  = "virtual"
	skipWireless = "WLAN"
	skipBridge   = "Bridge/Bond"
)

// Skipped describes an interface that is not a candidate.
type Skipped struct {
	Name, Reason string
}

// ListPorts reads sysRoot/class/net (normally: sysRoot = "/sys") and returns the physical
// Ethernet ports in a stable order (by device path, i.e. bus position).
func ListPorts(sysRoot string) ([]Port, []Skipped, error) {
	return ListPortsOpts(sysRoot, Options{})
}

// Options for ListPortsOpts.
type Options struct {
	// IncludeVirtual includes virtual Ethernet interfaces (veth). Only for the test lab
	// (test/lab), on real hardware those are Docker or VM interfaces.
	IncludeVirtual bool
}

// ListPortsOpts is ListPorts with options.
func ListPortsOpts(sysRoot string, opt Options) ([]Port, []Skipped, error) {
	base := filepath.Join(sysRoot, "class", "net")
	ents, err := os.ReadDir(base)
	if err != nil {
		return nil, nil, err
	}
	var ports []Port
	var skipped []Skipped
	for _, e := range ents {
		name := e.Name()
		dir := filepath.Join(base, name)
		p, reason, err := readPort(sysRoot, dir, name, opt)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		}
		if reason != "" {
			skipped = append(skipped, Skipped{name, reason})
			continue
		}
		ports = append(ports, p)
	}
	// Built-in ports first, USB adapters afterwards, within that by position on the bus.
	sort.Slice(ports, func(i, j int) bool {
		ui, uj := ports[i].Bus == "usb", ports[j].Bus == "usb"
		if ui != uj {
			return uj
		}
		return lessPath(ports[i].DevPath, ports[j].DevPath)
	})
	for i := range ports {
		ports[i].Number = i + 1
	}
	return ports, skipped, nil
}

func readPort(sysRoot, dir, name string, opt Options) (Port, string, error) {
	if name == "lo" {
		return Port{}, skipLoopback, nil
	}
	if t, _ := readTrim(filepath.Join(dir, "type")); t != "1" { // ARPHRD_ETHER
		return Port{}, skipNotEther, nil
	}
	if exists(filepath.Join(dir, "wireless")) || exists(filepath.Join(dir, "phy80211")) {
		return Port{}, skipWireless, nil
	}
	if exists(filepath.Join(dir, "bridge")) || exists(filepath.Join(dir, "bonding")) {
		return Port{}, skipBridge, nil
	}
	devLink := filepath.Join(dir, "device")
	devPath, err := filepath.EvalSymlinks(devLink)
	switch {
	case err == nil:
		if rel, err := filepath.Rel(sysRoot, devPath); err == nil {
			devPath = "/" + filepath.ToSlash(rel)
		}
	case opt.IncludeVirtual:
		devPath = "/devices/virtual/net/" + name
	default:
		return Port{}, skipVirtual, nil
	}
	p := Port{Name: name, DevPath: devPath}
	p.MAC, _ = readTrim(filepath.Join(dir, "address"))
	if s, err := readTrim(filepath.Join(dir, "ifindex")); err == nil {
		p.Index, _ = strconv.Atoi(s)
	}
	p.Driver = linkBase(filepath.Join(devLink, "driver"))
	p.Bus = linkBase(filepath.Join(devLink, "subsystem"))
	if s, err := readTrim(filepath.Join(dir, "flags")); err == nil {
		if f, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 32); err == nil {
			p.AdminUp = f&0x1 != 0 // IFF_UP
		}
	}
	switch s, err := readTrim(filepath.Join(dir, "carrier")); {
	case err != nil:
		p.Carrier = Unknown
	case s == "1":
		p.Carrier = Yes
	default:
		p.Carrier = No
	}
	p.OperState, _ = readTrim(filepath.Join(dir, "operstate"))
	if s, err := readTrim(filepath.Join(dir, "speed")); err == nil {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			p.SpeedMbps = v
		}
	}
	return p, "", nil
}

func readTrim(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func linkBase(path string) string {
	t, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	return filepath.Base(t)
}

// lessPath compares device paths section by section. PCI addresses (fixed width, hexadecimal) are
// compared as text, everything else "naturally", i.e. digit sequences numerically
// ("…/usb1/1-2" before "…/usb1/1-10").
func lessPath(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		if isPCIAddr(as[i]) && isPCIAddr(bs[i]) {
			return as[i] < bs[i]
		}
		return lessNatural(as[i], bs[i])
	}
	return len(as) < len(bs)
}

// isPCIAddr recognizes "0000:00:1f.6" and the short form "pci0000:00".
func isPCIAddr(s string) bool {
	s = strings.TrimPrefix(s, "pci")
	if len(s) != 12 && len(s) != 7 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(isDigit(c) || (c >= 'a' && c <= 'f') || c == ':' || c == '.') {
			return false
		}
	}
	return strings.Contains(s, ":")
}

func lessNatural(a, b string) bool {
	for a != "" && b != "" {
		ca, cb := a[0], b[0]
		if isDigit(ca) && isDigit(cb) {
			na, ra := leadingNum(a)
			nb, rb := leadingNum(b)
			if na != nb {
				return na < nb
			}
			a, b = ra, rb
			continue
		}
		if ca != cb {
			return ca < cb
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func leadingNum(s string) (uint64, string) {
	i := 0
	var n uint64
	for i < len(s) && isDigit(s[i]) {
		n = n*10 + uint64(s[i]-'0')
		i++
	}
	return n, s[i:]
}
