# SDN feature – state of the work (2026-11-08)

Everything below is in the working tree and **not committed**. `PLAN.md` is unchanged: the owner decided to adopt the concept
after stage 0 (Q-A in `docs/sdn-concept.md`).

## What exists now

| Part | Files | How it was checked |
|---|---|---|
| Schema `wireless` with strict validation | `internal/config/wireless.go` | unit tests |
| Page "Access points": setup, pairing with code comparison, AP list, edit, preview, remove, networks, guest and older-devices presets, segments, channel plan, English and German | `web/static/views/wireless.js`, `internal/i18n/de.json` | clicked through in the test instance, desktop and phone width |
| Segments on the router: VLAN-aware `lan0`, one VLAN interface per segment, DHCP and DNS, firewall (internet only, `reachable_from`) | `internal/gen/segments.go`, `networkd.go`, `nft.go`, `dnsmasq.go` | golden files, real `nft -c` and `dnsmasq --test`. Existing golden files unchanged: a router without segments generates what it did before |
| Pairing code | `internal/pairing/` | unit tests with fixed vector |
| Channel plan, AP bundle (hostapd, FT-SAE key lists, VLAN bridge) | `internal/gen/ap.go` | golden files |
| `vr-apd` on the router: TLS 1.3 with pinned Ed25519 keys, pairing, long-poll bundles, one AP at a time, reports | `internal/apd/`, `vr-webui apd`, `packaging/systemd/vr-apd.service` | end-to-end over loopback |
| Helper actions `wireless.*` and `ap.*`, `ap.*` only for the user `vr-apd` | `internal/helper/wireless*.go`, `server.go` | unit tests including the gate per user |
| Agent on the AP: pairing, follow, apply with rollback timer and health check, wipe when removed | `internal/apagent/`, `vrctl ap enable\|run\|rollback\|status`, `vr-ap.service`, `vr-ap-hostapd@.service` | end-to-end over loopback, and as a real process against real `vr-apd` in the test instance |

Full run: `test/check-all.sh` passed, the `.deb` builds and contains the new units.

Real chain tested in the test instance (`VR_APD=1 VR_WIFI=1 test/dev/webui.sh`): agent connects over TLS, shows the same code as the page, is
paired, receives its bundle, writes it (to a test folder), reports back and shows as online. When a second AP joined, it reloaded only the key
list. When removed, it wiped its files and keys and started asking again with a new key.

## Not done

1. **Role detection in the installer (concept 4.1).** Today an AP is made with Ubuntu Server, the package and `vrctl ap enable`. Changing the
   assistant needs a boot test in a VM.
2. **The router's own Wi-Fi card** stays in the `wifi` block and is not a node of the roaming network (concept: `local`).
3. **One owner confirmation across APs.** Each AP protects itself (health check, rollback timer), and the rollout is one at a time. While a
   change on the router waits for confirmation, APs stay on the old bundle. A later "no" does not take back what APs already applied.
4. **Switch from WPA2/WPA3 with a live device check** (concept 5.5).
5. Channel plan from measured neighborhood, automatic move after a radar event.

## Never tested on real hardware

No radio, no real AP, no VLAN switch was involved. Open until stage 0:

- whether `hostapd` accepts the generated files (6 GHz `op_class` and `he_6ghz_reg_pwr_type`, several BSS with `bssid=`, `rxkh_file`),
- whether FT-SAE works with these `r0kh`/`r1kh` lines, and whether clients really roam,
- whether `networkctl reload` turns on `VLANFiltering` on a running `lan0`, or the bridge needs to be recreated,
- whether renaming the cards through `.link` files and `udevadm trigger` works while running,
- whether the `iw` limits for transmit power are accepted, and radar detection (DFS) per card,
- the hardening of `vr-ap-hostapd@.service`.

Do not install this on the productive router (`router.home.arpa`): it runs a different code base.

## Assumptions (for the owner to check)

- `wireless` is an optional block next to `wifi`. Schema version stays 1.
- Segments are IPv4 only. Devices in a segment can resolve names of the home network through DNS, but cannot reach those devices.
- Country table for the plan: ETSI countries (6 GHz up to channel 93), US and CA (whole 6 GHz band), all others 5 GHz 36–48 only and no 6 GHz.
- Limits: 8 segments, 8 networks, at most 4 networks per band, 16 APs, 4 cards per AP.
- Port 7443 for `vr-apd`. The firewall keeps it to the home network, segments cannot reach it.
- An AP counts as removed only after 3 refusals in a row, because a rollback on the router may forget it for a moment.
- Removing an AP renews all roaming keys. Wi-Fi passwords stay, as in concept 4.4.
- ID collision: `PLAN.md` already uses **D-19** (signed package source). The concept's D-19..D-28 must be renumbered on adoption.
