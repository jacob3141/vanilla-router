// SPDX-License-Identifier: AGPL-3.0-or-later

// vr-install ist der Installations-Assistent auf dem Boot-Stick (PLAN.md 3.9.2, S1–S10).
// Er startet automatisch auf tty1. Bis zur ausdrücklichen Bestätigung in S6 wird nichts auf die
// Festplatte geschrieben.
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
	"vanilla-router/internal/tui"
	"vanilla-router/internal/wandetect/live"
)

const totalSteps = 10

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
	p := tui.New(os.Stdin, os.Stdout, tui.StdinFd())
	err := assistant(p, *rootfs, *dry, netdetect.Options{IncludeVirtual: *virtual})
	if errors.Is(err, tui.ErrEOF) {
		fmt.Println("\nAbgebrochen.")
		os.Exit(1)
	}
	if err != nil {
		fmt.Printf("\nDas hat leider nicht geklappt:\n  %v\n\nSie können den Rechner neu starten und es noch einmal versuchen.\n", err)
		os.Exit(1)
	}
}

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

func assistant(p *tui.Prompt, rootfs string, dry bool, lo netdetect.Options) error {
	p.Say("\nWillkommen bei vanilla-router %s!\n", version)
	p.Say("Dieser Assistent macht aus diesem Rechner einen Router. Er fragt nur, was er nicht selbst")
	p.Say("herausfinden kann. Bis Schritt 6 wird nichts auf die Festplatte geschrieben.")
	if !dry {
		if err := install.CheckRoot(); err != nil {
			return err
		}
	}

	// S1 Sprache
	p.Heading(1, totalSteps, "Sprache")
	p.Say("Sprache: Deutsch, Tastatur: deutsch. (Weitere Sprachen folgen, F-UI-05.)")

	// S2 Hardware
	p.Heading(2, totalSteps, "Hardware prüfen")
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
	p.Say("Gefunden: %d Netzwerkanschlüsse, %d passende Datenträger, %s Arbeitsspeicher.", len(ports), len(cands), memText())
	if len(ports) < 2 {
		return fmt.Errorf("Ein Router braucht mindestens zwei Netzwerkanschlüsse, gefunden: %d. Ein USB-Netzwerkadapter hilft", len(ports))
	}
	if len(cands) == 0 {
		return fmt.Errorf("Kein Datenträger mit mindestens 16 GB gefunden (der Stick selbst zählt nicht)")
	}
	p.Say("Passt.")

	// S3/S4 Internet-Anschluss
	p.Heading(3, totalSteps, "Internet-Anschluss erkennen")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var det live.Output
	if dry && os.Geteuid() != 0 {
		p.Say("(Probelauf ohne Root-Rechte: Erkennung übersprungen, Anschluss 1 gilt als Internet-Anschluss.)")
		det = fakeDetect(ports)
	} else {
		det, err = live.Run(ctx, p.Out, ports, live.Options{ListOpts: lo, Input: p.Lines})
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
	secrets := map[string]string{}

	p.Heading(4, totalSteps, "Zugang zum Internet")
	switch {
	case det.PPPoE != nil:
		if err := askPPPoE(p, cfg, secrets, det.PPPoE.VLAN); err != nil {
			return err
		}
	case det.DHCP != "":
		p.Say("Keine Zugangsdaten nötig, das Modem vergibt die Adresse automatisch.")
		if in.WANNet.IsValid() && in.WANNet.Addr().IsPrivate() {
			p.Say("Hinweis: Vor diesem Router steht noch ein Router des Anbieters (Netz %s).", det.DHCPNet)
			p.Say("Das funktioniert. Für Portweiterleitungen muss dort zusätzlich weitergeleitet werden.")
		}
	default:
		i, err := p.Choose("Wie kommt dieser Anschluss ins Internet?", []string{
			"Automatisch (DHCP), das Modem war wohl noch nicht bereit",
			"Mit Zugangsdaten (PPPoE, z. B. DSL der Telekom)",
		}, 0)
		if err != nil {
			return err
		}
		if i == 1 {
			vlan := 0
			if ok, err := p.YesNo("Telekom-Anschluss (VLAN 7)?", true); err != nil {
				return err
			} else if ok {
				vlan = 7
			}
			if err := askPPPoE(p, cfg, secrets, vlan); err != nil {
				return err
			}
		}
	}

	// S5 Heimnetz
	p.Heading(5, totalSteps, "Heimnetz")
	p.Say("Alle anderen Anschlüsse (%d) sind Ihr Heimnetz. Dort können Sie einen Switch oder", len(det.LAN))
	p.Say("WLAN-Access-Point anschließen. Adressbereich: %s, Geräte bekommen ihre Adresse automatisch.", cfg.LAN.IPv4)

	// S6 Datenträger
	p.Heading(6, totalSteps, "Datenträger wählen")
	var labels []string
	for _, d := range cands {
		labels = append(labels, d.Label()+"  "+d.Path)
	}
	di, err := p.Choose("Auf welchen Datenträger soll vanilla-router?", labels, 0)
	if err != nil {
		return err
	}
	target := cands[di]
	ok, err := p.Confirm(fmt.Sprintf("\nACHTUNG: Alle Daten auf %s werden gelöscht.", target.Label()), "LÖSCHEN")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("Installation auf Wunsch abgebrochen, nichts wurde verändert")
	}

	// S7 Name und Zeitzone
	p.Heading(7, totalSteps, "Name und Zeitzone")
	cfg.System.Hostname, err = p.Ask("Name des Routers im Netz", cfg.System.Hostname, func(s string) error {
		if !config.ValidHostname(s) {
			return errors.New("Bitte nur Kleinbuchstaben, Ziffern und Bindestrich")
		}
		return nil
	})
	if err != nil {
		return err
	}
	p.Say("So heißt Ihr Router im Netz: %s.%s", cfg.System.Hostname, cfg.DNS.Domain)
	cfg.System.Timezone, err = p.Ask("Zeitzone", cfg.System.Timezone, func(s string) error {
		if _, err := os.Stat("/usr/share/zoneinfo/" + s); err != nil && !dry {
			return errors.New("Unbekannte Zeitzone, z. B. Europe/Berlin oder Europe/Vienna")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	// S8 Installation
	p.Heading(8, totalSteps, "Installation")
	code := newSetupCode()
	plan := install.Plan{Disk: target.Path, Rootfs: rootfs, Config: cfg, Secrets: secrets, SetupCode: code}
	if dry {
		b, _ := json.MarshalIndent(UnattendedPlan{Disk: plan.Disk, Config: cfg, SetupCode: code}, "", "  ")
		p.Say("Probelauf: Es wurde nichts geschrieben. Dieser Plan würde installiert (Geheimnisse ausgelassen):\n%s", b)
		return nil
	}
	start := time.Now()
	inst := &install.Installer{Run: install.ExecRunner{}, Progress: func(n, t int, s string) {
		p.Say("  [%s] %s …", bar(n, t), s)
	}}
	if err := inst.Install(plan); err != nil {
		return err
	}
	p.Say("Fertig nach %s.", time.Since(start).Round(time.Second))

	// S9 Zusammenfassung
	p.Heading(9, totalSteps, "Zusammenfassung")
	p.Say("Ihr Router ist eingerichtet.\n")
	p.Say("  Öffnen Sie auf einem Gerät im Heimnetz:  https://%s.%s", cfg.System.Hostname, cfg.DNS.Domain)
	p.Say("  Einrichtungscode für das erste Anmelden: %s", code)
	p.Say("\nSchreiben Sie den Code auf. Er gilt nur für die erste Anmeldung.")

	// S10 Neustart
	p.Heading(10, totalSteps, "Neustart")
	p.Ask("Ziehen Sie jetzt den Stick ab und drücken Sie Enter", "", func(string) error { return nil })
	if out, err := (install.ExecRunner{}).Run(nil, nil, "systemctl", "reboot"); err != nil {
		return fmt.Errorf("Neustart: %v %s", err, out)
	}
	return nil
}

func askPPPoE(p *tui.Prompt, cfg *config.Config, secrets map[string]string, vlan int) error {
	telekom := vlan == 7
	if vlan == 7 {
		p.Say("Ihr Anschluss braucht Zugangsdaten. Das sieht nach einem Telekom-Anschluss aus (VLAN 7).")
	} else {
		p.Say("Ihr Anschluss braucht Zugangsdaten. Sie stehen im Brief Ihres Anbieters.")
	}
	var user string
	if telekom {
		p.Say("Bitte die Angaben aus dem Telekom-Brief „Ihre persönlichen Zugangsdaten“:")
		for {
			ak, err := p.Ask("  Anschlusskennung (12 Ziffern)", "", nil)
			if err != nil {
				return err
			}
			zn, err := p.Ask("  Zugangsnummer bzw. T-Online-Nummer (12 Ziffern)", "", nil)
			if err != nil {
				return err
			}
			mb, err := p.Ask("  Mitbenutzernummer", "0001", nil)
			if err != nil {
				return err
			}
			if user, err = config.TelekomUser(ak, zn, mb); err != nil {
				p.Say("  %v", err)
				continue
			}
			break
		}
	} else {
		var err error
		if user, err = p.Ask("Benutzername", "", nil); err != nil {
			return err
		}
	}
	for {
		pw, err := p.Password("Passwort bzw. persönliches Kennwort (wird nicht angezeigt)")
		if err != nil {
			return err
		}
		if pw == "" || strings.ContainsAny(pw, `"\`) {
			p.Say("  Das Passwort darf nicht leer sein und kein \" oder \\ enthalten.")
			continue
		}
		secrets["pppoe"] = pw
		break
	}
	cfg.WAN.Type = config.WANPPPoE
	cfg.WAN.PPPoE = &config.PPPoE{User: user, PasswordRef: "pppoe", VLAN: vlan}
	return cfg.Validate()
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
