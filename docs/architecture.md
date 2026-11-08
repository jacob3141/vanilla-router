# Architecture

This document explains how vanilla-router is built and why. It is for anyone who wants to understand or
change the code. How to set up the development environment is in [development.md](development.md), the
rules for contributions are in [CONTRIBUTING.md](../CONTRIBUTING.md).

[PLAN.md](../PLAN.md) is authoritative. It contains requirements, decisions and open questions with
stable IDs (`D-05`, `F-INST-03`, `Q-19` …). This document describes **what has been built** and refers to
the IDs instead of repeating them.

## Contents

1. [The idea in five sentences](#1-the-idea-in-five-sentences)
2. [The big picture](#2-the-big-picture)
3. [Programs](#3-programs)
4. [The heart: config.json → generate → apply](#4-the-heart-configjson--generate--apply)
5. [Web interface: three processes, three sets of rights](#5-web-interface-three-processes-three-sets-of-rights)
6. [Installation: stick, assistant, target system](#6-installation-stick-assistant-target-system)
7. [Detecting the internet port](#7-detecting-the-internet-port)
8. [Updates and rollback](#8-updates-and-rollback)
9. [Stick creator for Windows](#9-stick-creator-for-windows)
10. [Packages at a glance](#10-packages-at-a-glance)
11. [Files and services on the router](#11-files-and-services-on-the-router)
12. [Security model](#12-security-model)
13. [Where to start reading](#13-where-to-start-reading)
14. [Languages](#14-languages)
15. [Status: what has been tested for real](#15-status-what-has-been-tested-for-real)
16. [Wi-Fi access point](#16-wi-fi-access-point)

## 1. The idea in five sentences

1. A router is an **unmodified Ubuntu Server LTS** on an ordinary PC with two network ports, plus a
   package `vanilla-router` (D-10, D-12).
2. Everything that makes up the router is in **one file**, `/etc/vanilla-router/config.json` (D-02). The
   files of systemd-networkd, nftables, dnsmasq, pppd and so on are **generated** from it, never edited
   by hand.
3. Every change is **checked with the services' own tools before it is applied**, replaced atomically and
   rolled back on failure. A timer rolls back network and firewall changes if nobody confirms them (P-05).
4. Non-experts set up the router without knowing Linux: a **Windows program** writes the stick, an
   **assistant** on the stick only asks about physical things (which cable goes to the modem, which
   disk), and everything else happens in the **web interface** (D-15, P-11).
5. The code is **Go with the standard library only** and vanilla JS without a build step (D-01). There is
   no supply chain apart from Go itself and Ubuntu *main* (NF-09).

## 2. The big picture

```
   Windows PC                         the future router
 ┌─────────────────────┐   stick    ┌──────────────────────────────────────────────┐
 │ vr-stick.exe        │ ─────────▶ │ boot stick (mkosi image)                     │
 │  verify manifest    │            │  vr-install on tty1                          │
 │  download image     │            │   find ports → detect WAN → choose disk      │
 │  write stick        │            │   create config.json → write target system   │
 │  vr/stick.json      │            └──────────────────────┬───────────────────────┘
 └─────────────────────┘                                   │ restart without the stick
                                                           ▼
 ┌────────────────────────────────────────────────────────────────────────────────┐
 │ target system: Ubuntu Server LTS + package vanilla-router                      │
 │                                                                                │
 │   browser in home network ─HTTPS─▶ vr-webui serve ─socket─▶ vr-webui helper    │
 │                                     (no root)                (root, fixed      │
 │                                                               action list)     │
 │                                                                   │            │
 │                     /etc/vanilla-router/config.json ◀─────────────┘            │
 │                                   │                                            │
 │                                   ▼  internal/gen (pure function)              │
 │                         generated files ─▶ internal/apply ─▶ services          │
 │                                            check, back up,   networkd, nft,    │
 │                                            replace, reload,  dnsmasq, pppd     │
 │                                            health, roll back                   │
 │                                                                                │
 │   apt ─hook─▶ vrctl snapshot pre-update     vr-health after every start        │
 └────────────────────────────────────────────────────────────────────────────────┘
```

## 3. Programs

All programs are under `cmd/`. Each is a single static binary.

| Program | Runs where, as whom | Purpose |
|---|---|---|
| `vrctl` | router, root | Command line for the configuration: `validate`, `render`, `apply`, `confirm`, `rollback`, `status`, `default`, `secret`, `setup`, `firewall-load`, `snapshot`, `health`. See the top of [cmd/vrctl/main.go](../cmd/vrctl/main.go) |
| `vr-webui` | router | Web interface with the subcommands `serve` (user `vr-webui`), `helper` (root), `prepare` (root, before start), `files` (owner of the share), `setup-code`, `set-password`. See [section 5](#5-web-interface-three-processes-three-sets-of-rights) |
| `vr-install` | boot stick, root, tty1 | Installation assistant. Arrow keys and Enter only. `--dry-run` for a dry run without writing, `--unattended plan.json` for automated tests |
| `vr-detect` | router or stick, root | Demo of WAN detection, `-list` only shows the ports (no privileges, sends nothing) |
| `vr-stick` | Windows, administrator | Stick creator (Win32 window). Only a note on other systems |
| `vr-sign` | release manager's computer | Create keys, sign and verify an image's manifest |

There are also small generators that only run during development: `internal/logo/mklogo` (logo files),
`internal/winres/mkres` (Windows resources through `go generate`), `test/stick/mkimage` (test image).

## 4. The heart: config.json → generate → apply

### 4.1 Schema

[internal/config](../internal/config/config.go) is the schema of `config.json` (version 1). Loading is
strict: unknown fields are an error. `Validate` checks **all** values and returns all errors at once
(`config.Errors`, one plain-language text per field). After that the generators need no checks of their
own.

Secrets (the PPPoE and Wi-Fi passwords) are not in `config.json`, but in files under
`/etc/vanilla-router/secrets/` (0600). `config.json` only refers to them (`password_ref`). This makes the
configuration safe to export.

`config.Default` builds the default configuration from what the assistant detected: the MAC of the
internet port, the MACs of the other ports, the network on the internet port (so the home network does
not overlap). The home network is `192.168.42.0/24`, the router `.1`, DHCP `.100`–`.249`, a random ULA
prefix per RFC 4193.

### 4.2 Generating

[internal/gen](../internal/gen/gen.go) turns a validated configuration into a list of files (`gen.File`:
path, mode, group, content). It is a **pure function** without access to the system. That is why it can
be tested with golden files: for each example configuration under
`internal/gen/testdata/<case>/config.json`, the expected output is under `…/out/`.

| Group | Files | Reloaded with |
|---|---|---|
| `link` | `/etc/systemd/network/10-vr-*.link` (rename ports by MAC to `wan0`, `lanp1`, `lanp2` …) | only after a restart |
| `networkd` | `20-vr-*.netdev`, `30-vr-*.network` (bridge `lan0`, VLAN, DHCP, IPv6 PD, RA) | `networkctl reload` |
| `firewall` | `/etc/vanilla-router/nft/vr.nft` (own tables, never `flush ruleset`, D-03) | `vrctl firewall-load` |
| `dnsmasq` | `/etc/vanilla-router/dnsmasq.conf` (DNS, DHCPv4, reservations, local names) | restart `vr-dnsmasq` |
| `ppp` | `/etc/ppp/peers/vr-wan`, `chap-secrets`, `pap-secrets` | restart `vr-pppoe` |
| `wifi` | `/etc/vanilla-router/wifi/hostapd.conf` (0600, only while the access point is on, section 16) | enable and restart or disable `vr-hostapd` |
| `sysctl`, `resolved`, `ssh`, `apt` | forwarding, resolved without stub listener, sshd, unattended-upgrades | selectively per group |

Fixed names (PLAN.md 3.4): `wan0` is the internet port, `lan0` a bridge over all other ports (even if there
is only one), with PPPoE there is also `ppp0`, with VLAN `wan0.7`.

Each generated file starts with "Generated by vanilla-router, do not edit by hand". `gen.Managed` defines
which files belong to vanilla-router. What is there but no longer generated is removed by apply (for
example the VLAN file after switching from PPPoE to DHCP).

### 4.3 Applying

[internal/apply](../internal/apply/apply.go) applies the generated files:

```
generate ─▶ compare with the current state ─▶ check in staging (nft -c, dnsmasq --test)
         ─▶ back up the old version as a generation (/var/lib/vanilla-router/generations/<id>)
         ─▶ replace atomically (write + rename) ─▶ reload only the affected groups
         ─▶ health check (firewall loaded, vr-dnsmasq running, lan0 has its address)
         ─▶ on failure: restore the generation, reload, report the error
```

The health check ([internal/apply/health.go](../internal/apply/health.go)) deliberately does not check the
internet: the modem may be off, and that should not roll back a change.

**Confirmation** (`vrctl apply --confirm 60s`, used by the web interface for network and firewall): apply
creates `pending.json` and uses `systemd-run` to start a timer that calls `vrctl rollback --pending <id>`.
The timer runs independently of the calling process. If the browser or SSH session drops because the
change broke the network, the old state still comes back. `vrctl confirm` stops the timer.

Everything that starts programs goes through the interface `apply.Runner`. In tests it is replaced by a
recorder, and `Applier.Root` points to a test folder instead of `/`.

## 5. Web interface: three processes, three sets of rights

The web interface comes from a hand-built prototype that serves a real household (PLAN.md 2.3, 3.11). It
is split into three processes with separate rights (D-04):

```
browser ──HTTPS :443──▶ vr-webui serve            user vr-webui, only CAP_NET_BIND_SERVICE,
                        internal/api, auth, sys    ProtectSystem=strict, only writes its data folder
                           │            │
     Unix socket, one JSON line         │ HTTP over a Unix socket (only /api/files/*)
     per request, SO_PEERCRED           ▼
                           ▼         vr-webui files      owner of the share (NAS extension),
                    vr-webui helper  internal/files      no capabilities, no network, all paths
                    root, internal/helper                through os.Root
                    fixed list of actions, each
                    checks its parameters itself
                           │
                           ▼
                    config.json ─▶ internal/conf ─▶ config.Validate ─▶ gen ─▶ apply
```

- **`serve`** ([internal/api](../internal/api/server.go)): login, sessions, CSRF protection, static files,
  JSON endpoints, live values through server-sent events. Reads freely readable values itself
  ([internal/sys](../internal/sys/stats.go): `/proc`, leases, traffic), asks the helper for everything else.
- **`helper`** ([internal/helper](../internal/helper/protocol.go)): each connection carries exactly one
  request `{"action": "...", "params": {...}}` and one response. Only actions registered in a fixed list
  exist (`settings.wan.set`, `portfw.set`, `dhcp.reservation.set`, `service.restart` …, list them with
  `grep -rn 's.handle("' internal/helper`). Error texts only appear in the web interface if they are
  marked as `helper.UserError`, otherwise it says "internal error" and the details are in the journal.
- **`files`** ([internal/files](../internal/files/files.go)): only for the NAS extension. Deleting moves
  things to a trash folder, downloads always go out as attachments with `CSP: sandbox`.
- **`prepare`** runs as root before `serve` (`ExecStartPre=+`): creates or renews the TLS certificate if
  the name or address has changed, takes over the setup code from the installer, writes a note for the
  console (`/run/issue.d/50-vanilla-router.issue`).

[internal/conf](../internal/conf/conf.go) translates between the API's JSON forms (from the prototype) and
`config.json`. Validation happens **only** with `config.Validate`, generation **only** with
`internal/gen`. So there is no second source of truth.

**Frontend** ([web/static](../web/static)): vanilla JS as ES modules, a hash router (`app.js`), one file
per page under `views/`. The DOM is only created through the helper `h()` in `ui.js`, never through
`innerHTML`. The CSP forbids inline scripts and `style` attributes, so styles are only set through
`el.style`. The files are embedded into the binary through `go:embed` ([web/embed.go](../web/embed.go)).
There is no build step.

**Login**: password as PBKDF2-SHA256 with 600,000 rounds ([internal/auth](../internal/auth/password.go)),
sessions only in memory (30 minutes of inactivity, at most 12 hours), login throttle per IP. The first
password is set with a setup code that the installer creates and the router shows on its screen.

## 6. Installation: stick, assistant, target system

**Images** ([image/](../image/README.md), mkosi): `image/target` builds the target system as
`rootfs.tar.zst` (Ubuntu Server with the package vanilla-router), `image/stick` a small live system that
starts `vr-install` on tty1 and contains the target system. The package is built by
[packaging/build-deb.sh](../packaging/build-deb.sh), its dependencies are in `image/packages.txt` and only
come from Ubuntu *main* (checked by `test/check-main.sh`).

**Flow of the assistant** ([cmd/vr-install](../cmd/vr-install/main.go), PLAN.md 3.9.2):

| Step | Package | What happens |
|---|---|---|
| Language | `internal/stick` | read `vr/stick.json` from the stick (from the stick creator), otherwise one choice |
| Hardware | `internal/netdetect`, `internal/disk` | find ports and disks. Only visible when there is a problem (fewer than two ports, no disk ≥ 16 GB) |
| Internet port | `internal/wandetect/live` | see [section 7](#7-detecting-the-internet-port) |
| Disk | `internal/tui` | choice and confirmation, preselection "No, cancel" |
| Installation | `internal/install` | GPT (ESP + btrfs with `@`, `@log`, `@snapshots`), unpack the target system, generate the configuration, shim + GRUB, services, setup code |
| All done | | address of the web interface and setup code, then restart |

Principle: **nothing is written until the disk is confirmed.** `install.Install` even generates all files
before the first write, so that an error in the configuration comes to light before the disk is erased.

[internal/tui](../internal/tui/tui.go) is deliberately small: arrow keys and Enter only (no text input, so
the keyboard layout does not matter), works on any console, also serial.

## 7. Detecting the internet port

The core of the assistant (PLAN.md 3.9.3). "Unplug all cables, now plug in only the cable from the modem",
and the port with the first link is the internet port.

| Layer | Package | Purpose |
|---|---|---|
| Ports | `internal/netdetect` | Ethernet ports from sysfs (without Wi-Fi and virtual devices), sorted: built-in before USB, then by bus position. Link events through netlink (`RTMGRP_LINK`), standard library only |
| Flow | `internal/wandetect` | **pure state machine**: receives events (link on/off, time, result of a probe, input), returns actions. No I/O, time is passed in. This makes timeouts and ambiguities testable deterministically |
| Probes | `internal/probe` | DHCP discover and PPPoE PADI (without and with VLAN 7) over an AF_PACKET raw socket, frames built by hand. Takes no address, so it changes nothing |
| Wiring | `internal/wandetect/live` | connects the state machine, netlink, probes and the TUI menus |

This is tested without hardware in the **namespace lab** ([test/lab/netns-lab.sh](../test/lab/netns-lab.sh)):
namespaces with veth pairs, dnsmasq as a DHCP server, pppoe-server on VLAN 7. "Plugging in a cable" there
means "turning on the other side".

## 8. Updates and rollback

unattended-upgrades applies security updates. So that a broken update does not take the router down
(D-13, [internal/snapshot](../internal/snapshot/snapshot.go)):

1. An apt hook (`packaging/apt/80vr-snapshot`) calls `vrctl snapshot pre-update` before each apt run: btrfs
   snapshot of `@` to `@snapshots/<id>`, boot entry "state before the update", state `vr_pending=1` in a
   grubenv on the EFI partition (GRUB can write to FAT, but not to btrfs).
2. The next start is an **attempt**. `vr-health.service` (`vrctl health --mark-good --reboot-on-fail`) waits
   up to 10 minutes for the same health check as apply (firewall, DNS/DHCP, home network address).
3. Healthy: confirm the start. Not healthy: restart, GRUB takes the snapshot. Renaming the subvolumes makes
   the rollback permanent.
4. It restarts **only** after an update attempt. Otherwise a permanent failure would cause a restart loop.
5. `/etc/vanilla-router` and `/var/lib/vanilla-router` are subvolumes of their own (`@config`, `@state`), so a rollback
   keeps password, configuration and secrets (F-UPD-08). Files generated from them outside these folders go back to
   the snapshot's state until the next change is applied.

The stick only installs a small base system (D-17). `vr-complete.timer` runs `vrctl complete` until it succeeds: it
fetches `ubuntu-server` and restores all firmware packages (the image keeps only the firmware of network cards), then
runs `vrctl setup` again, because the new packages bring services. This goes through the same apt hook, so it is
covered by the snapshot and the health check.

vanilla-router itself updates from its own signed apt repository (D-19, `packaging/repo/`): the package brings
source entry and key, unattended-upgrades takes `origin=vanilla-router,archive=stable`, and the postinst restarts the
services. Sessions of the web interface end, the setup stays.

## 9. Stick creator for Windows

[cmd/vr-stick](../cmd/vr-stick) is a single `.exe` without installation (PLAN.md 3.12, D-14). The window is
pure Win32 through `syscall` (no third-party libraries).

| Part | Package | Purpose |
|---|---|---|
| Describing images | `internal/manifest` | `manifest.json` + `manifest.json.sig` (Ed25519): file, size, SHA-256, location of the reservation `vr/stick.json` |
| Signing | `cmd/vr-sign` | keys, manifest, verification. The private key belongs on a computer without network access (Q-19) |
| Downloading | `cmd/vr-stick/source.go` | fetch and verify the manifest, download with resume, cache, fallback to the last state without network, local file |
| Drives | `internal/windisk` | USB bus only, never the system, program or cache drive, lock and dismount volumes, write unbuffered |
| Writing | `internal/stickwrite` | verify completely **before** writing, partition table last (so Windows mounts nothing half-written), insert `stick.json`, read back and compare |
| Defaults from Windows | `internal/windefaults` | locale, keyboard (KLID), time zone (CLDR `windowsZones.xml`) → `vr/stick.json` |
| Resources | `internal/winres` | manifest (administrator rights, DPI) and icon as `.syso`, without third-party tools |

`stick.json` is a trick that keeps the creator simple: the image contains a file of fixed size (4096 bytes,
filled with spaces). Its location is in the manifest. When writing, the creator overwrites exactly these
bytes and does not need to understand any file system.

Everything except the window and drive access is platform-independent and tested on Linux.

## 10. Packages at a glance

Who imports whom (simplified, arrow = "uses"):

```
vrctl ─────────▶ apply ──▶ gen ──▶ config
vr-install ────▶ install ──▶ apply, gen, disk, config
               ▶ wandetect/live ──▶ wandetect, netdetect, probe
               ▶ tui, stick, i18n
vr-webui serve ▶ api ──▶ auth, sys, conf, i18n, helper (client only)
vr-webui helper▶ helper ──▶ apply, gen, conf, i18n ──▶ config
vr-webui files ▶ files
vr-stick ──────▶ manifest, stickwrite, windisk, windefaults, stick   (winres, logo through go generate)
```

`config` depends on no other package of the project, `gen` only on `config`. So anyone changing the schema
finds the consequences with `grep -rln '"vanilla-router/internal/config"'`.

| Package | Content | Tests |
|---|---|---|
| `config` | schema, strict loading, `Validate`, defaults, moving the home network | error cases |
| `gen` | generators for all services | golden files |
| `apply` | applying, generations, confirmation, rollback, health, `setup` | with a test folder and runner recording |
| `conf` | translation API ↔ `config.json` | through `helper` |
| `helper` | privileged actions | dry run |
| `api` | HTTP, middleware, endpoints | `httptest` |
| `auth` | password, sessions, login throttle | unit |
| `sys` | values from `/proc`, leases, history, online monitor, native ICMP, Wi-Fi adapters and `iw` output | fake files |
| `files` | file service with `os.Root` | real directories, escape attempts |
| `install`, `disk` | installation, disks | fake sysfs, runner recording |
| `netdetect`, `probe`, `wandetect` | WAN detection | fake kernel messages and frames, artificial clock |
| `snapshot` | snapshots and GRUB state | runner recording |
| `stick`, `tui` | defaults from the stick, text interface | unit |
| `i18n` | translation catalog, see [section 14](#14-languages) | unit, placeholder check |
| `manifest`, `stickwrite`, `windefaults`, `windisk`, `winres` | stick creator | memory device, `debug/pe`, Windows test read-only |
| `logo` | logo as geometry → SVG, PNG, ICO | files in the repo must match the code |

## 11. Files and services on the router

| Path | Content |
|---|---|
| `/etc/vanilla-router/config.json` | **the** configuration |
| `/etc/vanilla-router/secrets/` | secrets (0700), e.g. the PPPoE password |
| `/etc/vanilla-router/nft/vr.nft`, `dnsmasq.conf` | generated |
| `/etc/vanilla-router/wifi/hostapd.conf` | generated, contains the Wi-Fi password (0600), only while the access point is on |
| `/etc/vanilla-router/tls/` | certificate of the web interface (self-signed, Q-09) |
| `/etc/systemd/network/*-vr-*` | generated |
| `/var/lib/vanilla-router/generations/` | backups before every change |
| `/var/lib/vanilla-router/pending.json` | waiting, unconfirmed change |
| `/var/lib/vanilla-router/dnsmasq.leases` | DHCP leases |
| `/var/lib/vanilla-router/webui/` | password hash, history, device names (user `vr-webui`, 0700) |
| `/run/vr-webui/helper.sock` | socket of the helper |

| Unit | Purpose |
|---|---|
| `vr-firewall.service` | load the firewall **before** the network (`sysinit.target`) |
| `vr-firewall-reload.timer` | daily reload (time-based blocks, daylight saving) |
| `vr-dnsmasq.service` | dnsmasq from `dnsmasq-base` (*main*), hardened |
| `vr-pppoe.service` | pppd, only if PPPoE is configured |
| `vr-hostapd.service` | Wi-Fi access point, only if it is switched on (section 16) |
| `vr-webui-helper.service`, `vr-webui.service` | web interface |
| `vr-health.service` | health check after start |
| `vr-installer.service` | stick only: assistant instead of a login on tty1 |

What Ubuntu Server brings along and gets in a router's way (resolved stub on port 53, cloud-init, netplan,
snapd …) is not removed, but turned off once by `vrctl setup` (D-12, PLAN.md 3.2).

## 12. Security model

| Boundary | Measure |
|---|---|
| Internet → router | firewall loads before the network, everything incoming is dropped, IPv4 and IPv6 in **one** `inet` table (D-09, so IPv6 is not forgotten). SSH off by default |
| Home network → web interface | only on the home network address, TLS, CSRF through Origin and the `X-Routerui` header, strict CSP, login throttle |
| Web interface → root | web server without root, helper with a fixed list of actions, `SO_PEERCRED`, each action checks for itself (D-04) |
| Operating errors | checking before applying, generations, rollback timer, health check |
| Broken update | snapshot and automatic rollback (section 8) |
| Supply chain | Go standard library only, no npm packages, Ubuntu *main* (NF-09) |
| Stick creator | signed manifest, verification before writing, USB drives only |

Reporting vulnerabilities: see [SECURITY.md](../SECURITY.md).

## 13. Where to start reading

For a first tour of the code, in this order:

1. [internal/config/config.go](../internal/config/config.go): what a router actually is.
2. [internal/gen/gen.go](../internal/gen/gen.go) and a golden file output, e.g.
   [testdata/dhcp/out/etc/vanilla-router/nft/vr.nft](../internal/gen/testdata/dhcp/out/etc/vanilla-router/nft/vr.nft).
3. [internal/apply/apply.go](../internal/apply/apply.go): how changes become safe.
4. [cmd/vrctl/main.go](../cmd/vrctl/main.go): all of this from the command line.
5. [internal/helper/protocol.go](../internal/helper/protocol.go) and
   [internal/helper/settings.go](../internal/helper/settings.go): how the web interface triggers changes.
6. [cmd/vr-install/main.go](../cmd/vr-install/main.go) and
   [internal/wandetect/machine.go](../internal/wandetect/machine.go): the path for non-experts.

## 14. Languages

The code, documentation and comments are in English. English is also the default language of the product.
German can be chosen in the assistant (or comes from a German Windows through the stick creator).

- **One catalog:** [internal/i18n/de.json](../internal/i18n/de.json) maps English texts to German. `{}` stands
  for a value filled in at runtime. A missing entry only ever shows English.
- **Translated at the point of display,** not at the point of creation: `internal/tui` and the console note
  call `i18n.T`, and `h()` in `web/static/ui.js` translates all text it creates through
  [web/static/i18n.js](../web/static/i18n.js). So helper error messages can stay plain English strings.
- **The language follows `system.locale`** in `config.json`: `de*` means German, everything else English.
  `/api/state` reports it, `/api/i18n` delivers the catalog.
- **Adding a text:** write English in the code and add the German to `de.json`.
- The stick creator runs before there is any configuration. It keeps its own table for both languages in
  [cmd/vr-stick/texts.go](../cmd/vr-stick/texts.go) and follows the Windows language.

## 15. Status: what has been tested for real

Honestly: not much yet. The exact status per task is in PLAN.md 7.

| Part | Status |
|---|---|
| Unit tests, golden files, `nft -c`, `dnsmasq --test`, `systemd-analyze verify` | pass |
| Web interface | checked in the browser against the local test instance; on an installed router (Hyper-V VM) set up and used by hand, not yet with a systematic checklist |
| Port detection (`vr-detect -list`) | tested on real hardware |
| Stick creator | tested with a real stick and a test image |
| Boot stick (mkosi) | built, starts in Hyper-V with Secure Boot on; the assistant detects the cable and offers only the right disk |
| Installation | complete run in a Hyper-V VM, including the restart without the stick and the start from the disk with Secure Boot. **Not yet on real hardware** |
| Namespace lab | all 5 scenarios pass (WSL2, real netlink, raw sockets, VLAN 7) |
| Online step (`vrctl complete`) | runs in the VM in about 5 minutes; snapshot, next start and confirmation work |
| DHCP and DNS on the installed router | offer seen by a network-boot client in the VM; no complete lease yet |
| Rollback after a **broken** update, PPPoE on a real line | **untested**, test plan in PLAN.md 7.3 |
| Wi-Fi access point | unit tests, golden files, the real `hostapd` reads the generated files, apt pinning, page against the test instance with a simulated adapter. **Never run with a real radio**, see section 16 |

## 16. Wi-Fi access point

An optional extension (F-WIFI, D-18): a Wi-Fi adapter in the router becomes a port of the home network.

```
config.json "wifi" ─▶ gen ─▶ /etc/vanilla-router/wifi/hostapd.conf ─▶ vr-hostapd.service
secrets/wifi-<id> ───┘                                                   │ bridge=lan0
                                                                         ▼
                           Wi-Fi devices ──▶ wlp3s0 ──▶ lan0 ──▶ DHCP, DNS, firewall as on a cable
```

- **Bridge:** `hostapd` puts the adapter into `lan0` itself, because the kernel only allows that once the adapter is in
  access point mode. networkd leaves the adapter alone (`30-vr-wifi.network`, `Unmanaged=yes`). The firewall and dnsmasq
  know nothing about Wi-Fi, device blocks by MAC apply as usual.
- **Adapter name:** the kernel's own name (`wlp3s0`, `wlx…`) in `wifi.interface`. A `.link` file like the ports have would
  only take effect after a restart.
- **Password:** under `secrets/wifi-<id>`, a new password gets a new name, so a rollback finds the old one (as with PPPoE).
  The browser only learns whether there is one. `hostapd.conf` exists only while the access point is on.
- **Checking:** `hostapd` has no check mode, an exception to P-05. `config.Validate` restricts the values, the helper compares
  them with what the adapter reports (`iw phy`), and after the start apply checks that `iw dev … info` reports an access point.
  If not, it rolls back and passes on the last lines from `hostapd`. Every change waits 5 minutes for confirmation, because
  whoever is on Wi-Fi has to reconnect first.
- **Channels:** deliberately without DFS channels and without automatic selection (F-WIFI-09), 2.4 GHz only with 20 MHz, on
  5 GHz up to 80 MHz. Before the first start the kernel knows no country, so much of 5 GHz is reported as "no IR". The helper
  therefore lets those channels through and the start decides.
- **Packages (NF-09):** `hostapd` is in *universe*, the image only has *main*. "Download Wi-Fi software" on the page first writes
  `/etc/apt/sources.list.d/vr-wifi.sources` and `/etc/apt/preferences.d/vr-wifi`, which forbids everything from *universe* except
  `hostapd`, then runs `apt-get` as the unit `vr-wifi-install`. Routers without an adapter never do this. `image/packages-wifi.txt`
  is the list that `test/check-main.sh` checks, `test/universe-allow.txt` holds the exception.
- **Without hardware (F-WIFI-04):** `features.wifi` in `/api/state` comes from sysfs (`internal/sys/wifi.go`). No adapter: no menu
  item, and opened directly the page explains why. A PCI card of class `0x0280` without a wireless device: firmware is still
  missing, it comes with `vrctl complete` (D-17) and takes effect after a restart. If `/var/lib/vanilla-router/online-complete` already exists, the page says instead that Linux probably has no driver for this card. A USB adapter without firmware cannot be told
  apart, it stays invisible until it works. An access point that is set up keeps the page visible even if its adapter is gone.
