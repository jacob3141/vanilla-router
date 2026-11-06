// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/netip"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	AuditScript   = "/usr/lib/vanilla-router/audit-collect.sh" // extension, see api.AuditScript
	AuditLastFile = "/var/lib/vanilla-router/audit-last.json"
	updateUnit    = "vr-update"
)

var hostRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,62})(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,62}))*\.?$`)

// validTarget only allows IP addresses and host names, never anything that looks like an option.
func validTarget(s string) (string, error) {
	s = strings.TrimSpace(s)
	if a, err := netip.ParseAddr(s); err == nil {
		return a.String(), nil
	}
	if len(s) > 253 || !hostRe.MatchString(s) {
		return "", Userf("\"%s\" is neither an IP address nor a valid host name.", s)
	}
	return s, nil
}

type Update struct {
	Package  string `json:"package"`
	From     string `json:"from"`
	To       string `json:"to"`
	Security bool   `json:"security"`
}

type AuditCheck struct {
	Level string `json:"level"`
	ID    string `json:"id"`
	Text  string `json:"text"`
}

type AuditResult struct {
	Time   time.Time    `json:"time"`
	Checks []AuditCheck `json:"checks"`
}

func (s *Server) registerSystem() {
	s.handle("system.updates.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		out, _ := run(ctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", "upgrade", "--with-new-pkgs")
		var ups []Update
		// Inst package [old] (new source [arch])
		re := regexp.MustCompile(`^Inst (\S+) (?:\[(\S+)\] )?\((\S+) ([^)]*)\)`)
		for _, line := range strings.Split(out, "\n") {
			if m := re.FindStringSubmatch(line); m != nil {
				ups = append(ups, Update{Package: m[1], From: m[2], To: m[3], Security: strings.Contains(m[4], "-security")})
			}
		}
		_, rerr := os.Stat("/run/reboot-required")
		pkgs, _ := os.ReadFile("/run/reboot-required.pkgs")
		var listsAge *time.Time
		if entries, err := os.ReadDir("/var/lib/apt/lists"); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), "InRelease") {
					if info, err := e.Info(); err == nil && (listsAge == nil || info.ModTime().After(*listsAge)) {
						t := info.ModTime()
						listsAge = &t
					}
				}
			}
		}
		job, _ := run(ctx, "systemctl", "is-active", updateUnit)
		return map[string]any{
			"updates": ups, "reboot_required": rerr == nil,
			"reboot_packages": strings.Fields(string(pkgs)), "lists_updated": listsAge,
			"job": strings.TrimSpace(job),
		}, nil
	})

	s.handle("system.update.start", func(ctx context.Context, _ json.RawMessage) (any, error) {
		if st, _ := run(ctx, "systemctl", "is-active", updateUnit); strings.TrimSpace(st) == "active" || strings.TrimSpace(st) == "activating" {
			return nil, Userf("An update is already running.")
		}
		run(ctx, "systemctl", "reset-failed", updateUnit+".service")
		if s.cfg.DryRun {
			s.cfg.Log.Info("dry run: update skipped")
			return nil, nil
		}
		// Fixed command line without user input; local configurations stay on conflicts
		_, err := run(ctx, "systemd-run", "--unit="+updateUnit, "--description=Updates from the web interface",
			"--setenv=DEBIAN_FRONTEND=noninteractive", "/bin/sh", "-c",
			"apt-get update && apt-get -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold upgrade --with-new-pkgs")
		return nil, err
	})

	s.handle("system.update.status", func(ctx context.Context, _ json.RawMessage) (any, error) {
		st, _ := run(ctx, "systemctl", "show", "--property=ActiveState,Result,ExecMainStatus", updateUnit+".service")
		logs, _ := run(ctx, "journalctl", "-u", updateUnit+".service", "-n", "400", "--no-pager", "-o", "cat")
		return map[string]string{"state": st, "log": logs}, nil
	})

	s.handle("system.power", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Action string `json:"action"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if p.Action != "reboot" && p.Action != "poweroff" {
			return nil, Userf("Unknown action %q.", p.Action)
		}
		s.cfg.Log.Warn("system: " + p.Action + " from the web interface")
		// Delayed, so the response still reaches the browser
		return nil, s.apply(ctx, "systemd-run", "--on-active=3", "--unit=vr-power", "/usr/bin/systemctl", p.Action)
	})

	s.handle("system.audit.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		b, err := os.ReadFile(s.Path(AuditLastFile))
		if errors.Is(err, fs.ErrNotExist) && s.cfg.DryRun {
			b, err = os.ReadFile(AuditLastFile)
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		var r AuditResult
		return r, json.Unmarshal(b, &r)
	})

	// The audit only reads, which is why it really runs in the test instance too
	s.handle("system.audit.run", func(ctx context.Context, _ json.RawMessage) (any, error) {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		out, err := run(ctx, "bash", AuditScript)
		if err != nil && !strings.Contains(out, "CHECK|") {
			return nil, err
		}
		r := AuditResult{Time: time.Now().UTC(), Checks: []AuditCheck{}}
		for _, line := range strings.Split(out, "\n") {
			if p := strings.SplitN(line, "|", 4); len(p) == 4 && p[0] == "CHECK" {
				r.Checks = append(r.Checks, AuditCheck{Level: p[1], ID: p[2], Text: p[3]})
			}
		}
		b, _ := json.MarshalIndent(r, "", "  ")
		if err := writeFile(s.Path(AuditLastFile), b, 0o600); err != nil {
			return nil, err
		}
		return r, nil
	})

	s.handle("tool.ping", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Target string `json:"target"`
			Count  int    `json:"count"`
			IPv6   bool   `json:"ipv6"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		t, err := validTarget(p.Target)
		if err != nil {
			return nil, err
		}
		fam := "-4"
		if p.IPv6 {
			fam = "-6"
		}
		count := strconv.Itoa(min(max(p.Count, 1), 10))
		out, _ := run(ctx, "ping", fam, "-c", count, "-W", "2", "--", t)
		return map[string]string{"text": out}, nil
	})

	s.handle("tool.trace", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Target string `json:"target"`
			IPv6   bool   `json:"ipv6"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		t, err := validTarget(p.Target)
		if err != nil {
			return nil, err
		}
		fam := "-4"
		if p.IPv6 {
			fam = "-6"
		}
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		out, _ := run(ctx, "tracepath", fam, "-n", "-m", "20", "--", t)
		return map[string]string{"text": out}, nil
	})

	s.handle("tool.dns", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name   string `json:"name"`
			Type   string `json:"type"`
			Server string `json:"server"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		t, err := validTarget(p.Name)
		if err != nil {
			return nil, err
		}
		typ := strings.ToUpper(p.Type)
		switch typ {
		case "A", "AAAA", "MX", "TXT", "NS", "CNAME", "PTR", "SOA", "SRV":
		default:
			return nil, Userf("Query type %q is not supported.", p.Type)
		}
		server := "127.0.0.1"
		if c, err := s.loadConfig(); err == nil {
			_, r, _ := c.LANPrefix()
			server = r.String()
		}
		if p.Server != "" {
			a, err := netip.ParseAddr(p.Server)
			if err != nil {
				return nil, Userf("The DNS server must be given as an IP address.")
			}
			server = a.String()
		}
		args := []string{"+time=3", "+tries=1", "@" + server}
		if typ == "PTR" {
			if _, err := netip.ParseAddr(t); err != nil {
				return nil, Userf("For PTR please give an IP address.")
			}
			args = append(args, "-x", t)
		} else {
			args = append(args, t, typ)
		}
		out, _ := run(ctx, "dig", args...)
		return map[string]string{"text": out}, nil
	})
}
