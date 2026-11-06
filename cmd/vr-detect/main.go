// SPDX-License-Identifier: AGPL-3.0-or-later

// vr-detect guides through detecting the internet port (demo for M1, T-M1-07).
//
// Warning: turns on all Ethernet ports and sends DHCP and PPPoE requests. Only use on an
// installation machine or in the test lab, not on a running router.
//
//	vr-detect -list           show ports, change nothing (no root needed)
//	sudo vr-detect            start the flow
//	sudo vr-detect -json      also print the result as JSON on stdout
package main

import (
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
	"vanilla-router/internal/tui"
	"vanilla-router/internal/wandetect"
	"vanilla-router/internal/wandetect/live"
)

func main() {
	list := flag.Bool("list", false, "only show the ports")
	sysRoot := flag.String("sys", "/sys", "sysfs root (for tests)")
	only := flag.String("ports", "", "only consider these ports (comma-separated), e.g. for the test lab")
	asJSON := flag.Bool("json", false, "print the result as JSON on stdout, texts on stderr")
	probeTimeout := flag.Duration("probe-timeout", 5*time.Second, "duration of one probe")
	retryDelay := flag.Duration("retry-delay", wandetect.DefaultConfig.RetryDelay, "pause between two probes without an answer")
	virtual := flag.Bool("include-virtual", false, "also consider virtual interfaces (veth), test lab only")
	flag.Parse()

	lo := netdetect.Options{IncludeVirtual: *virtual}
	ports, skipped, err := netdetect.ListPortsOpts(*sysRoot, lo)
	if err != nil {
		fatal("reading network ports: %v", err)
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
		fatal("This computer has %d network port(s). A router needs at least two.", len(ports))
	}
	if os.Geteuid() != 0 {
		fatal("vr-detect needs root (turning ports on, raw sockets). To only show them: vr-detect -list")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(out, "Found: %d network ports.\n", len(ports))
	term := tui.IsTerminal(0)
	if term {
		defer tui.RawMode(0)()
	}
	p := tui.New(os.Stdin, out, !term)
	res, err := live.Run(ctx, out, ports, live.Options{ProbeTimeout: *probeTimeout, RetryDelay: *retryDelay,
		ListOpts: lo, Menu: p.MenuAsync, Spinner: p.StartSpinner})
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

// FilterPorts keeps only the named ports and renumbers them.
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

// PrintPorts shows the ports as a table.
func PrintPorts(w io.Writer, ports []netdetect.Port, skipped []netdetect.Skipped) {
	fmt.Fprintf(w, "%-26s %-12s %-17s %-12s %-6s %-6s %s\n", "Name", "Name", "MAC", "Driver", "Bus", "Cable", "Speed")
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
		fmt.Fprintf(w, "\nNot considered: %s\n", strings.Join(s, ", "))
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "vr-detect: "+format+"\n", args...)
	os.Exit(1)
}
