// SPDX-License-Identifier: AGPL-3.0-or-later

// vr-install is the installation assistant on the boot stick (PLAN.md 3.9.2). It starts
// automatically on tty1, is operated with arrow keys and Enter only, and asks only about physical
// things (P-11). Nothing is written to the disk until the explicit confirmation in S6.
//
//	vr-install                         assistant
//	vr-install --dry-run               assistant without writing (shows the plan at the end)
//	vr-install --unattended plan.json  no questions (F-INST-08)
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
	"vanilla-router/internal/i18n"

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
	dry := flag.Bool("dry-run", false, "ask everything, but write nothing")
	rootfs := flag.String("rootfs", "/usr/share/vanilla-router/rootfs.tar.zst", "image of the target system")
	unattended := flag.String("unattended", "", "plan as JSON, install without questions")
	virtual := flag.Bool("include-virtual", false, "allow virtual ports (test lab only)")
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
		p.Say("\nSorry, that did not work:\n  %v\n", err)
		var ie *installError
		if errors.As(err, &ie) {
			p.Say("The installation is incomplete. This computer will not start as a router yet.")
		} else {
			p.Say("Nothing was changed.")
		}
		p.WaitEnter("Press Enter to start over.")
	}
	restore()
	if err != nil {
		os.Exit(1)
	}
}

// errAborted: the user cancelled before wiping.
var errAborted = errors.New("cancelled")

// installError: an error after writing to the disk has begun.
type installError struct{ err error }

func (e *installError) Error() string { return e.err.Error() }
func (e *installError) Unwrap() error { return e.err }

// UnattendedPlan is the format for --unattended.
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
		return errors.New("disk and config are required")
	}
	if up.SetupCode == "" {
		up.SetupCode = newSetupCode()
	}
	pl := install.Plan{Disk: up.Disk, Rootfs: rootfs, Config: up.Config, Secrets: up.Secrets, SetupCode: up.SetupCode}
	if dry {
		fmt.Printf("Dry run: would wipe %s and install.\n", up.Disk)
		return up.Config.Validate()
	}
	if err := install.CheckRoot(); err != nil {
		return err
	}
	in := &install.Installer{Run: install.ExecRunner{}, Progress: func(n, t int, s string) { fmt.Printf("[%d/%d] %s\n", n, t, s) }}
	if err := in.Install(pl); err != nil {
		return err
	}
	fmt.Println("Setup code:", up.SetupCode)
	return nil
}

// assistant is the flow from PLAN.md 3.9.2 (as of 0.8): it only asks about physical things, namely
// which cable goes to the modem and which disk may be wiped (P-11). It is operated with arrow keys
// and Enter only. Everything else is set up afterwards in the web interface.
func assistant(p *tui.Prompt, rootfs string, dry bool, lo netdetect.Options, mounts []string) error {
	// Keyboard layout and time zone from the stick creator, if there is a stick.json. The assistant is in English,
	// the language will become a setting in the web interface.
	p.Clear()
	info, _ := stick.Find(mounts)

	p.Banner("vanilla-router "+version, "Welcome!")
	p.Say("This computer is about to become a router. You only need to do two things:")
	p.Say("  1. plug in the cable from your modem,")
	p.Say("  2. confirm which disk to install to.")
	p.Say("You can set up everything else in your browser afterwards.")
	if !dry {
		if err := install.CheckRoot(); err != nil {
			return err
		}
	}

	// S2 hardware: only visible when something is missing (F-INST-18)
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
		cands = []disk.Disk{{Name: "dry run", Path: "/dev/dryrun", SizeBytes: 128e9, Model: "Fake disk (dry run)"}}
	}
	if dry && os.Geteuid() != 0 && len(ports) < 2 {
		// Dry run on a development machine: fake ports so the whole flow can be seen
		ports = nil
		for i := 1; i <= 3; i++ {
			ports = append(ports, netdetect.Port{Name: fmt.Sprintf("probe%d", i), MAC: fmt.Sprintf("02:00:00:00:00:%02x", i), Number: i})
		}
	}
	if len(ports) < 2 {
		return fmt.Errorf("This computer has %d network port(s). A router needs at least two, "+
			"for example with a USB network adapter. Turn the computer off, connect an adapter "+
			"and then start from the stick again", len(ports))
	}
	if len(cands) == 0 {
		return fmt.Errorf("There is no disk with at least 16 GB (the stick itself does not count)")
	}

	// S3/S4 internet port: detect it, nothing to type
	p.Heading("Internet port")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var det live.Output
	if dry && os.Geteuid() != 0 {
		p.Say("(Dry run without root: detection skipped, port 1 counts as the internet port.)")
		det = fakeDetect(ports)
	} else {
		det, err = live.Run(ctx, p.Out, ports, live.Options{ListOpts: lo, Menu: p.MenuAsync, Spinner: p.StartSpinner})
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
	// F-INST-15: record the result, the web interface then asks for credentials (F-UI-08)
	switch {
	case det.PPPoE != nil:
		cfg.WAN.Detected = &config.Detected{Method: config.WANPPPoE, VLAN: det.PPPoE.VLAN}
		p.Say("You will enter the credentials from your provider's letter in your browser after the installation.")
	case det.DHCP != "":
		cfg.WAN.Detected = &config.Detected{Method: config.WANDHCP}
		if in.WANNet.IsValid() && in.WANNet.Addr().IsPrivate() {
			p.Say("There is another provider router in front of this router. That works fine.")
		}
	default:
		cfg.WAN.Detected = &config.Detected{Method: config.DetectedNone}
		p.Say("You will choose how your connection works in your browser after the installation.")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	// S6 disk: always confirm deliberately, even if there is only one
	p.Heading("Hard disk")
	target := cands[0]
	if len(cands) > 1 {
		p.Say("Which disk should vanilla-router go on?")
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
	p.Say("WARNING: All data on this disk will be erased:\n\n    %s\n", target.Label())
	i, err := p.Menu([]string{"No, cancel", "Yes, erase everything and install"}, 0)
	if err != nil {
		return err
	}
	if i == 0 {
		p.Say("\nCancelled. Nothing was changed.")
		p.WaitEnter("Press Enter to turn the computer off.")
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
		p.Say("Dry run: nothing was written. This plan would be installed:\n%s", b)
		return nil
	}
	start := time.Now()
	stopSpin := func() {}
	inst := &install.Installer{Run: install.ExecRunner{}, Progress: func(n, t int, s string) {
		stopSpin()
		p.Say("  [%s] %s …", bar(n, t), i18n.T(s))
		stopSpin = p.StartSpinner("Please wait")
	}}
	err = inst.Install(plan)
	stopSpin()
	if err != nil {
		return &installError{err}
	}
	p.Say("Done after %s.", time.Since(start).Round(time.Second))

	// S9 summary
	_, router, _ := cfg.LANPrefix()
	p.Heading("All done")
	p.Say("Your router is installed. Here is what comes next:\n")
	p.Say("  1. Connect a laptop or phone to your home network,")
	p.Say("     that is, to one of the other ports or your Wi-Fi access point.")
	p.Say("  2. Open in your browser:  https://%s.%s", cfg.System.Hostname, cfg.DNS.Domain)
	p.Say("     (or https://%s)", router)
	p.Say("  3. Setup code for the first login:  %s\n", code)
	p.Say("After restarting, the router also shows the address and code on this screen.")

	// S10 restart
	// From here on the stick, and with it the whole running system, may be gone. So keep this program
	// in memory and restart without calling any other program. The target is already unmounted.
	syscall.Mlockall(syscall.MCL_CURRENT | syscall.MCL_FUTURE)
	if err := p.WaitEnter("\nNow unplug the stick and press Enter."); err != nil {
		return err
	}
	syscall.Sync()
	if err := syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART); err != nil {
		return &installError{fmt.Errorf("restart: %w", err)}
	}
	return nil
}

// newSetupCode creates a code like "K7PM-Q2XA" without easily confused characters (0/O, 1/I/L).
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
		return "an unknown amount"
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "MemTotal:" {
			kb, _ := strconv.ParseInt(f[1], 10, 64)
			return fmt.Sprintf("%.1f GB", float64(kb)/1e6)
		}
	}
	return "an unknown amount"
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
