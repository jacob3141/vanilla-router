// SPDX-License-Identifier: AGPL-3.0-or-later

package sys

import (
	"os"
	"path/filepath"
	"testing"
)

// Shortened output in the form of iw 6.x. Not from a real device: only the format matters here, and there has
// been no adapter to record one from (T-M4-09).
const iwPhySample = `Wiphy phy0
	wiphy index: 0
	max # scan SSIDs: 4
	Supported Ciphers:
		* CCMP-128 (00-0f-ac:4)
	Band 1:
		Capabilities: 0x11ef
			RX LDPC
			HT20/HT40
			SM Power Save disabled
		Frequencies:
			* 2412.0 MHz [1] (20.0 dBm)
			* 2467.0 MHz [12] (20.0 dBm) (no IR)
			* 2484.0 MHz [14] (disabled)
	Band 2:
		Capabilities: 0x11ef
			HT20/HT40
		VHT Capabilities (0x039071f6):
			Max MPDU length: 11454
		Frequencies:
			* 5180.0 MHz [36] (23.0 dBm) (no IR)
			* 5260.0 MHz [52] (20.0 dBm) (no IR, radar detection)
			* 5825.0 MHz [165] (disabled)
	Band 4:
		Frequencies:
			* 5955.0 MHz [1] (disabled)
	Supported interface modes:
		 * IBSS
		 * managed
		 * AP
		 * AP/VLAN
		 * monitor
	software interface modes (can always be added):
		 * AP/VLAN
`

func TestParseIwPhy(t *testing.T) {
	c := ParseIwPhy(iwPhySample)
	if !c.Known || !c.AP {
		t.Fatalf("%+v", c)
	}
	if len(c.Bands) != 2 {
		t.Fatalf("bands: %+v", c.Bands)
	}
	b24, b5 := c.Bands[0], c.Bands[1]
	if b24.Band != "2.4" || !b24.HT40 || b24.VHT || len(b24.Channels) != 3 {
		t.Errorf("%+v", b24)
	}
	if b24.Channels[0].NoIR || b24.Channels[0].Disabled || !b24.Channels[1].NoIR || !b24.Channels[2].Disabled {
		t.Errorf("%+v", b24.Channels)
	}
	if b5.Band != "5" || !b5.HT40 || !b5.VHT || len(b5.Channels) != 3 {
		t.Errorf("%+v", b5)
	}
	if !b5.Channels[1].Radar || !b5.Channels[1].NoIR || !b5.Channels[2].Disabled {
		t.Errorf("%+v", b5.Channels)
	}
}

// A card that is only a client must not count as an access point, not even through "AP/VLAN" further down.
func TestParseIwPhyNoAP(t *testing.T) {
	c := ParseIwPhy("Wiphy phy1\n\tSupported interface modes:\n\t\t * managed\n\t\t * monitor\n\tsoftware interface modes (can always be added):\n\t\t * AP/VLAN\n")
	if !c.Known || c.AP {
		t.Errorf("%+v", c)
	}
	// Checks fail on missing data (L-17)
	if c := ParseIwPhy("command failed: No such device (-19)"); c.Known || c.AP || len(c.Bands) != 0 {
		t.Errorf("%+v", c)
	}
}

func TestParseIwLink(t *testing.T) {
	l := ParseIwLink("Interface wlp3s0\n\tifindex 4\n\twdev 0x1\n\taddr 02:00:00:00:00:30\n\tssid Küche WLAN\n\ttype AP\n\twiphy 0\n\tchannel 36 (5180 MHz), width: 80 MHz, center1: 5210 MHz\n\ttxpower 20.00 dBm\n")
	if !l.AP || l.SSID != "Küche WLAN" || l.Channel != 36 || l.Width != 80 {
		t.Errorf("%+v", l)
	}
	if l := ParseIwLink("Interface wlp3s0\n\ttype managed\n"); l.AP || l.SSID != "" {
		t.Errorf("%+v", l)
	}
}

func TestParseIwStations(t *testing.T) {
	st := ParseIwStations(`Station 02:00:00:00:00:AA (on wlp3s0)
	inactive time:	304 ms
	rx bytes:	18816
	signal:  	-29 [-29, -43] dBm
	tx bitrate:	866.7 MBit/s VHT-MCS 9 80MHz short GI VHT-NSS 2
	rx bitrate:	6.0 MBit/s
	connected time:	1234 seconds
Station 02:00:00:00:00:bb (on wlp3s0)
	inactive time:	1 ms
`)
	if len(st) != 2 {
		t.Fatalf("%+v", st)
	}
	a := st[0]
	if a.MAC != "02:00:00:00:00:aa" || !a.HasSignal || a.SignalDBm != -29 || a.TxBitrate != 866.7 || a.RxBitrate != 6 || a.Connected != 1234 || a.InactiveMS != 304 {
		t.Errorf("%+v", a)
	}
	if st[1].HasSignal {
		t.Errorf("%+v", st[1])
	}
	if got := ParseIwStations(""); got == nil || len(got) != 0 {
		t.Errorf("%#v: the web interface expects an empty list", got)
	}
}

func fakeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWiFiAdapters(t *testing.T) {
	root := t.TempDir()
	if WiFiPresent(root) || WiFiCardWithoutDriver(root) || len(WiFiAdapters(root)) != 0 {
		t.Error("empty system counts as having Wi-Fi")
	}
	// An Ethernet card is not a Wi-Fi card
	fakeFile(t, root, "sys/bus/pci/devices/0000:01:00.0/class", "0x020000\n")
	fakeFile(t, root, "sys/class/net/lanp1/address", "02:00:00:00:00:02\n")
	if WiFiPresent(root) || len(WiFiAdapters(root)) != 0 {
		t.Error("Ethernet counts as Wi-Fi")
	}
	// A Wi-Fi card without firmware: known on the bus, but no wireless device
	fakeFile(t, root, "sys/bus/pci/devices/0000:02:00.0/class", "0x028000\n")
	if !WiFiPresent(root) || !WiFiCardWithoutDriver(root) || len(WiFiAdapters(root)) != 0 {
		t.Error("card without firmware not recognized")
	}
	// The driver has loaded
	fakeFile(t, root, "sys/class/ieee80211/phy0/index", "0\n")
	fakeFile(t, root, "sys/class/net/wlp2s0/address", "02:00:00:00:00:30\n")
	fakeFile(t, root, "sys/class/net/wlp2s0/phy80211/name", "phy0\n")
	if !WiFiPresent(root) || WiFiCardWithoutDriver(root) {
		t.Error("working card not recognized")
	}
	a := WiFiAdapters(root)
	if len(a) != 1 || a[0].Name != "wlp2s0" || a[0].Phy != "phy0" || a[0].MAC != "02:00:00:00:00:30" {
		t.Errorf("%+v", a)
	}
}
