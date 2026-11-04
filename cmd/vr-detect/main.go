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
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"vanilla-router/internal/netdetect"
	"vanilla-router/internal/wandetect"
	"vanilla-router/internal/wandetect/live"
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

	lo := netdetect.Options{IncludeVirtual: *virtual}
	ports, skipped, err := netdetect.ListPortsOpts(*sysRoot, lo)
	if err != nil {
		fatal("Netzwerkanschlüsse lesen: %v", err)
	}
	if *only != "" {
		ports = FilterPorts(ports, strings.Split(*only, ","))
	}
	if *list {
		PrintPorts(os.Stdout, ports, skipped)
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(out, "Gefunden: %d Netzwerkanschlüsse. Eingabe einer Nummer wählt einen Anschluss von Hand.\n", len(ports))
	res, err := live.Run(ctx, out, ports, live.Options{ProbeTimeout: *probeTimeout, RetryDelay: *retryDelay,
		ListOpts: lo, Input: lines(os.Stdin)})
	if err != nil {
		fatal("%v", err)
	}
	fmt.Fprintln(out)
	fmt.Fprint(out, res.Summary())
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(res)
	}
}

func lines(r io.Reader) <-chan string {
	ch := make(chan string)
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			ch <- sc.Text()
		}
		close(ch)
	}()
	return ch
}

// FilterPorts behält nur die genannten Ports und nummeriert neu.
func FilterPorts(ports []netdetect.Port, names []string) []netdetect.Port {
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

// PrintPorts zeigt die Ports als Tabelle.
func PrintPorts(w io.Writer, ports []netdetect.Port, skipped []netdetect.Skipped) {
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
