// SPDX-License-Identifier: AGPL-3.0-or-later

// vr-detect führt durch die Erkennung des Internet-Anschlusses (Vorführer für M1, T-M1-07).
//
// Achtung: Schaltet alle Ethernet-Ports ein und sendet DHCP- und PPPoE-Anfragen. Nur auf einem
// Installationsrechner oder im Testlabor verwenden, nicht auf einem laufenden Router.
//
//	vr-detect -list           Ports anzeigen, nichts verändern (ohne Root-Rechte)
//	sudo vr-detect            Ablauf starten
//	sudo vr-detect -json      Ergebnis zusätzlich als JSON auf stdout
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vanilla-router/internal/netdetect"
	"vanilla-router/internal/probe"
	"vanilla-router/internal/wandetect"
)

func main() {
	list := flag.Bool("list", false, "nur Ports anzeigen")
	sysRoot := flag.String("sys", "/sys", "Wurzel von sysfs (für Tests)")
	only := flag.String("ports", "", "nur diese Ports beachten (Komma-getrennt), z. B. für das Testlabor")
	asJSON := flag.Bool("json", false, "Ergebnis als JSON auf stdout ausgeben, Texte auf stderr")
	probeTimeout := flag.Duration("probe-timeout", 5*time.Second, "Dauer einer Probe")
	retryDelay := flag.Duration("retry-delay", wandetect.DefaultConfig.RetryDelay, "Pause zwischen zwei Proben ohne Antwort")
	virtual := flag.Bool("include-virtual", false, "auch virtuelle Interfaces (veth) beachten, nur für das Testlabor")
	flag.Parse()

	listOpts = netdetect.Options{IncludeVirtual: *virtual}
	ports, skipped, err := netdetect.ListPortsOpts(*sysRoot, listOpts)
	if err != nil {
		fatal("Netzwerkanschlüsse lesen: %v", err)
	}
	if *only != "" {
		ports = filterPorts(ports, strings.Split(*only, ","))
	}
	if *list {
		printPorts(os.Stdout, ports, skipped)
		return
	}
	out := io.Writer(os.Stdout)
	if *asJSON {
		out = os.Stderr
	}
	if len(ports) < 2 {
		fatal("Dieser Rechner hat %d Netzwerkanschluss/-anschlüsse. Ein Router braucht mindestens zwei.", len(ports))
	}
	if os.Geteuid() != 0 {
		fatal("vr-detect braucht Root-Rechte (Ports einschalten, Rohsockets). Nur anzeigen: vr-detect -list")
	}

	res, err := runDetect(out, ports, *probeTimeout, *retryDelay)
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(res)
	}
}

// Output ist das Ergebnis als JSON (später Teil von config.json, F-INST-08).
type Output struct {
	WAN    string            `json:"wan"`
	LAN    []string          `json:"lan"`
	Method wandetect.Method  `json:"method"`
	DHCP   string            `json:"dhcp_offer,omitempty"`
	PPPoE  *pppoeOut         `json:"pppoe,omitempty"`
	Ports  map[string]string `json:"ports"` // Name → MAC, damit die Zuordnung Umbenennungen übersteht
}

type pppoeOut struct {
	VLAN   int    `json:"vlan"`
	ACName string `json:"ac_name"`
}

func runDetect(out io.Writer, ports []netdetect.Port, probeTimeout, retryDelay time.Duration) (Output, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	byName := map[string]netdetect.Port{}
	labels := map[string]string{}
	for _, p := range ports {
		byName[p.Name] = p
		labels[p.Name] = p.Label()
	}

	// Erst den Watcher öffnen, dann einschalten und den Zustand lesen: So geht kein Ereignis verloren.
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
	time.Sleep(500 * time.Millisecond) // Treiber melden den Link oft erst kurz nach dem Einschalten

	cfg := wandetect.DefaultConfig
	cfg.RetryDelay = retryDelay
	m := wandetect.New(cfg, labels)
	links, err := readLinks(ports)
	if err != nil {
		return Output{}, err
	}

	fmt.Fprintf(out, "Gefunden: %d Netzwerkanschlüsse.\n", len(ports))
	fmt.Fprintln(out, "Eingaben: Nummer + Enter wählt einen Anschluss von Hand. Bei „antwortet kein Modem“: w = wiederholen, j = trotzdem verwenden.")

	probeDone := make(chan wandetect.ProbeResult, 1)
	var cancelProbe context.CancelFunc
	userEv := make(chan wandetect.Event, 4)
	go readUser(os.Stdin, ports, userEv)

	exec := func(acts []wandetect.Action) (*wandetect.Result, error) {
		for _, a := range acts {
			switch a := a.(type) {
			case wandetect.Say:
				fmt.Fprintln(out, "→ "+a.Text)
			case wandetect.StartProbe:
				p := byName[a.Port]
				pctx, cancel := context.WithCancel(ctx)
				cancelProbe = cancel
				go func() {
					probeDone <- runProbe(pctx, p, probeTimeout)
				}()
			case wandetect.CancelProbe:
				if cancelProbe != nil {
					cancelProbe()
					cancelProbe = nil
				}
			case wandetect.Finished:
				r := a.Result
				return &r, nil
			}
		}
		return nil, nil
	}

	finish := func(r *wandetect.Result) Output {
		if cancelProbe != nil {
			cancelProbe()
		}
		return buildOutput(out, ports, *r)
	}

	if r, _ := exec(m.Start(links, time.Now())); r != nil {
		return finish(r), nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return Output{}, errors.New("abgebrochen")
		}
		var acts []wandetect.Action
		select {
		case pr := <-probeDone:
			acts = m.Handle(wandetect.ProbeDone{Result: pr}, time.Now())
		case ev := <-userEv:
			acts = m.Handle(ev, time.Now())
		default:
			evs, err := w.Read(200 * time.Millisecond)
			if errors.Is(err, netdetect.ErrResync) {
				links, err = readLinks(ports)
				if err != nil {
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
		r, err := exec(acts)
		if err != nil {
			return Output{}, err
		}
		if r != nil {
			return finish(r), nil
		}
	}
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
	}
	for _, po := range res.PPPoE {
		if po.Error != "" {
			continue
		}
		// Ohne VLAN bevorzugen, falls beides antwortet
		if !pr.PPPoE || po.VLAN == 0 {
			pr.PPPoE, pr.PPPoEVLAN, pr.ACName = true, po.VLAN, po.ACName
		}
	}
	return pr
}

var listOpts netdetect.Options

func readLinks(ports []netdetect.Port) (map[string]bool, error) {
	cur, _, err := netdetect.ListPortsOpts("/sys", listOpts)
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

func readUser(r io.Reader, ports []netdetect.Port, ch chan<- wandetect.Event) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		s := strings.ToLower(strings.TrimSpace(sc.Text()))
		switch s {
		case "w":
			ch <- wandetect.UserRetry{}
		case "j":
			ch <- wandetect.UserAccept{}
		default:
			if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(ports) {
				ch <- wandetect.UserManual{Port: ports[n-1].Name}
			}
		}
	}
}

func buildOutput(out io.Writer, ports []netdetect.Port, r wandetect.Result) Output {
	o := Output{WAN: r.Port, Method: r.Method, Ports: map[string]string{}}
	var lanLabels []string
	for _, p := range ports {
		o.Ports[p.Name] = p.MAC
		if p.Name != r.Port {
			o.LAN = append(o.LAN, p.Name)
			lanLabels = append(lanLabels, p.Label())
		}
	}
	if r.Probe.DHCP {
		o.DHCP = r.Probe.Offer
	}
	if r.Method == wandetect.MethodPPPoE {
		o.PPPoE = &pppoeOut{VLAN: r.Probe.PPPoEVLAN, ACName: r.Probe.ACName}
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Internet-Anschluss: %s (%s)\n", labelOf(ports, r.Port), r.Port)
	switch {
	case o.DHCP != "":
		fmt.Fprintf(out, "  Das Modem bietet an: %s\n", o.DHCP)
	case o.PPPoE != nil && o.PPPoE.VLAN != 0:
		fmt.Fprintf(out, "  PPPoE über VLAN %d (Zugangsserver %q). Zugangsdaten werden im nächsten Schritt abgefragt.\n", o.PPPoE.VLAN, o.PPPoE.ACName)
	case o.PPPoE != nil:
		fmt.Fprintf(out, "  PPPoE (Zugangsserver %q). Zugangsdaten werden im nächsten Schritt abgefragt.\n", o.PPPoE.ACName)
	}
	fmt.Fprintf(out, "Heimnetz: %s\n", strings.Join(lanLabels, ", "))
	return o
}

func labelOf(ports []netdetect.Port, name string) string {
	for _, p := range ports {
		if p.Name == name {
			return p.Label()
		}
	}
	return name
}

func filterPorts(ports []netdetect.Port, names []string) []netdetect.Port {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.TrimSpace(n)] = true
	}
	var out []netdetect.Port
	for _, p := range ports {
		if want[p.Name] {
			p.Number = len(out) + 1
			out = append(out, p)
		}
	}
	return out
}

func printPorts(w io.Writer, ports []netdetect.Port, skipped []netdetect.Skipped) {
	fmt.Fprintf(w, "%-26s %-12s %-17s %-12s %-6s %-6s %s\n", "Bezeichnung", "Name", "MAC", "Treiber", "Bus", "Kabel", "Geschw.")
	for _, p := range ports {
		speed := "-"
		if p.SpeedMbps > 0 {
			speed = fmt.Sprintf("%d Mbit/s", p.SpeedMbps)
		}
		fmt.Fprintf(w, "%-26s %-12s %-17s %-12s %-6s %-6s %s\n", p.Label(), p.Name, p.MAC, p.Driver, p.Bus, p.Carrier, speed)
	}
	if len(skipped) > 0 {
		var s []string
		for _, sk := range skipped {
			s = append(s, sk.Name+" ("+sk.Reason+")")
		}
		fmt.Fprintf(w, "\nNicht berücksichtigt: %s\n", strings.Join(s, ", "))
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "vr-detect: "+format+"\n", args...)
	os.Exit(1)
}
