// SPDX-License-Identifier: AGPL-3.0-or-later

// Package apagent runs on an access point (docs/sdn-concept.md 3.1, 4.5). It pairs with the router, keeps fetching
// the configuration the router renders for this AP and applies it with a rollback that does not depend on the
// router: if a change cuts the connection, a systemd timer restores the previous state.
package apagent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"vanilla-router/internal/apd"
	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
	"vanilla-router/internal/pairing"
)

// DefaultDir is the agent's state on an AP: its key, the router's key and the current and previous bundle (0700).
const DefaultDir = "/var/lib/vanilla-router/ap-agent"

// RollbackUnit is the systemd timer that restores the previous bundle.
const RollbackUnit = "vr-ap-rollback"

// Commands runs programs. The tests replace it.
type Commands interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// Agent is the AP's side.
type Agent struct {
	Dir      string // default DefaultDir
	Root     string // prefix for the written files, empty on a real AP
	Cmd      Commands
	Routers  func() []string // host:port to try, in order
	Cards    func(context.Context) []gen.RadioCaps
	Hostname string
	Model    string
	MAC      string
	Version  string
	Show     func(string) // shows a line on the AP's screen
	Log      *slog.Logger

	Retry         time.Duration // pause after an error, default 15 s
	Settle        time.Duration // time the radios get before the health check, default 10 s
	RollbackAfter time.Duration // deadline of the timer, default 3 min

	mu     sync.Mutex
	seen   ed25519.PublicKey // the router's key during pairing
	router string            // the address that answered last
}

// ErrRevoked ends the work of an AP the router no longer knows. Run starts over with pairing.
var ErrRevoked = errors.New("removed from the network by the router")

func (a *Agent) dir() string {
	if a.Dir == "" {
		return DefaultDir
	}
	return a.Dir
}

func (a *Agent) retry() time.Duration {
	if a.Retry == 0 {
		return 15 * time.Second
	}
	return a.Retry
}

func (a *Agent) log() *slog.Logger {
	if a.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return a.Log
}

func (a *Agent) show(s string) {
	if a.Show != nil {
		a.Show(s)
	}
}

// key returns this AP's identity and creates it on first use.
func (a *Agent) key() (ed25519.PrivateKey, error) {
	p := filepath.Join(a.dir(), "key")
	if b, err := os.ReadFile(p); err == nil {
		seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("%s is damaged", p)
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	_, priv, err := pairing.NewKey()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(a.dir(), 0o700); err != nil {
		return nil, err
	}
	return priv, config.WriteFileAtomic(p, []byte(hex.EncodeToString(priv.Seed())+"\n"), 0o600)
}

func (a *Agent) routerKey() ed25519.PublicKey {
	b, err := os.ReadFile(filepath.Join(a.dir(), "router.pub"))
	if err != nil {
		return nil
	}
	k, err := pairing.ParseKey(strings.TrimSpace(string(b)))
	if err != nil {
		return nil
	}
	return k
}

// client talks TLS 1.3. With pin set only that key is accepted, without it the key is remembered for the code.
func (a *Agent) client(priv ed25519.PrivateKey, pin ed25519.PublicKey) (*http.Client, error) {
	cert, err := apd.Certificate(priv)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13,
		InsecureSkipVerify: true, // no CA: VerifyConnection below compares the key itself
		VerifyConnection: func(cs tls.ConnectionState) error {
			k, err := apd.PeerKey(cs)
			if err != nil {
				return err
			}
			if pin != nil {
				if !k.Equal(pin) {
					return errors.New("the router's key has changed")
				}
				return nil
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.seen != nil && !a.seen.Equal(k) {
				return errors.New("the router's key changed during pairing")
			}
			a.seen = k
			return nil
		},
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: cfg, DisableKeepAlives: true}, Timeout: 75 * time.Second}, nil
}

// do sends one request. out may be nil. It returns the HTTP status.
func do(ctx context.Context, c *http.Client, method, url string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return 0, err
	}
	res, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK && out != nil {
		return res.StatusCode, json.NewDecoder(io.LimitReader(res.Body, 1<<21)).Decode(out)
	}
	io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	return res.StatusCode, nil
}

// findRouter asks the candidates until a vanilla-router answers.
func (a *Agent) findRouter(ctx context.Context, c *http.Client) (string, error) {
	var last error = errors.New("no router found")
	for _, host := range a.Routers() {
		var h apd.Hello
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		st, err := do(cctx, c, "GET", "https://"+host+"/v1/hello", nil, &h)
		cancel()
		if err != nil {
			last = err
			continue
		}
		if st == http.StatusOK && h.Role == "vanilla-router" {
			a.mu.Lock()
			a.router = host
			a.mu.Unlock()
			return host, nil
		}
	}
	return "", last
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// Pair runs until the owner has accepted this AP (concept 4.3). A rejected AP starts again with a new code.
func (a *Agent) Pair(ctx context.Context) error {
	priv, err := a.key()
	if err != nil {
		return err
	}
	for {
		err := a.pairOnce(ctx, priv)
		if err == nil || ctx.Err() != nil {
			return err
		}
		a.log().Warn("pairing", "err", err)
		a.show("Looking for the router …")
		if err := sleep(ctx, a.retry()); err != nil {
			return err
		}
	}
}

func (a *Agent) pairOnce(ctx context.Context, priv ed25519.PrivateKey) error {
	a.mu.Lock()
	a.seen = nil
	a.mu.Unlock()
	c, err := a.client(priv, nil)
	if err != nil {
		return err
	}
	host, err := a.findRouter(ctx, c)
	if err != nil {
		return err
	}
	base := "https://" + host
	nonce, err := pairing.Nonce()
	if err != nil {
		return err
	}
	var cards []gen.RadioCaps
	if a.Cards != nil {
		cards = a.Cards(ctx)
	}
	var reply apd.PairCommitReply
	st, err := do(ctx, c, "POST", base+"/v1/pair/commit", apd.PairCommit{Commitment: hex.EncodeToString(pairing.Commit(nonce)),
		MAC: a.MAC, Hostname: a.Hostname, Model: a.Model, Radios: cards}, &reply)
	if err != nil {
		return err
	}
	if st != http.StatusOK {
		return fmt.Errorf("the router refused the pairing (%d)", st)
	}
	routerNonce, err := hex.DecodeString(reply.RouterNonce)
	if err != nil {
		return err
	}
	if st, err = do(ctx, c, "POST", base+"/v1/pair/reveal", apd.PairReveal{APNonce: hex.EncodeToString(nonce)}, nil); err != nil {
		return err
	}
	if st != http.StatusOK {
		return fmt.Errorf("the router refused the pairing (%d)", st)
	}
	a.mu.Lock()
	router := a.seen
	a.mu.Unlock()
	code, err := pairing.Code(router, priv.Public().(ed25519.PublicKey), routerNonce, nonce)
	if err != nil {
		return err
	}
	a.show("Open the router's web interface and confirm this access point. Code: " + code)
	for {
		var s apd.PairStatus
		if st, err = do(ctx, c, "GET", base+"/v1/pair/status", nil, &s); err == nil && st == http.StatusOK {
			switch s.State {
			case apd.PairPaired:
				if err := config.WriteFileAtomic(filepath.Join(a.dir(), "router.pub"), []byte(pairing.FormatKey(router)+"\n"), 0o600); err != nil {
					return err
				}
				a.show("Paired with the router.")
				return nil
			case apd.PairUnknown:
				return errors.New("the request was rejected or has expired")
			}
		}
		if err := sleep(ctx, 3*time.Second); err != nil {
			return err
		}
	}
}

// Run is the whole life of the agent: pair if necessary, then follow the router.
func (a *Agent) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		if a.routerKey() == nil {
			a.show("Looking for the router …")
			if err := a.Pair(ctx); err != nil {
				return err
			}
		}
		err := a.follow(ctx)
		if errors.Is(err, ErrRevoked) {
			a.log().Warn("removed by the router, deleting keys and Wi-Fi settings")
			if err := a.Wipe(ctx); err != nil {
				a.log().Error("wipe", "err", err)
			}
			continue
		}
		if err != nil && ctx.Err() == nil {
			return err
		}
	}
	return ctx.Err()
}

// A router that restores an old configuration may forget an AP for a moment, so one answer is not enough.
const revokedAfter = 3

func (a *Agent) follow(ctx context.Context) error {
	priv, err := a.key()
	if err != nil {
		return err
	}
	c, err := a.client(priv, a.routerKey())
	if err != nil {
		return err
	}
	revoked := 0
	lastErr := ""
	for {
		host, err := a.findRouter(ctx, c)
		if err != nil {
			// The router is away: the AP keeps offering Wi-Fi with its last configuration (concept 4.5)
			a.log().Info("router not reachable", "err", err)
			if err := sleep(ctx, a.retry()); err != nil {
				return err
			}
			continue
		}
		cur, _ := a.load("current.json")
		var b apd.Bundle
		st, err := do(ctx, c, "GET", "https://"+host+"/v1/bundle?have="+cur.Generation, nil, &b)
		if err != nil {
			a.log().Warn("bundle", "err", err)
			if err := sleep(ctx, a.retry()); err != nil {
				return err
			}
			continue
		}
		switch st {
		case http.StatusForbidden:
			if revoked++; revoked >= revokedAfter {
				return ErrRevoked
			}
			if err := sleep(ctx, a.retry()); err != nil {
				return err
			}
			continue
		case http.StatusOK:
			revoked = 0
			lastErr = ""
			if err := a.Apply(ctx, b, c, host); err != nil {
				a.log().Error("change rolled back", "generation", b.Generation, "err", err)
				lastErr = err.Error()
			}
		case http.StatusNoContent:
			revoked = 0
		default:
			if err := sleep(ctx, a.retry()); err != nil {
				return err
			}
			continue
		}
		if err := a.report(ctx, c, host, lastErr); err != nil {
			a.log().Warn("report", "err", err)
		}
		if lastErr != "" {
			// Not at once again: the router offers the same bundle until the owner changes something
			if err := sleep(ctx, 5*time.Minute); err != nil {
				return err
			}
		}
	}
}

func (a *Agent) report(ctx context.Context, c *http.Client, host, errText string) error {
	cur, _ := a.load("current.json")
	r := apd.Report{Applied: cur.Generation, Version: a.Version, Clients: a.clients(ctx, cur), Error: errText}
	if a.Cards != nil {
		r.Radios = a.Cards(ctx)
	}
	st, err := do(ctx, c, "POST", "https://"+host+"/v1/report", r, nil)
	if err != nil {
		return err
	}
	if st != http.StatusNoContent {
		return fmt.Errorf("status %d", st)
	}
	return nil
}

func (a *Agent) clients(ctx context.Context, b apd.Bundle) int {
	n := 0
	for _, f := range b.Files {
		base := path.Base(f.Path)
		if !strings.HasPrefix(base, "40-vr-ap-") {
			continue
		}
		ifname := strings.TrimSuffix(strings.TrimPrefix(base, "40-vr-ap-"), ".network")
		out, err := a.Cmd.Run(ctx, "iw", "dev", ifname, "station", "dump")
		if err == nil {
			n += strings.Count(out, "Station ")
		}
	}
	return n
}

// --- Applying ---------------------------------------------------------------------------

func (a *Agent) load(name string) (apd.Bundle, error) {
	var b apd.Bundle
	raw, err := os.ReadFile(filepath.Join(a.dir(), name))
	if err != nil {
		return b, err
	}
	return b, json.Unmarshal(raw, &b)
}

func (a *Agent) store(name string, b apd.Bundle) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(filepath.Join(a.dir(), name), raw, 0o600)
}

var (
	networkFile = regexp.MustCompile(`^/etc/systemd/network/[0-9]{2}-vr-ap-[a-zA-Z0-9]+(\.[a-z]+)?\.(netdev|network|link)$`)
	apFile      = regexp.MustCompile(`^/etc/vanilla-router/ap/[a-zA-Z0-9.-]+$`)
	hostapdFile = regexp.MustCompile(`/hostapd-([a-z0-9]+)\.conf$`)
	rxkhFile    = regexp.MustCompile(`/rxkh-([a-z0-9]+)\.conf$`)
)

// checkBundle refuses paths outside the AP's own files. The router is trusted, a bug in it should still not be able
// to write anywhere on this computer.
func checkBundle(b apd.Bundle) error {
	if b.Generation == "" {
		return errors.New("bundle without a generation")
	}
	for _, f := range b.Files {
		if !networkFile.MatchString(f.Path) && !apFile.MatchString(f.Path) {
			return fmt.Errorf("the bundle wants to write %q", f.Path)
		}
	}
	return nil
}

// Apply installs a bundle. A timer restores the previous state if this process does not get to the end.
func (a *Agent) Apply(ctx context.Context, b apd.Bundle, c *http.Client, host string) error {
	if err := checkBundle(b); err != nil {
		return err
	}
	old, _ := a.load("current.json")
	if err := a.store("previous.json", old); err != nil {
		return err
	}
	if err := a.store("current.json", b); err != nil {
		return err
	}
	after := a.RollbackAfter
	if after == 0 {
		after = 3 * time.Minute
	}
	a.Cmd.Run(ctx, "systemctl", "stop", RollbackUnit+".timer")
	if _, err := a.Cmd.Run(ctx, "systemd-run", "--unit="+RollbackUnit, fmt.Sprintf("--on-active=%d", int(after.Seconds())),
		"/usr/sbin/vrctl", "ap", "rollback"); err != nil {
		a.store("current.json", old)
		return fmt.Errorf("rollback timer: %w", err)
	}
	failure := a.activate(ctx, old, b)
	if failure == nil {
		failure = a.healthy(ctx, b, c, host)
	}
	if failure != nil {
		if err := a.activate(ctx, b, old); err != nil {
			failure = errors.Join(failure, err)
		}
		a.store("current.json", old)
	}
	a.Cmd.Run(ctx, "systemctl", "stop", RollbackUnit+".timer")
	return failure
}

// Rollback is what the timer calls: back to the previous bundle.
func (a *Agent) Rollback(ctx context.Context) error {
	cur, err := a.load("current.json")
	if err != nil {
		return err
	}
	prev, _ := a.load("previous.json")
	if err := a.activate(ctx, cur, prev); err != nil {
		return err
	}
	return a.store("current.json", prev)
}

func radioOf(p string) (string, bool) {
	m := hostapdFile.FindStringSubmatch(p)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func byPath(b apd.Bundle) map[string]gen.File {
	out := map[string]gen.File{}
	for _, f := range b.Files {
		out[f.Path] = f
	}
	return out
}

// activate replaces the files of from with those of to and touches only the services that are affected.
func (a *Agent) activate(ctx context.Context, from, to apd.Bundle) error {
	oldF, newF := byPath(from), byPath(to)
	var net, link bool
	restart := map[string]bool{}
	rxkhOnly := map[string]bool{}
	note := func(p string) {
		switch {
		case strings.HasSuffix(p, ".link"):
			link = true
		case strings.HasPrefix(p, "/etc/systemd/network/"):
			net = true
		}
		if r, ok := radioOf(p); ok {
			restart[r] = true
		}
		if m := rxkhFile.FindStringSubmatch(p); m != nil {
			rxkhOnly[m[1]] = true
		}
	}
	for p, f := range newF {
		if o, ok := oldF[p]; !ok || !bytes.Equal(o.Content, f.Content) || o.Mode != f.Mode {
			if err := config.WriteFileAtomic(a.Root+p, f.Content, os.FileMode(f.Mode)); err != nil {
				return err
			}
			note(p)
		}
	}
	for p := range oldF {
		if _, ok := newF[p]; !ok {
			if err := os.Remove(a.Root + p); err != nil && !os.IsNotExist(err) {
				return err
			}
			note(p)
		}
	}
	// Stop what is gone before the network changes
	for p := range oldF {
		if r, ok := radioOf(p); ok {
			if _, still := newF[p]; !still {
				a.Cmd.Run(ctx, "systemctl", "disable", "--now", "vr-ap-hostapd@"+r+".service")
				delete(restart, r)
			}
		}
	}
	if link {
		a.Cmd.Run(ctx, "udevadm", "control", "--reload")
		if _, err := a.Cmd.Run(ctx, "udevadm", "trigger", "--action=add", "--subsystem-match=net"); err != nil {
			return err
		}
	}
	if net {
		if _, err := a.Cmd.Run(ctx, "networkctl", "reload"); err != nil {
			return err
		}
	}
	var radios []string
	for r := range restart {
		radios = append(radios, r)
	}
	slices.Sort(radios)
	for _, r := range radios {
		if _, err := a.Cmd.Run(ctx, "systemctl", "enable", "vr-ap-hostapd@"+r+".service"); err != nil {
			return err
		}
		if _, err := a.Cmd.Run(ctx, "systemctl", "restart", "vr-ap-hostapd@"+r+".service"); err != nil {
			return err
		}
	}
	// New key lists without bringing the radio down: devices stay connected (concept 5.4)
	var lists []string
	for b := range rxkhOnly {
		if radio, _, _ := strings.Cut(b, "b"); !restart[radio] && !restart[b] {
			if _, exists := newF["/etc/vanilla-router/ap/rxkh-"+b+".conf"]; exists {
				lists = append(lists, b)
			}
		}
	}
	slices.Sort(lists)
	for _, b := range lists {
		if _, err := a.Cmd.Run(ctx, "hostapd_cli", "-p", "/run/hostapd", "-i", b, "raw", "RELOAD_RXKHS"); err != nil {
			radio, _, _ := strings.Cut(b, "b")
			if _, err := a.Cmd.Run(ctx, "systemctl", "restart", "vr-ap-hostapd@"+radio+".service"); err != nil {
				return err
			}
		}
	}
	return a.setPower(ctx, to)
}

// Transmit power in units of 1/100 dBm. "high" leaves the limit to the regulatory database.
var powerLevels = map[string]string{config.PowerLow: "1000", config.PowerMedium: "1700"}

func (a *Agent) setPower(ctx context.Context, b apd.Bundle) error {
	f, ok := byPath(b)["/etc/vanilla-router/ap/radios.json"]
	if !ok {
		return nil
	}
	var doc struct {
		Radios []struct {
			If    string `json:"interface"`
			Power string `json:"power"`
			Idle  bool   `json:"idle"`
		} `json:"radios"`
	}
	if err := json.Unmarshal(f.Content, &doc); err != nil {
		return err
	}
	for _, r := range doc.Radios {
		if r.Idle {
			continue
		}
		args := []string{"dev", r.If, "set", "txpower", "auto"}
		if mbm, ok := powerLevels[r.Power]; ok {
			args = []string{"dev", r.If, "set", "txpower", "limit", mbm}
		}
		// A card that does not allow it is no reason to give up the whole change
		if _, err := a.Cmd.Run(ctx, "iw", args...); err != nil {
			a.log().Warn("transmit power", "interface", r.If, "err", err)
		}
	}
	return nil
}

// healthy: hostapd runs on every card and the router is still reachable through the pinned connection.
func (a *Agent) healthy(ctx context.Context, b apd.Bundle, c *http.Client, host string) error {
	settle := a.Settle
	if settle == 0 {
		settle = 10 * time.Second
	}
	if err := sleep(ctx, settle); err != nil {
		return err
	}
	for _, f := range b.Files {
		if r, ok := radioOf(f.Path); ok {
			if _, err := a.Cmd.Run(ctx, "systemctl", "is-active", "--quiet", "vr-ap-hostapd@"+r+".service"); err != nil {
				return fmt.Errorf("hostapd on %s does not run", r)
			}
		}
	}
	var last error
	for range 6 {
		var h apd.Hello
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		st, err := do(cctx, c, "GET", "https://"+host+"/v1/hello", nil, &h)
		cancel()
		if err == nil && st == http.StatusOK {
			return nil
		}
		last = err
		if err := sleep(ctx, 5*time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("the router is not reachable after the change: %w", last)
}

// Wipe is what an AP does when the router removed it: no Wi-Fi, no passwords, no keys (concept 4.4).
func (a *Agent) Wipe(ctx context.Context) error {
	cur, _ := a.load("current.json")
	err := a.activate(ctx, cur, apd.Bundle{})
	for _, n := range []string{"key", "router.pub", "current.json", "previous.json"} {
		if e := os.Remove(filepath.Join(a.dir(), n)); e != nil && !os.IsNotExist(e) {
			err = errors.Join(err, e)
		}
	}
	a.show("This access point was removed from the network.")
	return err
}
