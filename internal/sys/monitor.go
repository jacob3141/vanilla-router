// SPDX-License-Identifier: AGPL-3.0-or-later

package sys

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Online monitor: every 30 seconds a ping to the gateway and two targets on the internet.
// If both internet targets do not answer, the internet counts as down.

const (
	probeEvery   = 30 * time.Second
	probesPerDay = int(24 * time.Hour / probeEvery)
	maxOutages   = 200
)

type Probe struct {
	T   int64   `json:"t"`
	RTT float32 `json:"rtt"` // milliseconds, -1 = no answer
}

type Target struct {
	Name     string  `json:"name"`
	Addr     string  `json:"addr"`
	Internet bool    `json:"internet"`
	Probes   []Probe `json:"probes"`
}

type Outage struct {
	Start time.Time  `json:"start"`
	End   *time.Time `json:"end,omitempty"` // nil = still ongoing
}

type Monitor struct {
	mu      sync.Mutex
	Targets []*Target `json:"targets"`
	Outages []Outage  `json:"outages"`
	file    string
	ping    func(ctx context.Context, addr string) (float64, bool)
}

func NewMonitor(dataDir string) *Monitor {
	m := &Monitor{
		file: filepath.Join(dataDir, "monitor.json"),
		ping: systemPing,
		Targets: []*Target{
			{Name: "Gateway", Addr: "", Internet: false},
			{Name: "Google DNS", Addr: "8.8.8.8", Internet: true},
			{Name: "Cloudflare DNS", Addr: "1.1.1.1", Internet: true},
		},
	}
	if b, err := os.ReadFile(m.file); err == nil {
		var saved Monitor
		if json.Unmarshal(b, &saved) == nil {
			for _, st := range saved.Targets {
				for _, t := range m.Targets {
					if t.Name == st.Name {
						t.Probes = st.Probes
					}
				}
			}
			m.Outages = saved.Outages
		}
	}
	return m
}

func systemPing(ctx context.Context, addr string) (float64, bool) {
	return icmpPing(ctx, addr, 2*time.Second)
}

// InternetOK checks immediately whether the internet is reachable (same targets as the monitor, one is enough).
// For the check after a change to the internet port (F-UI-08).
func (m *Monitor) InternetOK(ctx context.Context) bool {
	m.mu.Lock()
	var addrs []string
	for _, t := range m.Targets {
		if t.Internet {
			addrs = append(addrs, t.Addr)
		}
	}
	ping := m.ping
	m.mu.Unlock()
	ok := make(chan bool, len(addrs))
	for _, a := range addrs {
		go func() { _, r := ping(ctx, a); ok <- r }()
	}
	for range addrs {
		if <-ok {
			return true
		}
	}
	return false
}

func (m *Monitor) Run(stop <-chan struct{}) {
	m.round()
	t := time.NewTicker(probeEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			m.round()
		}
	}
}

func (m *Monitor) round() {
	now := time.Now()
	gw, _ := DefaultGatewayV4()
	m.mu.Lock()
	m.Targets[0].Addr = gw
	targets := make([]Target, len(m.Targets))
	for i, t := range m.Targets {
		targets[i] = Target{Name: t.Name, Addr: t.Addr, Internet: t.Internet}
	}
	m.mu.Unlock()

	results := make([]float32, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, addr string) {
			defer wg.Done()
			results[i] = -1
			if addr == "" {
				return
			}
			if rtt, ok := m.ping(context.Background(), addr); ok {
				results[i] = float32(rtt)
			}
		}(i, t.Addr)
	}
	wg.Wait()
	m.record(now, results)
}

// record saves the results of a round and keeps the outage log.
func (m *Monitor) record(now time.Time, results []float32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	internetUp := false
	for i, t := range m.Targets {
		t.Probes = append(t.Probes, Probe{T: now.Unix(), RTT: results[i]})
		if len(t.Probes) > probesPerDay {
			t.Probes = t.Probes[len(t.Probes)-probesPerDay:]
		}
		if t.Internet && results[i] >= 0 {
			internetUp = true
		}
	}
	var open *Outage
	if n := len(m.Outages); n > 0 && m.Outages[n-1].End == nil {
		open = &m.Outages[n-1]
	}
	switch {
	case !internetUp && open == nil:
		m.Outages = append(m.Outages, Outage{Start: now})
		if len(m.Outages) > maxOutages {
			m.Outages = m.Outages[len(m.Outages)-maxOutages:]
		}
	case internetUp && open != nil:
		end := now
		open.End = &end
	}
}

type TargetStats struct {
	Name   string  `json:"name"`
	Addr   string  `json:"addr"`
	Last   float32 `json:"last"`
	Avg    float32 `json:"avg"`
	Loss   float32 `json:"loss"` // percent
	Probes []Probe `json:"probes"`
}

// Snapshot returns key figures and measurements from the given moment.
func (m *Monitor) Snapshot(since time.Time) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	var stats []TargetStats
	for _, t := range m.Targets {
		s := TargetStats{Name: t.Name, Addr: t.Addr, Last: -1, Probes: []Probe{}}
		var sum float64
		var ok, all int
		for _, p := range t.Probes {
			if p.T < since.Unix() {
				continue
			}
			s.Probes = append(s.Probes, p)
			all++
			if p.RTT >= 0 {
				ok++
				sum += float64(p.RTT)
			}
		}
		if n := len(t.Probes); n > 0 {
			s.Last = t.Probes[n-1].RTT
		}
		if ok > 0 {
			s.Avg = float32(sum / float64(ok))
		}
		if all > 0 {
			s.Loss = float32(100 * float64(all-ok) / float64(all))
		}
		stats = append(stats, s)
	}
	outages := []Outage{}
	for _, o := range m.Outages {
		if o.End == nil || o.End.After(since) {
			outages = append(outages, o)
		}
	}
	down := len(m.Outages) > 0 && m.Outages[len(m.Outages)-1].End == nil
	return map[string]any{"targets": stats, "outages": outages, "internet_down": down, "interval_s": int(probeEvery.Seconds())}
}

// OutagesSince counts outages since the given moment (for the hints).
func (m *Monitor) OutagesSince(t time.Time) (count int, down bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, o := range m.Outages {
		if o.End == nil || o.End.After(t) {
			count++
		}
	}
	down = len(m.Outages) > 0 && m.Outages[len(m.Outages)-1].End == nil
	return
}

func (m *Monitor) Save() error {
	m.mu.Lock()
	b, _ := json.Marshal(struct {
		Targets []*Target `json:"targets"`
		Outages []Outage  `json:"outages"`
	}{m.Targets, m.Outages})
	m.mu.Unlock()
	tmp := m.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.file)
}
