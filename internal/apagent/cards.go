// SPDX-License-Identifier: AGPL-3.0-or-later

package apagent

import (
	"context"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
	"vanilla-router/internal/sys"
)

// System is the real computer.
type System struct{}

// Run runs a program without a shell.
func (System) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C.UTF-8"}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Known chips by PCI ID. Everything else shows its ID, the reference list is in the concept (3.2).
var chips = map[string]string{
	"14c3:7915": "MediaTek MT7915", "14c3:7906": "MediaTek MT7916", "14c3:7916": "MediaTek MT7916",
	"14c3:0616": "MediaTek MT7922", "14c3:7922": "MediaTek MT7922", "14c3:7990": "MediaTek MT7996",
}

var bandLine = regexp.MustCompile(`(?m)^\s*Band ([0-9]+):`)

// ParseRadio reads the bands and abilities a card has as an access point from `iw phy <x> info`.
func ParseRadio(out string) (bands []string, dfs, width160 bool) {
	for _, m := range bandLine.FindAllStringSubmatch(out, -1) {
		if b := map[string]string{"1": config.WiFiBand24, "2": config.WiFiBand5, "4": config.WiFiBand6}[m[1]]; b != "" && !slices.Contains(bands, b) {
			bands = append(bands, b)
		}
	}
	return bands, strings.Contains(out, "radar detect widths"), strings.Contains(out, "160 MHz")
}

// radioID turns a PCI address into the name config.json uses.
func radioID(pci string) string { return "pci-" + strings.NewReplacer(":", "-", ".", "-").Replace(pci) }

func read(p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

// Cards lists the PCI Wi-Fi cards that can run as access points. USB adapters are not supported (concept 3.2).
func Cards(ctx context.Context, c Commands) []gen.RadioCaps {
	out := []gen.RadioCaps{}
	for _, a := range sys.WiFiAdapters("/") {
		if a.USB {
			continue
		}
		dev, err := filepath.EvalSymlinks("/sys/class/ieee80211/" + a.Phy + "/device")
		if err != nil {
			continue
		}
		pci := filepath.Base(dev)
		info, err := c.Run(ctx, "iw", "phy", a.Phy, "info")
		if err != nil || !sys.ParseIwPhy(info).AP {
			continue
		}
		bands, dfs, w160 := ParseRadio(info)
		id := strings.TrimPrefix(read(dev+"/vendor"), "0x") + ":" + strings.TrimPrefix(read(dev+"/device"), "0x")
		chip, ok := chips[id]
		if !ok {
			chip = id
		}
		out = append(out, gen.RadioCaps{ID: radioID(pci), Path: "pci-" + pci, MAC: a.MAC, Chip: chip, Bands: bands, DFS: dfs, Width160: w160})
		if len(out) == config.MaxRadios {
			break
		}
	}
	return out
}

// Gateway reads the default route from the kernel: on an AP that is the router.
func Gateway() string {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(l)
		if len(f) < 3 || f[1] != "00000000" || len(f[2]) != 8 {
			continue
		}
		b, err := hex.DecodeString(f[2])
		if err != nil {
			continue
		}
		return net.IPv4(b[3], b[2], b[1], b[0]).String() // the kernel writes it little endian
	}
	return ""
}

// Uplink returns the permanent address of the wired port with the default route, and the model of the computer.
func Uplink() (mac, model string) {
	model = read("/sys/class/dmi/id/product_name")
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", model
	}
	for _, l := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(l)
		if len(f) >= 2 && f[1] == "00000000" {
			mac = read("/sys/class/net/" + f[0] + "/address")
			break
		}
	}
	return mac, model
}

// Dry stands in for the computer in a test: programs are only logged, and every status question is answered with yes.
type Dry struct{ Log func(string) }

// Run logs the command line.
func (d Dry) Run(_ context.Context, name string, args ...string) (string, error) {
	if d.Log != nil {
		d.Log(name + " " + strings.Join(args, " "))
	}
	return "", nil
}

// DryCards is the card of the simulated access point.
func DryCards(context.Context) []gen.RadioCaps {
	return []gen.RadioCaps{{ID: "pci-0000-06-00-0", Path: "pci-0000:06:00.0", MAC: "02:00:00:00:06:00", Chip: "MediaTek MT7916", Bands: []string{"5", "6"}, DFS: true, Width160: true}}
}
