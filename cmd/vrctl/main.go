// SPDX-License-Identifier: AGPL-3.0-or-later

// vrctl verwaltet die Konfiguration von vanilla-router (M2, PLAN.md 3.3).
//
//	vrctl validate [datei]            Konfiguration prüfen
//	vrctl render --out ordner         alle Dateien erzeugen, ohne etwas zu ändern
//	vrctl apply [--dry-run] [--confirm 60s]
//	vrctl confirm                     wartende Änderung bestätigen
//	vrctl rollback [--pending id]     letzte Änderung zurücknehmen
//	vrctl status                      wartende Änderung und Sicherungen anzeigen
//	vrctl default --wan-mac … --lan-mac …[,…] [--wan-net 192.0.2.0/24] [--write]
//	vrctl secret set <name>           Geheimnis von stdin lesen (z. B. PPPoE-Passwort)
//	vrctl firewall-load               Firewall mit der Zeitzone der Konfiguration laden (für die Unit)
//	vrctl setup [--dry-run]           Ubuntu Server einmalig anpassen (D-12)
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"vanilla-router/internal/apply"
	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
)

const secretsDir = "/etc/vanilla-router/secrets"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "validate":
		err = cmdValidate(args)
	case "render":
		err = cmdRender(args)
	case "apply":
		err = cmdApply(args)
	case "confirm":
		err = applier().Confirm()
	case "rollback":
		err = cmdRollback(args)
	case "status":
		err = cmdStatus()
	case "default":
		err = cmdDefault(args)
	case "secret":
		err = cmdSecret(args)
	case "firewall-load":
		err = cmdFirewallLoad()
	case "setup":
		err = cmdSetup(args)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "vrctl: unbekannter Befehl %q\n", cmd)
		usage()
	}
	if err != nil {
		var es config.Errors
		if errors.As(err, &es) {
			fmt.Fprintln(os.Stderr, "Die Konfiguration enthält Fehler:")
			for _, e := range es {
				fmt.Fprintf(os.Stderr, "  %s: %s\n", e.Field, e.Text)
			}
		} else {
			fmt.Fprintln(os.Stderr, "vrctl:", err)
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `vrctl validate | render | apply | confirm | rollback | status | default | secret | firewall-load | setup
Hilfe zu einem Befehl: vrctl <befehl> -h
`)
	os.Exit(2)
}

func applier() *apply.Applier {
	return &apply.Applier{Root: "/", Run: apply.ExecRunner{}, Log: os.Stdout}
}

func cmdValidate(args []string) error {
	path := config.Path
	if len(args) > 0 {
		path = args[0]
	}
	if _, err := config.Load(path); err != nil {
		return err
	}
	fmt.Println("Konfiguration in Ordnung.")
	return nil
}

func cmdRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	cfg := fs.String("config", config.Path, "Konfiguration")
	out := fs.String("out", "", "Zielordner (Pflicht)")
	sec := fs.String("secrets", secretsDir, "Ordner mit Geheimnissen")
	fs.Parse(args)
	if *out == "" {
		return errors.New("--out fehlt")
	}
	c, err := config.Load(*cfg)
	if err != nil {
		return err
	}
	files, err := gen.Render(c, apply.DirSecrets{Dir: *sec})
	if err != nil {
		return err
	}
	for _, f := range files {
		dst := filepath.Join(*out, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, f.Content, os.FileMode(f.Mode)); err != nil {
			return err
		}
		fmt.Printf("%-10s %s\n", f.Group, f.Path)
	}
	return nil
}

func cmdApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	cfg := fs.String("config", config.Path, "Konfiguration")
	dry := fs.Bool("dry-run", false, "nur prüfen und anzeigen")
	confirm := fs.Duration("confirm", 0, "ohne Bestätigung nach dieser Zeit zurücknehmen (z. B. 60s)")
	fs.Parse(args)
	c, err := config.Load(*cfg)
	if err != nil {
		return err
	}
	if !*dry && os.Geteuid() != 0 {
		return errors.New("apply braucht Root-Rechte (Probelauf: --dry-run)")
	}
	a := applier()
	self, _ := os.Executable()
	res, err := a.Apply(c, apply.DirSecrets{Dir: secretsDir}, apply.Options{DryRun: *dry, Confirm: *confirm, Self: self})
	if err != nil {
		return err
	}
	if *dry {
		for _, ch := range res.Changes {
			fmt.Printf("%-7s %-9s %s\n", ch.Action, ch.Group, ch.Path)
		}
		fmt.Printf("%d Änderungen (Probelauf, nichts geändert)\n", len(res.Changes))
		return nil
	}
	if len(res.Changes) > 0 {
		fmt.Printf("%d Dateien geändert, neu geladen: %s\n", len(res.Changes), strings.Join(res.Reloaded, ", "))
	}
	if res.RebootNeeded {
		fmt.Println("Hinweis: Die Zuordnung der Anschlüsse (wan0, lanp*) gilt erst nach einem Neustart.")
	}
	if res.Pending {
		fmt.Printf("Bitte bis %s bestätigen: vrctl confirm\n", res.Deadline.Local().Format("15:04:05"))
	}
	return nil
}

func cmdRollback(args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ExitOnError)
	pend := fs.String("pending", "", "nur diese wartende Änderung zurücknehmen (Aufruf durch den Timer)")
	fs.Parse(args)
	if *pend != "" {
		return applier().RollbackPending(*pend)
	}
	return applier().RollbackLast()
}

func cmdStatus() error {
	a := applier()
	if p := a.Pending(); p != nil {
		fmt.Printf("Wartet auf Bestätigung bis %s (vrctl confirm)\n", p.Deadline.Local().Format("15:04:05"))
	} else {
		fmt.Println("Keine Änderung wartet auf Bestätigung.")
	}
	gens, _ := a.Generations()
	fmt.Printf("%d Sicherungen", len(gens))
	if len(gens) > 0 {
		fmt.Printf(", letzte: %s", gens[len(gens)-1])
	}
	fmt.Println()
	return nil
}

func cmdDefault(args []string) error {
	fs := flag.NewFlagSet("default", flag.ExitOnError)
	wanMAC := fs.String("wan-mac", "", "MAC des Internet-Anschlusses")
	lanMACs := fs.String("lan-mac", "", "MACs der Heimnetz-Anschlüsse, Komma-getrennt")
	wanNet := fs.String("wan-net", "", "Netz am Internet-Anschluss (vermeiden), z. B. 192.168.0.0/24")
	write := fs.Bool("write", false, "nach "+config.Path+" schreiben statt auf stdout")
	fs.Parse(args)
	in := config.DefaultInput{WANMAC: *wanMAC}
	for _, m := range strings.Split(*lanMACs, ",") {
		if m = strings.TrimSpace(m); m != "" {
			in.LANMACs = append(in.LANMACs, m)
		}
	}
	if *wanNet != "" {
		p, err := netip.ParsePrefix(*wanNet)
		if err != nil {
			return fmt.Errorf("--wan-net: %w", err)
		}
		in.WANNet = p
	}
	c, err := config.Default(in)
	if err != nil {
		return err
	}
	if *write {
		if _, err := os.Stat(config.Path); err == nil {
			return fmt.Errorf("%s existiert schon, wird nicht überschrieben", config.Path)
		}
		return c.Save(config.Path)
	}
	b, _ := c.Marshal()
	os.Stdout.Write(b)
	return nil
}

func cmdSecret(args []string) error {
	if len(args) != 2 || args[0] != "set" {
		return errors.New("Aufruf: vrctl secret set <name> (Wert auf stdin)")
	}
	r := bufio.NewReader(os.Stdin)
	v, err := r.ReadString('\n')
	if err != nil && v == "" {
		return errors.New("kein Wert auf stdin")
	}
	return apply.DirSecrets{Dir: secretsDir}.Set(args[1], strings.TrimRight(v, "\r\n"))
}

// cmdFirewallLoad lädt die Firewall beim Booten und täglich neu (Zeitsperren und Sommerzeit).
// Ohne gültige Konfiguration wird trotzdem die vorhandene Datei geladen: lieber alte Regeln als keine.
func cmdFirewallLoad() error {
	tz := "Europe/Berlin"
	if c, err := config.Load(config.Path); err == nil {
		tz = c.System.Timezone
	} else {
		fmt.Fprintln(os.Stderr, "Warnung: Konfiguration nicht lesbar, lade vorhandene Regeln:", err)
	}
	out, err := apply.ExecRunner{}.Run([]string{"TZ=" + tz}, "nft", "-f", "/etc/vanilla-router/nft/vr.nft")
	if err != nil {
		return fmt.Errorf("nft: %v %s", err, out)
	}
	return nil
}

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	dry := fs.Bool("dry-run", false, "nur anzeigen")
	fs.Parse(args)
	if !*dry && os.Geteuid() != 0 {
		return errors.New("setup braucht Root-Rechte")
	}
	steps, err := applier().Setup(*dry)
	for _, s := range steps {
		fmt.Println("•", s)
	}
	return err
}
