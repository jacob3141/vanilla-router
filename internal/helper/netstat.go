// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Port struct {
	Proto   string `json:"proto"`
	Addr    string `json:"addr"`
	Port    int    `json:"port"`
	Process string `json:"process"`
	Scope   string `json:"scope"` // local, lan, all
}

var ssProcRe = regexp.MustCompile(`\(\("([^"]+)"`)

func listPorts(ctx context.Context) ([]Port, error) {
	out, err := run(ctx, "ss", "-tulnpH")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var res []Port
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		local := f[4]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		port, _ := strconv.Atoi(local[i+1:])
		// ss schreibt z. B. "[fe80::1]%lan0:53": erst die Schnittstelle abtrennen, dann die Klammern
		addr := local[:i]
		if j := strings.Index(addr, "%"); j >= 0 {
			addr = addr[:j]
		}
		addr = strings.Trim(addr, "[]")
		p := Port{Proto: f[0], Addr: addr, Port: port}
		if m := ssProcRe.FindStringSubmatch(line); m != nil {
			p.Process = m[1]
		}
		// UDP-Sockets im Bereich für ausgehende Verbindungen sind Clients, keine Dienste
		if p.Proto == "udp" && port >= 32768 && port <= 60999 && (addr == "0.0.0.0" || addr == "*") {
			continue
		}
		switch {
		case strings.HasPrefix(addr, "127.") || addr == "::1":
			p.Scope = "local"
		case addr == "0.0.0.0" || addr == "*" || addr == "::":
			p.Scope = "all"
		default:
			p.Scope = "lan"
		}
		key := p.Proto + p.Addr + strconv.Itoa(p.Port)
		if !seen[key] {
			seen[key] = true
			res = append(res, p)
		}
	}
	sort.Slice(res, func(i, j int) bool {
		if res[i].Port != res[j].Port {
			return res[i].Port < res[j].Port
		}
		return res[i].Proto < res[j].Proto
	})
	return res, nil
}

type countEntry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func top(m map[string]int, n int) []countEntry {
	out := make([]countEntry, 0, len(m))
	for k, v := range m {
		out = append(out, countEntry{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

var dnsQueryRe = regexp.MustCompile(`^query\[(\w+)\] (\S+) from (\S+)$`)

// dnsStats wertet das Journal von dnsmasq aus (dnsmasq protokolliert mit log-queries jede Anfrage).
// client: nur Anfragen dieser IP zählen (leer = alle).
func dnsStats(ctx context.Context, hours int, client string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "journalctl", "-u", "vr-dnsmasq.service", "--since", "-"+strconv.Itoa(hours)+"h", "--no-pager", "-o", "cat")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C.UTF-8"}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	domains, clients, types := map[string]int{}, map[string]int{}, map[string]int{}
	var queries, cached, forwarded, local int
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "query["):
			m := dnsQueryRe.FindStringSubmatch(line)
			if m == nil || (client != "" && m[3] != client) {
				continue
			}
			queries++
			types[m[1]]++
			domains[strings.ToLower(m[2])]++
			clients[m[3]]++
		case client != "":
			// Antworten lassen sich keinem Client zuordnen
		case strings.HasPrefix(line, "cached "):
			cached++
		case strings.HasPrefix(line, "forwarded "):
			forwarded++
		case strings.HasPrefix(line, "config "), strings.HasPrefix(line, "/etc/hosts "):
			local++
		}
	}
	cmd.Wait()
	return map[string]any{
		"hours": hours, "queries": queries, "cached": cached, "forwarded": forwarded, "local": local,
		"top_domains": top(domains, 30), "top_clients": top(clients, 20), "types": top(types, 10),
		"logging": queries > 0 || forwarded > 0,
	}, nil
}

func (s *Server) registerNetstat() {
	s.handle("ports.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return listPorts(ctx)
	})

	s.handle("conn.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		v4, err := dumpConntrack(syscall.AF_INET, 20000)
		if err != nil {
			return nil, Userf("Die Verbindungstabelle ist nicht lesbar: %v", err)
		}
		v6, _ := dumpConntrack(syscall.AF_INET6, 5000)
		return append(v4, v6...), nil
	})

	s.handle("dns.stats.get", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Hours  int    `json:"hours"`
			Client string `json:"client"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if p.Hours <= 0 || p.Hours > 24*7 {
			p.Hours = 24
		}
		if p.Client != "" {
			t, err := validTarget(p.Client)
			if err != nil {
				return nil, err
			}
			p.Client = t
		}
		return dnsStats(ctx, p.Hours, p.Client)
	})
}
