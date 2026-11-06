// SPDX-License-Identifier: AGPL-3.0-or-later

// vrctl manages the configuration of vanilla-router (M2, PLAN.md 3.3).
//
//	vrctl validate [file]             check the configuration
//	vrctl render --out folder         generate all files without changing anything
//	vrctl apply [--dry-run] [--confirm 60s]
//	vrctl confirm                     confirm a waiting change
//	vrctl rollback [--pending id]     roll back the last change
//	vrctl status                      show the waiting change and backups
//	vrctl default --wan-mac … --lan-mac …[,…] [--wan-net 192.0.2.0/24] [--write]
//	vrctl secret set <name>           read a secret from stdin (e.g. PPPoE password)
//	vrctl firewall-load               load the firewall with the configured time zone (for the unit)
//	vrctl setup [--dry-run]           adjust Ubuntu Server once (D-12)
//	vrctl snapshot pre-update|list    snapshots (M5), the apt hook calls pre-update
//	vrctl health [--mark-good] [--timeout 10m] [--reboot-on-fail]
//	vrctl complete                    fetch the rest of the system once online (D-17)
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
	"vanilla-router/internal/snapshot"
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
	case "snapshot":
		err = cmdSnapshot(args)
	case "health":
		err = cmdHealth(args)
	case "complete":
		err = applier().Complete()
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "vrctl: unknown command %q\n", cmd)
		usage()
	}
	if err != nil {
		var es config.Errors
		if errors.As(err, &es) {
			fmt.Fprintln(os.Stderr, "The configuration has errors:")
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
	fmt.Fprint(os.Stderr, `vrctl validate | render | apply | confirm | rollback | status | default | secret | firewall-load | setup | snapshot | health | complete
Help for a command: vrctl <command> -h
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
	fmt.Println("Configuration OK.")
	return nil
}

func cmdRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	cfg := fs.String("config", config.Path, "configuration")
	out := fs.String("out", "", "target folder (required)")
	sec := fs.String("secrets", secretsDir, "folder with secrets")
	fs.Parse(args)
	if *out == "" {
		return errors.New("--out is missing")
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
	cfg := fs.String("config", config.Path, "configuration")
	dry := fs.Bool("dry-run", false, "only check and show")
	confirm := fs.Duration("confirm", 0, "roll back after this time without confirmation (e.g. 60s)")
	fs.Parse(args)
	c, err := config.Load(*cfg)
	if err != nil {
		return err
	}
	if !*dry && os.Geteuid() != 0 {
		return errors.New("apply needs root (dry run: --dry-run)")
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
		fmt.Printf("%d changes (dry run, nothing changed)\n", len(res.Changes))
		return nil
	}
	if len(res.Changes) > 0 {
		fmt.Printf("%d files changed, reloaded: %s\n", len(res.Changes), strings.Join(res.Reloaded, ", "))
	}
	if res.RebootNeeded {
		fmt.Println("Note: the port assignment (wan0, lanp*) only takes effect after a restart.")
	}
	if res.Pending {
		fmt.Printf("Please confirm by %s: vrctl confirm\n", res.Deadline.Local().Format("15:04:05"))
	}
	return nil
}

func cmdRollback(args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ExitOnError)
	pend := fs.String("pending", "", "roll back only this waiting change (called by the timer)")
	fs.Parse(args)
	if *pend != "" {
		return applier().RollbackPending(*pend)
	}
	return applier().RollbackLast()
}

func cmdStatus() error {
	a := applier()
	if p := a.Pending(); p != nil {
		fmt.Printf("Waiting for confirmation until %s (vrctl confirm)\n", p.Deadline.Local().Format("15:04:05"))
	} else {
		fmt.Println("No change is waiting for confirmation.")
	}
	gens, _ := a.Generations()
	fmt.Printf("%d backups", len(gens))
	if len(gens) > 0 {
		fmt.Printf(", last: %s", gens[len(gens)-1])
	}
	fmt.Println()
	return nil
}

func cmdDefault(args []string) error {
	fs := flag.NewFlagSet("default", flag.ExitOnError)
	wanMAC := fs.String("wan-mac", "", "MAC of the internet port")
	lanMACs := fs.String("lan-mac", "", "MACs of the home network ports, comma-separated")
	wanNet := fs.String("wan-net", "", "network on the internet port (to avoid), e.g. 192.168.0.0/24")
	write := fs.Bool("write", false, "to "+config.Path+" instead of stdout")
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
			return fmt.Errorf("%s already exists, not overwriting", config.Path)
		}
		return c.Save(config.Path)
	}
	b, _ := c.Marshal()
	os.Stdout.Write(b)
	return nil
}

func cmdSecret(args []string) error {
	if len(args) != 2 || args[0] != "set" {
		return errors.New("Usage: vrctl secret set <name> (value on stdin)")
	}
	r := bufio.NewReader(os.Stdin)
	v, err := r.ReadString('\n')
	if err != nil && v == "" {
		return errors.New("no value on stdin")
	}
	return apply.DirSecrets{Dir: secretsDir}.Set(args[1], strings.TrimRight(v, "\r\n"))
}

// cmdFirewallLoad loads the firewall at boot and daily (time-based blocks and daylight saving time).
// Without a valid configuration the existing file is loaded anyway: old rules are better than none.
func cmdFirewallLoad() error {
	tz := "Europe/Berlin"
	if c, err := config.Load(config.Path); err == nil {
		tz = c.System.Timezone
	} else {
		fmt.Fprintln(os.Stderr, "Warning: configuration not readable, loading the existing rules:", err)
	}
	out, err := apply.ExecRunner{}.Run([]string{"TZ=" + tz}, "nft", "-f", "/etc/vanilla-router/nft/vr.nft")
	if err != nil {
		return fmt.Errorf("nft: %v %s", err, out)
	}
	return nil
}

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	dry := fs.Bool("dry-run", false, "only show")
	fs.Parse(args)
	if !*dry && os.Geteuid() != 0 {
		return errors.New("setup needs root")
	}
	steps, err := applier().Setup(*dry, "")
	for _, s := range steps {
		fmt.Println("•", s)
	}
	return err
}

func snapshots() *snapshot.Manager {
	return &snapshot.Manager{Run: apply.ExecRunner{}, Root: "/"}
}

func cmdSnapshot(args []string) error {
	if len(args) != 1 {
		return errors.New("Usage: vrctl snapshot pre-update | list")
	}
	switch args[0] {
	case "pre-update":
		// Snapshots turned off or no btrfs (e.g. conversion mode): silently do nothing.
		if c, err := config.Load(config.Path); err != nil || !c.Updates.Snapshots {
			return nil
		}
		if _, err := os.Stat("/.snapshots"); err != nil {
			return nil
		}
		info, err := snapshots().PreUpdate("before apt")
		if err == nil && info != nil {
			fmt.Println("vanilla-router: Snapshot", info.ID, "created")
		}
		return err
	case "list":
		list, err := snapshots().List()
		if err != nil {
			return err
		}
		for _, i := range list {
			fmt.Printf("%s  %-10s %s\n", i.Created.Local().Format("02.01.2006 15:04"), i.Kind, i.ID)
		}
		return nil
	}
	return fmt.Errorf("unknown: %s", args[0])
}

// cmdHealth runs after every start (vr-health.service). A failure only leads to a restart if this start
// was an attempt after an update: GRUB then picks the state before the update. Without an update
// there is no restart, otherwise a loop would occur.
func cmdHealth(args []string) error {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	mark := fs.Bool("mark-good", false, "confirm the start on success (GRUB)")
	timeout := fs.Duration("timeout", 0, "how long to wait for health")
	reboot := fs.Bool("reboot-on-fail", false, "restart on failure after an update (rollback)")
	fs.Parse(args)
	c, err := config.Load(config.Path)
	if err != nil {
		return err
	}
	err = apply.WaitHealthy(apply.ExecRunner{}, c, *timeout, nil)
	sm := snapshots()
	if err != nil {
		if *reboot && sm.Pending() {
			fmt.Println("Router not healthy after the update:", err, "– restarting into the state before the update.")
			apply.ExecRunner{}.Run(nil, "systemctl", "reboot")
		}
		return err
	}
	fmt.Println("Router is healthy.")
	if *mark {
		if _, err := os.Stat("/boot/efi/vr"); err != nil && !sm.Pending() {
			return nil // no update with a snapshot has ever run
		}
		rb, err := sm.MarkGood()
		if err != nil {
			return err
		}
		if rb {
			fmt.Println("The last update was rolled back. The old state becomes permanent at the next restart.")
		}
	}
	return nil
}
