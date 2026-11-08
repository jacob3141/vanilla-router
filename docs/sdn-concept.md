# Concept: vanilla-router as the center of a home network with several access points

| | |
|---|---|
| **Status** | Proposal. Detail questions decided on 2026-11-07 on the owner's behalf (section 12). Adoption into `PLAN.md` approved by the owner on 2026-11-08, after stage 0 (Q-A). Built in the working tree on 2026-11-08 at the owner's request, not tested on real radios: see [webui-sdn-log.md](webui-sdn-log.md) |
| **As of** | 2026-11-07, based on PLAN.md 0.21 |
| **Wish** | Pair Wi-Fi access points (APs) with the router without managing each one, and let clients change to a better AP without interruption |
| **Reads with** | [PLAN.md](../PLAN.md) (authoritative), [architecture.md, section 16](architecture.md#16-wi-fi-access-point), D-18, F-WIFI |

All IDs in section 11 are proposals. PLAN.md stays unchanged until the owner adopts them.

## 1. Summary

**An access point is a small PC with Ubuntu Server and the package `vanilla-router`,** like the router itself, only with a Wi-Fi
card instead of a second network port. It is installed from the same boot stick, runs the same code for configuration, checks,
rollback, snapshots and updates, and gets its configuration from the router. The router is the only place where anything is set.

Rules set by the owner on 2026-11-06/07:

- **No foreign systems.** No integration of or coexistence with other controllers or APs (Omada, UniFi and the like), no vendor
  protocols. With them the task cannot be solved properly.
- **Ubuntu Server everywhere, no OpenWrt.** One operating system, one code base, one way of updating.
- **No weak hardware.** A current x86 mini PC draws about 6–10 W at idle [S-1] and has reserves for years.
- **Newer is better.** A modern system optimized for ease of use, without giving up extensibility and without technical legacy. If
  buying a new device is cheaper than carrying an old one along in the project, buy the new one.

What follows from these rules, in short:

| Topic | Decision |
|---|---|
| Wi-Fi generation | **Wi-Fi 6E** with 6 GHz as the reference today. Wi-Fi 7 as soon as Ubuntu ships `hostapd` 2.12 or newer, the schema is prepared for it (section 3.3) |
| Security | **Home network WPA3 only,** protected management frames required, the same on every band. WPA2 only in a separate, isolated network for older devices that the owner switches on when needed (section 5.4) |
| Roaming | **Fast roaming (802.11r), neighbor lists (802.11k) and transition support (802.11v) on by default** in the home network (section 5.5) |
| Channels | Planned centrally by the router, **DFS allowed** where the card detects radar, 6 GHz preferred (section 5.6) |
| Networks | **Segments (VLANs) built in from the start:** home, guests, older devices. One mechanism for all (section 5.7) |
| Backhaul | **Cable only,** 2.5 Gbit/s recommended. No wireless backhaul (section 5.8) |

Three findings shape everything else:

1. **The Wi-Fi card is the crux.** Many common cards, Intel's above all, refuse to be an access point on 5 GHz (section 3.2).
2. **"Seamless" cannot be promised, only made likely.** The client decides when to roam (section 5.1).
3. **WPA3 only locks out older devices** (section 5.3). That is accepted deliberately for the home network, with a clear way out
   instead of a weaker home network.

Before any code: a measurement on real hardware (stage 0). Nothing here has been tested with a real radio yet, and even the router's
own access point (T-M4-09) still has not.

**What "SDN" means here:** one central place describes the Wi-Fi, the APs carry it out. Client traffic is bridged locally on the AP,
never tunneled to the router. Managing switches is out of scope. In the product the word does not appear (P-02): the page is called
"Access points".

## 2. Requirements from PLAN.md

| Source | Consequence |
|---|---|
| P-01, D-10, D-12 | An AP is an unmodified Ubuntu Server LTS plus the package `vanilla-router`, nothing else |
| P-04, D-02 | AP configuration lives in the router's `config.json`. Observed state (online, clients, versions) does not, it lives under `/var/lib/vanilla-router/` |
| P-05, F-WIFI-06 | Check, apply, health check, roll back, now across several computers, also when the connection drops |
| P-06, D-13 | Snapshots and automatic rollback after updates apply on every AP as on the router |
| P-03, F-WIFI-05 | No default passwords, secrets only under `secrets/`, firewall on every AP, SSH off |
| P-08, D-01 | Go standard library only. It has TLS 1.3 with Ed25519 and client certificates [S-2] |
| D-18, NF-09 | `hostapd` from Ubuntu stays the only package from *universe*. **No own build of `hostapd`,** so features wait for Ubuntu's version |
| P-09 | No cloud, the APs talk only to the router |
| P-11, D-15 | The AP's installation asks nothing that can be detected. Pairing means comparing, not typing |
| D-04 | The new listening service on the router gets no root and keeps no secrets on disk |
| NF-07 | No constant load, a long poll per AP is enough |
| F-WIFI-08 | "WPA2 and WPA3 at the same time" changes to WPA3 only for the home network (section 9) |
| 1.4 Non-goals, Q-08 | "No Wi-Fi mesh, roaming or guest SSIDs" has to change (section 9). x86-64 only, as for the router |

## 3. Architecture

### 3.1 Roles

```
                       config.json (wireless: segments, networks, access points)
                                  │
router ── vr-webui helper ── gen(cfg, node) ── bundle per node ── vr-apd ══ TLS, pinned keys ══╗
  │                                │                                                          ║
  │  local radio = node "local" ───┘ applied by internal/apply as today                       ║
  │                                                                                           ║
  └── lan0 (VLAN-aware) ── switch ── AP "hall" (Ubuntu Server + vanilla-router, role ap) ◀════╝
                                        vr-ap: fetch bundle → internal/apply (check, back up, apply,
                                        health, roll back) → hostapd, bridge, firewall of the AP
```

- **One generator for all nodes.** `gen` today turns `config.json` into the router's files. For an AP it produces that AP's files:
  `hostapd.conf`, networkd files for a VLAN-aware bridge of its port and its radios, its firewall, its time and update settings. Pure
  function, golden files (NF-04).
- **One apply engine for all nodes.** The AP runs `internal/apply` on the bundle it receives.
- **The router's own radio** becomes the node `local`. F-WIFI is then a special case of this concept, not a second code path.
- **On the AP there is no web interface and no `config.json` of its own.** It keeps its identity key, the router's public key and
  the last good bundle.

### 3.2 Hardware

| Part | Decision | Evidence |
|---|---|---|
| Computer | x86-64 mini PC, N100 class or better, **2.5 Gbit/s Ethernet** (1 Gbit/s works, but a Wi-Fi 6E radio at 160 MHz can exceed it), M.2 A/E-key slot or mini PCIe, external antennas | 6–10 W idle at the wall [S-1] |
| Wi-Fi, reference | **MediaTek Filogic, driver `mt76` in the kernel:** MT7916 (Wi-Fi 6E, VyOS reports 6 GHz AP mode) [S-3] and MT7915 (Wi-Fi 6, 2.4 and 5 GHz at the same time on one card) [S-4] [S-5] | Driver supports AP mode [S-4]. 6 GHz results vary per module [S-3]: stage 0 decides which modules go on the list |
| Wi-Fi, to test | MT7922 (Wi-Fi 6E laptop card, 6 GHz AP reported by VyOS [S-3], 160 MHz AP since a 2025 patch [S-6]) | Cheaper and widespread. On the list only after stage 0 |
| Wi-Fi 7 | MT7996/MT7992 exist as AP chips, but so far mainly on ARM boards with separate firmware and problems with 6 GHz [S-7]. Not yet for x86 | Watch, see 3.3 |
| Not supported | Intel AX200/AX210/BE200 and relatives (no 5 GHz AP, "no IR") [S-8], Qualcomm ath11k (mixed reports) [S-9], USB adapters | Old or unsuitable hardware is not carried along. Replacing a card costs less than supporting it |
| Radios per AP | At least 5 GHz and 6 GHz. 2.4 GHz only when the older-devices network is on (5.7) | One MT7916 for 5/6 GHz plus one MT7915 for 2.4/5 GHz is a plausible pair, stage 0 checks which combinations the cards really allow |

The list of supported cards is maintained like the provider list (Q-14). The installer recognizes the card by its PCI ID and states clearly
whether it is supported (F-WIFI-04).

### 3.3 Prepared for Wi-Fi 7, without building it now

`hostapd` 2.12 was released in August 2026 with more complete Wi-Fi 7 support [S-10]. Ubuntu 26.04 ships 2.11 [S-11], whose binary has no
`ieee80211be` option [S-31]. D-18 rules out an own build. So:

- The schema has no fixed upper limits that would block Wi-Fi 7: channel widths are a list of allowed values from the card, not an enum
  ending at 160, and a radio can later belong to a multi-link group (MLO) without changing existing fields.
- The security in 5.4 already matches what Wi-Fi 7 requires (WPA3, protected management frames, H2E), so no change of security is
  needed when Wi-Fi 7 arrives.
- Wi-Fi 7 comes with the first Ubuntu LTS that has `hostapd` 2.12 or newer, together with x86 cards that have mainline firmware.

### 3.4 Rejected alternatives

| Option | Why not |
|---|---|
| OpenWrt with an agent of ours | Owner's decision (2026-11-07): a second operating system, its own update path without automatic security updates, weak hardware |
| Integrating or coexisting with Omada, UniFi and the like | Owner's decision (2026-11-06) |
| CAPWAP (RFC 5415/5416) | Protocol from 2009, DTLS is not in the Go standard library, open implementations are niche [S-12] [S-13]. Nothing to gain when both sides are ours |
| TR-069 / USP (TR-369) | Built for providers, needs protobuf plus WebSocket, MQTT or STOMP [S-14] |
| Wi-Fi EasyMesh | Its point is cross-vendor operation, which is not a goal [S-15] |
| OpenWISP, OpenWiFi/uCentral | Large server stacks [S-16] or a deprecated SDK [S-17]. Good references for pull, test and restore [S-18] |
| Own `hostapd` build for Wi-Fi 7 now | Against D-18, and the hardware is not ready for x86 anyway (3.3) |
| Tunneling client traffic to the router | Router load, a single point of failure for all Wi-Fi |

## 4. Installation and pairing

### 4.1 Becoming an AP without a question

Decided by the owner on 2026-11-07 (Q-C). The boot stick is the same. The assistant already waits for a cable and probes it (3.9.3). New:
before DHCP or PPPoE, it checks whether the cable leads to a **vanilla-router**, which answers on a fixed address with its public key.

| Found on the cable | Role | What the assistant says |
|---|---|---|
| a vanilla-router | **access point** | "This computer becomes an access point for your router." |
| modem or provider router | router, as today | unchanged |
| a vanilla-router, but no supported Wi-Fi card | none | "This computer has no Wi-Fi card that works as an access point" with the card's name |

An AP needs one Ethernet port, not two (NF-01 differs per role).

### 4.2 Trust model

| Element | Design | Why |
|---|---|---|
| Identity | Each node creates its own Ed25519 key at installation, the router has one controller key. Private keys never leave their computer | No shared secret that one lost AP would reveal |
| Transport | TLS 1.3, mutual, self-signed certificates as containers for the keys, each side **pins** the other's public key [S-2] | No CA, no expiry, no revocation list |
| Direction | The AP connects to the router, nothing listens on the AP | No management port on the AP |
| Finding the router | Name under `home.arpa`, fallback the DHCP gateway. The key is pinned, the address does not matter | The home network may move (`config.MoveLAN`) |
| Consent | Nothing is released before a person approves the AP in the web interface | No silent joins |
| Revocation | Public key removed from `config.json`, the next handshake fails | One source of truth, works through rollback too |

### 4.3 Pairing: compare, not type

```
AP screen:      "Open the router's web interface and confirm this access point.  Code: 418 207"
Web interface:  "New access point: Mini PC, card MediaTek MT7916, port 02:00:00:00:00:10.  Code: 418 207
                 Is this the same code as on its screen?"   [Yes, add it]   [No]
```

The code is computed on both sides from **both public keys** (short authentication string, like Bluetooth's numeric comparison). Somebody in
between would cause different codes. This authenticates both directions at once, and nothing is typed (P-11). After "Yes" the AP's public key
is in `config.json`, the installation finishes, and the AP restarts already paired. The pairing endpoint on the router is only open while an
installation waits, with a rate limit.

### 4.4 Taking an AP back

| Situation | What happens | Honest limit |
|---|---|---|
| AP is reachable | "Remove" → AP wipes its Wi-Fi secrets and keys, stops sending, key deleted from `config.json` | |
| AP is gone (stolen, sold, broken) | Key deleted, it receives nothing more. FT keys are renewed, invisible to clients | **The AP still knows the Wi-Fi passwords.** The web interface offers a password change, which means reconnecting all devices. The owner decides |
| Router replaced | Export and "reinstall, keep settings" (3.9.5) take over the controller key | Without a backup: APs keep their last configuration, re-pair after reinstalling from the stick |

### 4.5 Configuration and rollback

**Schema sketch.** Secrets stay outside, the file stays exportable.

```json
"wireless": {
  "country": "DE",
  "segments": [
    { "id": "home",  "vlan": 1,  "access": "lan" },
    { "id": "guest", "vlan": 20, "access": "internet" },
    { "id": "older", "vlan": 30, "access": "internet", "reachable_from": ["home"] }
  ],
  "networks": [
    { "id": "home",  "ssid": "Home", "segment": "home", "password_ref": "secret:wifi-1",
      "security": "wpa3", "bands": ["5", "6"], "roaming": { "mobility_domain": "a1b2", "rxkh_ref": "secret:ft-1" } },
    { "id": "older", "ssid": "Home Older Devices", "segment": "older", "password_ref": "secret:wifi-2",
      "security": "wpa2-wpa3", "bands": ["2.4"], "enabled": false }
  ],
  "access_points": [
    { "id": "local", "kind": "local", "radios": [ { "interface": "wlp3s0" } ] },
    { "id": "hall", "name": "Hall", "kind": "remote", "public_key": "ed25519:…", "mac": "02:00:00:00:00:10",
      "radios": [ { "card": "pci:…" }, { "card": "pci:…" } ], "power": "medium" }
  ]
}
```

Channels and widths are **not** in the configuration: the router plans them (5.6) and keeps the plan as observed state, so a radar event or a new
AP does not change `config.json`. The owner can pin a channel per radio, then it is configuration. `wireless` replaces today's `wifi` block.

**Applying across computers:**

```
vrctl apply
 ├─ router's own groups as today (networkd, firewall, dnsmasq, local radio)
 └─ group "wireless": a new generation is offered, one AP at a time
       AP: internal/apply → back up → replace → reload → own deadline timer → health check
           (hostapd runs the expected networks, bridge and VLANs up, router reachable through pinned TLS)
       healthy    → report → router moves to the next AP
       not healthy, or router unreachable until the deadline → AP restores by itself and reports afterwards
 Owner confirms within 5 minutes → generation good → APs drop their backups
 No confirmation → router offers the previous generation, an AP that cannot be reached falls back through its own timer
```

| Rule | Reason |
|---|---|
| Rollback timer on **both** sides, independent of each other | The change that breaks the connection must not prevent its own rollback. The AP uses the same `systemd-run` timer as the router (architecture 4.3) |
| An offline AP does not block a change | It catches up when it returns. The router's health check stays independent of APs |
| One AP at a time, the one serving the browser last | A bad change takes one AP away, not all Wi-Fi |
| Last good configuration persists on the AP | Router off, AP restarted: Wi-Fi still comes up |

`hostapd` has no check mode (D-18): `config.Validate` restricts values, the AP reports its cards' capabilities (`iw phy`), the generator checks
against them, the start decides.

**On the router** (D-04): a new process `vr-apd` under its own user, one TCP port on `lan0` only, open only while APs exist or an installation
waits. No secrets on disk, bundles come from the helper through fixed actions (`ap.*`). HTTP long poll over TLS from the standard library.

### 4.6 Updates

APs are Ubuntu Servers, so PLAN.md 3.7 applies unchanged: security updates, snapshot before each apt run, automatic rollback (D-13). The router
staggers the maintenance windows of its APs, so the house never loses all Wi-Fi at once. The router updates first, then the APs one by one (Q-22).

## 5. Roaming and the radio

### 5.1 What "seamless" honestly means

The client **decides** to leave, **finds** a target, and **re-authenticates**. The network influences the last two and can only suggest the first.
Apple: the device decides [S-19]. An iPhone holds on until −70 dBm (a Mac −75 dBm), a candidate must be 8 dB stronger during traffic or 12 dB
when idle [S-20].

| Situation | Realistic experience with this setup |
|---|---|
| Streaming, downloads | Not noticed |
| Video or voice call while walking | Short dropout possible, often not noticed on clients that use FT. Without 802.11k scanning "can add several seconds" [S-20] |
| SSH, open TCP connections | Survive, same address |
| Device between two APs that does not move | Stays where it is |

**Promise in the product:** "Your devices can change between access points without losing the connection, usually without you noticing. Your
devices decide when." No figures before stage 0 measured them.

### 5.2 Who does what

| Part | Router | AP | Client |
|---|---|---|---|
| Same network (5.7) | DHCP, DNS, firewall per segment | Bridges clients into their VLAN | Keeps its address |
| **802.11r** (FT) | Generates mobility domain and keys | Exchanges key material with other APs | Must support FT and choose it |
| **802.11k** (neighbor lists) | Knows all BSSIDs and channels, generates the lists | Answers requests | Scans fewer channels. Apple uses the first six entries [S-20] |
| **802.11v** (BSS Transition) | Decides whether to suggest (later) | Sends the request | May ignore it |
| Decision to roam | No | No | **Yes** |

### 5.3 Clients

Clients are other people's devices and cannot be changed, so they count even under the owner's rules. They decide the security choice in 5.4.

| Client | WPA3-Personal | 802.11r/k/v | Source and limit |
|---|---|---|---|
| iPhone, iPad (iOS 13+), Macs (macOS 10.15+) | Yes | iPhone 5s and later, Apple silicon Macs: yes. Intel Macs: no FT, but they connect | [S-19] [S-20] [S-21] |
| Windows 10 1903+, Windows 11 | Yes, depending on the driver | Microsoft documents FT for 802.1X only, not for PSK [S-22]. FT with SAE is not mentioned, to be measured | [S-21] [S-22] |
| Android 10+ | Yes, depending on the chipset. Android 12 adds H2E | Chipset-dependent [S-23] | [S-21] [S-24] |
| ESP32 devices | Yes, also in transition networks [S-25] | Rarely | Espressif documentation |
| Many cheap IoT devices, Raspberry Pi Pico W, older printers | **Often no** | No, old drivers may even fail on FT entries [S-26] | Study and forums [S-25] [S-27] |

### 5.4 Security: WPA3 only in the home network

**Decision.** The home network is **WPA3-Personal only** (`wpa_key_mgmt=SAE FT-SAE`), protected management frames **required**
(`ieee80211w=2`), SAE with both methods on 2.4/5 GHz and H2E on 6 GHz (`sae_pwe=2`).

**Why, beyond "newer is better":**

1. **6 GHz forces it.** On 6 GHz only WPA3 is allowed, without transition mode, and protected management frames are mandatory [S-28] [S-29].
2. **One network name with different security per band roams badly.** Several vendors allow transition mode on 2.4/5 GHz and WPA3 on 6 GHz under one
   name, yet advise against it because of roaming [S-30]. WPA3 everywhere avoids that.
3. **FT becomes simple and safe.** No mixed `WPA-PSK FT-PSK SAE FT-SAE` list, which old drivers stumble over (5.3), just one key management.
4. **Wi-Fi 7 needs nothing new** (3.3).

**The way out for older devices** is not a weaker home network but a separate, isolated one:

- **"Older devices"** network: own name, WPA2/WPA3 transition mode, 2.4 GHz, **no** FT, own segment with internet only. Devices of the home network
  may reach it (printer, TV), it cannot reach the home network.
- **Off by default.** It appears when the owner needs it: the switch to WPA3 only (5.5) shows which devices did not come back and offers this network
  right there.
- It is an optional extra that can be removed entirely, never a fallback inside the home network. If an old device bothers the owner, the advice in
  the web interface is to replace it.

**Key distribution for FT-SAE.** Unlike FT-PSK, which each AP can compute locally, FT-SAE needs key material to travel between APs, and local generation
breaks FT with SAE [S-31] [S-32]. So the router writes complete `r0kh`/`r1kh` lists for all APs, never wildcards:

| Setting | Value | Reason |
|---|---|---|
| `mobility_domain` | Random once, in `config.json`, stable across name changes | Deriving it from the SSID, as some systems do [S-33], changes it on a rename |
| `r0kh`, `r1kh` | One line per AP with MAC, identifier and 256-bit key, through `rxkh_file` | Wildcard entries learn unknown peers on the fly [S-31] |
| Key storage | `secrets/ft-<id>`, new reference on rotation | D-18 pattern, rollback finds the old one |
| Adding an AP | write `rxkh_file`, reload | Lists in a file reload without bringing the interface down [S-31] |
| `ft_over_ds` | `0` | `hostapd` defaults to 1 [S-31]. Over the air has fewer moving parts |
| `rrm_neighbor_report`, `bss_transition` | `1` | Off by default [S-31] |

**Checked in Ubuntu 26.04** (`hostapd 2:2.11-0ubuntu5`, strings and shipped documentation, not run on a radio): `mobility_domain`, `r0kh`, `r1kh`,
`rrm_neighbor_report`, `bss_transition`, `FT-SAE`, `FT-SAE-EXT-KEY`, `SET_NEIGHBOR`, `BSS_TM_REQ` are there, `ieee80211be` and RSN overriding are not
[S-31].

### 5.5 Roaming on by default

**Decision.** In the home network 802.11r, 802.11k and 802.11v are on by default, without a separate switch. The earlier caution (FT only on request)
came from mixed WPA2/WPA3 networks. With WPA3 only, the devices that stumble over FT entries are WPA2 devices, which are in the older-devices network
anyway.

What remains is the **one-time switch** from today's WPA2/WPA3 network to WPA3 only, for example on a router that existed before this feature:

1. The router notes which Wi-Fi devices were connected recently (the device list knows them).
2. The change applies with the usual 5-minute confirmation.
3. The page shows live: "21 of 23 devices are back. Still missing: Printer, Doorbell." Then: keep, set up the older-devices network for them, or undo.

Active steering (disconnecting clients, BSS transition suggestions from the router) stays off: clients decide with good neighbor lists. Suggestions
through `BSS_TM_REQ` come later and only with measurements from stage 0.

### 5.6 Channels and power: planned by the router

**Decision.** The router plans channels and widths for all APs centrally. A new AP, a radar event or the owner's "optimize" triggers a new plan. No
automatic channel selection per AP, because independent decisions of neighboring APs fight each other.

| Band | Plan | Reason |
|---|---|---|
| **6 GHz** (preferred) | 5945–6425 MHz, indoor only, 80 or 160 MHz | No radar rules, little interference, WPA3 only [S-34]. In the EU the band is for indoor use at low power [S-34] [S-35]. Linux carries it per country as `NO-OUTDOOR` [S-35] |
| **5 GHz** | 80 MHz, **including DFS channels** 52–64 and 100–144, if the card detects radar | Without DFS there is only one 80 MHz block (36–48) [S-36]. `mt76` has radar detection for MT7915, with fixes in recent years [S-37]. Stage 0 checks it per card. Channels 120–128 (weather radar) are avoided, verify against ETSI EN 301 893 |
| **2.4 GHz** | 20 MHz, channels 1, 6, 11, only for the older-devices network | Crowded, slow, only needed for old devices |

- **DFS behavior:** a channel with radar detection needs a listening time of typically 60 s before the AP sends [S-36]. The plan therefore puts DFS
  channels where a short gap after a restart does not matter, and the router moves an AP after a radar event by itself. A card without radar detection
  only gets 36–48.
- **Transmit power:** "low, medium, high" per AP, medium by default. Too strong makes clients cling to a distant AP (thresholds in 5.1). Later the
  router can adjust it from what APs hear of each other.
- **F-WIFI-09** (DFS and automatic channel selection) is thus answered: yes, but planned centrally, not per AP.

### 5.7 Segments: VLANs from the start

**Decision.** Every wireless network belongs to a **segment**, every segment is a VLAN. The home segment stays untagged, so a router without guest or
older-devices networks looks exactly as it does today.

| Part | Implementation |
|---|---|
| Router | `lan0` becomes a VLAN-aware bridge (`VLANFiltering=yes`, `[BridgeVLAN]` per port, systemd-networkd [S-38]). Per segment its own address range, DHCP, DNS and firewall zone (`inet vr_filter`, D-09) |
| AP | VLAN-aware bridge of its port and its radios, each network's interface in the VLAN of its segment |
| Between them | Tagged frames on the cable. Most simple switches pass them, but that is not guaranteed. **Requirement for guest and older-devices networks: switches that pass VLAN tags.** A new switch costs less than troubleshooting |
| Firewall | Guest: internet only, isolated from each other. Older devices: internet only, reachable from home. Home: as today |

The guest network (F-NET-10) and the older-devices network then cost no new mechanism, only a segment and a network. Segments for other purposes
(cameras, a lab) can be added later the same way.

### 5.8 Backhaul: cable only

**Decision.** APs are connected by cable. Wireless backhaul (mesh) costs half of the airtime, makes errors hard to understand for non-experts, and the
modern answer for a hard-to-reach room is a cable, not a radio link. 2.5 Gbit/s is recommended, because a 6 GHz radio at 160 MHz can exceed 1 Gbit/s.
To be revisited only with Wi-Fi 7 multi-link and real demand.

Roaming needs **one Layer 2 network per segment** between all APs: FT messages and the Layer 2 Update frame that tells switches the new port of a client
travel there [S-39]. APs must not be isolated from each other.

## 6. Code on the AP

| Part | Content |
|---|---|
| Package | `vanilla-router`, the same `.deb`. The role decides which units run: on an AP `vr-ap` instead of `vr-webui`, `vr-dnsmasq` and `vr-pppoe` |
| `vr-ap` | Connects to the router, fetches bundles, calls `internal/apply`, reports state, clients and signals (from `hostapd_cli`, with `LC_ALL=C`, L-17) |
| `internal/apply` | Unchanged, with a node root instead of the router root |
| `gen` | New entry `gen.Node(cfg, nodeID, plan, secrets)`. The router's files are the node `router` |
| Without the router | Everything keeps running, `vr-ap` retries quietly |

## 7. Stages

| Stage | Content | Accepted when |
|---|---|---|
| **0. Measure** | T-M4-09 on a real radio. Two mini PCs with MT7916 and MT7915, one MT7922, one Intel card, **in a separate test network, not on the real router**, configured by hand: WPA3 only, FT-SAE, 6 GHz, DFS, VLANs. Walk test with iPhone, Android, Windows, Mac: continuous ping, losses, `hostapd` logs for FT. Test which old devices fail on WPA3 only | Card list decided, FT-SAE measured per client, DFS per card known |
| **1. Install and see** | Role detection, pairing with the comparison code, `vr-apd`, `vr-ap` read-only: page "Access points" with state, clients, signal, versions. Removal | AP installed from the stick without a question, paired, listed, removed, re-paired. A forged router in between produces different codes |
| **2. Configure** | `wireless` schema with segments, VLAN-aware bridges, `gen.Node`, staged apply with rollback on both sides, channel plan, local radio in the same model, staggered updates | Wrong value on one AP → only that AP returns. Unplugging the AP during a change → it restores itself. Confirmation timeout → all return |
| **3. Roam** | WPA3 only, FT-SAE with generated keys, neighbor lists, switch with device check, key rotation on removal | Stage 0 values reached with generated configuration |
| **4. More segments** | Guest network and older-devices network | Guest isolated, older devices reachable from home only, both through the same AP |
| **5. Later** | BSS transition suggestions, power adjustment from measurements, Wi-Fi 7 with Ubuntu's `hostapd` 2.12+ | Per feature |

Protocol, generation, channel plan and rollback can be tested in CI with namespaces, fake radios and dropped connections. Real roaming only by hand.

## 8. Risks

| ID (proposed) | Risk | Countermeasure |
|---|---|---|
| R-11 | Suitable Wi-Fi cards are hard to buy or 6 GHz differs per module | Tested list by PCI ID with more than one chip family, clear message for unsupported cards |
| R-12 | WPA3 only locks out devices the owner needs | Device check on the switch, older-devices network offered at once, advice to replace |
| R-13 | Expectations: "seamless" fails because the client decides | Wording from 5.1, signal per client visible |
| R-14 | Stolen or sold AP knows the Wi-Fi passwords | Inherent to PSK/SAE. Offer a password change after removal |
| R-15 | A mini PC is a poor radio (metal case, antennas, placement) | External antennas required, measure in stage 0, signal per AP visible |
| R-16 | Distributed rollback leaves mixed states | Timers on both sides, generation numbers, state per AP visible, dropped connections as a fixed test |
| R-17 | New listening service on the router is an attack surface | No root, no secrets on disk, pinned keys first, pairing only while an installation waits |
| R-18 | Switches drop VLAN tags | Requirement stated in the web interface before the first extra segment, check through the AP's health check (VLAN reachable) |
| R-19 | Radar events on DFS channels interrupt an AP | Central plan, DFS only on cards with radar detection, weather radar channels avoided, automatic move |
| R-20 | Wi-Fi 7 arrives later than hoped because Ubuntu's `hostapd` lags | Accepted (D-18). The schema is ready, nothing needs migrating |
| R-21 | Testing roaming is expensive for one person (R-06) | Stage 0 table as a repeatable protocol |

## 9. What changes compared to today

- **D-18** stays. **F-WIFI-08** ("WPA2 and WPA3 at the same time") changes: WPA3 only in the home network, WPA2 only in the older-devices network.
- **F-WIFI-09** (DFS, automatic channels) is answered by the central channel plan (5.6). **F-WIFI-10** (Wi-Fi 6, guest network, several adapters) is
  covered by segments and nodes.
- **R-10** changes: external APs are vanilla-router APs.
- **F-NET-10** (guest network) is implemented through segments (5.7).
- **1.4 Non-goals:** "No Wi-Fi mesh, roaming or guest SSIDs" becomes "No wireless backhaul, no Wi-Fi client mode, no hardware below PC class, no foreign
  controllers or APs".
- **NF-01** gets a line for APs: one Ethernet port (2.5 Gbit/s recommended) and a supported Wi-Fi card.
- `wifi` in `config.json` becomes `wireless`.

## 10. Rejected details in one sentence each

- **WPA2/WPA3 transition mode in the home network:** keeps old weaknesses alive, does not exist on 6 GHz and roams badly across bands (5.4).
- **Automatic channel selection per AP:** neighboring APs fight each other (5.6).
- **A shared secret for all APs:** one lost AP would expose the whole fleet (4.2).
- **Typing a code into the AP:** comparing is enough and protects both directions (4.3).
- **A web interface on each AP:** a second place to change things, against P-04.
- **Forcing clients through deauthentication:** a device without a better AP simply loses Wi-Fi.

## 11. Proposed entries for PLAN.md

Numbers are the next free ones as of PLAN.md 0.21 and may have changed when adopted, since another session is editing PLAN.md.

**Principle**

| ID | Principle |
|---|---|
| P-12 | **Modern hardware, no legacy.** PC-class hardware only, as router and as access point. New devices and standards are preferred over carrying old ones along: if replacing a device is cheaper than supporting it, the project recommends replacing it. Extensibility is never given up for this. Set by the owner on 2026-11-07 |

**Decisions**

| ID | Decision |
|---|---|
| D-19 | Access points are Ubuntu Server LTS with the package `vanilla-router` in the role `ap`, installed from the same stick. A vanilla-router on the cable makes the computer an AP. No OpenWrt (owner, 2026-11-07) |
| D-20 | `wireless` in `config.json` as the single source, `gen.Node` per node, `internal/apply` on every node, rollback timers on router and AP independently |
| D-21 | No foreign systems (owner, 2026-11-06). CAPWAP, TR-069/USP, EasyMesh, OpenWISP and uCentral rejected |
| D-22 | Pairing through a comparison code derived from both public keys, pinned TLS 1.3 afterwards |
| D-23 | Home network WPA3 only with required PMF. WPA2 only in an isolated older-devices network, off by default |
| D-24 | 802.11r, k and v on by default in the home network, no forced disconnects |
| D-25 | Channels planned centrally by the router, DFS on cards with radar detection, 6 GHz preferred |
| D-26 | Segments as VLANs from the start, home untagged |
| D-27 | Cable backhaul only, 2.5 Gbit/s recommended |
| D-28 | Reference hardware MediaTek Filogic (MT7916, MT7915) with `mt76`. Wi-Fi 7 only with Ubuntu's `hostapd` 2.12 or newer, no own build |

**Requirements**

| ID | Prio | Requirement |
|---|---|---|
| F-AP-01 | MUST | The assistant recognizes a vanilla-router on the cable and installs an AP without a question |
| F-AP-02 | MUST | Pairing by comparing a code derived from both keys |
| F-AP-03 | MUST | Remove an AP with immediate effect, offer a password change for an unreachable AP |
| F-AP-04 | MUST | APs in `config.json`, secrets outside, the router's own radio is one AP of the same model |
| F-AP-05 | MUST | Changes roll out one AP at a time with rollback on both sides. An offline AP does not block and catches up |
| F-AP-06 | MUST | An AP keeps its last good configuration without the router |
| F-AP-07 | MUST | Web interface shows state, version, clients and signal per AP, no polling without an open page |
| F-AP-08 | MUST | Security updates and rollback on APs as on the router, maintenance windows staggered |
| F-AP-09 | MUST | List of supported Wi-Fi cards, checked by PCI ID in the assistant |
| F-ROAM-01 | MUST | FT-SAE with generated keys, no wildcards, neighbor lists and BSS transition on |
| F-ROAM-02 | MUST | Device check with undo when switching an existing network to WPA3 only |
| F-ROAM-03 | SHOULD | Central channel plan including DFS and 6 GHz |
| F-ROAM-04 | SHOULD | Segments: guest network and older-devices network |
| F-ROAM-05 | MAY | BSS transition suggestions and power adjustment from measurements |
| NF-10 | MUST | Protocol, generator, channel plan and rollback covered by tests without radios, plus the stage 0 protocol by hand per release |

**Risks** R-11 to R-21 from section 8. **Milestone** M7 "Several access points" after M6, tasks T-M7-01 to 06 following stages 0 to 5. **Sources** S-1
onward become S-19 and following. Open questions as Q-NN only for what section 12 leaves open.

## 12. Decisions

The owner decided the role detection (Q-C) and on 2026-11-07 delegated the other detail questions with the guideline "modern, easy to use, extensible,
no legacy, newer is better". Each can be revised before adoption.

| ID | Question | Decision | Main reason |
|---|---|---|---|
| **Q-A** | Should this become part of the plan? | **Yes, after stage 0** (owner, 2026-11-08). `PLAN.md` stays unchanged until the stage 0 measurements exist, then the proposals from section 11 are adopted with the measured results | Hardware claims need real radios first |
| **Q-B** | Reference hardware | **Mini PC with 2.5 Gbit/s, MediaTek MT7916 and MT7915**, MT7922 after testing. No Intel, no USB | Open driver, AP mode, 6 GHz (3.2) |
| **Q-C** | Role detection | **Automatic through the cable** (owner, 2026-11-07) | P-11 |
| **Q-D** | FT and security | **WPA3 only, FT-SAE, 802.11k/v on by default.** WPA2 only in an isolated older-devices network on demand | 6 GHz forces WPA3, one security on all bands roams better (5.4, 5.5) |
| **Q-E** | DFS and channels | **Yes, central plan, DFS on cards with radar detection, 6 GHz preferred** | Otherwise one 80 MHz block on 5 GHz (5.6) |
| **Q-F** | Guest network | **Segments (VLANs) in the architecture from stage 2, guest and older devices in stage 4** | One mechanism for everything, no later rebuild (5.7) |
| **Q-G** | Wireless backhaul | **No. Cable, 2.5 Gbit/s recommended** | Airtime, comprehensibility. Revisit with Wi-Fi 7 (5.8) |
| **Q-H** | Wi-Fi 7 | **With Ubuntu's `hostapd` 2.12+, schema prepared now, no own build** | D-18, no x86 hardware ready yet (3.3) |

## 13. Sources

Retrieved 2026-11-06/07. Primary sources first where available. `openwrt.org` and `git.w1.fi` block automated retrieval through bot protection, which was
not circumvented. `hostapd` documentation comes from the copy in Ubuntu's package. Forums and vendor guides are marked and need confirmation in stage 0.

| ID | Source | Content |
|---|---|---|
| S-1 | [selfhosting.sh: N100 mini PC](https://selfhosting.sh/hardware/intel-n100-mini-pc/) · [Minisforum UN100C measurement](https://idlewatt.vercel.app/models/minisforum-un100c) · [measuring idle wattage](https://minilabhq-com.pages.dev/posts/measuring-mini-pc-idle-wattage) | Secondary: 6–10 W idle at the wall |
| S-2 | [Go: crypto/tls](https://pkg.go.dev/crypto/tls) | TLS 1.3, Ed25519, client certificates, own verification |
| S-3 | [VyOS T6320: Wi-Fi 6E](https://vyos.dev/T6320) · [OpenWrt forum: 6 GHz](https://forum.openwrt.org/t/6-ghz-router-openwrt/160061) | 6 GHz AP with MT7916 and MT7922, module-specific failures (forum) |
| S-4 | [TechInfoDepot: mt76](https://techinfodepot.shoutwiki.com/wiki/TechInfoDepot:Linux_driver/mt76) · [AsiaRF AW7916-AED](https://techinfodepot.shoutwiki.com/wiki/AsiaRF_AW7916-AED) · [AsiaRF AW7915-NPD](https://techinfodepot.shoutwiki.com/wiki/AsiaRF_AW7915-NPD) | Driver modes, chips, modules |
| S-5 | [AsiaRF AW7915-AED manual](https://manuals.plus/asiarf/aw7915-aed-wifi6-mini-pcie-dual-concurrents-dbdc-mpcie-card-manual.pdf) | Dual band concurrent on one card |
| S-6 | [160 MHz AP for mt7922](https://lists.openwall.net/linux-kernel/2025/06/04/60) · [mt76: AP mode for mt7921](https://isrc.iscas.ac.cn/gitlab/mirrors/github.com/sifive_riscv-linux/-/commit/116c69603b01f2d6e4499ca5d535f5b71c52052c) | Kernel patches |
| S-7 | [Banana Pi wiki: BPI-R4](https://wiki.banana-pi.org/Banana_Pi_BPI-R4) · [Banana Pi forum: BE14 Wi-Fi 7 module](https://forum.banana-pi.org/t/banana-pi-bpi-r4-bpi-be14-wi-fi7-nic-module/17182?page=10) | Wi-Fi 7 cards on ARM, firmware and 6 GHz problems (forum) |
| S-8 | [Intel community: AX210 AP](https://community.intel.com/t5/Wireless/Intel-AX210-Gentoo-Linux-OS-Unable-To-Enable-AP-Operation-at-Any/m-p/1294450) · [Intel community: AX200 5 GHz](https://community.intel.com/t5/Wireless/Wi-Fi-6-AX200-cannot-open-the-5G-frequency-band-for-hotspot-use/m-p/1313142) · [Arch forum](https://bbs.archlinux.org/viewtopic.php?id=281341) | Forums: no 5 GHz AP, "no IR" |
| S-9 | [linux-wireless: QCN9074](https://lore.kernel.org/all/CAJrE-q4qstJoqHvxE9FAQU+efa9D1UeXa_7mzxoHv9CqyM1VqQ@mail.gmail.com/T/) | Mixed reports |
| S-10 | [hostapd/wpa_supplicant v2.12 release](https://lists.infradead.org/pipermail/hostap/2026-August/045441.html) · [AlmaLinux: CONFIG_IEEE80211BE](https://git.almalinux.org/rpms/hostapd/commit/30c95af22b23c8e815a4e1f13262a5fbcc033c1c) | 2.12 in August 2026, Wi-Fi 7 more complete |
| S-11 | [Ubuntu packages: hostapd in 26.04](https://packages.ubuntu.com/en/resolute/hostapd) | 2:2.11-0ubuntu5, *universe* |
| S-12 | [RFC 5415](https://www.rfc-editor.org/rfc/rfc5415.html) | CAPWAP |
| S-13 | [FreeWTP](https://github.com/thz/freewtp) · [openCAPWAP](https://github.com/vollero/openCAPWAP) | Open implementations |
| S-14 | [CDRouter USP guide](https://support.qacafe.com/user-guide/cdrouter-usp-user-guide/) | USP transports |
| S-15 | [Wi-Fi Alliance: EasyMesh](https://www.wi-fi.org/knowledge-center/faq/what-is-wi-fi-easymesh) | Controller and agents across vendors |
| S-16 | [OpenWISP: architecture](https://openwisp.io/docs/stable/general/architecture.html) | Server components |
| S-17 | [OpenWiFi docs](https://tip-1.gitbook.io/openwifi/2.0.0/api) · [rXg manual](https://www.rgnets.com/manual/wireless/openwifi) | WebSocket, mTLS, deprecation note |
| S-18 | [OpenWISP agent: settings](https://openwisp.io/docs/stable/openwrt-config-agent/user/settings.html) | Test and restore after applying |
| S-19 | [Apple: Wi-Fi roaming support](https://support.apple.com/102002) | The device decides, support per device |
| S-20 | [Apple Deployment Guide: Wi-Fi roaming](https://support.apple.com/en-gb/guide/deployment/dep98f116c0f/web) | Thresholds, margins, neighbor list, scan time |
| S-21 | [FRITZ!: WPA3](https://en.fritz.com/guide/wpa3-the-next-generation-of-wi-fi-security) · [New Orleans ITI: client requirements](https://neworleansiti.github.io/networkstandards/wifi/wireless-client-requirements.html) | Secondary: WPA3 per operating system version |
| S-22 | [Microsoft: Fast Roaming in Windows 10](https://learn.microsoft.com/en-us/windows-hardware/drivers/network/fast-roaming-with-802-11k--802-11v--and-802-11r) | FT with 802.1X only, OKC |
| S-23 | [Purple: Android 802.11r and 802.11k](https://www.purple.ai/en-gb/guides/android-802-11r-802-11k-fast-roaming) | Vendor guide, secondary |
| S-24 | [Android: WPA3 and Wi-Fi Enhanced Open](https://source.android.com/docs/core/connect/wifi-wpa3-owe?hl=en) | WPA3, H2E from Android 12 |
| S-25 | [Espressif: Wi-Fi security](https://docs.espressif.com/projects/esp-idf/en/stable/esp32/api-guides/wifi-security.html) · [study on IoT boards](https://iris.unibs.it/handle/11379/614913) | ESP32 with WPA3, Pico W without |
| S-26 | [Cisco Community: 802.11r and legacy clients](https://community.cisco.com/t5/wireless/802-11r-fast-transition-multiple-wlans-required-for-legacy/m-p/2185017) · [MikroTik forum](https://forum.mikrotik.com/t/certain-clients-cant-connect-with-ft-enabled/271860) | Forums: old drivers and FT |
| S-27 | [Reddit r/Ubiquiti: WPA3 experiences](https://pol1.lr.ggtyler.dev/r/Ubiquiti/comments/1iefijy/what_are_your_experiences_with_enabling_wpa3_and) | Forum: IoT devices fail on WPA3 only |
| S-28 | [Cisco: Wi-Fi 6E WLAN layer](https://bxin.cisco.com/c/it_it/support/docs/wireless/catalyst-9800-series-wireless-controllers/220712-configure-and-verify-wi-fi-6e-wlan-layer.pdf) · [hostap: H2E on 6 GHz](https://lists.infradead.org/pipermail/hostap/2022-November/041096.html) | WPA3, PMF and H2E on 6 GHz |
| S-29 | [Ezurio: transition mode on 6 GHz](https://www.ezurio.com/support/faqs/in-6ghz-band-is-wpa3-able-to-run-in-transition-mode) · [Meraki: WPA3 guide](https://documentation.meraki.com/MR/Encryption_and_Authentication/WPA3_Encryption_and_Configuration_Guide) | No transition mode on 6 GHz |
| S-30 | [Juniper Mist: Wi-Fi 6E](https://juniper.net/documentation/us/en/software/mist/mist-wireless/topics/concept/wi-fi-6e-info.html) · [Nile: security across bands](https://nilesecure.com/?p=8065) · [HPE Aruba forum](https://airheads.hpe.com/discussion/single-ssid-for-wpa2-and-wpa3-on-5-ghz-and-6-ghz) | Different security per band under one name: roaming concerns |
| S-31 | `hostapd.conf` in Ubuntu 26.04's `hostapd 2:2.11-0ubuntu5`, `/usr/share/doc/hostapd/examples/`, and strings of the binary, checked 2026-11-06, program not run | FT options, RxKH lists and reload, defaults, `SET_NEIGHBOR`, `BSS_TM_REQ`, missing `ieee80211be` |
| S-32 | [OpenWrt: hostapd: fix 11r defaults when using SAE](https://lists.infradead.org/pipermail/lede-commits/2024-March/020186.html) · [hostap list: FT-SAE](https://lists.infradead.org/pipermail/hostap/2021-September/039903.html) | Local generation breaks FT with SAE |
| S-33 | [OpenWrt: hostapd.sh](https://raw.githubusercontent.com/openwrt/openwrt/main/package/network/config/wifi-scripts/files/lib/netifd/hostapd.sh) | Mobility domain derived from the SSID |
| S-34 | [EU Implementing Decision 2021/1067](https://eur-lex.europa.eu/eli/dec_impl/2021/1067/oj/eng) · [Bundesnetzagentur: Amtsblatt 25/4](https://bundesnetzagentur.de/DE/Allgemeines/Presse/Amtsblatt/Einzeldownloads/Amtsblatt_25_4.pdf) | 5945–6425 MHz, indoor low power. Amended by 2025/913, check the current text |
| S-35 | [wireless-regdb: DE on 6 GHz](https://lists.infradead.org/pipermail/wireless-regdb/2021-July/001260.html) · [wireless-regdb: EU 6 GHz unified](https://lists.infradead.org/pipermail/wireless-regdb/2022-June/001361.html) · [wireless-regdb: CEPT 6 GHz 2025](https://lists.infradead.org/pipermail/wireless-regdb/2025-July/001765.html) | `NO-OUTDOOR`, 23 dBm, 320 MHz proposal |
| S-36 | [Keenetic: DFS](https://support.keenetic.com/peak/kn-2710/en/43659-dfs.html) · [Purple: DFS](https://www.purple.ai/en-gb/guides/dfs-channels-what-they-are-and-when-to-avoid-them) | Secondary: EU channels, 60 s check. Verify against ETSI EN 301 893 |
| S-37 | [mt76: mt7915 radar detector fixes](https://lore.kernel.org/all/20210820203531.20706-2-greearb@candelatech.com/t/) · [mt76: mt7916 and background radar detection](https://patches.ctt.linaro.org/patch/540063) | Radar detection in `mt76` |
| S-38 | [systemd-networkd: VLAN on bridge ports](https://git.alternativebit.fr/picnoir/Systemd/commit/13b498f967c5117a88d72304bed1f8c0b9c1bb87) · [VLAN-aware bridge with networkd](https://cyayon.github.io/linux/Linux%20systemd%20Bridge-VLAN-aware) | `VLANFiltering`, `[BridgeVLAN]` |
| S-39 | [Linux: mac80211 Layer 2 Update patch](https://lore.kernel.org/patchwork/patch/1181168/) | Switches learn the client's new port |
