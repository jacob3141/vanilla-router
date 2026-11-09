// SPDX-License-Identifier: AGPL-3.0-or-later

package home

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	gen2Identity = `{"name":null,"id":"shellyplus2pm-020000000010","mac":"020000000010","slot":1,"model":"SNSW-102P16EU","gen":2,"ver":"1.7.5","app":"Plus2PM","auth_en":false,"profile":"cover"}`
	gen1Identity = `{"type":"SHSW-25","mac":"020000000020","auth":false,"fw":"20230913-112234/v1.14.0-gcb84623","num_outputs":2,"mode":"roller"}`
	cover        = `{"cover:0":{"id":0,"state":"open","apower":0.0,"aenergy":{"total":2369.361},"current_pos":100},"input:0":{"id":0,"state":null},"sys":{"uptime":1}}`
	switches     = `{"input:0":{"id":0,"state":true},"switch:0":{"id":0,"output":false,"apower":0.0,"aenergy":{"total":3099.981}},"switch:1":{"id":1,"output":true,"apower":12.5,"aenergy":{"total":14.0}},"sys":{}}`
	pmMini       = `{"pm1:0":{"id":0,"apower":2072.7,"aenergy":{"total":642133.329}},"sys":{}}`
	threePhase   = `{"em:0":{"id":0,"a_act_power":2044.0,"total_act_power":4264.024},"emdata:0":{"id":0,"total_act":11771109.71,"total_act_ret":1697571.13},"modbus":{},"sys":{}}`
)

func TestIdentity(t *testing.T) {
	a, id, m, fw, _, err := parseIdentity([]byte(gen2Identity))
	if err != nil || a != adapterShelly2 || id != "shelly-020000000010" || m != "Plus2PM" || fw != "1.7.5" {
		t.Errorf("%s %s %s %s %v", a, id, m, fw, err)
	}
	a, _, m, fw, _, err = parseIdentity([]byte(gen1Identity))
	if err != nil || a != adapterShelly1 || m != "SHSW-25" || fw != "v1.14.0-gcb84623" {
		t.Errorf("%s %s %s %v", a, m, fw, err)
	}
	for _, bad := range []string{`{}`, `{"hello":"world"}`, `not json`, `{"mac":"12","gen":2}`} {
		if _, _, _, _, _, err := parseIdentity([]byte(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestGen2(t *testing.T) {
	e, err := parseGen2([]byte(cover))
	if err != nil || len(e) != 1 || e[0].Kind != KindCover || e[0].State != "open" || *e[0].Position != 100 || *e[0].Energy != 2369.361 {
		t.Fatalf("%+v %v", e, err)
	}
	e, _ = parseGen2([]byte(switches))
	if len(e) != 3 || e[0].Kind != KindBinary || !*e[0].On || e[1].ID != "switch:0" || *e[1].On || !*e[2].On || *e[2].Power != 12.5 {
		t.Fatalf("%+v", e)
	}
	e, _ = parseGen2([]byte(pmMini))
	if len(e) != 1 || e[0].Kind != KindPower || *e[0].Power != 2072.7 {
		t.Fatalf("%+v", e)
	}
	e, _ = parseGen2([]byte(threePhase))
	if len(e) != 1 || *e[0].Power != 4264.024 || *e[0].Energy != 11771109.71 {
		t.Fatalf("%+v", e)
	}
	if _, err := parseGen2([]byte(`[`)); err == nil {
		t.Error("broken answer accepted")
	}
}

func TestGen1(t *testing.T) {
	e, err := parseGen1([]byte(`{"relays":[{"ison":false},{"ison":false}],"rollers":[{"state":"stop","power":0.0,"current_pos":40}],"meters":[{"power":0.0}]}`))
	if err != nil || len(e) != 1 || e[0].Kind != KindCover || e[0].State != "stopped" || *e[0].Position != 40 {
		t.Fatalf("%+v %v", e, err)
	}
	e, _ = parseGen1([]byte(`{"relays":[{"ison":true}],"meters":[{"power":7.5}]}`))
	if len(e) != 1 || !*e[0].On || *e[0].Power != 7.5 {
		t.Fatalf("%+v", e)
	}
	e, _ = parseGen1([]byte(`{"lights":[{"ison":true,"brightness":50,"power":3.0},{"ison":false,"brightness":10}]}`))
	if len(e) != 2 || e[0].Kind != KindLight || *e[0].Level != 50 || *e[1].On {
		t.Fatalf("%+v", e)
	}
}

func TestStore(t *testing.T) {
	p := filepath.Join(t.TempDir(), "home.json")
	s, err := OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.SetRoom("", "Kitchen")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetRoom("", "kitchen"); err == nil {
		t.Error("same name accepted")
	}
	for _, bad := range []string{"", " x", "a\nb", strings.Repeat("x", 65)} {
		if _, err := s.SetRoom("", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := s.SetDevice("d1", "Spots", r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDevice("d2", "x", "nope"); err == nil {
		t.Error("unknown room accepted")
	}
	s2, err := OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Device("d1"); got.Name != "Spots" || got.Room != r.ID {
		t.Errorf("%+v", got)
	}
	if err := s2.DeleteRoom(r.ID); err != nil {
		t.Fatal(err)
	}
	if got := s2.Device("d1"); got.Room != "" || got.Name != "Spots" {
		t.Errorf("after delete: %+v", got)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %o", fi.Mode().Perm())
	}
	os.WriteFile(p, []byte(`{"schema":1,"rooms":[],"devices":{},"extra":1}`), 0o600)
	if _, err := OpenStore(p); err == nil {
		t.Error("unknown field accepted")
	}
}

// A device that answers, one that is something else, one that disappears.
func TestRound(t *testing.T) {
	var fail bool
	dev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "gone", 500)
			return
		}
		switch r.URL.Path {
		case "/shelly":
			fmt.Fprint(w, gen2Identity)
		case "/rpc/Shelly.GetStatus":
			fmt.Fprint(w, cover)
		}
	}))
	defer dev.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>") }))
	defer other.Close()

	st, _ := OpenStore(filepath.Join(t.TempDir(), "home.json"))
	s := &Service{Store: st}
	s.init()
	targets := []Target{{Host: strings.TrimPrefix(dev.URL, "http://"), Name: "bathroom-blind"}, {Host: strings.TrimPrefix(other.URL, "http://"), Name: "printer"}}
	ctx := context.Background()
	s.round(ctx, targets)
	o := s.Overview()
	if len(o.Devices) != 1 || o.Devices[0].Name != "bathroom-blind" || !o.Devices[0].Online || o.Devices[0].Entities[0].Kind != KindCover {
		t.Fatalf("%+v", o)
	}
	if err := st.SetDevice(o.Devices[0].ID, "Blind", ""); err != nil {
		t.Fatal(err)
	}
	if got := s.Overview().Devices[0].Name; got != "Blind" {
		t.Errorf("name %q", got)
	}
	fail = true
	for range offlineAfter {
		s.round(ctx, targets)
	}
	o = s.Overview()
	if o.Devices[0].Online || len(o.Devices[0].Entities) != 1 {
		t.Errorf("%+v", o.Devices[0])
	}
}

// commandDevice answers like a Shelly and records the paths it was asked for.
func commandDevice(t *testing.T, identity, status string) (*httptest.Server, *[]string) {
	t.Helper()
	var got []string
	dev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/shelly":
			fmt.Fprint(w, identity)
		case "/rpc/Shelly.GetStatus", "/status":
			fmt.Fprint(w, status)
		default:
			got = append(got, r.URL.RequestURI())
			fmt.Fprint(w, "{}")
		}
	}))
	t.Cleanup(dev.Close)
	return dev, &got
}

func serviceWith(t *testing.T, devs ...*httptest.Server) *Service {
	t.Helper()
	st, _ := OpenStore(filepath.Join(t.TempDir(), "home.json"))
	s := &Service{Store: st}
	s.init()
	var targets []Target
	for _, d := range devs {
		targets = append(targets, Target{Host: strings.TrimPrefix(d.URL, "http://"), Name: "d"})
	}
	s.round(context.Background(), targets)
	return s
}

func TestCommandsGen2(t *testing.T) {
	dev, got := commandDevice(t, gen2Identity, `{"switch:1":{"id":1,"output":false},"cover:0":{"id":0,"state":"open","current_pos":100},"input:0":{"id":0,"state":true},"pm1:0":{"id":0,"apower":1}}`)
	s := serviceWith(t, dev)
	id := s.Overview().Devices[0].ID
	ctx := context.Background()
	for _, c := range [][3]string{{"switch:1", ActionOn, "/rpc/Switch.Set?id=1&on=true"}, {"switch:1", ActionOff, "/rpc/Switch.Set?id=1&on=false"},
		{"cover:0", ActionOpen, "/rpc/Cover.Open?id=0"}, {"cover:0", ActionClose, "/rpc/Cover.Close?id=0"}, {"cover:0", ActionStop, "/rpc/Cover.Stop?id=0"}} {
		*got = nil
		if err := s.Command(ctx, id, c[0], c[1], nil); err != nil {
			t.Fatalf("%v: %v", c, err)
		}
		if len(*got) != 1 || (*got)[0] != c[2] {
			t.Errorf("%v: %v", c, *got)
		}
	}
	*got = nil
	for _, c := range [][3]string{{id, "switch:1", "open"}, {id, "switch:1", "reboot"}, {id, "cover:0", ActionOn}, {id, "input:0", ActionOn}, {id, "pm1:0", ActionOff},
		{id, "switch:9", ActionOn}, {"nobody", "switch:1", ActionOn}, {id, "switch:1&on=true", ActionOn}} {
		if err := s.Command(ctx, c[0], c[1], c[2], nil); err == nil || !errors.Is(err, ErrInvalid) {
			t.Errorf("%v accepted: %v", c, err)
		}
	}
	if len(*got) != 0 {
		t.Errorf("a refused command reached the device: %v", *got)
	}
}

func TestCommandsGen1(t *testing.T) {
	relay, gr := commandDevice(t, `{"type":"SHSW-1","mac":"020000000030","auth":false,"fw":"x/v1"}`, `{"relays":[{"ison":false}]}`)
	roller, gc := commandDevice(t, gen1Identity, `{"relays":[{"ison":false}],"rollers":[{"state":"stop","current_pos":40}]}`)
	color, gl := commandDevice(t, `{"type":"SHRGBW2","mac":"020000000040","auth":false,"fw":"x/v1"}`, `{"mode":"color","lights":[{"ison":true,"gain":50}]}`)
	white, gw := commandDevice(t, `{"type":"SHRGBW2","mac":"020000000050","auth":false,"fw":"x/v1"}`, `{"mode":"white","lights":[{"ison":true},{"ison":false}]}`)
	s := serviceWith(t, relay, roller, color, white)
	ctx := context.Background()
	for _, c := range []struct {
		id, entity, action, want string
		got                      *[]string
	}{
		{"shelly-020000000030", "relay:0", ActionOn, "/relay/0?turn=on", gr},
		{"shelly-020000000020", "roller:0", ActionOpen, "/roller/0?go=open", gc},
		{"shelly-020000000040", "light:0", ActionOff, "/color/0?turn=off", gl},
		{"shelly-020000000050", "light:1", ActionOn, "/white/1?turn=on", gw},
	} {
		if err := s.Command(ctx, c.id, c.entity, c.action, nil); err != nil {
			t.Fatalf("%v: %v", c.id, err)
		}
		if len(*c.got) != 1 || (*c.got)[0] != c.want {
			t.Errorf("%s: %v", c.id, *c.got)
		}
	}
}

func TestCommandProtectedDevice(t *testing.T) {
	id := strings.Replace(gen2Identity, `"auth_en":false`, `"auth_en":true`, 1)
	dev, got := commandDevice(t, id, `{"switch:0":{"id":0,"output":false}}`)
	s := serviceWith(t, dev)
	if err := s.Command(context.Background(), s.Overview().Devices[0].ID, "switch:0", ActionOn, nil); err == nil || !strings.Contains(err.Error(), "password") {
		t.Errorf("%v", err)
	}
	if len(*got) != 0 {
		t.Errorf("%v", *got)
	}
}

func TestCommandsWithValue(t *testing.T) {
	n := func(v int) *int { return &v }
	gen2, g2 := commandDevice(t, gen2Identity, `{"light:0":{"id":0,"output":true,"brightness":40},"rgb:1":{"id":1,"output":true,"brightness":10},"cover:0":{"id":0,"state":"open","current_pos":100},"cover:1":{"id":1,"state":"stopped"},"switch:0":{"id":0,"output":true}}`)
	roller, gr := commandDevice(t, gen1Identity, `{"rollers":[{"state":"stop","current_pos":40}]}`)
	color, gc := commandDevice(t, `{"type":"SHRGBW2","mac":"020000000040","auth":false,"fw":"x/v1"}`, `{"mode":"color","lights":[{"ison":true,"gain":50}]}`)
	white, gw := commandDevice(t, `{"type":"SHRGBW2","mac":"020000000050","auth":false,"fw":"x/v1"}`, `{"mode":"white","lights":[{"ison":true,"brightness":30}]}`)
	s := serviceWith(t, gen2, roller, color, white)
	ctx := context.Background()
	const g = "shelly-020000000010"
	for _, c := range []struct {
		id, entity, action string
		value              int
		want               string
		got                *[]string
	}{
		{g, "light:0", ActionLevel, 55, "/rpc/Light.Set?id=0&on=true&brightness=55", g2},
		{g, "rgb:1", ActionLevel, 100, "/rpc/RGB.Set?id=1&on=true&brightness=100", g2},
		{g, "cover:0", ActionPos, 0, "/rpc/Cover.GoToPosition?id=0&pos=0", g2},
		{"shelly-020000000020", "roller:0", ActionPos, 70, "/roller/0?go=to_pos&roller_pos=70", gr},
		{"shelly-020000000040", "light:0", ActionLevel, 20, "/color/0?turn=on&gain=20", gc},
		{"shelly-020000000050", "light:0", ActionLevel, 80, "/white/0?turn=on&brightness=80", gw},
	} {
		*c.got = nil
		if err := s.Command(ctx, c.id, c.entity, c.action, n(c.value)); err != nil {
			t.Fatalf("%v: %v", c, err)
		}
		if len(*c.got) != 1 || (*c.got)[0] != c.want {
			t.Errorf("%s %s: %v", c.id, c.entity, *c.got)
		}
	}
	*g2 = nil
	for _, c := range []struct {
		entity, action string
		value          *int
	}{
		{"light:0", ActionLevel, n(0)}, {"light:0", ActionLevel, n(101)}, {"light:0", ActionLevel, nil}, {"cover:0", ActionPos, n(-1)},
		{"cover:0", ActionPos, n(101)}, {"cover:0", ActionPos, nil}, {"switch:0", ActionLevel, n(50)}, {"light:0", ActionPos, n(50)},
		{"cover:1", ActionPos, n(50)}, {"switch:0", ActionOn, n(1)},
	} {
		if err := s.Command(ctx, g, c.entity, c.action, c.value); err == nil || !errors.Is(err, ErrInvalid) {
			t.Errorf("%v accepted: %v", c, err)
		}
	}
	if len(*g2) != 0 {
		t.Errorf("a refused command reached the device: %v", *g2)
	}
	for _, d := range s.Overview().Devices {
		if d.Model == "SHRGBW2" && d.Entities[0].Level == nil {
			t.Errorf("no level from %s", d.ID)
		}
	}
}
