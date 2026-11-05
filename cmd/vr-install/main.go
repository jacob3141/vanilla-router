// SPDX-License-Identifier: AGPL-3.0-or-later

// vr-install ist der Installations-Assistent auf dem Boot-Stick (PLAN.md 3.9.2). Er startet
// automatisch auf tty1, wird nur mit Pfeiltasten und Enter bedient und fragt nur nach Physischem
// (P-11). Bis zur ausdrücklichen Bestätigung in S6 wird nichts auf die Festplatte geschrieben.
//
//	vr-install                         Assistent
//	vr-install --dry-run               Assistent ohne Schreiben (zeigt am Ende den Plan)
//	vr-install --unattended plan.json  ohne Fragen (F-INST-08)
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vanilla-router/internal/config"
	"vanilla-router/internal/disk"
	"vanilla-router/internal/install"
	"vanilla-router/internal/netdetect"
	"vanilla-router/internal/stick"
	"vanilla-router/internal/tui"
	"vanilla-router/internal/wandetect/live"
)

var version = "dev"

func main() {
	dry := flag.Bool("dry-run", false, "alles abfragen, aber nichts schreiben")
	rootfs := flag.String("rootfs", "/usr/share/vanilla-router/rootfs.tar.zst", "Image des Zielsystems")
	unattended := flag.String("unattended", "", "Plan als JSON, ohne Fragen installieren")
	virtual := flag.Bool("include-virtual", false, "virtuelle Ports zulassen (nur Testlabor)")
	flag.Parse()

	if *unattended != "" {
		if err := runUnattended(*unattended, *rootfs, *dry); err != nil {
			fmt.Fprintln(os.Stderr, "vr-install:", err)
			os.Exit(1)
		}
		return
	}
	term := tui.IsTerminal(0)
	restore := func() {}
	if term {
		restore = tui.RawMode(0)
	}
	p := tui.New(os.Stdin, os.Stdout, !term)
	err := assistant(p, *rootfs, *dry, netdetect.Options{IncludeVirtual: *virtual}, stick.Mounts)
	if err != nil && !errors.Is(err, tui.ErrEOF) && !errors.Is(err, errAborted) {
		p.Say("\nDas hat leider nicht geklappt:\n  %v\n", err)
		var ie *installError
		if errors.As(err, &ie) {
			p.Say("Die Installation ist unvollständig. Der Rechner startet so noch nicht als Router.")
		} else {
			p.Say("Es wurde nichts verändert.")
		}
		p.WaitEnter("Drücken Sie Enter, um von vorn zu beginnen.")
	}
	restore()
	if err != nil {
		os.Exit(1)
	}
}

// errAborted: Der Nutzer hat vor dem Löschen abgebrochen.
var errAborted = errors.New("abgebrochen")

// installError: Fehler nach dem Beginn des Schreibens auf die Festplatte.
type installError struct{ err error }

func (e *installError) Error() string { return e.err.Error() }
func (e *installError) Unwrap() error { return e.err }

// UnattendedPlan ist das Format für --unattended.
type UnattendedPlan struct {
	Disk      string            `json:"disk"`
	Config    *config.Config    `json:"config"`
	Secrets   map[string]string `json:"secrets"`
	SetupCode string            `json:"setup_code,omitempty"`
}

func runUnattended(path, rootfs string, dry bool) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var up UnattendedPlan
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&up); err != nil {
		return err
	}
	if up.Config == nil || up.Disk == "" {
		return errors.New("disk und config sind Pflicht")
	}
	if up.SetupCode == "" {
		up.SetupCode = newSetupCode()
	}
	pl := install.Plan{Disk: up.Disk, Rootfs: rootfs, Config: up.Config, Secrets: up.Secrets, SetupCode: up.SetupCode}
	if dry {
		fmt.Printf("Probelauf: würde %s löschen und installieren.\n", up.Disk)
		return up.Config.Validate()
	}
	if err := install.CheckRoot(); err != nil {
		return err
	}
	in := &install.Installer{Run: install.ExecRunner{}, Progress: func(n, t int, s string) { fmt.Printf("[%d/%d] %s\n", n, t, s) }}
	if err := in.Install(pl); err != nil {
		return err
	}
	fmt.Println("Einrichtungscode:", up.SetupCode)
	return nil
}

// assistant ist der Ablauf aus PLAN.md 3.9.2 (Stand 0.8): gefragt wird nur nach Physischem, also
// welches Kabel ins Modem geht und welche Festplatte gelöscht werden darf (P-11). Bedient wird nur
// mit Pfeiltasten und Enter. Alles andere stellt man danach in der Weboberfläche ein.
func assistant(p *tui.Prompt, rootfs string, dry bool, lo netdetect.Options, mounts []string) error {
	p.Banner("vanilla-router "+version, "Willkommen!")
	p.Say("Aus diesem Rechner wird jetzt ein Router. Sie müssen nur zwei Dinge tun:")
	p.Say("  1. das Kabel vom Modem einstecken,")
	p.Say("  2. bestätigen, auf welche Festplatte installiert wird.")
	p.Say("Alles Weitere stellen Sie danach bequem im Browser ein.")
	if !dry {
		if err := install.CheckRoot(); err != nil {
			return err
		}
	}

	// S1 Sprache: aus dem Stick-Ersteller, sonst eine Frage (Texte vorerst deutsch, F-INST-10)
	info, ok := stick.Find(mounts)
	if !ok {
		p.Heading("Sprache und Region")
		var labels []string
		for _, l := range stick.Languages {
			labels = append(labels, l.Label)
		}
		i, err := p.Menu(labels, 0)
		if err != nil {
			return err
		}
		info = stick.Languages[i].Info()
	}

	// S2 Hardware: nur sichtbar, wenn etwas fehlt (F-INST-18)
	ports, _, err := netdetect.ListPortsOpts("/sys", lo)
	if err != nil {
		return err
	}
	boot := disk.BootDisk("/sys", "/proc/self/mountinfo", "/")
	disks, err := disk.List("/sys", boot)
	if err != nil {
		return err
	}
	cands := disk.Candidates(disks)
	if dry && len(cands) == 0 {
		cands = []disk.Disk{{Name: "probelauf", Path: "/dev/probelauf", SizeBytes: 128e9, Model: "Schein-Datenträger (Probelauf)"}}
	}
	if dry && os.Geteuid() != 0 && len(ports) < 2 {
		// Probelauf auf einem Entwicklungsrechner: Schein-Anschlüsse, damit der ganze Ablauf zu sehen ist
		ports = nil
		for i := 1; i <= 3; i++ {
			ports = append(ports, netdetect.Port{Name: fmt.Sprintf("probe%d", i), MAC: fmt.Sprintf("02:00:00:00:00:%02x", i), Number: i})
		}
	}
	if len(ports) < 2 {
		return fmt.Errorf("Dieser Rechner hat %d Netzwerkanschluss/-anschlüsse. Ein Router braucht mindestens zwei, "+
			"zum Beispiel mit einem USB-Netzwerkadapter. Schalten Sie den Rechner aus, schließen Sie einen Adapter an "+
			"und starten Sie dann wieder vom Stick", len(ports))
	}
	if len(cands) == 0 {
		return fmt.Errorf("Es gibt keine Festplatte mit mindestens 16 GB (der Stick selbst zählt nicht)")
	}

	// S3/S4 Internet-Anschluss: erkennen, nichts eingeben
	p.Heading("Internet-Anschluss")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var det live.Output
	if dry && os.Geteuid() != 0 {
		p.Say("(Probelauf ohne Root-Rechte: Erkennung übersprungen, Anschluss 1 gilt als Internet-Anschluss.)")
		det = fakeDetect(ports)
	} else {
		det, err = live.Run(ctx, p.Out, ports, live.Options{ListOpts: lo, Menu: p.MenuAsync})
		if err != nil {
			return err
		}
	}
	p.Say("\n%s", det.Summary())

	in := config.DefaultInput{WANMAC: det.WANMAC, LANMACs: det.LANMACs}
	if det.DHCPNet != "" {
		in.WANNet, _ = netip.ParsePrefix(det.DHCPNet)
	}
	cfg, err := config.Default(in)
	if err != nil {
		return err
	}
	info.Apply(cfg)
	// F-INST-15: Ergebnis vermerken, die Oberfläche fragt dann nach Zugangsdaten (F-UI-08)
	switch {
	case det.PPPoE != nil:
		cfg.WAN.Detected = &config.Detected{Method: config.WANPPPoE, VLAN: det.PPPoE.VLAN}
		p.Say("Die Zugangsdaten aus dem Brief Ihres Anbieters geben Sie nach der Installation im Browser ein.")
	case det.DHCP != "":
		cfg.WAN.Detected = &config.Detected{Method: config.WANDHCP}
		if in.WANNet.IsValid() && in.WANNet.Addr().IsPrivate() {
			p.Say("Vor diesem Router steht noch ein Router des Anbieters. Das funktioniert so.")
		}
	default:
		cfg.WAN.Detected = &config.Detected{Method: config.DetectedNone}
		p.Say("Wie Ihr Anschluss funktioniert, wählen Sie nach der Installation im Browser.")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	// S6 Festplatte: immer bewusst bestätigen, auch wenn es nur eine gibt
	p.Heading("Festplatte")
	target := cands[0]
	if len(cands) > 1 {
		p.Say("Auf welche Festplatte soll vanilla-router?")
		var labels []string
		for _, d := range cands {
			labels = append(labels, d.Label())
		}
		i, err := p.Menu(labels, 0)
		if err != nil {
			return err
		}
		target = cands[i]
	}
	p.Say("ACHTUNG: Alle Daten auf dieser Festplatte werden gelöscht:\n\n    %s\n", target.Label())
	i, err := p.Menu([]string{"Nein, abbrechen", "Ja, alles löschen und installieren"}, 0)
	if err != nil {
		return err
	}
	if i == 0 {
		p.Say("\nAbgebrochen. Es wurde nichts verändert.")
		p.WaitEnter("Drücken Sie Enter, um den Rechner auszuschalten.")
		if !dry {
			install.ExecRunner{}.Run(nil, nil, "systemctl", "poweroff")
		}
		return errAborted
	}

	// S8 Installation
	p.Heading("Installation")
	code := newSetupCode()
	plan := install.Plan{Disk: target.Path, Rootfs: rootfs, Config: cfg, Secrets: map[string]string{}, SetupCode: code}
	if dry {
		b, _ := json.MarshalIndent(UnattendedPlan{Disk: plan.Disk, Config: cfg, SetupCode: code}, "", "  ")
		p.Say("Probelauf: Es wurde nichts geschrieben. Dieser Plan würde installiert:\n%s", b)
		return nil
	}
	start := time.Now()
	inst := &install.Installer{Run: install.ExecRunner{}, Progress: func(n, t int, s string) {
		p.Say("  [%s] %s …", bar(n, t), s)
	}}
	if err := inst.Install(plan); err != nil {
		return &installError{err}
	}
	p.Say("Fertig nach %s.", time.Since(start).Round(time.Second))

	// S9 Zusammenfassung
	_, router, _ := cfg.LANPrefix()
	p.Heading("Geschafft")
	p.Say("Ihr Router ist installiert. So geht es weiter:\n")
	p.Say("  1. Verbinden Sie einen Laptop oder ein Handy mit dem Heimnetz,")
	p.Say("     also mit einem der anderen Anschlüsse oder Ihrem WLAN-Access-Point.")
	p.Say("  2. Öffnen Sie im Browser:  https://%s.%s", cfg.System.Hostname, cfg.DNS.Domain)
	p.Say("     (oder https://%s)", router)
	p.Say("  3. Einrichtungscode für die erste Anmeldung:  %s\n", code)
	p.Say("Der Router zeigt Adresse und Code nach dem Neustart auch auf diesem Bildschirm an.")

	// S10 Neustart
	if err := p.WaitEnter("\nZiehen Sie jetzt den Stick ab und drücken Sie Enter."); err != nil {
		return err
	}
	if out, err := (install.ExecRunner{}).Run(nil, nil, "systemctl", "reboot"); err != nil {
		return fmt.Errorf("Neustart: %v %s", err, out)
	}
	return nil
}

// newSetupCode erzeugt einen Code wie „K7PM-Q2XA“ ohne verwechselbare Zeichen (0/O, 1/I/L).
func newSetupCode() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	rand.Read(b)
	out := make([]byte, 0, 9)
	for i, x := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, alphabet[int(x)%len(alphabet)])
	}
	return string(out)
}

func bar(n, total int) string {
	return strings.Repeat("■", n) + strings.Repeat("·", total-n)
}

func memText() string {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "unbekannt viel"
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "MemTotal:" {
			kb, _ := strconv.ParseInt(f[1], 10, 64)
			return fmt.Sprintf("%.1f GB", float64(kb)/1e6)
		}
	}
	return "unbekannt viel"
}

func fakeDetect(ports []netdetect.Port) live.Output {
	o := live.Output{WAN: ports[0].Name, WANMAC: ports[0].MAC, WANLabel: ports[0].Label(), Method: "manual",
		Ports: map[string]string{}, Labels: map[string]string{}}
	for i, p := range ports {
		o.Ports[p.Name], o.Labels[p.Name] = p.MAC, p.Label()
		if i > 0 {
			o.LAN = append(o.LAN, p.Name)
			o.LANMACs = append(o.LANMACs, p.MAC)
		}
	}
	return o
}
