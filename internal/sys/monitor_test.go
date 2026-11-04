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
	add(60, 50, 0) // neue Minute schließt die erste ab
	pts := h.Since(base)
	if len(pts) != 1 {
		t.Fatalf("%d Punkte, erwartet 1", len(pts))
	}
	if p := pts[0]; p.CPU != 20 || p.WanRx != 2000 || p.Mem != 40 || p.T != base.Unix() {
		t.Errorf("Mittelwert falsch: %+v", p)
	}
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestICMPEchoChecksum(t *testing.T) {
	p := icmpEcho(0x1234, 7, []byte("routerui"))
	if p[0] != 8 || p[1] != 0 || len(p) != 16 {
		t.Fatalf("Kopf falsch: %x", p)
	}
	// Eine korrekte Prüfsumme ergibt über das ganze Paket 0
	if c := icmpChecksum(p); c != 0 {
		t.Errorf("Prüfsumme falsch: Rest %04x", c)
	}
}

func TestICMPPingLocalhost(t *testing.T) {
	// Läuft nur, wo ICMP-Datagram-Sockets erlaubt sind (ping_group_range), z. B. auf dem Router
	if rtt, ok := icmpPing(t.Context(), "127.0.0.1", time.Second); ok {
		if rtt < 0 || rtt > 1000 {
			t.Errorf("unplausible Antwortzeit %v ms", rtt)
		}
	} else {
		t.Skip("ICMP-Datagram-Socket hier nicht erlaubt")
	}
}

func TestMonitorOutages(t *testing.T) {
	m := NewMonitor(t.TempDir())
	now := time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC)
	step := func(results ...float32) {
		m.record(now, results)
		now = now.Add(30 * time.Second)
	}
	step(1, 15, 14)  // alles gut
	step(1, -1, 13)  // ein Ziel weg: kein Ausfall
	step(1, -1, -1)  // beide Internet-Ziele weg: Ausfall beginnt
	step(-1, -1, -1) // dauert an
	if c, down := m.OutagesSince(now.Add(-time.Hour)); c != 1 || !down {
		t.Fatalf("Ausfall nicht erkannt: %d %v", c, down)
	}
	step(1, 16, 15) // wieder da
	snap := m.Snapshot(now.Add(-time.Hour))
	if snap["internet_down"].(bool) {
		t.Error("Ausfall nicht beendet")
	}
	o := snap["outages"].([]Outage)
	if len(o) != 1 || o[0].End == nil || o[0].End.Sub(o[0].Start) != time.Minute {
		t.Errorf("Ausfallprotokoll falsch: %+v", o)
	}
	st := snap["targets"].([]TargetStats)
	if st[1].Loss != 60 { // 3 von 5 verloren
		t.Errorf("Paketverlust Google = %v, erwartet 60", st[1].Loss)
	}
}
