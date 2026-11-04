// SPDX-License-Identifier: AGPL-3.0-or-later

// Package wandetect führt den Nutzer durch die Erkennung des Internet-Anschlusses
// („Ziehen Sie alle Kabel ab … stecken Sie jetzt nur das Kabel vom Modem ein“, PLAN.md 3.9.3, T-M1-04).
//
// Machine ist ein reiner Zustandsautomat ohne eigene Ein- und Ausgabe: Er bekommt Ereignisse
// (Link an/aus, Zeit, Ergebnis einer Probe, Eingaben des Nutzers) und gibt Aktionen zurück
// (Text anzeigen, Probe starten/abbrechen, fertig). Zeit wird immer übergeben, nie gelesen.
// So ist jeder Übergang ohne Hardware und ohne Warten testbar.
package wandetect

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// State ist der Zustand des Ablaufs.
type State int

const (
	StateInit      State = iota
	StateUnplug          // es stecken noch Kabel, warten bis alle ab sind
	StateWaitModem       // alle ab, warten auf das Modem-Kabel
	StateSettling        // ein Port hat Link, kurz warten, ob noch einer dazukommt
	StateProbing         // DHCP/PPPoE wird auf dem Kandidaten versucht
	StateRetryWait       // Probe ohne Antwort, Pause vor dem nächsten Versuch
	StateNoAnswer        // mehrere Proben ohne Antwort, Nutzer entscheidet
	StateDone
)

var stateNames = [...]string{"init", "unplug", "wait-modem", "settling", "probing", "retry-wait", "no-answer", "done"}

func (s State) String() string {
	if int(s) < len(stateNames) {
		return stateNames[s]
	}
	return fmt.Sprintf("state(%d)", int(s))
}

// Config enthält die Zeiten des Ablaufs.
type Config struct {
	Settle     time.Duration // nach dem ersten Link so lange auf weitere warten
	HintAfter  time.Duration // ohne Modem-Kabel nach dieser Zeit einen Tipp zeigen
	RetryDelay time.Duration // Pause zwischen zwei Proben ohne Antwort
	MaxProbes  int           // so viele Proben, bevor der Nutzer gefragt wird
}

// DefaultConfig: Modems brauchen nach dem Einstecken oft einige Sekunden, bis sie DHCP beantworten,
// Kabelmodems nach dem Einschalten auch Minuten. Drei Versuche mit 20 s Pause decken den
// häufigen Fall ab, ohne dass jemand lange vor einem stummen Bildschirm sitzt.
var DefaultConfig = Config{Settle: 3 * time.Second, HintAfter: 60 * time.Second, RetryDelay: 20 * time.Second, MaxProbes: 3}

// ProbeResult ist das Ergebnis einer Probe auf dem Kandidaten-Port.
type ProbeResult struct {
	Port      string
	DHCP      bool   // DHCP-Angebot erhalten
	Offer     string // z. B. "192.0.2.10/24 über 192.0.2.1", nur zur Anzeige
	PPPoE     bool   // PPPoE-Zugangsserver gefunden
	PPPoEVLAN int    // 0 = ohne VLAN, sonst VLAN-ID (Telekom: 7)
	ACName    string // Name des PPPoE-Zugangsservers
	Err       string // technischer Fehler (z. B. Socket), wird wie „keine Antwort“ behandelt
}

func (r ProbeResult) answered() bool { return r.DHCP || r.PPPoE }

// Method beschreibt, woran der Port erkannt wurde.
type Method string

const (
	MethodDHCP   Method = "dhcp"
	MethodPPPoE  Method = "pppoe"
	MethodLink   Method = "link"   // nur Link, keine Antwort, vom Nutzer bestätigt
	MethodManual Method = "manual" // vom Nutzer aus der Liste gewählt
)

// Result ist das Ergebnis des ganzen Ablaufs.
type Result struct {
	Port   string
	Method Method
	Probe  ProbeResult
}

// Ereignisse an den Automaten.
type (
	Event interface{ isEvent() }
	// LinkChanged meldet den Link-Zustand eines Ports. Ports, die nicht in Start vorkamen, werden ignoriert.
	LinkChanged struct {
		Port string
		Up   bool
	}
	// Tick meldet nur, dass Zeit vergangen ist.
	Tick struct{}
	// ProbeDone liefert das Ergebnis der zuletzt gestarteten Probe.
	ProbeDone struct{ Result ProbeResult }
	// UserRetry: Nutzer will es noch einmal versuchen (im Zustand NoAnswer).
	UserRetry struct{}
	// UserAccept: Nutzer bestätigt den Port trotz fehlender Antwort (z. B. feste IP).
	UserAccept struct{}
	// UserManual: Nutzer wählt den Port selbst aus der Liste (Rückfall, jederzeit möglich).
	UserManual struct{ Port string }
)

func (LinkChanged) isEvent() {}
func (Tick) isEvent()        {}
func (ProbeDone) isEvent()   {}
func (UserRetry) isEvent()   {}
func (UserAccept) isEvent()  {}
func (UserManual) isEvent()  {}

// Aktionen des Automaten.
type (
	Action interface{ isAction() }
	// Say zeigt einen Text. ID ist stabil (für Tests und Übersetzungen), Text ist für Laien.
	Say struct{ ID, Text string }
	// StartProbe startet DHCP- und PPPoE-Probe auf dem Port. Das Ergebnis kommt als ProbeDone.
	StartProbe struct{ Port string }
	// CancelProbe bricht eine laufende Probe ab. Ein später noch eintreffendes Ergebnis wird ignoriert.
	CancelProbe struct{ Port string }
	// Finished: Der Internet-Anschluss steht fest.
	Finished struct{ Result Result }
)

func (Say) isAction()         {}
func (StartProbe) isAction()  {}
func (CancelProbe) isAction() {}
func (Finished) isAction()    {}

// Machine ist der Automat. Der Nullwert ist nicht nutzbar, siehe New.
type Machine struct {
	cfg       Config
	labels    map[string]string // Port → Text für Laien („Anschluss 2“)
	links     map[string]bool
	state     State
	candidate string
	since     time.Time // Eintritt in den aktuellen Zustand
	attempts  int
	hinted    bool
	result    Result
}

// New legt den Automaten an. labels ordnet jedem Port eine Bezeichnung für Laien zu und legt
// fest, welche Ports überhaupt beachtet werden.
func New(cfg Config, labels map[string]string) *Machine {
	if cfg.MaxProbes < 1 {
		cfg.MaxProbes = 1
	}
	return &Machine{cfg: cfg, labels: labels, links: map[string]bool{}}
}

// State liefert den aktuellen Zustand.
func (m *Machine) State() State { return m.state }

// Candidate liefert den Port, der gerade geprüft wird (oder leer).
func (m *Machine) Candidate() string { return m.candidate }

// Result liefert das Ergebnis, sobald State() == StateDone.
func (m *Machine) Result() Result { return m.result }

// Start setzt den Anfangszustand aus den aktuellen Link-Zuständen.
func (m *Machine) Start(links map[string]bool, now time.Time) []Action {
	for p := range m.labels {
		m.links[p] = links[p]
	}
	return m.toUnplugOrWait(now, nil)
}

// Handle verarbeitet ein Ereignis.
func (m *Machine) Handle(ev Event, now time.Time) []Action {
	if m.state == StateDone {
		return nil
	}
	if um, ok := ev.(UserManual); ok {
		if _, known := m.labels[um.Port]; !known {
			return []Action{Say{"manual-unknown", "Diesen Anschluss gibt es nicht."}}
		}
		var acts []Action
		if m.state == StateProbing {
			acts = append(acts, CancelProbe{m.candidate})
		}
		return append(acts, m.finish(Result{Port: um.Port, Method: MethodManual}, now)...)
	}
	if lc, ok := ev.(LinkChanged); ok {
		if _, known := m.labels[lc.Port]; !known {
			return nil
		}
		if m.links[lc.Port] == lc.Up {
			return m.handleTime(now) // doppelte Meldung, nur Zeit auswerten
		}
		m.links[lc.Port] = lc.Up
		return m.handleLink(lc, now)
	}
	switch e := ev.(type) {
	case Tick:
		return m.handleTime(now)
	case ProbeDone:
		return m.handleProbe(e.Result, now)
	case UserRetry:
		if m.state == StateNoAnswer {
			m.attempts = 0
			return m.probe(now)
		}
	case UserAccept:
		if m.state == StateNoAnswer {
			return m.finish(Result{Port: m.candidate, Method: MethodLink}, now)
		}
	}
	return nil
}

func (m *Machine) handleLink(lc LinkChanged, now time.Time) []Action {
	switch m.state {
	case StateUnplug:
		if len(m.up()) == 0 {
			return m.enter(StateWaitModem, now, m.sayPlugModem())
		}
		return []Action{m.sayUnplug()} // Liste der noch steckenden Ports aktualisieren
	case StateWaitModem:
		if lc.Up {
			m.candidate = lc.Port
			return m.enter(StateSettling, now, Say{"settling", "Kabel erkannt an " + m.label(lc.Port) + ". Einen Moment …"})
		}
	case StateSettling:
		if lc.Up {
			m.candidate = ""
			return m.toUnplugOrWait(now, &Say{"ambiguous", "Es stecken mehrere Kabel. Bitte nur das Kabel vom Modem einstecken."})
		}
		if lc.Port == m.candidate {
			m.candidate = ""
			return m.toUnplugOrWait(now, nil)
		}
	case StateProbing, StateRetryWait, StateNoAnswer:
		var acts []Action
		if m.state == StateProbing {
			acts = append(acts, CancelProbe{m.candidate})
		}
		if lc.Up {
			m.candidate = ""
			return append(acts, m.toUnplugOrWait(now, &Say{"ambiguous", "Es stecken mehrere Kabel. Bitte nur das Kabel vom Modem einstecken."})...)
		}
		if lc.Port == m.candidate {
			m.candidate = ""
			return append(acts, m.toUnplugOrWait(now, &Say{"lost", "Die Verbindung zum Modem ist weg. Steckt das Kabel noch richtig?"})...)
		}
	}
	return nil
}

func (m *Machine) handleTime(now time.Time) []Action {
	switch m.state {
	case StateSettling:
		if now.Sub(m.since) >= m.cfg.Settle {
			m.attempts = 0
			return m.probe(now)
		}
	case StateRetryWait:
		if now.Sub(m.since) >= m.cfg.RetryDelay {
			return m.probe(now)
		}
	case StateWaitModem:
		if !m.hinted && m.cfg.HintAfter > 0 && now.Sub(m.since) >= m.cfg.HintAfter {
			m.hinted = true
			return []Action{Say{"hint-modem", "Noch kein Kabel erkannt. Ist das Modem eingeschaltet? " +
				"Das Kabel gehört am Modem in die Buchse „LAN“ oder „Ethernet“, nicht in „DSL“ oder „Kabel“."}}
		}
	}
	return nil
}

func (m *Machine) handleProbe(r ProbeResult, now time.Time) []Action {
	if m.state != StateProbing || r.Port != m.candidate {
		return nil // veraltetes Ergebnis nach Abbruch
	}
	if r.answered() {
		method := MethodDHCP
		if !r.DHCP {
			method = MethodPPPoE
		}
		return m.finish(Result{Port: r.Port, Method: method, Probe: r}, now)
	}
	if m.attempts < m.cfg.MaxProbes {
		return m.enter(StateRetryWait, now, Say{"retry", fmt.Sprintf(
			"Das Modem antwortet noch nicht. Manche Modems brauchen etwas Zeit, neuer Versuch in %d Sekunden …",
			int(m.cfg.RetryDelay.Seconds()))})
	}
	return m.enter(StateNoAnswer, now, Say{"no-answer", "An " + m.label(m.candidate) + " antwortet kein Modem. " +
		"Ist das wirklich das Kabel vom Modem? Sie können es noch einmal versuchen oder diesen Anschluss trotzdem verwenden."})
}

func (m *Machine) probe(now time.Time) []Action {
	m.attempts++
	acts := m.enter(StateProbing, now, Say{"probing", fmt.Sprintf("Prüfe die Verbindung zum Modem an %s (Versuch %d von %d) …",
		m.label(m.candidate), m.attempts, m.cfg.MaxProbes)})
	return append(acts, StartProbe{m.candidate})
}

func (m *Machine) finish(r Result, now time.Time) []Action {
	m.result = r
	m.candidate = r.Port
	text := m.label(r.Port) + " ist jetzt Ihr Internet-Anschluss."
	switch r.Method {
	case MethodDHCP:
		text = "Erkannt: " + text + " Das Modem vergibt automatisch eine Adresse."
	case MethodPPPoE:
		text = "Erkannt: " + text + " Ihr Anschluss braucht Zugangsdaten (PPPoE)."
	}
	acts := m.enter(StateDone, now, Say{"done", text})
	return append(acts, Finished{r})
}

func (m *Machine) toUnplugOrWait(now time.Time, prefix *Say) []Action {
	var acts []Action
	if prefix != nil {
		acts = append(acts, *prefix)
	}
	if len(m.up()) > 0 {
		return append(acts, m.enter(StateUnplug, now, m.sayUnplug())...)
	}
	return append(acts, m.enter(StateWaitModem, now, m.sayPlugModem())...)
}

func (m *Machine) enter(s State, now time.Time, say Say) []Action {
	m.state = s
	m.since = now
	return []Action{say}
}

func (m *Machine) sayUnplug() Say {
	var names []string
	for _, p := range m.up() {
		names = append(names, m.label(p))
	}
	return Say{"unplug", "Bitte ziehen Sie alle Netzwerkkabel aus diesem Rechner ab. Noch eingesteckt: " + strings.Join(names, ", ") + "."}
}

func (m *Machine) sayPlugModem() Say {
	return Say{"plug-modem", "Stecken Sie jetzt nur das Kabel vom Modem oder Internet-Router in einen der Netzwerkanschlüsse."}
}

// up liefert die Ports mit Link, nach Bezeichnung sortiert.
func (m *Machine) up() []string {
	var ps []string
	for p, up := range m.links {
		if up {
			ps = append(ps, p)
		}
	}
	sort.Slice(ps, func(i, j int) bool { return m.label(ps[i]) < m.label(ps[j]) })
	return ps
}

func (m *Machine) label(p string) string {
	if l := m.labels[p]; l != "" {
		return l
	}
	return p
}
