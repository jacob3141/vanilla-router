// SPDX-License-Identifier: AGPL-3.0-or-later

package apply

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
)

// Ports are assigned by their .link files, which udev only reads when a device appears, so normally at
// boot. A restart is no way to change a port, because the rollback timer does not survive it. relink
// therefore gives the running ports the names the configuration wants, and a rollback does the same with
// the old configuration.

func portsDiffer(a, b *config.Config) bool {
	return !strings.EqualFold(a.WAN.MatchMAC, b.WAN.MatchMAC) || !slices.Equal(a.LAN.Members, b.LAN.Members)
}

// wantedNames maps the MAC of every port in use to its name.
func wantedNames(c *config.Config) map[string]string {
	w := map[string]string{strings.ToLower(c.WAN.MatchMAC): gen.WANIf}
	for i, m := range c.LAN.Members {
		w[strings.ToLower(m)] = gen.LANPort + strconv.Itoa(i+1)
	}
	return w
}

type linkEntry struct {
	Name    string `json:"ifname"`
	Address string `json:"address"`
	Type    string `json:"link_type"`
	Parent  string `json:"link"` // VLANs share the MAC of their parent
}

// offName is the name of a port without a use. It must not match wan0 or lanp*, the .network files
// would adopt it otherwise.
func offName(mac string) string {
	return "vroff-" + strings.ReplaceAll(mac, ":", "")[6:]
}

func (a *Applier) relink(c *config.Config) error {
	out, err := a.Run.Run(nil, "ip", "-j", "link", "show")
	if err != nil {
		return fmt.Errorf("list ports: %w", err)
	}
	var links []linkEntry
	if len(out) > 0 {
		if err := json.Unmarshal(out, &links); err != nil {
			return fmt.Errorf("list ports: %w", err)
		}
	}
	want := wantedNames(c)
	type rename struct{ from, tmp, to string }
	var todo []rename
	for _, l := range links {
		if l.Type != "ether" || l.Parent != "" || l.Name == gen.LANIf {
			continue
		}
		mac := strings.ToLower(l.Address)
		to, used := want[mac]
		if !used {
			if l.Name != gen.WANIf && !strings.HasPrefix(l.Name, gen.LANPort) {
				continue // never ours, for example a container interface
			}
			to = offName(mac)
		}
		if l.Name != to {
			todo = append(todo, rename{from: l.Name, tmp: "vrtmp" + strconv.Itoa(len(todo)), to: to})
		}
	}
	if len(todo) == 0 {
		return nil
	}
	ip := func(args ...string) error {
		if out, err := a.Run.Run(nil, "ip", args...); err != nil {
			return fmt.Errorf("ip %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// Two passes, because a swap needs a name that is still taken. A renamed port stays in its bridge and
	// keeps its addresses, so both are removed first.
	for _, r := range todo {
		for _, args := range [][]string{
			{"link", "set", "dev", r.from, "down"},
			{"link", "set", "dev", r.from, "nomaster"},
			{"addr", "flush", "dev", r.from},
			{"link", "set", "dev", r.from, "name", r.tmp},
		} {
			if err := ip(args...); err != nil {
				return err
			}
		}
	}
	for _, r := range todo {
		if err := ip("link", "set", "dev", r.tmp, "name", r.to); err != nil {
			return err
		}
		if err := ip("link", "set", "dev", r.to, "up"); err != nil {
			return err
		}
	}
	if out, err := a.Run.Run(nil, "networkctl", "reload"); err != nil {
		return fmt.Errorf("networkctl reload: %v %s", err, strings.TrimSpace(string(out)))
	}
	names := []string{"reconfigure"}
	for _, r := range todo {
		names = append(names, r.to)
	}
	if out, err := a.Run.Run(nil, "networkctl", names...); err != nil {
		return fmt.Errorf("networkctl reconfigure: %v %s", err, strings.TrimSpace(string(out)))
	}
	a.logf("Ports assigned anew: %d renamed.", len(todo))
	return nil
}
