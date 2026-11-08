// SPDX-License-Identifier: AGPL-3.0-or-later

package apagent

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"vanilla-router/internal/apd"
	"vanilla-router/internal/gen"
	"vanilla-router/internal/pairing"
)

// backend plays the helper for vr-apd.
type backend struct {
	mu       sync.Mutex
	router   ed25519.PublicKey
	paired   map[string]string
	requests []apd.AddPairing
	bundle   apd.Bundle
	reports  []apd.Report
}

func (b *backend) Call(_ context.Context, action string, params, out any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch action {
	case "ap.authorize":
		*out.(*apd.Authorized) = apd.Authorized{ID: b.paired[params.(apd.KeyParam).PublicKey]}
	case "ap.pairing.add":
		p := params.(apd.AddPairing)
		c, _ := hex.DecodeString(p.Commitment)
		n, _ := hex.DecodeString(p.APNonce)
		if !pairing.CheckCommit(c, n) {
			return errors.New("bad commitment")
		}
		b.requests = append(b.requests, p)
	case "ap.pairing.status":
		st := apd.PairWaiting
		if b.paired[params.(apd.KeyParam).PublicKey] != "" {
			st = apd.PairPaired
		}
		*out.(*apd.PairStatus) = apd.PairStatus{State: st}
	case "ap.bundle":
		*out.(*apd.Bundle) = b.bundle
	case "ap.report":
		b.reports = append(b.reports, params.(apd.ReportParam).Report)
	}
	return nil
}

func (b *backend) routerCode(t *testing.T) (string, string) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.requests) == 0 {
		return "", ""
	}
	r := b.requests[0]
	k, _ := pairing.ParseKey(r.PublicKey)
	nr, _ := hex.DecodeString(r.RouterNonce)
	na, _ := hex.DecodeString(r.APNonce)
	code, err := pairing.Code(b.router, k, nr, na)
	if err != nil {
		t.Fatal(err)
	}
	return code, r.PublicKey
}

type commands struct {
	mu   sync.Mutex
	log  []string
	fail map[string]bool
}

func (c *commands) Run(_ context.Context, name string, args ...string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	line := name + " " + strings.Join(args, " ")
	c.log = append(c.log, line)
	for f := range c.fail {
		if strings.Contains(line, f) {
			return "", errors.New("failed")
		}
	}
	if name == "iw" && strings.Contains(line, "station dump") {
		return "Station 02:00:00:00:00:41 (on wlr0)\nStation 02:00:00:00:00:42 (on wlr0)\n", nil
	}
	return "", nil
}

func (c *commands) saw(s string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.ContainsFunc(c.log, func(l string) bool { return strings.Contains(l, s) })
}

func (c *commands) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.log = nil
}

func setup(t *testing.T) (*Agent, *backend, *commands, *httptest.Server) {
	t.Helper()
	_, key, _ := pairing.NewKey()
	b := &backend{router: key.Public().(ed25519.PublicKey), paired: map[string]string{}}
	s := &apd.Server{Backend: b, Key: key, Version: "test", Poll: 5 * time.Millisecond, Wait: 100 * time.Millisecond}
	ts := httptest.NewUnstartedServer(s.Handler())
	cfg, err := s.TLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	ts.TLS = cfg
	ts.StartTLS()
	t.Cleanup(ts.Close)
	cmd := &commands{fail: map[string]bool{}}
	dir := t.TempDir()
	a := &Agent{Dir: filepath.Join(dir, "state"), Root: filepath.Join(dir, "root"), Cmd: cmd, Hostname: "hall", Model: "Mini PC", MAC: "02:00:00:00:00:10", Version: "test",
		Routers: func() []string { return []string{"127.0.0.1:1", strings.TrimPrefix(ts.URL, "https://")} },
		Cards: func(context.Context) []gen.RadioCaps {
			return []gen.RadioCaps{{ID: "pci-0000-03-00-0", Bands: []string{"5", "6"}}}
		},
		Retry: 5 * time.Millisecond, Settle: time.Millisecond, RollbackAfter: time.Minute}
	return a, b, cmd, ts
}

func bundle(gen_ string, files ...gen.File) apd.Bundle {
	return apd.Bundle{Generation: gen_, Files: files}
}

func conf(radio, content string) gen.File {
	return gen.File{Path: "/etc/vanilla-router/ap/hostapd-" + radio + ".conf", Mode: 0o600, Content: []byte(content)}
}

func rxkh(bss, content string) gen.File {
	return gen.File{Path: "/etc/vanilla-router/ap/rxkh-" + bss + ".conf", Mode: 0o600, Content: []byte(content)}
}

// pairNow lets the agent pair and answers like the owner would.
func pairNow(t *testing.T, ctx context.Context, a *Agent, b *backend) string {
	t.Helper()
	var shown sync.Map
	a.Show = func(s string) { shown.Store("last", s) }
	done := make(chan error, 1)
	go func() { done <- a.Pair(ctx) }()
	var code, key string
	for i := 0; i < 500 && code == ""; i++ {
		code, key = b.routerCode(t)
		time.Sleep(5 * time.Millisecond)
	}
	if code == "" {
		t.Fatal("no request arrived")
	}
	var last any
	for i := 0; i < 500; i++ {
		if last, _ = shown.Load("last"); last != nil && strings.Contains(last.(string), "Code:") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.HasSuffix(last.(string), code) {
		t.Fatalf("the AP shows %q, the router computes %q", last, code)
	}
	b.mu.Lock()
	b.paired[key] = "ap1"
	b.mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pairing did not end")
	}
	return key
}

func TestPairAndFollow(t *testing.T) {
	a, b, cmd, _ := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pairNow(t, ctx, a, b)
	if a.routerKey() == nil || !a.routerKey().Equal(b.router) {
		t.Fatal("router key not pinned")
	}
	b.mu.Lock()
	b.bundle = bundle("g1", conf("wlr0", "one"), rxkh("wlr0", "keys 1"),
		gen.File{Path: "/etc/systemd/network/20-vr-ap-br0.netdev", Mode: 0o644, Content: []byte("[NetDev]")},
		gen.File{Path: "/etc/systemd/network/30-vr-ap-wlr0.link", Mode: 0o644, Content: []byte("[Link]")},
		gen.File{Path: "/etc/vanilla-router/ap/radios.json", Mode: 0o644, Content: []byte(`{"radios":[{"interface":"wlr0","power":"medium"}]}`)})
	b.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- a.follow(ctx) }()
	waitFor(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return len(b.reports) > 0 && b.reports[len(b.reports)-1].Applied == "g1"
	})
	got, err := os.ReadFile(a.Root + "/etc/vanilla-router/ap/hostapd-wlr0.conf")
	if err != nil || string(got) != "one" {
		t.Fatalf("%q %v", got, err)
	}
	if fi, _ := os.Stat(a.Root + "/etc/vanilla-router/ap/hostapd-wlr0.conf"); fi.Mode().Perm() != 0o600 {
		t.Error("hostapd.conf must be 0600")
	}
	for _, want := range []string{"systemd-run --unit=vr-ap-rollback --on-active=60", "networkctl reload", "udevadm trigger", "systemctl enable vr-ap-hostapd@wlr0",
		"systemctl restart vr-ap-hostapd@wlr0", "is-active --quiet vr-ap-hostapd@wlr0", "iw dev wlr0 set txpower limit 1700", "systemctl stop vr-ap-rollback.timer"} {
		if !cmd.saw(want) {
			t.Errorf("missing: %s\n%v", want, cmd.log)
		}
	}

	// Only the key list changes: no restart, devices stay connected
	cmd.reset()
	b.mu.Lock()
	files := slices.Clone(b.bundle.Files)
	files[1] = rxkh("wlr0", "keys 2")
	b.bundle = bundle("g2", files...)
	b.mu.Unlock()
	waitFor(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.reports[len(b.reports)-1].Applied == "g2"
	})
	if !cmd.saw("RELOAD_RXKHS") || cmd.saw("systemctl restart") {
		t.Errorf("%v", cmd.log)
	}

	// Removed by the router: after three answers keys and Wi-Fi settings are gone
	b.mu.Lock()
	b.paired = map[string]string{}
	b.mu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, ErrRevoked) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not revoked")
	}
	if err := a.Wipe(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.Root + "/etc/vanilla-router/ap/hostapd-wlr0.conf"); !os.IsNotExist(err) {
		t.Error("password file left behind")
	}
	if a.routerKey() != nil {
		t.Error("router key left behind")
	}
	if _, err := os.Stat(filepath.Join(a.Dir, "key")); !os.IsNotExist(err) {
		t.Error("AP key left behind")
	}
	if !cmd.saw("disable --now vr-ap-hostapd@wlr0") {
		t.Error("hostapd keeps running")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for range 1000 {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timeout")
}

func TestApplyFailureRollsBack(t *testing.T) {
	a, b, cmd, ts := setup(t)
	ctx := context.Background()
	pairNow(t, ctx, a, b)
	priv, _ := a.key()
	c, err := a.client(priv, a.routerKey())
	if err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(ts.URL, "https://")
	if err := a.Apply(ctx, bundle("g1", conf("wlr0", "good")), c, host); err != nil {
		t.Fatal(err)
	}
	cmd.fail["is-active"] = true
	err = a.Apply(ctx, bundle("g2", conf("wlr0", "bad"), conf("wlr1", "new")), c, host)
	if err == nil || !strings.Contains(err.Error(), "does not run") {
		t.Fatalf("%v", err)
	}
	got, _ := os.ReadFile(a.Root + "/etc/vanilla-router/ap/hostapd-wlr0.conf")
	if string(got) != "good" {
		t.Errorf("old file not restored: %q", got)
	}
	if _, err := os.Stat(a.Root + "/etc/vanilla-router/ap/hostapd-wlr1.conf"); !os.IsNotExist(err) {
		t.Error("new file left behind")
	}
	if cur, _ := a.load("current.json"); cur.Generation != "g1" {
		t.Errorf("current is %s", cur.Generation)
	}
}

func TestTimerRollback(t *testing.T) {
	a, b, _, ts := setup(t)
	ctx := context.Background()
	pairNow(t, ctx, a, b)
	priv, _ := a.key()
	c, _ := a.client(priv, a.routerKey())
	host := strings.TrimPrefix(ts.URL, "https://")
	a.Apply(ctx, bundle("g1", conf("wlr0", "good")), c, host)
	a.Apply(ctx, bundle("g2", conf("wlr0", "changed")), c, host)
	if err := a.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(a.Root + "/etc/vanilla-router/ap/hostapd-wlr0.conf")
	if string(got) != "good" {
		t.Errorf("%q", got)
	}
}

func TestBundlePathsChecked(t *testing.T) {
	for _, p := range []string{"/etc/passwd", "/etc/vanilla-router/ap/../../passwd", "/etc/systemd/network/99-other.network", "/etc/systemd/network/40-vr-ap-x/../../y.network"} {
		if err := checkBundle(bundle("g", gen.File{Path: p})); err == nil {
			t.Errorf("%s accepted", p)
		}
	}
	if err := checkBundle(bundle("g", conf("wlr0", "x"), gen.File{Path: "/etc/systemd/network/40-vr-ap-wlr1b1.network"}, gen.File{Path: "/etc/systemd/network/20-vr-ap-br0.netdev"})); err != nil {
		t.Error(err)
	}
}

// Another key answering at the router's address is refused.
func TestPinnedKey(t *testing.T) {
	a, b, _, ts := setup(t)
	ctx := context.Background()
	pairNow(t, ctx, a, b)
	other, _, _ := pairing.NewKey()
	priv, _ := a.key()
	c, _ := a.client(priv, other)
	if _, err := do(ctx, c, "GET", ts.URL+"/v1/hello", nil, nil); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("%v", err)
	}
}
