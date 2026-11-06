// SPDX-License-Identifier: AGPL-3.0-or-later

// Package wandetect guides the user through detecting the internet port
// ("Unplug all cables … now plug in only the cable from the modem", PLAN.md 3.9.3, T-M1-04).
//
// Machine is a pure state machine without input or output of its own: it receives events
// (link on/off, time, result of a probe, user input) and returns actions
// (show text, start/cancel probe, done). Time is always passed in, never read.
// That makes every transition testable without hardware and without waiting.
package wandetect

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// State is the state of the flow.
type State int

const (
	StateInit      State = iota
	StateUnplug          // cables are still plugged in, wait until all are out
	StateWaitModem       // all out, waiting for the modem cable
	StateSettling        // a port has link, wait briefly in case another one joins
	StateProbing         // DHCP/PPPoE is being tried on the candidate
	StateRetryWait       // probe without an answer, pause before the next attempt
	StateNoAnswer        // several probes without an answer, the user decides
	StateDone
)

var stateNames = [...]string{"init", "unplug", "wait-modem", "settling", "probing", "retry-wait", "no-answer", "done"}

func (s State) String() string {
	if int(s) < len(stateNames) {
		return stateNames[s]
	}
	return fmt.Sprintf("state(%d)", int(s))
}

// Config contains the times of the flow.
type Config struct {
	Settle     time.Duration // after the first link, wait this long for more
	HintAfter  time.Duration // show a tip after this time without a modem cable
	RetryDelay time.Duration // pause between two probes without an answer
	MaxProbes  int           // this many probes before asking the user
}

// DefaultConfig: modems often need a few seconds after plugging in before they answer DHCP,
// cable modems even minutes after being turned on. Three attempts with 20 s pauses cover the
// common case without anyone sitting long in front of a silent screen.
var DefaultConfig = Config{Settle: 3 * time.Second, HintAfter: 60 * time.Second, RetryDelay: 20 * time.Second, MaxProbes: 3}

// ProbeResult is the result of a probe on the candidate port.
type ProbeResult struct {
	Port      string
	DHCP      bool   // DHCP offer received
	Offer     string // e.g. "192.0.2.10/24 via 192.0.2.1", for display only
	OfferNet  string // offered network, e.g. "192.0.2.0/24" (the home network must avoid it)
	PPPoE     bool   // PPPoE access server found
	PPPoEVLAN int    // 0 = without VLAN, otherwise VLAN ID (Telekom: 7)
	ACName    string // name of the PPPoE access server
	Err       string // technical error (e.g. socket), treated like "no answer"
}

func (r ProbeResult) answered() bool { return r.DHCP || r.PPPoE }

// Method describes how the port was recognized.
type Method string

const (
	MethodDHCP   Method = "dhcp"
	MethodPPPoE  Method = "pppoe"
	MethodLink   Method = "link"   // only link, no answer, confirmed by the user
	MethodManual Method = "manual" // chosen by the user from the list
)

// Result is the result of the whole flow.
type Result struct {
	Port   string
	Method Method
	Probe  ProbeResult
}

// Events to the automaton.
type (
	Event interface{ isEvent() }
	// LinkChanged reports the link state of a port. Ports not in Start are ignored.
	LinkChanged struct {
		Port string
		Up   bool
	}
	// Tick only reports that time has passed.
	Tick struct{}
	// ProbeDone returns the result of the most recently started probe.
	ProbeDone struct{ Result ProbeResult }
	// UserRetry: the user wants to try again (in state NoAnswer).
	UserRetry struct{}
	// UserAccept: the user confirms the port despite no answer (e.g. fixed IP).
	UserAccept struct{}
	// UserManual: the user chooses the port themselves from the list (fallback, possible at any time).
	UserManual struct{ Port string }
)

func (LinkChanged) isEvent() {}
func (Tick) isEvent()        {}
func (ProbeDone) isEvent()   {}
func (UserRetry) isEvent()   {}
func (UserAccept) isEvent()  {}
func (UserManual) isEvent()  {}

// Actions of the automaton.
type (
	Action interface{ isAction() }
	// Say shows a text. ID is stable (for tests and translations), Text is for non-experts.
	Say struct{ ID, Text string }
	// StartProbe starts DHCP and PPPoE probes on the port. The result comes as ProbeDone.
	StartProbe struct{ Port string }
	// CancelProbe cancels a running probe. A result arriving later is ignored.
	CancelProbe struct{ Port string }
	// Finished: the internet port is determined.
	Finished struct{ Result Result }
)

func (Say) isAction()         {}
func (StartProbe) isAction()  {}
func (CancelProbe) isAction() {}
func (Finished) isAction()    {}

// Machine is the automaton. The zero value is not usable, see New.
type Machine struct {
	cfg       Config
	labels    map[string]string // port → text for non-experts ("Port 2")
	links     map[string]bool
	state     State
	candidate string
	since     time.Time // entry into the current state
	attempts  int
	hinted    bool
	result    Result
}

// New creates the automaton. labels assigns each port a name for non-experts and determines
// which ports are considered at all.
func New(cfg Config, labels map[string]string) *Machine {
	if cfg.MaxProbes < 1 {
		cfg.MaxProbes = 1
	}
	return &Machine{cfg: cfg, labels: labels, links: map[string]bool{}}
}

// State returns the current state.
func (m *Machine) State() State { return m.state }

// Candidate returns the port currently being checked (or empty).
func (m *Machine) Candidate() string { return m.candidate }

// Result returns the result once State() == StateDone.
func (m *Machine) Result() Result { return m.result }

// Start sets the initial state from the current link states.
func (m *Machine) Start(links map[string]bool, now time.Time) []Action {
	for p := range m.labels {
		m.links[p] = links[p]
	}
	return m.toUnplugOrWait(now, nil)
}

// Handle processes an event.
func (m *Machine) Handle(ev Event, now time.Time) []Action {
	if m.state == StateDone {
		return nil
	}
	if um, ok := ev.(UserManual); ok {
		if _, known := m.labels[um.Port]; !known {
			return []Action{Say{"manual-unknown", "That port does not exist."}}
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
			return m.handleTime(now) // duplicate message, only evaluate the time
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
		return []Action{m.sayUnplug()} // update the list of ports still plugged in
	case StateWaitModem:
		if lc.Up {
			m.candidate = lc.Port
			return m.enter(StateSettling, now, Say{"settling", "Cable detected on " + m.label(lc.Port) + ". One moment …"})
		}
	case StateSettling:
		if lc.Up {
			m.candidate = ""
			return m.toUnplugOrWait(now, &Say{"ambiguous", "Several cables are plugged in. Please plug in only the cable from the modem."})
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
			return append(acts, m.toUnplugOrWait(now, &Say{"ambiguous", "Several cables are plugged in. Please plug in only the cable from the modem."})...)
		}
		if lc.Port == m.candidate {
			m.candidate = ""
			return append(acts, m.toUnplugOrWait(now, &Say{"lost", "The connection to the modem is gone. Is the cable still plugged in properly?"})...)
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
			return []Action{Say{"hint-modem", "No cable detected yet. Is the modem turned on? " +
				"On the modem, the cable goes into the socket marked \"LAN\" or \"Ethernet\", not \"DSL\" or \"Cable\"."}}
		}
	}
	return nil
}

func (m *Machine) handleProbe(r ProbeResult, now time.Time) []Action {
	if m.state != StateProbing || r.Port != m.candidate {
		return nil // outdated result after cancelling
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
			"The modem is not answering yet. Some modems need a little time, trying again in %d seconds …",
			int(m.cfg.RetryDelay.Seconds()))})
	}
	return m.enter(StateNoAnswer, now, Say{"no-answer", "On " + m.label(m.candidate) + " no modem is answering. " +
		"Is this really the cable from the modem? You can try again or use this port anyway."})
}

func (m *Machine) probe(now time.Time) []Action {
	m.attempts++
	acts := m.enter(StateProbing, now, Say{"probing", fmt.Sprintf("Checking the connection to the modem on %s (attempt %d of %d) …",
		m.label(m.candidate), m.attempts, m.cfg.MaxProbes)})
	return append(acts, StartProbe{m.candidate})
}

func (m *Machine) finish(r Result, now time.Time) []Action {
	m.result = r
	m.candidate = r.Port
	text := m.label(r.Port) + " is now your internet port."
	switch r.Method {
	case MethodDHCP:
		text = "Detected: " + text + " The modem hands out an address automatically."
	case MethodPPPoE:
		text = "Detected: " + text + " Your connection needs credentials (PPPoE)."
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
	return Say{"unplug", "Please unplug all network cables from this computer. Still plugged in: " + strings.Join(names, ", ") + "."}
}

func (m *Machine) sayPlugModem() Say {
	return Say{"plug-modem", "Now plug only the cable from the modem or internet router into one of the network ports."}
}

// up returns the ports with link, sorted by name.
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
