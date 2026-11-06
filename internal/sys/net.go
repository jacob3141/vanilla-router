// SPDX-License-Identifier: AGPL-3.0-or-later

package sys

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Interface describes a network interface for the overview.
type Interface struct {
	Name  string   `json:"name"`
	Up    bool     `json:"up"`
	MAC   string   `json:"mac"`
	IPv4  []string `json:"ipv4"`
	IPv6  []string `json:"ipv6"` // without link-local
	Speed int      `json:"speed_mbit,omitempty"`
}

func ReadInterface(name string) Interface {
	out := Interface{Name: name, IPv4: []string{}, IPv6: []string{}}
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return out
	}
	out.Up = ifc.Flags&net.FlagUp != 0
	out.MAC = ifc.HardwareAddr.String()
	addrs, _ := ifc.Addrs()
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ipn.IP.To4() != nil {
			out.IPv4 = append(out.IPv4, a.String())
		} else if !ipn.IP.IsLinkLocalUnicast() {
			out.IPv6 = append(out.IPv6, a.String())
		}
	}
	if b, err := os.ReadFile(p("sys/class/net/" + name + "/speed")); err == nil {
		var n int
		for _, c := range strings.TrimSpace(string(b)) {
			if c < '0' || c > '9' {
				n = 0
				break
			}
			n = n*10 + int(c-'0')
		}
		out.Speed = n
	}
	return out
}

// DefaultGatewayV4 reads the IPv4 default gateway from /proc/net/route.
func DefaultGatewayV4() (gw, dev string) {
	f, err := os.Open(p("proc/net/route"))
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) < 3 || fl[1] != "00000000" {
			continue
		}
		b, err := hex.DecodeString(fl[2])
		if err != nil || len(b) != 4 {
			continue
		}
		return net.IPv4(b[3], b[2], b[1], b[0]).String(), fl[0]
	}
	return "", ""
}

// Neighbor is an entry in the neighbor table (ARP/NDP).
type Neighbor struct {
	IP    string `json:"ip"`
	MAC   string `json:"mac"`
	State string `json:"state"`
}

// Online reports whether the neighbor was reachable recently.
func (n Neighbor) Online() bool {
	switch n.State {
	case "REACHABLE", "STALE", "DELAY", "PROBE":
		return n.MAC != ""
	}
	return false
}

// Neighbors reads the neighbor table of an interface through `ip -j neigh`.
func Neighbors(ctx context.Context, dev string) ([]Neighbor, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ip", "-j", "neigh", "show", "dev", dev).Output()
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Dst    string   `json:"dst"`
		Lladdr string   `json:"lladdr"`
		State  []string `json:"state"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	res := make([]Neighbor, 0, len(raw))
	for _, r := range raw {
		n := Neighbor{IP: r.Dst, MAC: strings.ToLower(r.Lladdr)}
		if len(r.State) > 0 {
			n.State = r.State[0]
		}
		res = append(res, n)
	}
	return res, nil
}

// UnitState returns the ActiveState of several systemd units (active, failed, inactive …).
func UnitState(ctx context.Context, units ...string) map[string]string {
	res := map[string]string{}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := append([]string{"show", "--property=Id,ActiveState", "--"}, units...)
	out, err := exec.CommandContext(ctx, "systemctl", args...).Output()
	if err != nil {
		return res
	}
	// blocks per unit, separated by empty lines
	var id string
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "Id="); ok {
			id = v
		} else if v, ok := strings.CutPrefix(line, "ActiveState="); ok && id != "" {
			res[strings.TrimSuffix(id, ".service")] = v
			id = ""
		}
	}
	return res
}
