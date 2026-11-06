// SPDX-License-Identifier: AGPL-3.0-or-later

package sys

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeRoot(t *testing.T, files map[string]string) {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := ProcRoot
	ProcRoot = root
	t.Cleanup(func() { ProcRoot = old })
}

func TestReadNetDev(t *testing.T) {
	fakeRoot(t, map[string]string{"proc/net/dev": `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  144560731 1296896    0    0    0     0          0         0 144560731 1296896    0    0    0     0       0          0
  wan0: 883477661617 743432921    0    0    0     0          0   2523554 74155433235 276604970   14   42    0     0       0          0
`})
	m, err := ReadNetDev()
	if err != nil {
		t.Fatal(err)
	}
	if m["wan0"].RxBytes != 883477661617 || m["wan0"].TxBytes != 74155433235 {
		t.Errorf("wan0 read wrongly: %+v", m["wan0"])
	}
	if _, ok := m["face |bytes"]; ok {
		t.Error("header line read as an interface")
	}
}

func TestDefaultGateway(t *testing.T) {
	fakeRoot(t, map[string]string{"proc/net/route": "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"wan0\t00000000\t010200C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"lan0\t0000000A\t00000000\t0001\t0\t0\t0\t000000FF\t0\t0\t0\n"})
	gw, dev := DefaultGatewayV4()
	if gw != "192.0.2.1" || dev != "wan0" {
		t.Errorf("gateway = %s via %s, expected 192.0.2.1 via wan0", gw, dev)
	}
}

func TestTemperaturePrefersPackage(t *testing.T) {
	fakeRoot(t, map[string]string{
		"sys/class/thermal/thermal_zone0/type": "acpitz\n",
		"sys/class/thermal/thermal_zone0/temp": "70000\n",
		"sys/class/thermal/thermal_zone1/type": "x86_pkg_temp\n",
		"sys/class/thermal/thermal_zone1/temp": "45500\n",
	})
	c, ok := Temperature()
	if !ok || c != 45.5 {
		t.Errorf("temperature = %v (%v), expected 45.5", c, ok)
	}
}

func TestMemory(t *testing.T) {
	fakeRoot(t, map[string]string{"proc/meminfo": "MemTotal:        7450000 kB\nMemFree:          240000 kB\nMemAvailable:    5300000 kB\n"})
	m, err := ReadMemory()
	if err != nil || m.Total != 7450000*1024 || m.Available != 5300000*1024 {
		t.Errorf("memory read wrongly: %+v %v", m, err)
	}
}
