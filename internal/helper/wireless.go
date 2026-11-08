// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
	"vanilla-router/internal/pairing"
)

// Access points with the router as their center (docs/sdn-concept.md). The helper keeps the wireless block in
// config.json and the router's identity. vr-apd talks to the APs and brings pairing requests and reports through the
// actions in wireless_ap.go, which keep them in the folders below. The test instance (--fake-wifi) adds simulated APs.
const (
	// apPairingDir holds pairing requests, one JSON file each (PairingRequest). Volatile: a request lives only
	// while the AP's installation waits.
	apPairingDir = "/run/vanilla-router/ap-pairing"
	// apOnlineDir holds the last report of each connected AP (APReport). Volatile as well.
	apOnlineDir = "/run/vanilla-router/ap-online"
	// apCapsDir keeps what each paired AP reported about its cards, so the router can plan and render while it is away.
	apCapsDir = "/var/lib/vanilla-router/ap"
	// The router's identity (Ed25519 seed, hex) in the secrets folder (concept 4.2: one controller key).
	controllerRef = "ap-controller"
	wnetRef       = "wnet-"
	ftRef         = "ft-"
	// A request older than this is ignored: the AP's installation has given up or restarts with a new one.
	pairingMaxAge = 30 * time.Minute
)

// PairingRequest is stored through ap.pairing.add once the AP has revealed its random value (internal/pairing).
type PairingRequest struct {
	ID          string          `json:"id"`
	PublicKey   string          `json:"public_key"`
	MAC         string          `json:"mac"`
	Hostname    string          `json:"hostname"`
	Model       string          `json:"model"`
	Radios      []gen.RadioCaps `json:"radios"`
	RouterNonce string          `json:"router_nonce"`
	APNonce     string          `json:"ap_nonce"`
	Commitment  string          `json:"commitment"`
	Received    time.Time       `json:"received"`
}

// APReport is the last report of a connected AP.
type APReport struct {
	Seen    time.Time `json:"seen"`
	Version string    `json:"version"`
	Clients int       `json:"clients"`
	Applied string    `json:"applied"`         // generation of the bundle the AP runs
	Error   string    `json:"error,omitempty"` // why the last change was rolled back on the AP
}

// WirelessInfo is the response of wireless.get. Passwords and keys never leave the helper.
type WirelessInfo struct {
	Configured   bool             `json:"configured"`
	Country      string           `json:"country"`
	Segments     []config.Segment `json:"segments"`
	Networks     []WNetworkInfo   `json:"networks"`
	AccessPoints []APInfo         `json:"access_points"`
	Pairing      []PairingInfo    `json:"pairing"`
	Plan         gen.ChannelPlan  `json:"plan"`
	Router       string           `json:"router_fingerprint"`
	LocalWiFi    string           `json:"local_wifi_ssid,omitempty"`
	LANAddress   string           `json:"lan_address"`
	Countries    []WiFiCountry    `json:"countries"`
	Limits       map[string]int   `json:"limits"`
	Pending      *PendingInfo     `json:"pending"`
	DryRun       bool             `json:"dry_run"`
	Simulated    bool             `json:"simulated"`
}

// WNetworkInfo is a network without its secrets.
type WNetworkInfo struct {
	ID             string   `json:"id"`
	SSID           string   `json:"ssid"`
	Segment        string   `json:"segment"`
	Security       string   `json:"security"`
	Bands          []string `json:"bands"`
	Enabled        bool     `json:"enabled"`
	HasPassword    bool     `json:"has_password"`
	MobilityDomain string   `json:"mobility_domain,omitempty"`
}

// APInfo is a paired AP with what is known about it.
type APInfo struct {
	config.AccessPoint
	Fingerprint string          `json:"fingerprint"`
	Generation  string          `json:"generation"` // the bundle this AP should run
	Online      bool            `json:"online"`
	Report      *APReport       `json:"report"`
	Caps        []gen.RadioCaps `json:"caps"`
}

// PairingInfo is a waiting AP with the code the owner compares.
type PairingInfo struct {
	ID       string          `json:"id"`
	Code     string          `json:"code"`
	MAC      string          `json:"mac"`
	Hostname string          `json:"hostname"`
	Model    string          `json:"model"`
	Radios   []gen.RadioCaps `json:"radios"`
	Received time.Time       `json:"received"`
}

var pairingIDRe = safeName // 1–15 characters, letters, digits, - and _

// controllerKey returns the router's identity and creates it on first use.
func (s *Server) controllerKey() (ed25519.PrivateKey, error) {
	if v, err := s.secrets().Get(controllerRef); err == nil {
		seed, err := hex.DecodeString(strings.TrimSpace(v))
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("the router's access point key is damaged (%s)", controllerRef)
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	_, priv, err := pairing.NewKey()
	if err != nil {
		return nil, err
	}
	if err := s.secrets().Set(controllerRef, hex.EncodeToString(priv.Seed())); err != nil {
		return nil, err
	}
	return priv, nil
}

func readJSONFile[T any](path string) (T, error) {
	var v T
	b, err := os.ReadFile(path)
	if err != nil {
		return v, err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	return v, dec.Decode(&v)
}

// pairingRequests lists valid, fresh requests with their codes. Invalid ones (wrong commitment, bad key, already
// paired) are skipped: a person never sees a code that could not be right.
func (s *Server) pairingRequests(c *config.Config, router ed25519.PublicKey) []PairingInfo {
	out := []PairingInfo{}
	dir := s.Path(apPairingDir)
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !pairingIDRe.MatchString(id) {
			continue
		}
		r, err := readJSONFile[PairingRequest](filepath.Join(dir, e.Name()))
		if err != nil || r.ID != id || time.Since(r.Received) > pairingMaxAge {
			continue
		}
		code, err := pairingCode(r, router)
		if err != nil {
			s.cfg.Log.Warn("pairing request refused", "id", id, "err", err)
			continue
		}
		if c.Wireless != nil && slices.ContainsFunc(c.Wireless.AccessPoints, func(a config.AccessPoint) bool { return a.PublicKey == r.PublicKey }) {
			continue
		}
		out = append(out, PairingInfo{ID: r.ID, Code: code, MAC: r.MAC, Hostname: r.Hostname, Model: r.Model, Radios: r.Radios, Received: r.Received})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Received.Before(out[j].Received) })
	return out
}

func pairingCode(r PairingRequest, router ed25519.PublicKey) (string, error) {
	ap, err := pairing.ParseKey(r.PublicKey)
	if err != nil {
		return "", err
	}
	if _, err := config.NormalizeMAC(r.MAC); err != nil {
		return "", err
	}
	nr, err1 := hex.DecodeString(r.RouterNonce)
	na, err2 := hex.DecodeString(r.APNonce)
	cm, err3 := hex.DecodeString(r.Commitment)
	if err1 != nil || err2 != nil || err3 != nil {
		return "", fmt.Errorf("invalid random values")
	}
	if !pairing.CheckCommit(cm, na) {
		return "", fmt.Errorf("the AP's random value does not match its commitment")
	}
	for _, rc := range r.Radios {
		if !rc.Valid() {
			return "", fmt.Errorf("invalid card %q", rc.ID)
		}
	}
	return pairing.Code(router, ap, nr, na)
}

// apCaps reads the stored card reports of all paired APs.
func (s *Server) apCaps(w *config.Wireless) map[string]map[string]gen.RadioCaps {
	out := map[string]map[string]gen.RadioCaps{}
	if w == nil {
		return out
	}
	for _, a := range w.AccessPoints {
		list, err := readJSONFile[[]gen.RadioCaps](filepath.Join(s.Path(apCapsDir), a.ID+".json"))
		if err != nil {
			continue
		}
		m := map[string]gen.RadioCaps{}
		for _, rc := range list {
			m[rc.ID] = rc
		}
		out[a.ID] = m
	}
	return out
}

func (s *Server) wirelessInfo(c *config.Config) (WirelessInfo, error) {
	priv, err := s.controllerKey()
	if err != nil {
		return WirelessInfo{}, err
	}
	pub := priv.Public().(ed25519.PublicKey)
	info := WirelessInfo{
		Segments: []config.Segment{}, Networks: []WNetworkInfo{}, AccessPoints: []APInfo{},
		Pairing: s.pairingRequests(c, pub), Router: pairing.Fingerprint(pub),
		Countries: countries(filepath.Join(zoneinfo, "iso3166.tab")), DryRun: s.cfg.DryRun, Simulated: s.fakeAPs(), Pending: s.pendingInfo(),
		Limits: map[string]int{"segments": config.MaxSegments, "networks": config.MaxNetworks,
			"networks_on_band": config.MaxNetworksOnBand, "access_points": config.MaxAccessPoints},
	}
	if p, err := netip4(c.LAN.IPv4); err == nil {
		info.LANAddress = p
	}
	if c.WiFi != nil {
		info.LocalWiFi = c.WiFi.SSID
	}
	w := c.Wireless
	if w == nil {
		info.Country = countryOfZone(filepath.Join(zoneinfo, "zone1970.tab"), c.System.Timezone)
		info.Plan = gen.PlanChannels(nil, nil)
		return info, nil
	}
	info.Configured, info.Country = true, w.Country
	info.Segments = append(info.Segments, w.Segments...)
	for _, n := range w.Networks {
		_, err := s.secrets().Get(n.PasswordRef)
		ni := WNetworkInfo{ID: n.ID, SSID: n.SSID, Segment: n.Segment, Security: n.Security, Bands: n.Bands,
			Enabled: n.Enabled, HasPassword: err == nil}
		if n.Roaming != nil {
			ni.MobilityDomain = n.Roaming.MobilityDomain
		}
		info.Networks = append(info.Networks, ni)
	}
	caps := s.apCaps(w)
	info.Plan = gen.PlanChannels(w, caps)
	for _, a := range w.AccessPoints {
		ai := APInfo{AccessPoint: a, Caps: []gen.RadioCaps{}}
		if k, err := pairing.ParseKey(a.PublicKey); err == nil {
			ai.Fingerprint = pairing.Fingerprint(k)
		}
		for _, r := range a.Radios {
			if rc, ok := caps[a.ID][r.ID]; ok {
				ai.Caps = append(ai.Caps, rc)
			}
		}
		if b, err := s.bundleOf(c, a.ID); err == nil {
			ai.Generation = b.Generation
		}
		if rep, err := readJSONFile[APReport](filepath.Join(s.Path(apOnlineDir), a.ID+".json")); err == nil {
			ai.Report = &rep
			ai.Online = time.Since(rep.Seen) < 2*time.Minute
		}
		info.AccessPoints = append(info.AccessPoints, ai)
	}
	return info, nil
}

func netip4(cidr string) (string, error) {
	addr, _, ok := strings.Cut(cidr, "/")
	if !ok {
		return "", fmt.Errorf("no prefix")
	}
	return addr, nil
}

// newMobilityDomain picks a random mobility domain that no other network uses (concept 5.4).
func newMobilityDomain(w *config.Wireless) string {
	for {
		b := make([]byte, 2)
		rand.Read(b)
		md := hex.EncodeToString(b)
		if md == "0000" {
			continue
		}
		if w == nil || !slices.ContainsFunc(w.Networks, func(n config.WNetwork) bool { return n.Roaming != nil && n.Roaming.MobilityDomain == md }) {
			return md
		}
	}
}

// newFTKey stores a fresh 256-bit roaming key under a new reference.
func (s *Server) newFTKey() (string, error) {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return "", err
	}
	ref := ftRef + newID()
	return ref, s.secrets().Set(ref, hex.EncodeToString(k))
}

// chooseBands gives each card of a new AP a band: 6 GHz first, then 5, then 2.4, one card per band as long as
// there are bands left (concept 3.2: at least 5 and 6 GHz). The owner can change it on the page.
func chooseBands(radios []gen.RadioCaps) []config.APRadio {
	served := map[string]bool{}
	var out []config.APRadio
	for _, rc := range radios {
		pick := ""
		for _, b := range []string{config.WiFiBand6, config.WiFiBand5, config.WiFiBand24} {
			if slices.Contains(rc.Bands, b) && !served[b] {
				pick = b
				break
			}
		}
		if pick == "" {
			for _, b := range []string{config.WiFiBand5, config.WiFiBand6, config.WiFiBand24} {
				if slices.Contains(rc.Bands, b) {
					pick = b
					break
				}
			}
		}
		if pick == "" {
			continue // no band as an access point: the card is not usable
		}
		served[pick] = true
		out = append(out, config.APRadio{ID: rc.ID, Band: pick})
		if len(out) == config.MaxRadios {
			break
		}
	}
	return out
}

// apName suggests a name for a new AP: its host name if it is a usable one, otherwise "Access point N".
func apName(w *config.Wireless, hostname string) string {
	taken := func(n string) bool {
		return w != nil && slices.ContainsFunc(w.AccessPoints, func(a config.AccessPoint) bool { return strings.EqualFold(a.Name, n) })
	}
	if config.ValidDisplayName(hostname) && !taken(hostname) {
		return hostname
	}
	for i := 1; ; i++ {
		if n := fmt.Sprintf("Access point %d", i); !taken(n) {
			return n
		}
	}
}

func apID(w *config.Wireless) string {
	for i := 1; ; i++ {
		id := fmt.Sprintf("ap%d", i)
		if w == nil || w.AccessPoint(id) == nil {
			return id
		}
	}
}

// commitWireless saves the configuration. A change to segments touches the router's network and must be confirmed,
// everything else only changes config.json. Secrets the new configuration no longer uses are deleted.
func (s *Server) commitWireless(nc *config.Config, created []string) (*PendingInfo, error) {
	pend, err := s.commitConfig(nc, confirmWiFi)
	if err != nil {
		for _, ref := range created {
			os.Remove(filepath.Join(s.secrets().Dir, ref))
		}
		return nil, err
	}
	if pend == nil {
		s.pruneSecrets()
	}
	return pend, nil
}

// wirelessSet is the whole page in one request: country, segments, networks and the settings of paired APs.
// Pairing and removing go through their own actions, because they change trust.
type wirelessSet struct {
	Country  string `json:"country"`
	Segments []struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		VLAN          int      `json:"vlan"`
		Access        string   `json:"access"`
		IPv4          string   `json:"ipv4"`
		ReachableFrom []string `json:"reachable_from"`
	} `json:"segments"`
	Networks []struct {
		ID       string   `json:"id"`
		SSID     string   `json:"ssid"`
		Segment  string   `json:"segment"`
		Security string   `json:"security"`
		Bands    []string `json:"bands"`
		Enabled  bool     `json:"enabled"`
		Password string   `json:"password"` // empty = keep
	} `json:"networks"`
	AccessPoints []struct {
		ID     string           `json:"id"`
		Name   string           `json:"name"`
		Power  string           `json:"power"`
		Radios []config.APRadio `json:"radios"`
	} `json:"access_points"`
}

func (s *Server) applyWirelessSet(c *config.Config, p wirelessSet) (*config.Config, []string, error) {
	old := c.Wireless
	w := &config.Wireless{Country: p.Country, Segments: []config.Segment{}, Networks: []config.WNetwork{}, AccessPoints: []config.AccessPoint{}}
	for _, sg := range p.Segments {
		id := sg.ID
		if id == "" {
			id = "seg-" + newID()[:6]
		}
		w.Segments = append(w.Segments, config.Segment{ID: id, Name: sg.Name, VLAN: sg.VLAN, Access: sg.Access, IPv4: sg.IPv4,
			ReachableFrom: sg.ReachableFrom})
	}
	var created []string
	fail := func(err error) (*config.Config, []string, error) {
		for _, ref := range created {
			os.Remove(filepath.Join(s.secrets().Dir, ref))
		}
		return nil, nil, err
	}
	oldNet := func(id string) *config.WNetwork {
		if old == nil || id == "" {
			return nil
		}
		for i := range old.Networks {
			if old.Networks[i].ID == id {
				return &old.Networks[i]
			}
		}
		return nil
	}
	for _, pn := range p.Networks {
		if pn.Password != "" && !config.ValidWiFiPassword(pn.Password) {
			return fail(Userf("The Wi-Fi password of %s needs 8 to 63 characters, without umlauts or other special letters.", pn.SSID))
		}
		n := config.WNetwork{ID: pn.ID, SSID: pn.SSID, Segment: pn.Segment, Security: pn.Security, Bands: pn.Bands, Enabled: pn.Enabled}
		if n.Bands == nil {
			n.Bands = []string{}
		}
		prev := oldNet(pn.ID)
		if n.ID == "" {
			n.ID = "net-" + newID()[:6]
		}
		if prev != nil {
			n.PasswordRef = prev.PasswordRef
		}
		if pn.Password != "" {
			ref := wnetRef + newID()
			if err := s.secrets().Set(ref, pn.Password); err != nil {
				return fail(err)
			}
			created = append(created, ref)
			n.PasswordRef = ref
		} else if _, err := s.secrets().Get(n.PasswordRef); n.PasswordRef == "" || err != nil {
			return fail(Userf("Please enter a password for %s.", pn.SSID))
		}
		// Roaming follows the security: WPA3 always, keep the mobility domain and key across changes
		if n.Security == config.SecurityWPA3 {
			if prev != nil && prev.Roaming != nil {
				r := *prev.Roaming
				n.Roaming = &r
			} else {
				ref, err := s.newFTKey()
				if err != nil {
					return fail(err)
				}
				created = append(created, ref)
				n.Roaming = &config.Roaming{MobilityDomain: newMobilityDomain(w), KeyRef: ref}
			}
		}
		w.Networks = append(w.Networks, n)
	}
	if len(p.AccessPoints) != len(apsOf(old)) {
		return fail(Userf("The list of access points has changed in the meantime. Please reload the page."))
	}
	for _, pa := range p.AccessPoints {
		var prev *config.AccessPoint
		if old != nil {
			prev = old.AccessPoint(pa.ID)
		}
		if prev == nil {
			return fail(Userf("The list of access points has changed in the meantime. Please reload the page."))
		}
		a := *prev
		a.Name, a.Power = pa.Name, pa.Power
		// Cards come from the AP, the page only chooses band and pins: unknown cards are refused
		if len(pa.Radios) != len(prev.Radios) {
			return fail(Userf("The cards of %s have changed in the meantime. Please reload the page.", prev.Name))
		}
		a.Radios = nil
		for i, r := range pa.Radios {
			if r.ID != prev.Radios[i].ID {
				return fail(Userf("The cards of %s have changed in the meantime. Please reload the page.", prev.Name))
			}
			a.Radios = append(a.Radios, r)
		}
		w.AccessPoints = append(w.AccessPoints, a)
	}
	nc := *c
	nc.Wireless = w
	if err := nc.Validate(); err != nil {
		return fail(userConfigError(err))
	}
	// A card must be able to serve the band chosen for it
	caps := s.apCaps(w)
	for _, a := range w.AccessPoints {
		for _, r := range a.Radios {
			if rc, ok := caps[a.ID][r.ID]; ok && !slices.Contains(rc.Bands, r.Band) {
				return fail(Userf("The card %s of %s cannot send on %s GHz.", r.ID, a.Name, r.Band))
			}
		}
	}
	return &nc, created, nil
}

func apsOf(w *config.Wireless) []config.AccessPoint {
	if w == nil {
		return nil
	}
	return w.AccessPoints
}

func (s *Server) registerWireless() {
	s.handle("wireless.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		return s.wirelessInfo(c)
	})

	s.handle("wireless.set", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p wirelessSet
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		nc, created, err := s.applyWirelessSet(c, p)
		if err != nil {
			return nil, err
		}
		pend, err := s.commitWireless(nc, created)
		if err != nil {
			return nil, err
		}
		return map[string]any{"pending": pend}, nil
	})

	// Pairing (concept 4.3): the owner has compared the code. The request's key goes into config.json, the AP's
	// card report is kept for planning. The code is checked once more: the page might show an old one.
	s.handle("wireless.pair", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !pairingIDRe.MatchString(p.ID) {
			return nil, Userf("Invalid parameters.")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		if c.Wireless == nil {
			return nil, Userf("Please set up the Wi-Fi networks first.")
		}
		priv, err := s.controllerKey()
		if err != nil {
			return nil, err
		}
		reqs := s.pairingRequests(c, priv.Public().(ed25519.PublicKey))
		i := slices.IndexFunc(reqs, func(r PairingInfo) bool { return r.ID == p.ID })
		if i < 0 {
			return nil, Userf("This access point is no longer waiting. Please restart its installation.")
		}
		if reqs[i].Code != p.Code {
			return nil, Userf("The code has changed. Please compare it again.")
		}
		r, err := readJSONFile[PairingRequest](filepath.Join(s.Path(apPairingDir), p.ID+".json"))
		if err != nil {
			return nil, err
		}
		w := *c.Wireless
		if len(w.AccessPoints) >= config.MaxAccessPoints {
			return nil, Userf("At most %d access points are possible.", config.MaxAccessPoints)
		}
		mac, _ := config.NormalizeMAC(r.MAC)
		ap := config.AccessPoint{ID: apID(&w), Name: apName(&w, r.Hostname), PublicKey: r.PublicKey, MAC: mac,
			Power: config.PowerMedium, Radios: chooseBands(r.Radios)}
		if ap.Radios == nil {
			ap.Radios = []config.APRadio{}
		}
		w.AccessPoints = append(slices.Clone(w.AccessPoints), ap)
		capsPath := filepath.Join(s.Path(apCapsDir), ap.ID+".json")
		cj, err := json.MarshalIndent(r.Radios, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := config.WriteFileAtomic(capsPath, append(cj, '\n'), 0o644); err != nil {
			return nil, err
		}
		nc := *c
		nc.Wireless = &w
		pend, err := s.commitWireless(&nc, nil)
		if err != nil {
			os.Remove(capsPath)
			return nil, err
		}
		os.Remove(filepath.Join(s.Path(apPairingDir), p.ID+".json"))
		if slices.ContainsFunc(fakeAPList, func(f fakeAP) bool { return f.mac == ap.MAC }) {
			s.simulatePaired(ap.ID)
		}
		return map[string]any{"id": ap.ID, "name": ap.Name, "pending": pend}, nil
	})

	s.handle("wireless.reject", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !pairingIDRe.MatchString(p.ID) {
			return nil, Userf("Invalid parameters.")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		err := os.Remove(filepath.Join(s.Path(apPairingDir), p.ID+".json"))
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	})

	// Removing (concept 4.4): the key leaves config.json, the AP gets nothing more. All roaming keys are renewed,
	// because the AP knew them. The Wi-Fi passwords it knew stay: changing them is the owner's decision.
	s.handle("wireless.remove", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		if c.Wireless == nil || c.Wireless.AccessPoint(p.ID) == nil {
			return nil, Userf("This access point does not exist (any more).")
		}
		w := *c.Wireless
		w.AccessPoints = slices.DeleteFunc(slices.Clone(w.AccessPoints), func(a config.AccessPoint) bool { return a.ID == p.ID })
		w.Networks = slices.Clone(w.Networks)
		var created []string
		for i, n := range w.Networks {
			if n.Roaming == nil {
				continue
			}
			ref, err := s.newFTKey()
			if err != nil {
				for _, r := range created {
					os.Remove(filepath.Join(s.secrets().Dir, r))
				}
				return nil, err
			}
			created = append(created, ref)
			r := *n.Roaming
			r.KeyRef = ref
			w.Networks[i].Roaming = &r
		}
		nc := *c
		nc.Wireless = &w
		pend, err := s.commitWireless(&nc, created)
		if err != nil {
			return nil, err
		}
		os.Remove(filepath.Join(s.Path(apCapsDir), p.ID+".json"))
		os.Remove(filepath.Join(s.Path(apOnlineDir), p.ID+".json"))
		return map[string]any{"rotated": len(created), "pending": pend}, nil
	})

	// Preview of the bundle an AP will get, without passwords and keys.
	s.handle("wireless.preview", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		if c.Wireless == nil || c.Wireless.AccessPoint(p.ID) == nil {
			return nil, Userf("This access point does not exist (any more).")
		}
		caps := s.apCaps(c.Wireless)
		files, err := gen.APFiles(c, p.ID, gen.PlanChannels(c.Wireless, caps), caps, nil, gen.APOptions{Redact: true})
		if err != nil {
			return nil, Userf("%s", firstLine(err.Error()))
		}
		type file struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		out := []file{}
		for _, f := range files {
			out = append(out, file{f.Path, string(f.Content)})
		}
		return out, nil
	})
}
