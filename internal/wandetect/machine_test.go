// SPDX-License-Identifier: AGPL-3.0-or-later

package wandetect

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

var labels = map[string]string{"eth0": "Port 1", "eth1": "Port 2", "eth2": "Port 3"}

var testCfg = Config{Settle: 3 * time.Second, HintAfter: 60 * time.Second, RetryDelay: 20 * time.Second, MaxProbes: 2}

// step is an event at time at (seconds after t0) with the expected state and expected actions.
// Actions are written in short form: "say:<id>", "probe:<port>", "cancel:<port>", "finish:<port>/<method>".
type step struct {
	at    float64
	ev    Event
	state State
	acts  string
}

func run(t *testing.T, start map[string]bool, startActs string, steps []step) *Machine {
	t.Helper()
	m := New(testCfg, labels)
	if got := summarize(m.Start(start, t0)); got != startActs {
		t.Fatalf("start: actions %q, expected %q", got, startActs)
	}
	for i, s := range steps {
		now := t0.Add(time.Duration(s.at * float64(time.Second)))
		got := summarize(m.Handle(s.ev, now))
		if got != s.acts || m.State() != s.state {
			t.Fatalf("step %d (%T %+v): state %v, actions %q; expected %v, %q", i+1, s.ev, s.ev, m.State(), got, s.state, s.acts)
		}
	}
	return m
}

func summarize(acts []Action) string {
	var parts []string
	for _, a := range acts {
		switch a := a.(type) {
		case Say:
			parts = append(parts, "say:"+a.ID)
		case StartProbe:
			parts = append(parts, "probe:"+a.Port)
		case CancelProbe:
			parts = append(parts, "cancel:"+a.Port)
		case Finished:
			parts = append(parts, "finish:"+a.Result.Port+"/"+string(a.Result.Method))
		}
	}
	return strings.Join(parts, " ")
}

func up(p string) LinkChanged   { return LinkChanged{p, true} }
func down(p string) LinkChanged { return LinkChanged{p, false} }

func TestNormalCaseDHCP(t *testing.T) {
	m := run(t, map[string]bool{"eth0": true, "eth2": true}, "say:unplug", []step{
		{1, down("eth0"), StateUnplug, "say:unplug"},
		{2, down("eth2"), StateWaitModem, "say:plug-modem"},
		{10, up("eth1"), StateSettling, "say:settling"},
		{11, Tick{}, StateSettling, ""},
		{13, Tick{}, StateProbing, "say:probing probe:eth1"},
		{15, ProbeDone{ProbeResult{Port: "eth1", DHCP: true, Offer: "192.0.2.10/24"}}, StateDone, "say:done finish:eth1/dhcp"},
		{16, up("eth0"), StateDone, ""}, // does not matter after completion
	})
	if r := m.Result(); r.Port != "eth1" || r.Method != MethodDHCP || r.Probe.Offer != "192.0.2.10/24" {
		t.Errorf("result %+v", r)
	}
}

func TestPPPoEWithVLAN(t *testing.T) {
	m := run(t, nil, "say:plug-modem", []step{
		{5, up("eth0"), StateSettling, "say:settling"},
		{8, Tick{}, StateProbing, "say:probing probe:eth0"},
		{12, ProbeDone{ProbeResult{Port: "eth0", PPPoE: true, PPPoEVLAN: 7, ACName: "AC1"}}, StateDone, "say:done finish:eth0/pppoe"},
	})
	if m.Result().Probe.PPPoEVLAN != 7 {
		t.Errorf("VLAN lost: %+v", m.Result())
	}
}

func TestAmbiguous(t *testing.T) {
	run(t, nil, "say:plug-modem", []step{
		{1, up("eth0"), StateSettling, "say:settling"},
		{2, up("eth1"), StateUnplug, "say:ambiguous say:unplug"},
		{3, down("eth0"), StateUnplug, "say:unplug"},
		{4, down("eth1"), StateWaitModem, "say:plug-modem"},
		{5, up("eth1"), StateSettling, "say:settling"},
	})
}

func TestCableUnpluggedAgainWhileWaiting(t *testing.T) {
	run(t, nil, "say:plug-modem", []step{
		{1, up("eth0"), StateSettling, "say:settling"},
		{2, down("eth0"), StateWaitModem, "say:plug-modem"},
		{6, Tick{}, StateWaitModem, ""}, // no probe start after the settle time has passed
	})
}

func TestConnectionLostDuringProbe(t *testing.T) {
	run(t, nil, "say:plug-modem", []step{
		{1, up("eth2"), StateSettling, "say:settling"},
		{4, Tick{}, StateProbing, "say:probing probe:eth2"},
		{5, down("eth2"), StateWaitModem, "cancel:eth2 say:lost say:plug-modem"},
		{6, ProbeDone{ProbeResult{Port: "eth2", DHCP: true}}, StateWaitModem, ""}, // outdated
	})
}

func TestSecondCableDuringProbe(t *testing.T) {
	run(t, nil, "say:plug-modem", []step{
		{1, up("eth0"), StateSettling, "say:settling"},
		{4, Tick{}, StateProbing, "say:probing probe:eth0"},
		{5, up("eth1"), StateUnplug, "cancel:eth0 say:ambiguous say:unplug"},
	})
}

func TestNoAnswerRetryAndConfirm(t *testing.T) {
	run(t, nil, "say:plug-modem", []step{
		{1, up("eth0"), StateSettling, "say:settling"},
		{4, Tick{}, StateProbing, "say:probing probe:eth0"},
		{9, ProbeDone{ProbeResult{Port: "eth0"}}, StateRetryWait, "say:retry"},
		{20, Tick{}, StateRetryWait, ""},
		{29, Tick{}, StateProbing, "say:probing probe:eth0"},
		{34, ProbeDone{ProbeResult{Port: "eth0", Err: "timeout"}}, StateNoAnswer, "say:no-answer"},
		{40, UserRetry{}, StateProbing, "say:probing probe:eth0"},
		{45, ProbeDone{ProbeResult{Port: "eth0"}}, StateRetryWait, "say:retry"},
		{65, Tick{}, StateProbing, "say:probing probe:eth0"},
		{70, ProbeDone{ProbeResult{Port: "eth0"}}, StateNoAnswer, "say:no-answer"},
		{80, UserAccept{}, StateDone, "say:done finish:eth0/link"},
	})
}

func TestCableOutInStateNoAnswer(t *testing.T) {
	run(t, nil, "say:plug-modem", []step{
		{1, up("eth0"), StateSettling, "say:settling"},
		{4, Tick{}, StateProbing, "say:probing probe:eth0"},
		{5, ProbeDone{ProbeResult{Port: "eth0"}}, StateRetryWait, "say:retry"},
		{6, down("eth0"), StateWaitModem, "say:lost say:plug-modem"}, // no running probe, no cancel
	})
}

func TestTipOnlyOnce(t *testing.T) {
	run(t, nil, "say:plug-modem", []step{
		{30, Tick{}, StateWaitModem, ""},
		{60, Tick{}, StateWaitModem, "say:hint-modem"},
		{120, Tick{}, StateWaitModem, ""},
	})
}

func TestManualChoice(t *testing.T) {
	run(t, map[string]bool{"eth0": true}, "say:unplug", []step{
		{1, UserManual{"eth9"}, StateUnplug, "say:manual-unknown"},
		{2, UserManual{"eth2"}, StateDone, "say:done finish:eth2/manual"},
	})
	run(t, nil, "say:plug-modem", []step{
		{1, up("eth0"), StateSettling, "say:settling"},
		{4, Tick{}, StateProbing, "say:probing probe:eth0"},
		{5, UserManual{"eth1"}, StateDone, "cancel:eth0 say:done finish:eth1/manual"},
	})
}

func TestUnknownPortsAndDuplicateMessages(t *testing.T) {
	run(t, nil, "say:plug-modem", []step{
		{1, up("docker0"), StateWaitModem, ""},
		{2, down("eth0"), StateWaitModem, ""}, // was already off
		{3, up("eth0"), StateSettling, "say:settling"},
		{4, up("eth0"), StateSettling, ""},
		{6, up("eth0"), StateProbing, "say:probing probe:eth0"}, // a duplicate message evaluates the time
	})
}

func TestTextsForNonExperts(t *testing.T) {
	m := New(testCfg, labels)
	acts := m.Start(map[string]bool{"eth1": true, "eth0": true}, t0)
	s := acts[0].(Say)
	if !strings.Contains(s.Text, "Port 1, Port 2") {
		t.Errorf("text does not name the ports understandably: %q", s.Text)
	}
	for _, w := range []string{"WAN", "LAN", "eth", "carrier", "Link"} {
		if strings.Contains(s.Text, w) {
			t.Errorf("jargon %q in text for non-experts: %q", w, s.Text)
		}
	}
}
