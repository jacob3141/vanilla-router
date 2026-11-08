// SPDX-License-Identifier: AGPL-3.0-or-later

package gen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vanilla-router/internal/config"
)

const (
	keyA = "ed25519:AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	keyB = "ed25519:ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="
)

func wirelessCase(t *testing.T) (*config.Config, map[string]map[string]RadioCaps) {
	c := base(t)
	c.Wireless = &config.Wireless{
		Country: "DE",
		Segments: []config.Segment{
			{ID: "home", Name: "Home network", VLAN: 1, Access: config.AccessLAN},
			{ID: "guest", Name: "Guests", VLAN: 20, Access: config.AccessInternet, IPv4: "192.168.20.1/24"},
			{ID: "older", Name: "Older devices", VLAN: 30, Access: config.AccessInternet, IPv4: "192.168.30.1/24", ReachableFrom: []string{"home"}},
		},
		Networks: []config.WNetwork{
			{ID: "home", SSID: "Home", Segment: "home", PasswordRef: "wnet-1", Security: config.SecurityWPA3, Bands: []string{"5", "6"}, Enabled: true,
				Roaming: &config.Roaming{MobilityDomain: "a1b2", KeyRef: "ft-1"}},
			{ID: "guest", SSID: "Guests", Segment: "guest", PasswordRef: "wnet-2", Security: config.SecurityWPA3, Bands: []string{"5"}, Enabled: true,
				Roaming: &config.Roaming{MobilityDomain: "c3d4", KeyRef: "ft-2"}},
			{ID: "older", SSID: "Home Older Devices", Segment: "older", PasswordRef: "wnet-3", Security: config.SecurityWPA2WPA3, Bands: []string{"2.4"}},
		},
		AccessPoints: []config.AccessPoint{
			{ID: "hall", Name: "Hall", PublicKey: keyA, MAC: "02:00:00:00:00:10", Power: config.PowerMedium,
				Radios: []config.APRadio{{ID: "pci-0000-03-00-0", Band: "6"}, {ID: "pci-0000-04-00-0", Band: "5"}}},
			{ID: "attic", Name: "Attic", PublicKey: keyB, MAC: "02:00:00:00:00:11", Power: config.PowerLow,
				Radios: []config.APRadio{{ID: "pci-0000-02-00-0", Band: "5"}, {ID: "pci-0000-05-00-0", Band: "2.4"}}},
		},
	}
	caps := map[string]map[string]RadioCaps{
		"hall": {
			"pci-0000-03-00-0": {ID: "pci-0000-03-00-0", Path: "pci-0000:03:00.0", MAC: "02:00:00:00:01:00", Chip: "MediaTek MT7916", Bands: []string{"5", "6"}, DFS: true, Width160: true},
			"pci-0000-04-00-0": {ID: "pci-0000-04-00-0", Path: "pci-0000:04:00.0", MAC: "02:00:00:00:02:00", Chip: "MediaTek MT7915", Bands: []string{"2.4", "5"}, DFS: true},
		},
		"attic": {
			"pci-0000-02-00-0": {ID: "pci-0000-02-00-0", Path: "pci-0000:02:00.0", MAC: "02:00:00:00:03:00", Chip: "MediaTek MT7922", Bands: []string{"2.4", "5", "6"}},
			"pci-0000-05-00-0": {ID: "pci-0000-05-00-0", Path: "pci-0000:05:00.0", MAC: "02:00:00:00:04:00", Chip: "MediaTek MT7915", Bands: []string{"2.4", "5"}, DFS: true},
		},
	}
	return c, caps
}

func TestPlanChannels(t *testing.T) {
	c, caps := wirelessCase(t)
	p := PlanChannels(c.Wireless, caps)
	want := map[string]RadioPlan{
		"hall/pci-0000-03-00-0":  {Band: "6", Channel: 1, Width: 160, Center: 15},
		"hall/pci-0000-04-00-0":  {Band: "5", Channel: 36, Width: 80, Center: 42},
		"attic/pci-0000-02-00-0": {Band: "5", Channel: 36, Width: 80, Center: 42}, // no radar detection: only 36–48
		"attic/pci-0000-05-00-0": {Band: "2.4", Idle: true},                       // older devices off
	}
	if len(p.Radios) != len(want) {
		t.Fatalf("%d radios: %+v", len(p.Radios), p.Radios)
	}
	for _, r := range p.Radios {
		w := want[r.AP+"/"+r.Radio]
		if r.Band != w.Band || r.Channel != w.Channel && !w.Idle || r.Width != w.Width && !w.Idle || r.Center != w.Center && !w.Idle || r.Idle != w.Idle {
			t.Errorf("%s/%s: %+v, want %+v", r.AP, r.Radio, r, w)
		}
	}
	if p.Radios[0].AP != "hall" || p.Radios[3].AP != "attic" {
		t.Errorf("order: %+v", p.Radios)
	}

	// With radar detection the second AP moves to a DFS block
	caps["attic"]["pci-0000-02-00-0"] = RadioCaps{DFS: true, MAC: "02:00:00:00:03:00"}
	p = PlanChannels(c.Wireless, caps)
	if r, _ := p.Radio("attic", "pci-0000-02-00-0"); r.Channel != 52 || !r.DFS {
		t.Errorf("DFS: %+v", r)
	}

	// A pinned card keeps its channel, the others avoid it
	c.Wireless.AccessPoints[0].Radios[1].Channel, c.Wireless.AccessPoints[0].Radios[1].Width = 52, 80
	p = PlanChannels(c.Wireless, caps)
	if r, _ := p.Radio("hall", "pci-0000-04-00-0"); r.Channel != 52 || !r.Pinned {
		t.Errorf("pinned: %+v", r)
	}
	if r, _ := p.Radio("attic", "pci-0000-02-00-0"); r.Channel != 36 {
		t.Errorf("next to pinned: %+v", r)
	}

	// Outside the table: no 6 GHz, 5 GHz only 36
	c.Wireless.Country = "JP"
	p = PlanChannels(c.Wireless, caps)
	if r, _ := p.Radio("hall", "pci-0000-03-00-0"); !r.Idle || r.Reason == "" {
		t.Errorf("6 GHz in JP: %+v", r)
	}
	if r, _ := p.Radio("attic", "pci-0000-02-00-0"); r.Channel != 36 {
		t.Errorf("5 GHz in JP: %+v", r)
	}
}

func TestPlanManyOn6(t *testing.T) {
	c, caps := wirelessCase(t)
	// Four 6 GHz cards do not fit into the three 160 MHz blocks of the EU: everyone gets 80 MHz
	for i, id := range []string{"a", "b", "c"} {
		c.Wireless.AccessPoints = append(c.Wireless.AccessPoints, config.AccessPoint{ID: id, Name: strings.ToUpper(id),
			PublicKey: "ed25519:" + strings.Repeat(string(rune('A'+i)), 43) + "=", MAC: "02:00:00:00:00:2" + id, Power: config.PowerMedium,
			Radios: []config.APRadio{{ID: "r", Band: "6"}}})
		caps[id] = map[string]RadioCaps{"r": {Width160: true, MAC: "02:00:00:00:05:0" + id}}
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	p := PlanChannels(c.Wireless, caps)
	seen := map[int]bool{}
	for _, r := range p.Radios {
		if r.Band != "6" {
			continue
		}
		if r.Width != 80 || seen[r.Channel] {
			t.Errorf("%+v", r)
		}
		seen[r.Channel] = true
	}
}

func TestAPGolden(t *testing.T) {
	c, caps := wirelessCase(t)
	sec := secrets{"wnet-1": "home secret 123", "wnet-2": "guest secret 123", "wnet-3": "older secret 123",
		"ft-1": strings.Repeat("11", 32), "ft-2": strings.Repeat("22", 32)}
	plan := PlanChannels(c.Wireless, caps)
	for _, ap := range []string{"hall", "attic"} {
		t.Run(ap, func(t *testing.T) {
			files, err := APFiles(c, ap, plan, caps, sec, APOptions{})
			if err != nil {
				t.Fatal(err)
			}
			golden(t, filepath.Join("testdata", "ap-"+ap), files)

			// The preview contains no secret at all
			red, err := APFiles(c, ap, plan, caps, nil, APOptions{Redact: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range red {
				for _, s := range sec {
					if bytes.Contains(f.Content, []byte(s)) {
						t.Errorf("%s contains a secret", f.Path)
					}
				}
			}
		})
	}
}

// The pair key is the same on both ends, so both APs can verify each other.
func TestFTKeySymmetric(t *testing.T) {
	m := bytes.Repeat([]byte{7}, 32)
	if ftKey(m, "home", "02:00:00:00:01:00", "02:00:00:00:03:00") != ftKey(m, "home", "02:00:00:00:03:00", "02:00:00:00:01:00") {
		t.Error("not symmetric")
	}
	if ftKey(m, "home", "a", "b") == ftKey(m, "guest", "a", "b") {
		t.Error("same key in two networks")
	}
}

func TestAPErrors(t *testing.T) {
	c, caps := wirelessCase(t)
	plan := PlanChannels(c.Wireless, caps)
	if _, err := APFiles(c, "cellar", plan, caps, nil, APOptions{Redact: true}); err == nil {
		t.Error("unknown AP accepted")
	}
	if _, err := APFiles(c, "hall", plan, caps, secrets{}, APOptions{}); err == nil {
		t.Error("missing password accepted")
	}
	delete(caps["hall"], "pci-0000-03-00-0")
	if _, err := APFiles(c, "hall", plan, caps, nil, APOptions{Redact: true}); err == nil {
		t.Error("card without address accepted")
	}
}

// golden compares files with testdata/<dir>/out, or rewrites them with -update.
func golden(t *testing.T, dir string, files []File) {
	t.Helper()
	if *update {
		os.RemoveAll(dir)
		for _, f := range files {
			write(t, filepath.Join(dir, "out", filepath.FromSlash(f.Path)), f.Content)
		}
		return
	}
	want := map[string]bool{}
	filepath.WalkDir(filepath.Join(dir, "out"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(filepath.Join(dir, "out"), p)
			want["/"+filepath.ToSlash(rel)] = true
		}
		return nil
	})
	for _, f := range files {
		exp, err := os.ReadFile(filepath.Join(dir, "out", filepath.FromSlash(f.Path)))
		if err != nil {
			t.Errorf("%s: new file without a golden file (go test -update)", f.Path)
			continue
		}
		delete(want, f.Path)
		if !bytes.Equal(exp, f.Content) {
			t.Errorf("%s differs:\n--- expected\n%s\n--- generated\n%s", f.Path, exp, f.Content)
		}
	}
	for p := range want {
		t.Errorf("%s is no longer generated", p)
	}
}
