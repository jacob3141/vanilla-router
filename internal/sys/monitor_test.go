// SPDX-License-Identifier: AGPL-3.0-or-later

package sys

import (
	"testing"
	"time"
)

func TestHistoryAveragesPerMinute(t *testing.T) {
	h := NewHistory(t.TempDir())
	base := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	add := func(sec int, cpu float64, rx float64) {
		h.Add(Live{Time: base.Add(time.Duration(sec) * time.Second), CPU: cpu,
			Mem: Memory{Total: 100, Available: 60}, Net: map[string]Rate{"wan0": {Rx: rx}}})
	}
	add(0, 10, 1000)
	add(30, 30, 3000)
	add(60, 50, 0) // a new minute closes the first
	pts := h.Since(base)
	if len(pts) != 1 {
		t.Fatalf("%d points, expected 1", len(pts))
	}
	if p := pts[0]; p.CPU != 20 || p.WanRx != 2000 || p.Mem != 40 || p.T != base.Unix() {
		t.Errorf("average wrong: %+v", p)
	}
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestICMPEchoChecksum(t *testing.T) {
	p := icmpEcho(0x1234, 7, []byte("routerui"))
	if p[0] != 8 || p[1] != 0 || len(p) != 16 {
		t.Fatalf("header wrong: %x", p)
	}
	// A correct checksum gives 0 over the whole packet
	if c := icmpChecksum(p); c != 0 {
		t.Errorf("checksum wrong: remainder %04x", c)
	}
}

func TestICMPPingLocalhost(t *testing.T) {
	// Only runs where ICMP datagram sockets are allowed (ping_group_range), e.g. on the router
	if rtt, ok := icmpPing(t.Context(), "127.0.0.1", time.Second); ok {
		if rtt < 0 || rtt > 1000 {
			t.Errorf("implausible response time %v ms", rtt)
		}
	} else {
		t.Skip("ICMP datagram socket not allowed here")
	}
}

func TestMonitorOutages(t *testing.T) {
	m := NewMonitor(t.TempDir())
	now := time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC)
	step := func(results ...float32) {
		m.record(now, results)
		now = now.Add(30 * time.Second)
	}
	step(1, 15, 14)  // all good
	step(1, -1, 13)  // one target gone: no outage
	step(1, -1, -1)  // both internet targets gone: outage begins
	step(-1, -1, -1) // still ongoing
	if c, down := m.OutagesSince(now.Add(-time.Hour)); c != 1 || !down {
		t.Fatalf("outage not detected: %d %v", c, down)
	}
	step(1, 16, 15) // back again
	snap := m.Snapshot(now.Add(-time.Hour))
	if snap["internet_down"].(bool) {
		t.Error("outage not ended")
	}
	o := snap["outages"].([]Outage)
	if len(o) != 1 || o[0].End == nil || o[0].End.Sub(o[0].Start) != time.Minute {
		t.Errorf("outage log wrong: %+v", o)
	}
	st := snap["targets"].([]TargetStats)
	if st[1].Loss != 60 { // 3 of 5 lost
		t.Errorf("packet loss Google = %v, expected 60", st[1].Loss)
	}
}
