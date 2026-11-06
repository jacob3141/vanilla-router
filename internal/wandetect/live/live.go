// SPDX-License-Identifier: AGPL-3.0-or-later

// Package live connects the automaton from wandetect with real hardware: netlink events,
// turning ports on and probes. Used by vr-detect and the installer (S3/S4).
package live

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"
	"vanilla-router/internal/i18n"

	"vanilla-router/internal/netdetect"
	"vanilla-router/internal/probe"
	"vanilla-router/internal/wandetect"
)

// Options controls the flow.
type Options struct {
	ProbeTimeout time.Duration
	RetryDelay   time.Duration
	ListOpts     netdetect.Options
	// Menu shows a choice while detection continues (tui.Prompt.MenuAsync), operated only
	// with arrow keys and Enter (P-11). nil = no input.
	Menu func(options []string, def int) (<-chan int, func())
	// Spinner shows that detection is alive while it waits (tui.Prompt.StartSpinner). nil = none.
	Spinner func(text string) (stop func())
}

// Menus during detection.
const (
	menuNoAnswer = "no-answer"
	menuWaiting  = "hint-modem"
	menuList     = "list"
)

// Output is the result.
type Output struct {
	WAN      string            `json:"wan"`
	WANMAC   string            `json:"wan_mac"`
	LAN      []string          `json:"lan"`
	LANMACs  []string          `json:"lan_macs"`
	Method   wandetect.Method  `json:"method"`
	DHCP     string            `json:"dhcp_offer,omitempty"`
	DHCPNet  string            `json:"dhcp_net,omitempty"` // network on the WAN, the LAN must avoid it
	PPPoE    *PPPoE            `json:"pppoe,omitempty"`
	Ports    map[string]string `json:"ports"`
	Labels   map[string]string `json:"labels"`
	WANLabel string            `json:"wan_label"`
}

// PPPoE describes the access server found.
type PPPoE struct {
	VLAN   int    `json:"vlan"`
	ACName string `json:"ac_name"`
}

// Run guides the user through detection. Needs root.
func Run(ctx context.Context, out io.Writer, ports []netdetect.Port, opt Options) (Output, error) {
	if opt.ProbeTimeout == 0 {
		opt.ProbeTimeout = 5 * time.Second
	}
	if opt.RetryDelay == 0 {
		opt.RetryDelay = wandetect.DefaultConfig.RetryDelay
	}
	byName := map[string]netdetect.Port{}
	labels := map[string]string{}
	for _, p := range ports {
		byName[p.Name] = p
		labels[p.Name] = p.Label()
	}

	// First open the watcher, then turn on and read the state: that way no event gets lost.
	w, err := netdetect.NewLinkWatcher()
	if err != nil {
		return Output{}, err
	}
	defer w.Close()
	for _, p := range ports {
		if err := netdetect.SetUp(p.Name); err != nil {
			return Output{}, err
		}
	}
	time.Sleep(500 * time.Millisecond) // Drivers often report the link only shortly after being turned on

	cfg := wandetect.DefaultConfig
	cfg.RetryDelay = opt.RetryDelay
	m := wandetect.New(cfg, labels)
	links, err := readLinks(ports, opt.ListOpts)
	if err != nil {
		return Output{}, err
	}

	probeDone := make(chan wandetect.ProbeResult, 1)
	var cancelProbe context.CancelFunc
	defer func() {
		if cancelProbe != nil {
			cancelProbe()
		}
	}()

	// At most one menu is open. Each new message from the automaton closes it, because the
	// state has changed (e.g. a cable has been plugged in in the meantime).
	var menuCh <-chan int
	var menuCancel func()
	var stopSpin func()
	spinOff := func() {
		if stopSpin != nil {
			stopSpin()
			stopSpin = nil
		}
	}
	defer spinOff()
	spinOn := func() {
		if opt.Spinner != nil && stopSpin == nil && menuCh == nil {
			if text := statusText(m.State()); text != "" {
				stopSpin = opt.Spinner(text)
			}
		}
	}
	menuKind, listFrom := "", ""
	closeMenu := func() {
		if menuCancel != nil {
			menuCancel()
		}
		menuCh, menuCancel, menuKind = nil, nil, ""
	}
	defer closeMenu()
	openMenu := func(kind string, options []string) {
		closeMenu()
		spinOff()
		if opt.Menu == nil {
			return
		}
		menuCh, menuCancel = opt.Menu(options, 0)
		menuKind = kind
	}
	listMenu := func() {
		var opts []string
		for _, p := range ports {
			opts = append(opts, p.Label())
		}
		spinOff()
		fmt.Fprintln(out, i18n.T("→ Which port is connected to the modem?"))
		openMenu(menuList, append(opts, "Back"))
	}

	exec := func(acts []wandetect.Action) *wandetect.Result {
		for _, a := range acts {
			switch a := a.(type) {
			case wandetect.Say:
				closeMenu()
				spinOff()
				fmt.Fprintln(out, "→ "+i18n.T(a.Text))
				switch a.ID {
				case menuNoAnswer:
					openMenu(menuNoAnswer, []string{"Try again", "Use this port anyway", "Choose the port myself"})
				case menuWaiting:
					openMenu(menuWaiting, []string{"Keep waiting", "Choose the port myself"})
				}
			case wandetect.StartProbe:
				p := byName[a.Port]
				pctx, cancel := context.WithCancel(ctx)
				cancelProbe = cancel
				go func() { probeDone <- runProbe(pctx, p, opt.ProbeTimeout) }()
			case wandetect.CancelProbe:
				if cancelProbe != nil {
					cancelProbe()
					cancelProbe = nil
				}
			case wandetect.Finished:
				spinOff()
				r := a.Result
				return &r
			}
		}
		return nil
	}

	if r := exec(m.Start(links, time.Now())); r != nil {
		return buildOutput(ports, *r), nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return Output{}, errors.New("cancelled")
		}
		spinOn()
		var acts []wandetect.Action
		select {
		case pr := <-probeDone:
			acts = m.Handle(wandetect.ProbeDone{Result: pr}, time.Now())
		case choice, ok := <-menuCh:
			kind := menuKind
			menuCh, menuCancel, menuKind = nil, nil, ""
			if !ok {
				break
			}
			if ev, list := menuEvent(kind, choice, ports); list {
				listFrom = kind
				listMenu()
			} else if kind == menuList && ev == nil && listFrom == menuNoAnswer && m.State() == wandetect.StateNoAnswer {
				// "Back" from the list: the previous choice again, otherwise you would be left without a menu
				openMenu(menuNoAnswer, []string{"Try again", "Use this port anyway", "Choose the port myself"})
			} else if ev != nil {
				acts = m.Handle(ev, time.Now())
			}
		default:
			evs, err := w.Read(200 * time.Millisecond)
			if errors.Is(err, netdetect.ErrResync) {
				if links, err = readLinks(ports, opt.ListOpts); err != nil {
					return Output{}, err
				}
				for name, up := range links {
					acts = append(acts, m.Handle(wandetect.LinkChanged{Port: name, Up: up}, time.Now())...)
				}
				break
			}
			if err != nil {
				return Output{}, err
			}
			for _, e := range evs {
				if _, ok := byName[e.Name]; ok {
					acts = append(acts, m.Handle(wandetect.LinkChanged{Port: e.Name, Up: e.HasLink()}, time.Now())...)
				}
			}
			acts = append(acts, m.Handle(wandetect.Tick{}, time.Now())...)
		}
		if r := exec(acts); r != nil {
			return buildOutput(ports, *r), nil
		}
	}
}

// menuEvent translates a choice into an event for the automaton. list = true: show the list of
// ports. No event and no list: do nothing (keep waiting, back).
func menuEvent(kind string, choice int, ports []netdetect.Port) (wandetect.Event, bool) {
	switch kind {
	case menuNoAnswer:
		switch choice {
		case 0:
			return wandetect.UserRetry{}, false
		case 1:
			return wandetect.UserAccept{}, false
		default:
			return nil, true
		}
	case menuWaiting:
		return nil, choice == 1
	case menuList:
		if choice >= 0 && choice < len(ports) {
			return wandetect.UserManual{Port: ports[choice].Name}, false
		}
	}
	return nil, false
}

func runProbe(ctx context.Context, p netdetect.Port, timeout time.Duration) wandetect.ProbeResult {
	pr := wandetect.ProbeResult{Port: p.Name}
	mac, err := net.ParseMAC(p.MAC)
	if err != nil {
		pr.Err = err.Error()
		return pr
	}
	res, err := probe.Run(ctx, p.Index, mac, probe.Options{Timeout: timeout})
	if err != nil {
		pr.Err = err.Error()
	}
	if res.DHCP != nil {
		pr.DHCP = true
		pr.Offer = res.DHCP.String()
		if a, ok := netip.AddrFromSlice(res.DHCP.YourIP.To4()); ok && res.DHCP.Mask != nil {
			ones, _ := res.DHCP.Mask.Size()
			pr.OfferNet = netip.PrefixFrom(a, ones).Masked().String()
		}
	}
	for _, po := range res.PPPoE {
		if po.Error != "" {
			continue
		}
		if !pr.PPPoE || po.VLAN == 0 { // prefer without VLAN if both answer
			pr.PPPoE, pr.PPPoEVLAN, pr.ACName = true, po.VLAN, po.ACName
		}
	}
	return pr
}

// statusText says what the flow is waiting for, shown next to the spinner. Empty: nothing to show,
// the user has to decide.
func statusText(s wandetect.State) string {
	switch s {
	case wandetect.StateUnplug:
		return "Waiting for the cables to be unplugged"
	case wandetect.StateWaitModem:
		return "Waiting for the cable"
	case wandetect.StateSettling, wandetect.StateProbing:
		return "Checking the connection"
	case wandetect.StateRetryWait:
		return "Waiting for the next attempt"
	}
	return ""
}

func readLinks(ports []netdetect.Port, lo netdetect.Options) (map[string]bool, error) {
	cur, _, err := netdetect.ListPortsOpts("/sys", lo)
	if err != nil {
		return nil, err
	}
	links := map[string]bool{}
	for _, c := range cur {
		links[c.Name] = c.Carrier == netdetect.Yes
	}
	for _, p := range ports {
		if _, ok := links[p.Name]; !ok {
			links[p.Name] = false
		}
	}
	return links, nil
}

func buildOutput(ports []netdetect.Port, r wandetect.Result) Output {
	o := Output{WAN: r.Port, Method: r.Method, Ports: map[string]string{}, Labels: map[string]string{}}
	for _, p := range ports {
		o.Ports[p.Name] = p.MAC
		o.Labels[p.Name] = p.Label()
		if p.Name == r.Port {
			o.WANMAC, o.WANLabel = p.MAC, p.Label()
		} else {
			o.LAN = append(o.LAN, p.Name)
			o.LANMACs = append(o.LANMACs, p.MAC)
		}
	}
	if r.Probe.DHCP {
		o.DHCP, o.DHCPNet = r.Probe.Offer, r.Probe.OfferNet
	}
	if r.Method == wandetect.MethodPPPoE {
		o.PPPoE = &PPPoE{VLAN: r.Probe.PPPoEVLAN, ACName: r.Probe.ACName}
	}
	return o
}

// Summary describes the result for non-experts.
func (o Output) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Internet port: %s\n", o.WANLabel)
	switch {
	case o.DHCP != "":
		fmt.Fprintf(&b, "  The modem hands out an address automatically (%s).\n", o.DHCP)
	case o.PPPoE != nil && o.PPPoE.VLAN != 0:
		fmt.Fprintf(&b, "  Dial-up with credentials (PPPoE over VLAN %d).\n", o.PPPoE.VLAN)
	case o.PPPoE != nil:
		b.WriteString("  Dial-up with credentials (PPPoE).\n")
	default:
		b.WriteString("  No modem answered. You can change the settings later.\n")
	}
	var lan []string
	for _, n := range o.LAN {
		lan = append(lan, o.Labels[n])
	}
	fmt.Fprintf(&b, "Home network: %s\n", strings.Join(lan, ", "))
	return i18n.T(b.String())
}
