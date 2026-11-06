// SPDX-License-Identifier: AGPL-3.0-or-later

package netdetect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSys builds a small sysfs tree as an office PC with a built-in card,
// an add-on card, two USB adapters, Wi-Fi and Docker would show.
type fakeSys struct {
	t    *testing.T
	root string
}

func newFakeSys(t *testing.T) *fakeSys {
	root := t.TempDir()
	for _, d := range []string{"class/net", "bus/pci/drivers/e1000e", "bus/pci/drivers/r8169",
		"bus/pci/drivers/iwlwifi", "bus/usb/drivers/r8152"} {
		mustMkdir(t, filepath.Join(root, d))
	}
	return &fakeSys{t, root}
}

// iface creates devices/<dev>/net/<name>. dev == "" means virtual.
func (f *fakeSys) iface(name, dev, bus, driver string, attrs map[string]string, dirs ...string) {
	t := f.t
	var netDir string
	if dev == "" {
		netDir = filepath.Join(f.root, "devices/virtual/net", name)
	} else {
		devDir := filepath.Join(f.root, "devices", dev)
		mustMkdir(t, devDir)
		up := strings.Repeat("../", strings.Count(dev, "/")+2)
		mustLink(t, up+"bus/"+bus+"/drivers/"+driver, filepath.Join(devDir, "driver"))
		mustLink(t, up+"bus/"+bus, filepath.Join(devDir, "subsystem"))
		netDir = filepath.Join(devDir, "net", name)
		mustMkdir(t, netDir)
		mustLink(t, "../../../"+filepath.Base(dev), filepath.Join(netDir, "device"))
	}
	mustMkdir(t, netDir)
	for k, v := range attrs {
		if err := os.WriteFile(filepath.Join(netDir, k), []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range dirs {
		mustMkdir(t, filepath.Join(netDir, d))
	}
	rel, _ := filepath.Rel(filepath.Join(f.root, "class/net"), netDir)
	mustLink(t, rel, filepath.Join(f.root, "class/net", name))
}

func ether(idx, mac, carrier string) map[string]string {
	m := map[string]string{"type": "1", "address": mac, "ifindex": idx, "flags": "0x1003", "operstate": "down"}
	if carrier != "" {
		m["carrier"] = carrier
	}
	return m
}

func mustMkdir(t *testing.T, p string) {
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustLink(t *testing.T, target, link string) {
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestListPorts(t *testing.T) {
	f := newFakeSys(t)
	f.iface("lo", "", "", "", map[string]string{"type": "772", "ifindex": "1"})
	f.iface("eno1", "pci0000:00/0000:00:1f.6", "pci", "e1000e", func() map[string]string {
		m := ether("2", "02:00:00:00:00:01", "1")
		m["speed"] = "1000"
		m["operstate"] = "up"
		return m
	}())
	f.iface("enp2s0", "pci0000:00/0000:00:1c.0/0000:02:00.0", "pci", "r8169", ether("3", "02:00:00:00:00:02", ""))
	f.iface("enx10", "pci0000:00/0000:00:14.0/usb2/2-10/2-10:1.0", "usb", "r8152", ether("5", "02:00:00:00:00:04", "0"))
	f.iface("enx2", "pci0000:00/0000:00:14.0/usb2/2-2/2-2:1.0", "usb", "r8152", ether("4", "02:00:00:00:00:03", "0"))
	f.iface("wlp3s0", "pci0000:00/0000:00:1c.4/0000:03:00.0", "pci", "iwlwifi", ether("6", "02:00:00:00:00:05", "0"), "wireless")
	f.iface("docker0", "", "", "", ether("7", "02:00:00:00:00:06", "0"), "bridge")
	f.iface("veth1", "", "", "", ether("8", "02:00:00:00:00:07", "1"))

	ports, skipped, err := ListPorts(f.root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range ports {
		got = append(got, p.Name)
	}
	want := "enp2s0 eno1 enx2 enx10"
	if strings.Join(got, " ") != want {
		t.Fatalf("order = %v, expected %s", got, want)
	}
	reasons := map[string]string{}
	for _, s := range skipped {
		reasons[s.Name] = s.Reason
	}
	for name, r := range map[string]string{"lo": skipLoopback, "wlp3s0": skipWireless, "docker0": skipBridge, "veth1": skipVirtual} {
		if reasons[name] != r {
			t.Errorf("%s: reason %q, expected %q", name, reasons[name], r)
		}
	}

	eno := ports[1]
	if eno.Number != 2 || eno.Driver != "e1000e" || eno.Bus != "pci" || eno.Index != 2 ||
		eno.Carrier != Yes || eno.SpeedMbps != 1000 || !eno.AdminUp || eno.MAC != "02:00:00:00:00:01" {
		t.Errorf("eno1 read wrongly: %+v", eno)
	}
	if eno.DevPath != "/devices/pci0000:00/0000:00:1f.6" {
		t.Errorf("DevPath = %q", eno.DevPath)
	}
	if ports[0].Carrier != Unknown {
		t.Errorf("enp2s0 without carrier file: %v, expected unknown", ports[0].Carrier)
	}
	if l := ports[3].Label(); l != "Port 4 (USB adapter)" {
		t.Errorf("Label = %q", l)
	}
}

func TestListPortsVirtualForLab(t *testing.T) {
	f := newFakeSys(t)
	f.iface("r1", "", "", "", ether("3", "02:00:00:00:00:02", "0"))
	f.iface("r0", "", "", "", ether("2", "02:00:00:00:00:01", "1"))
	f.iface("br0", "", "", "", ether("4", "02:00:00:00:00:03", "0"), "bridge")
	ports, _, err := ListPortsOpts(f.root, Options{IncludeVirtual: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 2 || ports[0].Name != "r0" || ports[1].Name != "r1" || ports[0].Carrier != Yes {
		t.Fatalf("%+v", ports)
	}
}

func TestLessPath(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"/devices/pci0000:00/0000:00:1c.0", "/devices/pci0000:00/0000:00:1f.6", true},
		{"/devices/pci0000:00/0000:0a:00.0", "/devices/pci0000:00/0000:01:00.0", false}, // hexadecimal
		{"/usb2/2-2/2-2:1.0", "/usb2/2-10/2-10:1.0", true},                              // natural
		{"/usb10/x", "/usb2/x", false},
		{"/a", "/a/b", true},
	}
	for _, c := range cases {
		if got := lessPath(c.a, c.b); got != c.want {
			t.Errorf("lessPath(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
