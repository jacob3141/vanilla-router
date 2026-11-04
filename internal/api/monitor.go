// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vanilla-router/internal/sys"
)

func (s *Server) registerMonitor() {
	s.route("GET /api/history", s.handleHistory)
	s.route("GET /api/monitor", s.handleMonitor)
	s.route("GET /api/hints", s.handleHints)
}

// rangeParam liest ?minutes= (Standard 60, höchstens 24 h).
func rangeParam(r *http.Request) time.Time {
	m, _ := strconv.Atoi(r.URL.Query().Get("minutes"))
	if m <= 0 {
		m = 60
	}
	return time.Now().Add(-time.Duration(min(m, 1440)) * time.Minute)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if s.cfg.History == nil {
		writeJSON(w, map[string]any{"points": []sys.Point{}})
		return
	}
	writeJSON(w, map[string]any{"points": s.cfg.History.Since(rangeParam(r))})
}

func (s *Server) handleMonitor(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Monitor == nil {
		writeError(w, http.StatusServiceUnavailable, "Der Online-Monitor läuft nicht.")
		return
	}
	writeJSON(w, s.cfg.Monitor.Snapshot(rangeParam(r)))
}

type hint struct {
	Level string `json:"level"` // crit, warn, info
	Title string `json:"title"`
	Text  string `json:"text,omitempty"`
	Link  string `json:"link,omitempty"`
}

func failedUnits(ctx context.Context) []string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "list-units", "--state=failed", "--no-legend", "--plain", "--no-pager").Output()
	if err != nil {
		return nil
	}
	var units []string
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			units = append(units, strings.TrimSuffix(f[0], ".service"))
		}
	}
	return units
}

// handleHints sammelt alles, was Aufmerksamkeit braucht, für die Übersicht.
func (s *Server) handleHints(w http.ResponseWriter, r *http.Request) {
	hints := []hint{}
	add := func(level, title, text, link string) { hints = append(hints, hint{level, title, text, link}) }

	if s.call(r, "health", nil, nil) != nil {
		add("crit", "Helper nicht erreichbar", "Änderungen über die Oberfläche sind gerade nicht möglich.", "#/services")
	}
	var set struct {
		NeedsSetup bool `json:"needs_setup"`
		Pending    *struct {
			Deadline time.Time `json:"deadline"`
		} `json:"pending"`
	}
	if s.call(r, "settings.get", nil, &set) == nil {
		if set.Pending != nil {
			add("crit", "Änderung wartet auf Bestätigung", "Ohne Bestätigung wird sie automatisch zurückgenommen.", "#/internet")
		}
		if set.NeedsSetup {
			add("warn", "Internetzugang einrichten", "Ihr Anschluss braucht noch Zugangsdaten oder die Anschlussart.", "#/internet?setup=1")
		}
	}
	if s.cfg.Monitor != nil {
		if n, down := s.cfg.Monitor.OutagesSince(time.Now().Add(-24 * time.Hour)); down {
			add("crit", "Internet ausgefallen", "Weder 8.8.8.8 noch 1.1.1.1 antworten.", "#/monitor")
		} else if n > 0 {
			add("warn", fmt.Sprintf("%d Internet-Ausfall/-Ausfälle in den letzten 24 Stunden", n), "", "#/monitor")
		}
	}
	if n := s.devices.NewSince(time.Now().Add(-24 * time.Hour)); n > 0 {
		add("info", fmt.Sprintf("%d neue(s) Gerät(e) in den letzten 24 Stunden", n), "Unbekannte Geräte im Heimnetz lohnt es sich anzusehen.", "#/devices")
	}
	if units := failedUnits(r.Context()); len(units) > 0 {
		add("warn", fmt.Sprintf("%d Dienst(e) fehlgeschlagen", len(units)), strings.Join(units, ", "), "#/services")
	}
	var upd struct {
		Updates []struct {
			Security bool `json:"security"`
		} `json:"updates"`
		RebootRequired bool     `json:"reboot_required"`
		RebootPackages []string `json:"reboot_packages"`
	}
	if s.call(r, "system.updates.get", nil, &upd) == nil {
		sec := 0
		for _, u := range upd.Updates {
			if u.Security {
				sec++
			}
		}
		if sec > 0 {
			add("warn", fmt.Sprintf("%d Sicherheitsupdates verfügbar", sec), "", "#/system")
		} else if len(upd.Updates) > 0 {
			add("info", fmt.Sprintf("%d Updates verfügbar", len(upd.Updates)), "", "#/system")
		}
		if upd.RebootRequired {
			add("info", "Neustart nötig", strings.Join(upd.RebootPackages, ", "), "#/system")
		}
	}
	var audit *struct {
		Time   time.Time `json:"time"`
		Checks []struct {
			Level string `json:"level"`
		} `json:"checks"`
	}
	if s.features()["audit"] && s.call(r, "system.audit.get", nil, &audit) == nil {
		if audit == nil || audit.Time.IsZero() {
			add("info", "Noch kein Sicherheits-Audit gelaufen", "", "#/system")
		} else {
			crit, warn := 0, 0
			for _, c := range audit.Checks {
				switch c.Level {
				case "CRIT":
					crit++
				case "WARN":
					warn++
				}
			}
			if crit > 0 {
				add("crit", fmt.Sprintf("Audit: %d kritische Befunde", crit), "", "#/system")
			}
			if age := time.Since(audit.Time); age > 30*24*time.Hour {
				add("info", fmt.Sprintf("Letztes Audit vor %d Tagen", int(age.Hours()/24)), "", "#/system")
			} else if warn > 0 && crit == 0 {
				add("info", fmt.Sprintf("Audit: %d Warnungen", warn), "", "#/system")
			}
		}
	}
	var st syscall.Statfs_t
	if syscall.Statfs("/", &st) == nil && st.Blocks > 0 {
		used := 100 - 100*float64(st.Bavail)/float64(st.Blocks)
		if used > 90 {
			add("crit", fmt.Sprintf("Speicher zu %.0f %% voll", used), "", "#/files")
		} else if used > 80 {
			add("warn", fmt.Sprintf("Speicher zu %.0f %% voll", used), "", "#/files")
		}
	}
	if t, ok := sys.Temperature(); ok && t > 80 {
		add("warn", fmt.Sprintf("CPU-Temperatur hoch: %.0f °C", t), "", "")
	}
	writeJSON(w, map[string]any{"hints": hints})
}
