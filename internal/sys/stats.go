// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sys reads system values that are accessible without root.
package sys

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcRoot allows tests with fake /proc and /sys trees.
var ProcRoot = "/"

func p(path string) string { return filepath.Join(ProcRoot, path) }

type cpuTimes struct{ idle, total uint64 }

func readCPU() (cpuTimes, error) {
	f, err := os.Open(p("proc/stat"))
	if err != nil {
		return cpuTimes{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var t cpuTimes
		for i, v := range fields[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			t.total += n
			if i == 3 || i == 4 { // idle, iowait
				t.idle += n
			}
		}
		return t, nil
	}
	return cpuTimes{}, os.ErrNotExist
}

// Memory in Bytes.
type Memory struct {
	Total     uint64 `json:"total"`
	Available uint64 `json:"available"`
}

func ReadMemory() (Memory, error) {
	f, err := os.Open(p("proc/meminfo"))
	if err != nil {
		return Memory{}, err
	}
	defer f.Close()
	var m Memory
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		n, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			m.Total = n * 1024
		case "MemAvailable:":
			m.Available = n * 1024
		}
	}
	return m, sc.Err()
}

// NetCounters are the cumulative counters of an interface.
type NetCounters struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

func ReadNetDev() (map[string]NetCounters, error) {
	f, err := os.Open(p("proc/net/dev"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]NetCounters{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		out[strings.TrimSpace(name)] = NetCounters{RxBytes: rx, TxBytes: tx}
	}
	return out, sc.Err()
}

func Uptime() time.Duration {
	b, err := os.ReadFile(p("proc/uptime"))
	if err != nil {
		return 0
	}
	f, _ := strconv.ParseFloat(strings.Fields(string(b))[0], 64)
	return time.Duration(f * float64(time.Second))
}

func LoadAvg() [3]float64 {
	var l [3]float64
	b, err := os.ReadFile(p("proc/loadavg"))
	if err != nil {
		return l
	}
	for i, v := range strings.Fields(string(b))[:3] {
		l[i], _ = strconv.ParseFloat(v, 64)
	}
	return l
}

// Temperature returns the CPU temperature in °C, preferring x86_pkg_temp.
func Temperature() (float64, bool) {
	zones, _ := filepath.Glob(p("sys/class/thermal/thermal_zone*"))
	best, found := 0.0, false
	for _, z := range zones {
		typ, _ := os.ReadFile(filepath.Join(z, "type"))
		raw, err := os.ReadFile(filepath.Join(z, "temp"))
		if err != nil {
			continue
		}
		milli, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err != nil {
			continue
		}
		c := milli / 1000
		if strings.TrimSpace(string(typ)) == "x86_pkg_temp" {
			return c, true
		}
		if !found || c > best {
			best, found = c, true
		}
	}
	return best, found
}

func OSRelease() string {
	b, err := os.ReadFile(p("etc/os-release"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

func Kernel() string {
	b, _ := os.ReadFile(p("proc/sys/kernel/osrelease"))
	return strings.TrimSpace(string(b))
}

// Rate is the current throughput of an interface in bytes per second.
type Rate struct {
	Rx      float64 `json:"rx"`
	Tx      float64 `json:"tx"`
	RxTotal uint64  `json:"rx_total"`
	TxTotal uint64  `json:"tx_total"`
}

// Live holds the values the sampler keeps updating.
type Live struct {
	Time time.Time       `json:"time"`
	CPU  float64         `json:"cpu"` // percent
	Mem  Memory          `json:"mem"`
	Net  map[string]Rate `json:"net"`
}

// Sampler measures CPU load and traffic at fixed intervals.
type Sampler struct {
	Labels  []string // key in Live.Net per interface, empty = name of the interface
	mu      sync.RWMutex
	live    Live
	lastCPU cpuTimes
	lastNet map[string]NetCounters
	lastAt  time.Time
	Ifaces  []string
}

func NewSampler(ifaces ...string) *Sampler { return &Sampler{Ifaces: ifaces} }

// NewWANLANSampler measures the uplink interface (wan0 or with PPPoE ppp0) and lan0, but always reports them
// under the keys "wan0" and "lan0", as the history and frontend expect.
func NewWANLANSampler(uplink, lan string) *Sampler {
	return &Sampler{Ifaces: []string{uplink, lan}, Labels: []string{"wan0", "lan0"}}
}

func (s *Sampler) Run(stop <-chan struct{}, every time.Duration) {
	s.sample()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.sample()
		}
	}
}

func (s *Sampler) sample() {
	now := time.Now()
	cpu, _ := readCPU()
	mem, _ := ReadMemory()
	net, _ := ReadNetDev()

	s.mu.Lock()
	defer s.mu.Unlock()
	live := Live{Time: now, Mem: mem, Net: map[string]Rate{}}
	if s.lastCPU.total > 0 && cpu.total > s.lastCPU.total {
		dt := float64(cpu.total - s.lastCPU.total)
		di := float64(cpu.idle - s.lastCPU.idle)
		live.CPU = 100 * (1 - di/dt)
	}
	secs := now.Sub(s.lastAt).Seconds()
	for i, name := range s.Ifaces {
		c := net[name]
		r := Rate{RxTotal: c.RxBytes, TxTotal: c.TxBytes}
		if prev, ok := s.lastNet[name]; ok && secs > 0 && c.RxBytes >= prev.RxBytes && c.TxBytes >= prev.TxBytes {
			r.Rx = float64(c.RxBytes-prev.RxBytes) / secs
			r.Tx = float64(c.TxBytes-prev.TxBytes) / secs
		}
		key := name
		if i < len(s.Labels) {
			key = s.Labels[i]
		}
		live.Net[key] = r
	}
	s.live, s.lastCPU, s.lastNet, s.lastAt = live, cpu, net, now
}

func (s *Sampler) Live() Live {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live
}
