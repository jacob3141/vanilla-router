// SPDX-License-Identifier: AGPL-3.0-or-later

package gen

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"

	"vanilla-router/internal/config"
)

// Access points with the router as their center (docs/sdn-concept.md). Nothing here runs on the router: the
// router plans the channels and renders one bundle per paired AP, which the AP fetches from vr-apd and applies itself
// (internal/apagent, concept 4.5). The web interface shows the same bundle without secrets as a preview.

// RadioCaps is what an AP reports about one of its Wi-Fi cards (iw phy, udev). It is observed state, not
// configuration, and arrives with the pairing request and later reports.
type RadioCaps struct {
	ID       string   `json:"id"`        // stable name, as in config.APRadio.ID
	Path     string   `json:"path"`      // udev ID_PATH, e.g. pci-0000:03:00.0
	MAC      string   `json:"mac"`       // permanent address of the card
	Chip     string   `json:"chip"`      // e.g. MediaTek MT7916
	Bands    []string `json:"bands"`     // bands the card can serve as an access point
	DFS      bool     `json:"dfs"`       // radar detection works (5 GHz channels 52–144)
	Width160 bool     `json:"width_160"` // 160 MHz as an access point
}

var (
	radioPathRe = regexp.MustCompile(`^[a-zA-Z0-9:._+-]{1,64}$`)
	chipRe      = regexp.MustCompile(`^[\p{L}\p{N} ._:/()+-]{0,40}$`)
)

// Valid checks a card report. It comes from the network and ends up in generated files, also in those of other APs.
func (r RadioCaps) Valid() bool {
	if !config.ValidRadioID(r.ID) || !chipRe.MatchString(r.Chip) || (r.Path != "" && !radioPathRe.MatchString(r.Path)) {
		return false
	}
	if m, err := config.NormalizeMAC(r.MAC); err != nil || m != r.MAC {
		return false
	}
	for _, b := range r.Bands {
		if !slices.Contains(config.WirelessBands, b) {
			return false
		}
	}
	return true
}

// RadioPlan is the planned channel of one card.
type RadioPlan struct {
	AP      string `json:"ap"`
	Radio   string `json:"radio"`
	Band    string `json:"band"`
	Channel int    `json:"channel"` // primary 20 MHz channel, 0 if idle
	Width   int    `json:"width"`
	Center  int    `json:"center"`
	Pinned  bool   `json:"pinned"`
	DFS     bool   `json:"dfs"`
	Idle    bool   `json:"idle"`             // no network on this band, or the band is not usable in this country
	Reason  string `json:"reason,omitempty"` // why idle
}

// ChannelPlan is the result of PlanChannels.
type ChannelPlan struct {
	Country string      `json:"country"`
	Radios  []RadioPlan `json:"radios"`
}

// Radio returns the plan of one card.
func (p ChannelPlan) Radio(ap, radio string) (RadioPlan, bool) {
	for _, r := range p.Radios {
		if r.AP == ap && r.Radio == radio {
			return r, true
		}
	}
	return RadioPlan{}, false
}

// Country groups for the plan. ETSI: EU, EEA, Switzerland and the United Kingdom, 6 GHz only up to 6425 MHz
// (channel 93), indoors. FCC: the whole 6 GHz band. Other countries: no 6 GHz and only 36–48 on 5 GHz, until
// somebody checks their rules. This is a conservative table, the AP's kernel (regulatory database) has the last word.
var (
	etsi = strings.Fields("AT BE BG CH CY CZ DE DK EE ES FI FR GB GR HR HU IE IS IT LI LT LU LV MT NL NO PL PT RO SE SI SK")
	fcc  = strings.Fields("CA US")
)

// planBlocks are the first channels of the blocks the planner may use, in order of preference.
// 5 GHz skips 116–128: the weather radar channels 120–128 have long listening times in the EU.
func planBlocks(country, band string, width int) []int {
	isETSI, isFCC := slices.Contains(etsi, country), slices.Contains(fcc, country)
	switch band {
	case config.WiFiBand24:
		return []int{1, 6, 11}
	case config.WiFiBand5:
		switch {
		case isETSI:
			return []int{36, 52, 100, 132}
		case isFCC:
			return []int{36, 149, 52, 100, 132}
		}
		return []int{36}
	case config.WiFiBand6:
		last := 0
		switch {
		case isETSI:
			last = 93
		case isFCC:
			last = 233
		default:
			return nil
		}
		n := width / 20
		var out []int
		for s := 1; s+4*(n-1) <= last; s += 4 * n {
			out = append(out, s)
		}
		return out
	}
	return nil
}

// PlanChannels gives every card of every paired AP a channel (concept 5.6). Pinned cards keep their channel, the
// others are spread over the free blocks so that neighboring APs do not share one. The plan does not know where
// the APs are: it spreads in the order of the list. Measured neighborhood is a later step.
func PlanChannels(w *config.Wireless, caps map[string]map[string]RadioCaps) ChannelPlan {
	plan := ChannelPlan{Radios: []RadioPlan{}}
	if w == nil {
		return plan
	}
	plan.Country = w.Country
	used := map[string]map[int]int{} // band → block start → count
	count := func(band string, start int) {
		if used[band] == nil {
			used[band] = map[int]int{}
		}
		used[band][start]++
	}
	n6 := 0
	for _, a := range w.AccessPoints {
		for _, r := range a.Radios {
			if r.Band == config.WiFiBand6 {
				n6++
			}
		}
	}
	width := func(band string, rc RadioCaps) int {
		switch band {
		case config.WiFiBand5:
			return 80
		case config.WiFiBand6:
			if rc.Width160 && n6 <= len(planBlocks(w.Country, band, 160)) {
				return 160
			}
			return 80
		}
		return 20
	}

	// First the pinned cards, they occupy their blocks
	for _, a := range w.AccessPoints {
		for _, r := range a.Radios {
			if r.Channel == 0 {
				continue
			}
			start, center, _ := config.ChannelBlock(r.Band, r.Channel, r.Width)
			plan.Radios = append(plan.Radios, RadioPlan{AP: a.ID, Radio: r.ID, Band: r.Band, Channel: r.Channel, Width: r.Width,
				Center: center, Pinned: true, DFS: config.DFS(r.Band, r.Channel)})
			count(r.Band, start)
		}
	}
	for _, a := range w.AccessPoints {
		for _, r := range a.Radios {
			if r.Channel != 0 {
				continue
			}
			rc := caps[a.ID][r.ID]
			rp := RadioPlan{AP: a.ID, Radio: r.ID, Band: r.Band}
			wd := width(r.Band, rc)
			var cands []int
			for _, s := range planBlocks(w.Country, r.Band, wd) {
				if config.DFS(r.Band, s) && !rc.DFS {
					continue
				}
				cands = append(cands, s)
			}
			if len(cands) == 0 {
				rp.Idle, rp.Reason = true, "band not planned for this country"
				plan.Radios = append(plan.Radios, rp)
				continue
			}
			best := cands[0]
			for _, s := range cands[1:] {
				if used[r.Band][s] < used[r.Band][best] {
					best = s
				}
			}
			count(r.Band, best)
			_, center, _ := config.ChannelBlock(r.Band, best, wd)
			rp.Channel, rp.Width, rp.Center, rp.DFS = best, wd, center, config.DFS(r.Band, best)
			plan.Radios = append(plan.Radios, rp)
		}
	}
	for i := range plan.Radios {
		if r := &plan.Radios[i]; !r.Idle && len(w.NetworksOn(r.Band)) == 0 {
			r.Idle, r.Reason = true, "no network on this band"
		}
	}
	// Order of the list, not of the two passes above
	order := map[string]int{}
	for i, a := range w.AccessPoints {
		for j, r := range a.Radios {
			order[a.ID+"/"+r.ID] = i*100 + j
		}
	}
	slices.SortStableFunc(plan.Radios, func(x, y RadioPlan) int { return order[x.AP+"/"+x.Radio] - order[y.AP+"/"+y.Radio] })
	return plan
}

// Paths in an AP's bundle. They are paths on the AP, never written on the router.
const (
	APDir     = "/etc/vanilla-router/ap/"
	APBridge  = "br0"
	apRadioIf = "wlr" // wlr0, wlr1 … one per card, further networks wlr0b1, wlr0b2 …
	redacted  = "(hidden)"
	powerFile = APDir + "radios.json"
)

// APOptions control APFiles.
type APOptions struct {
	// Redact replaces passwords and keys, for the preview in the web interface.
	Redact bool
}

// bssid is the address of the n-th network on a card. The first network uses the card's own address, the others a
// locally administered variant of it (hostapd needs them to differ only in the last bits).
func bssid(mac string, n int) string {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return ""
	}
	if n == 0 {
		return hw.String()
	}
	hw[0] |= 0x02
	hw[5] ^= byte(n)
	return hw.String()
}

type bss struct {
	ap, radio, mac string
}

// ftKey is the key between two network nodes for one network: derived from the network's key and both addresses, so
// that every pair has its own key and both sides compute the same one.
func ftKey(master []byte, network, a, b string) string {
	if a > b {
		a, b = b, a
	}
	m := hmac.New(sha256.New, master)
	m.Write([]byte("vanilla-router ft v1 " + network + " " + a + " " + b))
	return hex.EncodeToString(m.Sum(nil))
}

// APFiles renders the bundle of one paired AP: hostapd per card, the key lists for fast roaming, and systemd-networkd
// for a VLAN-aware bridge with the uplink port and all networks in their segments' VLANs.
func APFiles(c *config.Config, apID string, plan ChannelPlan, caps map[string]map[string]RadioCaps, sec Secrets, opt APOptions) ([]File, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	w := c.Wireless
	if w == nil {
		return nil, fmt.Errorf("no wireless block")
	}
	ap := w.AccessPoint(apID)
	if ap == nil {
		return nil, fmt.Errorf("unknown access point %q", apID)
	}
	secret := func(ref string) (string, error) {
		if opt.Redact {
			return redacted, nil
		}
		if sec == nil {
			return "", fmt.Errorf("%s: no secrets available", ref)
		}
		return sec.Get(ref)
	}

	// All network nodes per network, for the key lists. Cards without a reported address cannot take part.
	nodes := map[string][]bss{}
	for _, a := range w.AccessPoints {
		for _, r := range a.Radios {
			rp, _ := plan.Radio(a.ID, r.ID)
			mac := caps[a.ID][r.ID].MAC
			if rp.Idle || mac == "" {
				continue
			}
			for i, n := range w.NetworksOn(r.Band) {
				nodes[n.ID] = append(nodes[n.ID], bss{a.ID, r.ID, bssid(mac, i)})
			}
		}
	}

	var files []File
	type radioOut struct {
		ID    string `json:"id"`
		If    string `json:"interface"`
		Power string `json:"power"`
		Idle  bool   `json:"idle"`
	}
	var radios []radioOut
	usedVLAN := map[int]bool{1: true}
	for i, r := range ap.Radios {
		ifname := fmt.Sprintf("%s%d", apRadioIf, i)
		rp, _ := plan.Radio(ap.ID, r.ID)
		rc := caps[ap.ID][r.ID]
		radios = append(radios, radioOut{ID: r.ID, If: ifname, Power: ap.Power, Idle: rp.Idle})
		if rc.Path != "" {
			l := &ini{}
			l.section("Match")
			l.kv("Path", rc.Path)
			l.section("Link")
			l.kv("Name", ifname)
			files = append(files, File{Path: networkDir + fmt.Sprintf("30-vr-ap-%s.link", ifname), Mode: 0o644, Group: GroupLink,
				Content: []byte(header("#") + l.String())})
		}
		if rp.Idle {
			continue
		}
		if rc.MAC == "" {
			return nil, fmt.Errorf("card %s of %s: address unknown, the access point has not reported it", r.ID, ap.Name)
		}
		var b strings.Builder
		b.WriteString(header("#"))
		fmt.Fprintf(&b, "# Access point %s, card %s", ap.Name, r.ID)
		if rc.Chip != "" {
			fmt.Fprintf(&b, " (%s)", rc.Chip)
		}
		fmt.Fprintf(&b, ", channel %d, %d MHz", rp.Channel, rp.Width)
		if rp.Pinned {
			b.WriteString(", pinned")
		}
		b.WriteString("\n")
		kv := func(k, v string) { fmt.Fprintf(&b, "%s=%s\n", k, v) }
		kv("interface", ifname)
		kv("driver", "nl80211")
		kv("ctrl_interface", "/run/hostapd")
		kv("country_code", w.Country)
		kv("ieee80211d", "1")
		radioSettings(kv, rp)

		for j, n := range w.NetworksOn(r.Band) {
			seg := w.Segment(n.Segment)
			bif := ifname
			if j > 0 {
				bif = fmt.Sprintf("%sb%d", ifname, j)
				fmt.Fprintf(&b, "\n# Network %s\n", n.ID)
				kv("bss", bif)
				kv("bssid", bssid(rc.MAC, j))
			} else {
				fmt.Fprintf(&b, "\n# Network %s\n", n.ID)
			}
			kv("ssid", n.SSID)
			kv("utf8_ssid", "1")
			kv("bridge", APBridge)
			if seg.Access == config.AccessInternet {
				kv("ap_isolate", "1") // guests do not see each other
			}
			pw, err := secret(n.PasswordRef)
			if err != nil {
				return nil, fmt.Errorf("password of %s: %w", n.SSID, err)
			}
			if !opt.Redact && !config.ValidWiFiPassword(pw) {
				return nil, fmt.Errorf("The Wi-Fi password of %s needs 8 to 63 characters", n.SSID)
			}
			kv("auth_algs", "1")
			kv("wpa", "2")
			kv("rsn_pairwise", "CCMP")
			if n.Security == config.SecurityWPA3 {
				// WPA3 only (concept 5.4): SAE with FT, protected management frames required, H2E and hunting-and-pecking
				kv("wpa_key_mgmt", "SAE FT-SAE")
				kv("ieee80211w", "2")
				kv("sae_require_mfp", "1")
				kv("sae_pwe", "2")
				kv("sae_password", pw)
				kv("mobility_domain", n.Roaming.MobilityDomain)
				kv("nas_identifier", strings.ReplaceAll(bssid(rc.MAC, j), ":", ""))
				kv("ft_over_ds", "0")
				kv("pmk_r1_push", "1")
				kv("rxkh_file", APDir+"rxkh-"+bif+".conf")
				f, err := rxkhFile(APDir+"rxkh-"+bif+".conf", n, bssid(rc.MAC, j), nodes[n.ID], secret)
				if err != nil {
					return nil, err
				}
				files = append(files, f)
			} else {
				// Older devices (concept 5.4): transition mode, no FT
				kv("wpa_key_mgmt", "WPA-PSK SAE")
				kv("ieee80211w", "1")
				kv("sae_require_mfp", "1")
				kv("sae_pwe", "2")
				kv("wpa_passphrase", pw)
			}
			// 802.11k and v: clients get neighbor lists and may be told about a better AP (concept 5.5)
			kv("rrm_neighbor_report", "1")
			kv("bss_transition", "1")

			// The interface joins the bridge in its segment's VLAN, untagged on the radio
			nw := &ini{}
			nw.section("Match")
			nw.kv("Name", bif)
			nw.section("Network")
			nw.kv("Bridge", APBridge)
			nw.section("BridgeVLAN")
			nw.kv("PVID", fmt.Sprint(seg.VLAN))
			nw.kv("EgressUntagged", fmt.Sprint(seg.VLAN))
			files = append(files, File{Path: networkDir + fmt.Sprintf("40-vr-ap-%s.network", bif), Mode: 0o644, Group: GroupNetworkd,
				Content: []byte(header("#") + nw.String())})
			usedVLAN[seg.VLAN] = true
		}
		files = append(files, File{Path: APDir + "hostapd-" + ifname + ".conf", Mode: 0o600, Group: GroupWiFi, Content: []byte(b.String())})
	}

	files = append(files, apBridgeFiles(ap, usedVLAN)...)
	pj, err := json.MarshalIndent(map[string]any{"access_point": ap.ID, "radios": radios}, "", "  ")
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: powerFile, Mode: 0o644, Group: GroupWiFi, Content: append(pj, '\n')})
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

// radioSettings writes band, channel and width. 6 GHz needs the operating class, it has no HT/VHT.
func radioSettings(kv func(k, v string), rp RadioPlan) {
	switch rp.Band {
	case config.WiFiBand24:
		kv("hw_mode", "g")
		kv("channel", fmt.Sprint(rp.Channel))
		kv("wmm_enabled", "1")
		kv("ieee80211n", "1")
		kv("ieee80211ax", "1")
	case config.WiFiBand5:
		kv("hw_mode", "a")
		kv("channel", fmt.Sprint(rp.Channel))
		kv("ieee80211h", "1") // radar detection and transmit power control
		kv("wmm_enabled", "1")
		kv("ieee80211n", "1")
		if rp.Width >= 40 {
			kv("ht_capab", ht40(rp.Channel))
		}
		kv("ieee80211ac", "1")
		kv("ieee80211ax", "1")
		cw := map[int]string{20: "0", 40: "0", 80: "1", 160: "2"}[rp.Width]
		kv("vht_oper_chwidth", cw)
		kv("he_oper_chwidth", cw)
		if rp.Width >= 80 {
			kv("vht_oper_centr_freq_seg0_idx", fmt.Sprint(rp.Center))
			kv("he_oper_centr_freq_seg0_idx", fmt.Sprint(rp.Center))
		}
	case config.WiFiBand6:
		kv("hw_mode", "a")
		kv("op_class", map[int]string{20: "131", 40: "132", 80: "133", 160: "134"}[rp.Width])
		kv("channel", fmt.Sprint(rp.Channel))
		kv("wmm_enabled", "1")
		kv("ieee80211ax", "1")
		kv("he_oper_chwidth", map[int]string{20: "0", 40: "0", 80: "1", 160: "2"}[rp.Width])
		if rp.Width >= 40 {
			kv("he_oper_centr_freq_seg0_idx", fmt.Sprint(rp.Center))
		}
		kv("he_6ghz_reg_pwr_type", "0") // indoor access point (low power indoor), the only kind the EU allows
	}
}

// rxkhFile lists all other nodes of a network with their pairwise keys, never wildcards (concept 5.4). The key of a
// pair is derived from the network's key and both addresses, so both ends compute the same one without it ever
// travelling, and one leaked pair key reveals nothing about the others.
func rxkhFile(path string, n config.WNetwork, self string, nodes []bss, secret func(string) (string, error)) (File, error) {
	key, err := secret(n.Roaming.KeyRef)
	if err != nil {
		return File{}, fmt.Errorf("roaming key of %s: %w", n.SSID, err)
	}
	var master []byte
	if key != redacted {
		if master, err = hex.DecodeString(key); err != nil || len(master) != 32 {
			return File{}, fmt.Errorf("roaming key of %s is damaged", n.SSID)
		}
	}
	var b strings.Builder
	b.WriteString(header("#"))
	fmt.Fprintf(&b, "# Fast roaming in network %s, mobility domain %s\n", n.ID, n.Roaming.MobilityDomain)
	fmt.Fprintf(&b, "# This node: %s\n", self)
	peers := 0
	for _, peer := range nodes {
		if peer.mac == self {
			continue
		}
		peers++
		k := redacted
		if master != nil {
			k = ftKey(master, n.ID, self, peer.mac)
		}
		fmt.Fprintf(&b, "r0kh=%s %s %s\n", peer.mac, strings.ReplaceAll(peer.mac, ":", ""), k)
		fmt.Fprintf(&b, "r1kh=%s %s %s\n", peer.mac, peer.mac, k)
	}
	if peers == 0 {
		b.WriteString("# No other access point offers this network yet.\n")
	}
	return File{Path: path, Mode: 0o600, Group: GroupWiFi, Content: []byte(b.String())}, nil
}

// apBridgeFiles: the AP's port and its networks in one VLAN-aware bridge (concept 5.7). VLAN 1 (home) is untagged on
// the cable, so an AP without guest networks works behind any switch. The bridge gets its own address by DHCP in the
// home segment, that is how it reaches the router.
func apBridgeFiles(ap *config.AccessPoint, vlans map[int]bool) []File {
	var list []int
	for v := range vlans {
		list = append(list, v)
	}
	slices.Sort(list)

	nd := &ini{}
	nd.section("NetDev")
	nd.kv("Name", APBridge)
	nd.kv("Kind", "bridge")
	nd.section("Bridge")
	nd.kv("VLANFiltering", "yes")
	nd.kv("DefaultPVID", "1")
	nd.kv("STP", "no")

	up := &ini{}
	up.section("Match")
	up.kv("PermanentMACAddress", ap.MAC)
	up.section("Network")
	up.kv("Bridge", APBridge)
	for _, v := range list {
		up.section("BridgeVLAN")
		if v == 1 {
			up.kv("PVID", "1")
			up.kv("EgressUntagged", "1")
		} else {
			up.kv("VLAN", fmt.Sprint(v))
		}
	}

	br := &ini{}
	br.section("Match")
	br.kv("Name", APBridge)
	br.section("Network")
	br.kv("DHCP", "yes")
	br.kv("IPv6AcceptRA", "yes")
	br.section("BridgeVLAN")
	br.kv("PVID", "1")
	br.kv("EgressUntagged", "1")

	return []File{
		{Path: networkDir + "20-vr-ap-br0.netdev", Mode: 0o644, Group: GroupNetworkd, Content: []byte(header("#") + nd.String())},
		{Path: networkDir + "20-vr-ap-br0.network", Mode: 0o644, Group: GroupNetworkd, Content: []byte(header("#") + br.String())},
		{Path: networkDir + "20-vr-ap-uplink.network", Mode: 0o644, Group: GroupNetworkd, Content: []byte(header("#") + up.String())},
	}
}
