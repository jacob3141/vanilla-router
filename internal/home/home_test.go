// SPDX-License-Identifier: AGPL-3.0-or-later

package home

import (
	"context"
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
