// SPDX-License-Identifier: AGPL-3.0-or-later

package sys

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Point ist ein Minutenmittel. Raten in Byte pro Sekunde, CPU und RAM in Prozent.
type Point struct {
	T     int64   `json:"t"` // Unix-Sekunden, Beginn der Minute
	CPU   float32 `json:"cpu"`
	Mem   float32 `json:"mem"`
	WanRx float32 `json:"wr"`
	WanTx float32 `json:"wt"`
	LanRx float32 `json:"lr"`
	LanTx float32 `json:"lt"`
}

const historyMinutes = 24 * 60

// History hält 24 Stunden Minutenwerte und speichert sie im Datenordner,
// damit ein Neustart des Dienstes den Verlauf nicht löscht.
type History struct {
	mu     sync.Mutex
	points []Point
	cur    Point
	n      int
	file   string
}

func NewHistory(dataDir string) *History {
	h := &History{file: filepath.Join(dataDir, "history.json")}
	if b, err := os.ReadFile(h.file); err == nil {
		json.Unmarshal(b, &h.points)
	}
	h.trim(time.Now())
	return h
}

// Add nimmt einen Live-Wert auf und schließt beim Minutenwechsel einen Punkt ab.
func (h *History) Add(l Live) {
	if l.Time.IsZero() {
		return
	}
	minute := l.Time.Truncate(time.Minute).Unix()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.n > 0 && minute != h.cur.T {
		h.flush()
	}
	if h.n == 0 {
		h.cur = Point{T: minute}
	}
	h.n++
	memPct := float32(0)
	if l.Mem.Total > 0 {
		memPct = float32(100 * float64(l.Mem.Total-l.Mem.Available) / float64(l.Mem.Total))
	}
	// laufender Mittelwert
	f := func(acc *float32, v float32) { *acc += (v - *acc) / float32(h.n) }
	f(&h.cur.CPU, float32(l.CPU))
	f(&h.cur.Mem, memPct)
	f(&h.cur.WanRx, float32(l.Net["wan0"].Rx))
	f(&h.cur.WanTx, float32(l.Net["wan0"].Tx))
	f(&h.cur.LanRx, float32(l.Net["lan0"].Rx))
	f(&h.cur.LanTx, float32(l.Net["lan0"].Tx))
}

func (h *History) flush() {
	h.points = append(h.points, h.cur)
	h.n = 0
	h.trim(time.Unix(h.cur.T, 0))
}

func (h *History) trim(now time.Time) {
	cut := now.Add(-historyMinutes * time.Minute).Unix()
	i := 0
	for i < len(h.points) && h.points[i].T < cut {
		i++
	}
	h.points = h.points[i:]
}

// Since liefert alle abgeschlossenen Minuten ab dem Zeitpunkt.
func (h *History) Since(t time.Time) []Point {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []Point{}
	for _, p := range h.points {
		if p.T >= t.Unix() {
			out = append(out, p)
		}
	}
	return out
}

func (h *History) Save() error {
	h.mu.Lock()
	b, _ := json.Marshal(h.points)
	h.mu.Unlock()
	tmp := h.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, h.file)
}
