// SPDX-License-Identifier: AGPL-3.0-or-later

// Package apply applies generated files (PLAN.md 3.3, P-05):
//
//	generate → compare → check in staging (nft -c, dnsmasq --test) → back up the old version →
//	replace atomically → reload selectively → health check → on failure restore the old version.
//
// Network and firewall changes can also require confirmation: without
// `vrctl confirm`, a systemd timer rolls the change back when the deadline expires. The timer
// runs independently of this process, even if the web interface or SSH session drops.
package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
)

// GroupConfig is config.json itself. It is backed up and rolled back too.
const GroupConfig = "config"

// Runner runs programs. Replaced in tests.
type Runner interface {
	Run(env []string, name string, args ...string) ([]byte, error)
}

// Applier applies configurations.
type Applier struct {
	Root     string // filesystem root, "/" in production, a test folder in tests
	StateDir string // /var/lib/vanilla-router (relative to Root)
	Run      Runner
	Now      func() time.Time
	Log      io.Writer
	// HealthTimeout limits the health check. Default 15 s.
	HealthTimeout time.Duration
	// Sleep between two health check attempts (replaced in tests).
	Sleep func(time.Duration)
	// SkipHealth skips the health check (dry run of the test instance, nothing is loaded there).
	SkipHealth bool
}

// Options for Apply.
type Options struct {
	DryRun  bool          // only check and show, change nothing
	Confirm time.Duration // > 0: roll back after this time without confirmation
	Self    string        // path of vrctl for the rollback timer, default /usr/sbin/vrctl
}

// Change is a planned change.
type Change struct {
	Path   string
	Group  string
	Action string // create, update, remove
}

// Result describes what happened.
type Result struct {
	Changes      []Change
	Generation   string // ID of the backup, empty if nothing changed
	Reloaded     []string
	RebootNeeded bool // .link files changed: port names only take effect after a restart
	Pending      bool // waiting for confirmation
	Deadline     time.Time
}

func (a *Applier) p(rel string) string { return filepath.Join(a.Root, filepath.FromSlash(rel)) }

func (a *Applier) logf(format string, args ...any) {
	if a.Log != nil {
		fmt.Fprintf(a.Log, format+"\n", args...)
	}
}

func (a *Applier) stateDir() string {
	if a.StateDir == "" {
		return "/var/lib/vanilla-router"
	}
	return a.StateDir
}

// Plan compares the generated files with the system.
func (a *Applier) Plan(files []gen.File) ([]Change, error) {
	var ch []Change
	want := map[string]bool{}
	for _, f := range files {
		want[f.Path] = true
		old, err := os.ReadFile(a.p(f.Path))
		switch {
		case errors.Is(err, os.ErrNotExist):
			ch = append(ch, Change{f.Path, f.Group, "create"})
		case err != nil:
			return nil, err
		default:
			fi, err := os.Stat(a.p(f.Path))
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(old, f.Content) || uint32(fi.Mode().Perm()) != f.Mode {
				ch = append(ch, Change{f.Path, f.Group, "update"})
			}
		}
	}
	for _, m := range gen.Managed {
		ents, err := os.ReadDir(a.p(m.Dir))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			rel := path.Join(m.Dir, e.Name())
			if ok, _ := path.Match(m.Glob, e.Name()); ok && !e.IsDir() && !want[rel] {
				ch = append(ch, Change{rel, m.Group, "remove"})
			}
		}
	}
	sort.Slice(ch, func(i, j int) bool { return ch[i].Path < ch[j].Path })
	return ch, nil
}

// Apply generates, checks and applies the configuration. config.json is written as well.
func (a *Applier) Apply(c *config.Config, sec gen.Secrets, opt Options) (*Result, error) {
	if pend, _ := a.readPending(); pend != nil {
		return nil, fmt.Errorf("A change from %s is still waiting for confirmation (vrctl confirm or vrctl rollback)", pend.Created.Format("15:04:05"))
	}
	files, err := gen.Render(c, sec)
	if err != nil {
		return nil, err
	}
	cj, err := c.Marshal()
	if err != nil {
		return nil, err
	}
	files = append(files, gen.File{Path: config.Path, Mode: config.FileMode, Group: GroupConfig, Content: cj})

	changes, err := a.Plan(files)
	if err != nil {
		return nil, err
	}
	res := &Result{Changes: changes}
	if len(changes) == 0 {
		a.logf("No changes.")
		return res, nil
	}
	byPath := map[string]gen.File{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	if err := a.check(files, changes, c); err != nil {
		return nil, err
	}
	if opt.DryRun {
		return res, nil
	}

	old := a.current()
	genID, err := a.backup(changes)
	if err != nil {
		return nil, fmt.Errorf("backup of the old version: %w", err)
	}
	res.Generation = genID
	if err := a.install(changes, byPath); err != nil {
		a.logf("applying failed (%v), rolling back …", err)
		return nil, errors.Join(err, a.restore(genID))
	}
	groups := changedGroups(changes)
	res.RebootNeeded = groups[gen.GroupLink]
	sysErr := a.system(old, c, groups)
	res.Reloaded, err = a.reload(groups, c)
	err = errors.Join(err, sysErr)
	if err == nil && !a.SkipHealth {
		err = a.health(groups, c)
	}
	if err != nil {
		a.logf("error after applying: %v. Rolling back …", err)
		return nil, errors.Join(err, a.restore(genID))
	}
	if opt.Confirm > 0 && (groups[gen.GroupNetworkd] || groups[gen.GroupFirewall] || groups[gen.GroupPPP]) {
		if err := a.schedule(genID, opt); err != nil {
			a.logf("rollback timer not started (%v), rolling back immediately …", err)
			return nil, errors.Join(err, a.restore(genID))
		}
		res.Pending = true
		res.Deadline = a.now().Add(opt.Confirm)
	}
	a.prune(10)
	return res, nil
}

func (a *Applier) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func changedGroups(ch []Change) map[string]bool {
	g := map[string]bool{}
	for _, c := range ch {
		g[c.Group] = true
	}
	return g
}

// check validates changed files with the services' own tools before anything is replaced.
func (a *Applier) check(files []gen.File, changes []Change, c *config.Config) error {
	groups := changedGroups(changes)
	stage, err := os.MkdirTemp("", "vr-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, f := range files {
		if f.Group != gen.GroupFirewall && f.Group != gen.GroupDNSmasq {
			continue
		}
		dst := filepath.Join(stage, filepath.Base(f.Path))
		if err := os.WriteFile(dst, f.Content, 0o600); err != nil {
			return err
		}
		switch {
		case f.Group == gen.GroupFirewall && groups[gen.GroupFirewall]:
			if out, err := a.Run.Run(tzEnv(c), "nft", "-c", "-f", dst); err != nil {
				return fmt.Errorf("firewall rules rejected (nft -c): %v %s", err, strings.TrimSpace(string(out)))
			}
		case f.Group == gen.GroupDNSmasq && groups[gen.GroupDNSmasq]:
			if out, err := a.Run.Run(nil, "dnsmasq", "--test", "--conf-file="+dst); err != nil {
				return fmt.Errorf("dnsmasq configuration rejected: %v %s", err, strings.TrimSpace(string(out)))
			}
		}
	}
	return nil
}

func tzEnv(c *config.Config) []string { return []string{"TZ=" + c.System.Timezone} }

// manifest describes a backup.
type manifest struct {
	Created time.Time       `json:"created"`
	Entries []manifestEntry `json:"entries"`
}

type manifestEntry struct {
	Path    string `json:"path"`
	Group   string `json:"group"`
	Existed bool   `json:"existed"`
	Mode    uint32 `json:"mode,omitempty"`
}

func (a *Applier) genDir(id string) string { return a.p(path.Join(a.stateDir(), "generations", id)) }

// backup saves the old state of all affected files.
func (a *Applier) backup(changes []Change) (string, error) {
	id := a.now().UTC().Format("20060102T150405.000000000Z")
	dir := a.genDir(id)
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o700); err != nil {
		return "", err
	}
	m := manifest{Created: a.now()}
	for _, c := range changes {
		e := manifestEntry{Path: c.Path, Group: c.Group}
		data, err := os.ReadFile(a.p(c.Path))
		if err == nil {
			fi, _ := os.Stat(a.p(c.Path))
			e.Existed, e.Mode = true, uint32(fi.Mode().Perm())
			dst := filepath.Join(dir, "files", filepath.FromSlash(c.Path))
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return "", err
			}
			if err := os.WriteFile(dst, data, 0o600); err != nil {
				return "", err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		m.Entries = append(m.Entries, e)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := config.WriteFileAtomic(filepath.Join(dir, "manifest.json"), b, 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func (a *Applier) install(changes []Change, files map[string]gen.File) error {
	for _, c := range changes {
		switch c.Action {
		case "remove":
			if err := os.Remove(a.p(c.Path)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			a.logf("  removed  %s", c.Path)
		default:
			f := files[c.Path]
			if err := config.WriteFileAtomic(a.p(f.Path), f.Content, os.FileMode(f.Mode)); err != nil {
				return err
			}
			a.logf("  %-8s  %s", map[string]string{"create": "new", "update": "changed"}[c.Action], c.Path)
		}
	}
	return nil
}

// restore puts a backup back and reloads the affected services.
func (a *Applier) restore(id string) error {
	dir := a.genDir(id)
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("backup %s not readable: %w", id, err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	var errs []error
	groups := map[string]bool{}
	before := a.current()
	for _, e := range m.Entries {
		groups[e.Group] = true
		if !e.Existed {
			if err := os.Remove(a.p(e.Path)); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, "files", filepath.FromSlash(e.Path)))
		if err == nil {
			err = config.WriteFileAtomic(a.p(e.Path), data, os.FileMode(e.Mode))
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	// Reload with the restored configuration (time zone for the blocks).
	c := a.current()
	if c != nil {
		if err := a.system(before, c, groups); err != nil {
			errs = append(errs, err)
		}
	}
	if _, err := a.reload(groups, c); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("rollback incomplete: %w", err)
	}
	a.logf("Old version (%s) restored.", id)
	return nil
}

// reload reloads the services of the changed groups in a sensible order.
// c may be nil (rollback without a readable configuration), then the defaults apply.
func (a *Applier) reload(groups map[string]bool, c *config.Config) ([]string, error) {
	var done []string
	var errs []error
	run := func(what string, env []string, name string, args ...string) {
		if out, err := a.Run.Run(env, name, args...); err != nil {
			errs = append(errs, fmt.Errorf("%s: %v %s", what, err, strings.TrimSpace(string(out))))
			return
		}
		done = append(done, what)
	}
	pppoe := c != nil && c.WAN.Type == config.WANPPPoE
	ssh := c != nil && c.Services.SSH.Enabled
	var env []string
	if c != nil {
		env = tzEnv(c)
	}
	if groups[gen.GroupSysctl] {
		run("sysctl", nil, "sysctl", "-q", "-p", a.p("/etc/sysctl.d/90-vr.conf"))
	}
	if groups[gen.GroupFirewall] {
		run("firewall", env, "nft", "-f", a.p("/etc/vanilla-router/nft/vr.nft"))
	}
	if groups[gen.GroupResolved] {
		run("resolved", nil, "systemctl", "restart", "systemd-resolved.service")
	}
	if groups[gen.GroupNetworkd] {
		run("networkd", nil, "networkctl", "reload")
	}
	if groups[gen.GroupPPP] || groups[gen.GroupNetworkd] && pppoe {
		if pppoe {
			run("pppoe", nil, "systemctl", "enable", "--now", "vr-pppoe.service")
			run("pppoe", nil, "systemctl", "restart", "vr-pppoe.service")
		} else {
			run("pppoe", nil, "systemctl", "disable", "--now", "vr-pppoe.service")
		}
	}
	if groups[gen.GroupDNSmasq] {
		run("dnsmasq", nil, "systemctl", "restart", "vr-dnsmasq.service")
	}
	if groups[gen.GroupSSH] {
		if ssh {
			run("ssh", nil, "systemctl", "enable", "--now", "ssh.socket")
			run("ssh", nil, "systemctl", "try-reload-or-restart", "ssh.service")
		} else {
			run("ssh", nil, "systemctl", "disable", "--now", "ssh.socket", "ssh.service")
		}
	}
	return dedup(done), errors.Join(errs...)
}

// current reads the applied config.json, nil if it is missing or invalid.
func (a *Applier) current() *config.Config {
	c, err := config.Load(a.p(config.Path))
	if err != nil {
		return nil
	}
	return c
}

// system takes over settings that are not in generated files: host name and time zone of the
// system. If the router's name or address changes, the web interface restarts shortly afterwards
// (new certificate, new address). A changed time zone reloads the firewall, because the
// time-based blocks apply in local time. old may be nil (first setup).
func (a *Applier) system(old, c *config.Config, groups map[string]bool) error {
	if old == nil {
		return nil
	}
	var errs []error
	run := func(name string, args ...string) {
		if out, err := a.Run.Run(nil, name, args...); err != nil {
			errs = append(errs, fmt.Errorf("%s: %v %s", name, err, strings.TrimSpace(string(out))))
		}
	}
	if old.System.Hostname != c.System.Hostname {
		run("hostnamectl", "set-hostname", c.System.Hostname)
	}
	if old.System.Timezone != c.System.Timezone {
		run("timedatectl", "set-timezone", c.System.Timezone)
		groups[gen.GroupFirewall] = true
	}
	pppoe := func(x *config.Config) bool { return x.WAN.Type == config.WANPPPoE }
	if old.System.Hostname != c.System.Hostname || old.DNS.Domain != c.DNS.Domain || old.LAN.IPv4 != c.LAN.IPv4 ||
		pppoe(old) != pppoe(c) {
		// New certificate, new address or a different uplink interface (wan0/ppp0) for the statistics.
		// Delayed, so the response to the browser still goes out. try-restart: if the
		// web interface is not running, it stays off.
		run("systemd-run", "--on-active=3s", "--timer-property=AccuracySec=1s",
			"systemctl", "try-restart", "vr-webui.service")
	}
	return errors.Join(errs...)
}

func dedup(s []string) []string {
	var out []string
	for i, v := range s {
		if i == 0 || s[i-1] != v {
			out = append(out, v)
		}
	}
	return out
}

// health checks after applying whether the home network still works. The internet is deliberately
// not checked: a failed modem should not trigger a rollback.
func (a *Applier) health(groups map[string]bool, c *config.Config) error {
	timeout := a.HealthTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	sleep := a.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	var checks []func() error
	if groups[gen.GroupFirewall] {
		checks = append(checks, func() error {
			_, err := a.Run.Run(nil, "nft", "list", "table", "inet", "vr_filter")
			return err
		})
	}
	if groups[gen.GroupDNSmasq] || groups[gen.GroupNetworkd] {
		checks = append(checks, func() error {
			_, err := a.Run.Run(nil, "systemctl", "is-active", "--quiet", "vr-dnsmasq.service")
			if err != nil {
				return fmt.Errorf("DNS/DHCP service is not running")
			}
			return nil
		})
	}
	if groups[gen.GroupNetworkd] {
		checks = append(checks, func() error {
			out, err := a.Run.Run(nil, "ip", "-4", "-o", "addr", "show", "dev", gen.LANIf)
			if err != nil || !strings.Contains(string(out), c.LAN.IPv4) {
				return fmt.Errorf("home network address %s is missing on %s", c.LAN.IPv4, gen.LANIf)
			}
			return nil
		})
	}
	deadline := a.now().Add(timeout)
	for {
		var failed error
		for _, ch := range checks {
			if err := ch(); err != nil {
				failed = err
				break
			}
		}
		if failed == nil {
			return nil
		}
		if !a.now().Before(deadline) {
			return fmt.Errorf("health check: %w", failed)
		}
		sleep(time.Second)
	}
}

// pending is a change that has not been confirmed yet.
type pending struct {
	Generation string    `json:"generation"`
	Created    time.Time `json:"created"`
	Deadline   time.Time `json:"deadline"`
	Unit       string    `json:"unit"`
}

func (a *Applier) pendingPath() string { return a.p(path.Join(a.stateDir(), "pending.json")) }

func (a *Applier) readPending() (*pending, error) {
	b, err := os.ReadFile(a.pendingPath())
	if err != nil {
		return nil, err
	}
	var p pending
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Pending returns a waiting change or nil.
func (a *Applier) Pending() *pending {
	p, _ := a.readPending()
	return p
}

func (a *Applier) schedule(genID string, opt Options) error {
	self := opt.Self
	if self == "" {
		self = "/usr/sbin/vrctl"
	}
	unit := "vr-rollback-" + strings.NewReplacer(".", "-", ":", "-").Replace(genID)
	p := pending{Generation: genID, Created: a.now(), Deadline: a.now().Add(opt.Confirm), Unit: unit}
	b, _ := json.MarshalIndent(p, "", "  ")
	if err := config.WriteFileAtomic(a.pendingPath(), b, 0o600); err != nil {
		return err
	}
	secs := fmt.Sprintf("%ds", int(opt.Confirm.Seconds()))
	if out, err := a.Run.Run(nil, "systemd-run", "--unit="+unit, "--on-active="+secs, "--timer-property=AccuracySec=1s",
		self, "rollback", "--pending", genID); err != nil {
		os.Remove(a.pendingPath())
		return fmt.Errorf("%v %s", err, strings.TrimSpace(string(out)))
	}
	a.logf("Please confirm within %s (vrctl confirm), otherwise the change is rolled back.", secs)
	return nil
}

// Confirm confirms a waiting change.
func (a *Applier) Confirm() error {
	p, err := a.readPending()
	if err != nil {
		return fmt.Errorf("No change is waiting for confirmation")
	}
	a.Run.Run(nil, "systemctl", "stop", p.Unit+".timer")
	if err := os.Remove(a.pendingPath()); err != nil {
		return err
	}
	a.logf("Change confirmed.")
	return nil
}

// RollbackPending rolls back the waiting change if it belongs to genID (called by the
// timer). If it has already been confirmed, nothing happens.
func (a *Applier) RollbackPending(genID string) error {
	p, err := a.readPending()
	if err != nil || p.Generation != genID {
		a.logf("Nothing to roll back (already confirmed).")
		return nil
	}
	err = a.restore(genID)
	os.Remove(a.pendingPath())
	return err
}

// RollbackLast rolls back the most recently applied change (manually).
func (a *Applier) RollbackLast() error {
	if p, err := a.readPending(); err == nil {
		a.Run.Run(nil, "systemctl", "stop", p.Unit+".timer")
		err := a.restore(p.Generation)
		os.Remove(a.pendingPath())
		return err
	}
	gens, err := a.Generations()
	if err != nil || len(gens) == 0 {
		return fmt.Errorf("No backup available")
	}
	last := gens[len(gens)-1]
	if err := a.restore(last); err != nil {
		return err
	}
	return os.RemoveAll(a.genDir(last)) // do not restore the same backup twice
}

// Generations lists the backups, oldest first.
func (a *Applier) Generations() ([]string, error) {
	ents, err := os.ReadDir(a.p(path.Join(a.stateDir(), "generations")))
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range ents {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (a *Applier) prune(keep int) {
	ids, err := a.Generations()
	if err != nil || len(ids) <= keep {
		return
	}
	for _, id := range ids[:len(ids)-keep] {
		os.RemoveAll(a.genDir(id))
	}
}
