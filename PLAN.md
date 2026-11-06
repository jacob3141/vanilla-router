---
document: vanilla-router-plan
title: "vanilla-router: planning document"
version: 0.21
status: draft
license: AGPL-3.0-or-later
updated: 2026-11-06
language: en
id_schema:
  principle: "P-NN"
  lesson: "L-NN"
  requirement: "F-<AREA>-NN (functional) | NF-NN (non-functional)"
  decision: "D-NN"
  question: "Q-NN"
  risk: "R-NN"
  milestone: "MN"
  task: "T-MN-NN"
  source: "S-NN"
status_values: [open, in-progress, done, rejected]
priority_values: [MUST, SHOULD, MAY]
---

# vanilla-router: planning document

> **Note for agents:** this document is the project's authoritative planning source. IDs are stable: they are never
> reassigned and never renumbered. What is dropped gets the status `rejected` and stays. New entries get the next free
> number. With every change of content, update `version` and `updated` at the top and add an entry to the change
> history below. Decisions (D-NN) only change with a reason.

---

## 1. Vision

A **router built from a perfectly ordinary Linux** on **any standard hardware** with at least two network ports, set up
from a **boot stick that guides even non-experts step by step**.

The flow from the user's point of view (set by the project owner on 2026-11-04, D-15):

1. **Create the stick:** on Windows, download a program and start it without installation, choose the USB stick, "Create
   stick". There are no questions and no prior configuration (3.12).
2. **Boot:** unplug the stick, plug it into the future router, turn it on. The computer starts a small Linux from the stick.
3. **Only the essentials:** the assistant only asks what cannot be detected, that is, what is physical:
   > "Now plug the cable from your modem into one of the network ports."

   The assistant recognizes the port and makes it the internet port (WAN), all others become the home network (LAN). There
   is also the confirmation of which disk gets erased. Nothing else.
4. **Everything else in the browser:** the provider's credentials, the name, networks and every other setting go through
   the web interface.

### 1.1 Why

Specialized router operating systems like OpenWrt are built for devices with a few MB of flash and RAM. This leads to
stripped-down tools, a package world of their own, a limited interface and a high barrier to entry. Used office PCs or
mini PCs with several network ports often cost only €100–200 today and draw about 6–15 W at idle. They run a full Linux
with all packages, security updates and tools. The concept "router OS for minimal hardware" is obsolete for these devices.

The existing alternatives for x86 hardware (OPNsense/pfSense on FreeBSD, IPFire, VyOS) are either appliances for
professionals or distributions of their own. **An unmodified Debian/Ubuntu with an assistant for non-experts in front of it
does not exist.** vanilla-router wants to fill this gap.

### 1.2 Principles

| ID | Principle | Meaning |
|---|---|---|
| P-01 | **Vanilla instead of a special OS** | The base is an unmodified standard distribution (LTS). No kernel of its own and no package sources of its own for the base system. Everything vanilla-router adds is a normal package or normal configuration that you can understand and change by hand if needed. |
| P-02 | **Suitable for non-experts** | From a bare computer to a running router without Linux knowledge. No jargon without explanation, feedback at every step, sensible defaults instead of questions. |
| P-03 | **Secure by default** | Firewall for IPv4 **and** IPv6 before the first packet, no open services to the outside, automatic security updates, no default passwords. |
| P-04 | **One source of truth** | The desired state is in a single declarative file. All configuration files are generated from it, never edited by text search. |
| P-05 | **Check before applying, roll back on failure** | Every change is generated, checked with the service's tool (`nft -c`, `dnsmasq --test` …), replaced atomically and checked for health after reloading. On failure it rolls back automatically. |
| P-06 | **Repairable** | Updates and configuration changes can be undone (snapshots, the old state in the boot menu). A router that does not come up after an update is a total loss for non-experts. |
| P-07 | **Installable offline** | The stick contains everything for a working base installation. Internet is only needed for updates. |
| P-08 | **Few dependencies** | Own code in Go with the standard library only, frontend without frameworks and without a CDN. This keeps the attack surface small and the project long-lived. |
| P-09 | **No telemetry** | The router does not phone home. Connection checks only go to neutral, documented targets. |
| P-10 | **Comfort over optimization** | When convenience (a well-known distribution, a wide choice of packages, common guides, familiar tools) and optimization (smallest system, fewest packages, the last percent of performance) conflict, comfort wins. The hardware has enough reserves. Security (P-03) is exempt and is never sacrificed for comfort. Set by the project owner on 2026-11-04. |
| P-11 | **Only ask what cannot be detected** | The assistant on the stick only asks about physical things: which cable goes to the modem and which disk may be erased. Everything else gets a sensible default and can be changed later in the web interface. There is no text input on the stick, only arrow keys and Enter. So the keyboard layout does not matter either. Set by the project owner on 2026-11-04. |

### 1.3 Target groups

| ID | Group | Expectation |
|---|---|---|
| Z-1 | **Non-experts** (household, family) | Start a program on Windows, choose a stick, put the stick in the router, plug in the modem cable, done. After that only the web interface, never a console. |
| Z-2 | **Tinkerers and self-hosters** | Full control over a normal Linux, Docker, NAS and their own services on the same device, without the router function suffering. |
| Z-3 | **Small offices, clubs** | Like Z-1, plus guest network (later VLAN), port forwards and remote access (later VPN). |

### 1.4 Non-goals (for now)

- No replacement for enterprise firewalls (HA clusters, IDS/IPS, deep packet inspection).
- No Wi-Fi mesh, roaming or guest SSIDs, and no Wi-Fi client mode. A Wi-Fi adapter in the router can be an access point in the home network (F-WIFI, D-18). External access points keep working as before.
- No support for ARM single-board computers in version 1 (x86-64 first, see Q-08).
- No modem: DSL, cable and fiber modems stay separate devices (bridge mode or a router in front).

---

## 2. Starting point: the prototype

The prototype is a hand-built router in the project founder's household, in use since 2025. In November 2026 it was backed
up, hardened and given a web interface (`routerui`). The code for it is currently in a **private** working repo and is to be
taken over after cleaning (M2).

### 2.1 Hardware and base system

| Item | Prototype |
|---|---|
| Device | Dell OptiPlex 3050 (Intel Core i3-6100T, 4 threads, 8 GB RAM, NVMe 2 TB, UEFI) |
| Network | 2 × Ethernet: onboard (WAN, link forced to 100 Mbit/s) and a second card (LAN, 1 Gbit/s) |
| System | Ubuntu 26.04 LTS (originally 25.04, upgraded on 2026-11-03), kernel 7.0 |
| Connection | WAN through DHCP behind a provider router (cable, double NAT). Public IPv6 only on the WAN interface, no prefix delegation into the LAN |
| Secondary roles | NAS (Samba share ≈ 212 GB), Docker (Home Assistant, Omada controller, Portainer, GitLab) |

### 2.2 Building blocks of the prototype

| Building block | Implementation in the prototype | Assessment for vanilla-router |
|---|---|---|
| Interface names | netplan, renaming by MAC to `wan0`/`lan0` | **take over** (fixed, meaningful names) |
| IPv4 firewall + NAT | own nft table `ip router_v4` (filter input/forward, masquerade), loaded by `router-v4-firewall.service` early at boot (`Before=network-pre.target`) | **take over** |
| IPv6 firewall | own nft table `ip6 router_v6`, ICMPv6 per RFC 4890, DHCPv6 client responses allowed | **take over**, extend with forwarding for a LAN prefix |
| Port forwards | own table `ip routerui_portfw` (DNAT), allowed in router_v4 through `ct status dnat accept` | **take over** |
| Device blocks | own table `inet routerui_block` (forward, priority −10), time-based blocks with `meta hour/day`, time zone through `TZ` on load | **take over** |
| DNS | dnsmasq, local names under `home.arpa` (RFC 8375) through `host-record` (also provides PTR) | **take over** |
| DHCP | isc-dhcp-server with managed include files, dynamic ranges are split automatically around reservations | **replace** with dnsmasq DHCP (isc-dhcp is discontinued and only in universe), see D-05 |
| Web interface | `routerui`: Go (standard library only), three processes with separate rights (see 2.3) | **take over and expand**, becomes the core of the project |
| Security audit | Bash script that only reads (≈ 30 checks), comparison with the last run | **take over** as a built-in self-check |
| Configuration backup | Bash script (`/etc`, crontabs, leases, Docker data, metadata) | **take over**, later as an export in the web interface |
| Updates | unattended-upgrades (security only), no automatic restart, no snapshots | **expand**: snapshots and rollback (F-UPD) |

### 2.3 routerui: architecture of the prototype

```
browser ──HTTPS──▶ routerui serve        (user routerui, only CAP_NET_BIND_SERVICE, heavily hardened)
                     │  login, sessions, CSRF, static frontend (go:embed)
                     ├─Unix socket──▶ routerui helper   (root, fixed actions only, SO_PEERCRED)
                     │                 generates/checks/applies configuration, firewall, services, updates
                     └─Unix socket──▶ routerui files    (user of the NAS share, no network, no caps)
                                       file browser, paths only through os.Root
```

- **Source of truth** is the helper's `state.json` (reservations, DNS names, port forwards, blocks). The package `conf`
  contains validation rules and generators, it reads and writes nothing itself.
- **Flow of every change:** check the whole state → generate files → check with the service's tool → replace atomically →
  reload → check health → on failure restore the old version. Firewall changes are active immediately and are rolled back
  after 60 s without confirmation.
- **Test instance:** the same binary with `--dev` on `127.0.0.1:9443`, helper in dry-run mode (only writes under a test root
  folder, restarts nothing), **same systemd hardening** as the real one.
- **Security:**
  - Password as a PBKDF2-SHA256 hash (600k rounds), setup only with a one-time setup code
  - Sessions only in memory, ending after 30 min without activity, at most 12 h
  - Login throttle and fail2ban jail
  - Strict CSP, DOM only through an `h()` function (no `innerHTML`)
  - Downloads always as attachments with `CSP: sandbox`
- **Feature set** (as of 2026-11-04, role models FRITZ!Box, OpenWrt LuCI, OPNsense, Pi-hole, UniFi):
  - Overview with live traffic, 24-hour history and a notes center, online monitor (native ICMP, outage log)
  - Devices with a detail page, names and notes, "new" detection, fixed addresses, Wake-on-LAN, device block and time-based block, CSV export
  - DNS names and DNS statistics, firewall with port forwards and fail2ban
  - Connections (conntrack through netlink), network status (interfaces, routes, neighbors, IPv6, open ports)
  - Services, containers and logs (system and change log)
  - System with updates, NVMe SMART through ioctl, audit, restart
  - File browser for the NAS share with a trash folder, tools (ping, tracepath, DNS, port check)
  - Session management, export/import of the configuration, quick search (Ctrl+K)

### 2.4 Lessons from the prototype

These points came up while hardening and expanding the prototype. Each is a requirement for vanilla-router (reference in the
last column).

| ID | Lesson | Consequence for vanilla-router |
|---|---|---|
| L-01 | The IPv4 firewall was tight, for **IPv6 there were no rules at all**, and SSH was reachable through the public IPv6 address. | Firewall for both protocols from the start, self-test (F-SEC-01, F-SEC-08) |
| L-02 | The firewall was set through an `@reboot` cron job: at boot there was a time window without a filter. | Firewall as a systemd unit before `network-pre.target` (F-SEC-02) |
| L-03 | The system ran for 10 months on an **Ubuntu version without support**, so unattended-upgrades did nothing. | LTS only, a note long before the end of support, guided release upgrade (F-UPD-05) |
| L-04 | AppArmor was turned off because a container could not be stopped (problem with the runc profile). | Never silently turn off security mechanisms, the web interface shows their state (F-SEC-06) |
| L-05 | A release upgrade aborted because `/etc/timezone` was an **empty directory** (a leftover of a Docker mount). | Pre-check before upgrades, snapshot beforehand (F-UPD-02, F-UPD-06) |
| L-06 | During cleanup, purging `grub-pc` **also deleted `/etc/default/grub`**. | No cleanup actions without a snapshot, a defined and tested package list (F-UPD-02) |
| L-07 | `dhcp-host` in dnsmasq had no effect because isc-dhcp did DHCP. Two services for DNS and DHCP are confusing. | One service for DHCP and DNS (D-05) |
| L-08 | Docker containers through ipvlan sit directly in the LAN and bypass the host's firewall. A container with `docker.sock` amounts to root on the router. | Design container networks deliberately, warnings in the web interface (F-SEC-09) |
| L-09 | The program `ping` (file capability `cap_net_raw`) did not start under the web interface's `CapabilityBoundingSet`. The test instance was less strictly hardened, so this only came to light in production. | Test environment with identical hardening, no external programs with file capabilities from hardened services (NF-05) |
| L-10 | The hash of `nft list ruleset` changes constantly (packet counters). | Always compare with `nft -s` (stateless), applies to tests and self-checks |
| L-11 | A slow package mirror (≈ 200 KB/s) delayed the upgrade by hours. | Choose a mirror or check the speed before large downloads (F-UPD-07) |
| L-12 | A DHCP pool from `.0` (permissible in the larger network) assigned an address ending in `.0`, which some devices misread as a network address. | Pools never start at `.0` or `.255` of a /24 (F-NET-07) |
| L-13 | Many devices report random MAC addresses (Wi-Fi privacy). Reservations and blocks by MAC then do not apply. | Note in the web interface (implemented), in the long run device identification beyond the MAC (Q-12) |
| L-14 | The search domain handed out through DHCP was a fantasy name instead of the local domain, so short names did not work. | Search domain `home.arpa` as the default (F-NET-05) |
| L-15 | Without a backup, the whole NAS share was on a single SSD. | Plan a backup function for secondary roles (F-ADD-03) |
| L-16 | With blocks across midnight, the assignment of weekdays was wrong at first (the morning belongs to the next day). | Secure time logic with tests (implemented, keep it) |
| L-18 | When taking over the prototype's code, test data contained real values from the home network (a device's MAC, WAN address, device names), also as bytes and hex values. | Search taken-over code for addresses, MACs and names before committing, also in byte and hex notation; test data only with documentation values |
| L-17 | The package check (T-M1-08) found nothing on the German-language prototype because apt writes "Hängt ab von:" instead of "Depends:". On a system without *universe* in its sources, universe packages slipped through as "unknown". | Evaluate tool output only with `LC_ALL=C`. Checks must fail on missing data, not pass |
| L-19 | The first boot stick lacked `sfdisk`: on Ubuntu it is in `fdisk`, which only comes along as a recommendation, and mkosi installs without recommendations. The error only appeared after the disk was confirmed. | The stick's build checks that all tools the installer calls exist (`image/stick/mkosi.finalize.chroot`) |
| L-21 | In VirtualBox on an active Hyper-V, the installer seemed to hang while waiting for the cable. In fact the VM stood still because timer interrupts got lost; the first keypress let everything continue. Without any sign of life, a frozen program and a waiting one cannot be told apart. | Show activity whenever the assistant waits (spinner with seconds, `tui.StartSpinner`). Workaround for this VM setup in docs/development.md, 5.5 |
| L-20 | mkosi's presets enable all services of the package after the postinst scripts. On the stick the firewall, DHCP and web interface would have started. | Switch services in `mkosi.finalize.chroot`, which runs after the presets |

### 2.5 What is still manual work on the prototype

This is the list of what the installer has to automate:

1. Install the base system, partitioning, boot loader.
2. Assign network cards, name `wan0`/`lan0` by MAC (netplan).
3. IP forwarding (`sysctl`), set the LAN address and network.
4. Install firewall files and units for IPv4/IPv6.
5. Configure dnsmasq (DNS) and isc-dhcp (DHCP), choose upstream DNS.
6. Harden SSH, create users, set up unattended-upgrades.
7. Build and install routerui (units, user, TLS certificate, fail2ban jail).
8. Optionally Samba, Docker etc. for secondary roles.

---

## 3. Target architecture

### 3.1 Overview

```
┌──────────────── Stick creator (Windows, 3.12) ────────────────────┐
│ one .exe without installation · downloads and verifies the image ·│
│ writes the stick · adds language, keyboard, time zone from Windows│
└──────────────┬────────────────────────────────────────────────────┘
               │ USB stick
               ▼
┌───────────────────── Boot stick (live image) ─────────────────────┐
│ minimal Linux · installation assistant · image of the target system│
└──────────────┬────────────────────────────────────────────────────┘
               │ detects hardware, asks, installs
               ▼
┌──────────────────────── Target system (disk) ─────────────────────┐
│ Ubuntu LTS (unmodified, D-10) + package vanilla-router            │
│                                                                   │
│  /etc/vanilla-router/config.json   ◀── single source of truth     │
│            │                                                      │
│            ▼  vrctl apply (validate → generate → test → apply)    │
│  systemd-networkd · nftables · dnsmasq · pppd · timesyncd · u-u   │
│            ▲                                                      │
│  routerui (web interface) ──── helper ── reads/writes config      │
└───────────────────────────────────────────────────────────────────┘
```

### 3.2 Base system

- **Distribution:** **Ubuntu Server LTS** (first 26.04) with the standard package set of the metapackage `ubuntu-server` (D-10,
  D-12, decided on 2026-11-04, P-10). Reasons: the prototype runs on it, Ubuntu is the best-known distribution, has the most
  guides and supports new hardware early (HWE kernel). Anyone who knows Ubuntu Server immediately finds their way around, every
  guide for Ubuntu Server applies.
  - **What Ubuntu Server brings along and how it is handled** (D-12): packages are not removed, because `ubuntu-server-minimal`
    has them as hard dependencies. Only configuration is adapted:
    | Component | Problem for a router | Handling |
    |---|---|---|
    | `systemd-resolved` | stub listener occupies port 53, which dnsmasq needs (masked in the prototype) | `DNSStubListener=no` through a drop-in, resolved stays active and points to dnsmasq |
    | `cloud-init` | searches for cloud data sources at every start, can reconfigure the network | `/etc/cloud/cloud-init.disabled` |
    | `netplan` | second network configuration next to networkd | empty configuration, networkd files come from vrctl (D-06) |
    | `snapd` | service without a purpose, no snaps installed | stays installed, service and socket disabled (extensions may turn it on) |
    | `multipath-tools`, `open-iscsi`, `lxd-installer` | unneeded services (audit finding in the prototype) | services disabled |
    | `chrony` | none, it is Ubuntu Server's time service | stays, later also an NTP server for the home network |
  - **Security updates only from *main*** (as of 26.04, checked on the prototype [S-15]):
    | Package | Archive | Consequence |
    |---|---|---|
    | `dnsmasq-base` (the program) | main | security updates guaranteed |
    | `dnsmasq` (only unit and default configuration) | universe | **do not install**. Own unit in the package vanilla-router that starts `dnsmasq-base` |
    | `nftables`, `ppp`, `btrfs-progs`, `kea-dhcp4-server` | main | fine |
    | `fail2ban` | universe | **replace** with a throttle of its own in routerui (exists in the prototype) and nft rate limiting. SSH is off by default (F-SEC-04) |
    | `snapper` | universe | rollback with a small tool of its own through `btrfs subvolume snapshot`, or an exception with a reason (Q-03) |
    | `isc-dhcp-server` | universe, discontinued | dropped (D-05) |
    | `hostapd` | universe | **exception for the Wi-Fi extension only** (D-18), never part of the base system, pinned |
  - Rule for the package vanilla-router: **dependencies only from *main***. Exceptions from *universe* only with a reason in a
    D-NN and only for extensions (F-ADD). A CI check enforces this (T-M1-08).
  - Ubuntu Pro (free for private users, covers *universe*) is offered, but **not required**.
  - No snaps are installed, snapd is off (see the table above).
  - Support: 5 years of standard support per LTS, a new LTS every two years with a guided upgrade (F-UPD-05).
- **Network stack:** systemd-networkd directly, without netplan as an intermediate layer (see D-06). networkd can do DHCPv4/v6
  client, prefix delegation, router advertisements and bridges.
- **Package set of the base installation:** `ubuntu-server` and additionally:
  - Network: `systemd-networkd`, `nftables`, `dnsmasq-base`, `ppp` (PPPoE through pppd's rp-pppoe plugin)
  - Time: `chrony` (comes with Ubuntu Server)
  - Security: `unattended-upgrades`, `apparmor`, `openssh-server` (installed, but off). No fail2ban (see above)
  - Tools: `ethtool`, `iproute2`
  - Snapshots: `btrfs-progs` + snapshot tool (see F-UPD)
  - Package `vanilla-router`: routerui, vrctl, units
- **No** desktop, no Docker and no Samba in the base installation. Those are extensions (F-ADD).

### 3.3 Configuration model

One file `/etc/vanilla-router/config.json` describes the desired state. The tool `vrctl` (same code base as routerui) generates
all files from it. A sketch of the schema (defined exactly and versioned in M2):

```json
{
  "schema": 1,
  "system":   { "hostname": "router", "timezone": "Europe/Berlin", "locale": "de_DE.UTF-8" },
  "wan":      { "interface": "wan0", "match_mac": "aa:bb:…", "type": "dhcp",
                "pppoe": { "user": "…", "password_ref": "secret:pppoe", "vlan": 7 },
                "ipv6": { "mode": "auto", "request_prefix": 56 } },
  "lan":      { "interface": "lan0", "members": ["aa:bb:…", "cc:dd:…"],
                "ipv4": "192.168.42.1/24", "ipv6": { "ula": "fd12:3456:789a::/48", "from_wan_prefix": true } },
  "dhcp":     { "pool": ["192.168.42.100", "192.168.42.249"], "lease": "12h",
                "reservations": [ { "name": "printer", "mac": "…", "ip": "192.168.42.20" } ] },
  "dns":      { "domain": "home.arpa", "upstreams": ["9.9.9.9", "1.1.1.1"], "records": [] },
  "firewall": { "port_forwards": [], "blocks": [], "ipv6_pinholes": [] },
  "services": { "ssh": { "enabled": false }, "webui": { "listen": "lan" } },
  "updates":  { "security_auto": true, "reboot_window": "04:00-05:00", "snapshots": true }
}
```

- The **ULA prefix** is generated **randomly** at installation per RFC 4193 (40-bit global ID) and never changed afterwards. The
  value in the example above is only a placeholder (F-NET-12).
- **wan.type** is one of `dhcp`, `pppoe`, `static`, later `dslite`. With `pppoe`, `vlan` is optional.
- **Secrets** (PPPoE password, hash of the UI password) are not in `config.json`, but in `/etc/vanilla-router/secrets/` (0600).
  `config.json` only refers to them. This makes the configuration safe to export.
- **Generated files** have a header "Generated by vanilla-router, do not edit by hand" and are in known places
  (`/etc/systemd/network/10-vr-*.network`, `/etc/nftables.d/vr-*.nft`, `/etc/dnsmasq.d/vr.conf`). Hand-written additions stay
  possible through designated files without the `vr-` prefix.
- **Flow of `vrctl apply`** (taken over from the prototype):
  1. Check the schema and validate values (e.g. overlaps, duplicate IPs).
  2. Generate all files into a temporary folder.
  3. Check with the services' tools (`nft -c`, `dnsmasq --test`, `networkctl` …).
  4. Replace atomically, keep the old version.
  5. Reload services selectively.
  6. Health check (LAN reachable, DNS answers, WAN connection as before).
  7. On failure restore the old version, reload, report the error.
  For network and firewall changes the 60 s confirmation applies as well.

### 3.4 Network defaults

- **Names:** `wan0` for the internet port. `lan0` is a bridge over all other ports, even if there is only one, so later ports
  can be added without rework.
- **LAN IPv4:** a /24 from `192.168.0.0/16` or `10.0.0.0/8` that does **not** overlap with the WAN network. The installer chooses
  it automatically once it knows the WAN network (F-NET-03).
- **DHCP pool:** `.100`–`.249` (L-12), reservations preferably below `.100`.
- **IPv6:** DHCPv6 with prefix delegation on WAN (ask for a size, typically /56 or /62), a /64 from it for the LAN, router
  advertisements with SLAAC in the LAN. Always a ULA prefix as well for stable local addresses. Without a delegated prefix only
  ULA, the web interface explains why (as in the prototype). systemd-networkd can do this natively, provided networkd manages
  **both** interfaces [S-03] [S-04]:

  ```ini
  # 10-vr-wan.network (with PPPoE: match on ppp0, DHCPv6 runs over the PPP connection)
  [Match]
  Name=wan0
  [Network]
  DHCP=yes
  IPv6AcceptRA=yes
  [DHCPv6]
  PrefixDelegationHint=::/56
  [DHCPPrefixDelegation]
  UplinkInterface=:self
  SubnetId=0
  Announce=no

  # 10-vr-lan.network
  [Match]
  Name=lan0
  [Network]
  Address=192.168.42.1/24
  DHCPPrefixDelegation=yes
  IPv6SendRA=yes
  [DHCPPrefixDelegation]
  UplinkInterface=wan0
  SubnetId=1
  [IPv6Prefix]          # ULA in addition, independent of the provider
  Prefix=fd12:3456:789a:1::/64
  Assign=yes
  ```
  Q-04 decides whether RA comes from networkd or from dnsmasq. There may only be **one** source of RA in the LAN.
- **DNS:** dnsmasq as a resolver and cache in the LAN, local names under `home.arpa`. Upstream of your choice (provider, Quad9,
  Cloudflare, your own). On request DNS over TLS with a lean additional service (Q-10).
- **Time:** NTP client, optionally an NTP server for the LAN.

### 3.5 WAN connection types

| Type | Detection by the installer | Version |
|---|---|---|
| DHCP (cable or fiber router in front, modem in bridge mode with DHCP) | DHCPOFFER on the detected port | 1 |
| PPPoE (DSL modem in bridge mode, fiber ONT with many providers) | PADI/PADO response on the port, without and with a VLAN tag (Telekom: 7) | 1 |
| Static IP | manual only | 1 |
| DS-Lite (only IPv6 from the provider, IPv4 through a tunnel) | IPv6 lease without IPv4, AFTR through a DHCPv6 option | 2 |
| Double NAT behind a provider router | WAN address from a private range | 1, note in the web interface |

**Important providers in Germany** (research 2026-11, from forums and help pages, not officially confirmed, check in the test lab):

| Provider / connection | Characteristics | Consequence |
|---|---|---|
| Telekom DSL, VDSL, fiber | PPPoE **over VLAN 7**. User name = line ID + access number + co-user number (usually `0001`) + `@t-online.de`. The fiber modem is only a media converter, the router does the dial-up. IPv6 through DHCPv6 over PPP, prefix /56 [S-05] [S-06] | Input form in the web interface (F-UI-08) with fields exactly as in the Telekom letter, the user name is assembled. The assistant only detects PPPoE over VLAN 7 |
| Vodafone cable | Router or station from the provider. In bridge mode, depending on the region and time, real dual stack **or DS-Lite**, which can change at any time. Bridge mode not available everywhere [S-07] | Detect instead of assuming, later DS-Lite (F-NET-09). The most common case remains double NAT behind the station (as in the prototype) |
| Fiber from other providers (Deutsche Glasfaser, o2, 1&1, municipal utilities) | partly DHCP, partly PPPoE, partly with a VLAN ID per provider | Provider list with VLAN IDs in the web interface (maintain data, Q-14) |

**The technology behind it:** systemd-networkd cannot do PPPoE itself. For that, `pppd` runs with the `rp-pppoe` plugin on `wan0`
or `wan0.7` and creates `ppp0`. networkd then manages `ppp0` for DHCPv6 PD. DS-Lite needs an ipip6 tunnel (networkd: `[Tunnel]
Kind=ip6tnl Mode=ipip6`) to the provider's AFTR. Whether networkd evaluates the AFTR option (DHCPv6 option 64) is to be checked
(Q-15).

### 3.6 Firewall model

- Zones `wan`, `lan`, later `guest` and `vpn`. Default: drop everything incoming on WAN, LAN may do anything. Forwarded is LAN →
  WAN, back only for existing connections and port forwards.
- Own nft tables per purpose, so Docker and other services can exist next to them with their own tables (from the prototype, L-08):
  - `inet vr_filter`: filter for IPv4 **and** IPv6 in one table (D-09). The prototype had separate tables, and that is exactly
    why IPv6 was missing unnoticed for a long time (L-01).
  - `ip vr_nat`: masquerade
  - `ip vr_portfw`: DNAT
  - `inet vr_block`: blocks
- Each table is replaced atomically with the pattern `table … ; delete table … ; table … { … }`, never with `flush ruleset` (D-03).
- ICMPv6 per RFC 4890, ping from outside with rate limiting.
- IPv6 pinholes for individual devices and ports, the counterpart of port forwarding.

### 3.7 Updates and rollback

- Security updates automatically (unattended-upgrades), restart only in the maintenance window and only if needed.
- **A snapshot of the system before each update.** Options and state of research (decision in Q-03):
  - *btrfs + snapper*: snapshot before and after each apt run through an apt hook. Sources differ on whether the hook is built
    in or needs an additional package [S-08] [S-09].
  - *grub-btrfs*: snapshots appear in the boot menu. On Debian-based systems (so also Ubuntu) they are said not to be reliably
    bootable without adapting the initramfs [S-10]. That is a risk and must be tested. In Ubuntu 26.04 grub-btrfs does not exist
    as a package, snapper only in *universe* [S-15].
  - *A/B partitions* (e.g. with systemd-sysupdate): very robust, but fits "normal apt" badly (P-01).
  - A **ready-made automatic health check with rollback does not exist** [S-08]. It has to be built. Approach: counting boot
    attempts (systemd-boot boot assessment or a `grubenv` counter) and a service that only "confirms" the start after a successful
    check. Otherwise the next start boots the last good state.
- **Health check after a restart:** does the LAN run, does DNS answer, does WAN come up? If not, it automatically boots back into
  the last snapshot and the web interface shows what happened. That is a research step of its own in M5 (spike before the
  implementation).
- Release upgrades (new LTS) are guided and **never started automatically**. The web interface announces them months before the
  end of support.

### 3.8 Web interface

- The basis is routerui from the prototype (2.3), switched to `config.json` as the source of truth.
- A new **setup mode** right after installation (F-UI-02, F-UI-08):
  1. Set the password. The setup code is on the router's screen, as text and QR code.
  2. **Internet access**, only if needed: if the assistant detected PPPoE, the web interface asks for the credentials, with
     provider templates (Telekom letter, F-NET-13) and VLAN. With DHCP this step is skipped. Without an answer from the modem it
     offers DHCP, PPPoE and a static address to choose from.
  3. Connection check ("internet reachable") and a short tour.
- What the assistant no longer asks can be changed here at any time (F-UI-09): router name, home network (address, DHCP range), time
  zone, language.
- Secondary roles (NAS, Docker) as optional modules, only visible when installed.

### 3.9 Installer

#### 3.9.1 Boot medium

- UEFI-bootable image for USB sticks (later also ISO), built reproducibly with **mkosi** (see Q-02). mkosi builds bootable Debian and
  Ubuntu disk images that can be written directly to a stick [S-11]. mkosi has no ready-made "installer" type, so the assistant is an
  application in the live image that writes the second image (target system) to the disk. It contains:
  - a minimal live system (kernel, firmware for common network cards, drivers),
  - the assistant (Go binary, text interface on the console),
  - a ready-made **image of the target system** (compressed), so the installation is offline and deterministic (P-07, D-07).
- The assistant starts automatically on `tty1`, no login needed.
- The stick creator writes the stick (3.12). Other writing programs (Rufus, Etcher, `dd`) also work, then only the defaults from Windows
  are missing and the assistant asks for the language.
- On the EFI partition, the image contains a reserved file `vr/stick.json` of fixed size (padded with spaces). When writing, the stick
  creator enters language, keyboard layout and time zone from Windows there. The location of the file in the image is in the image's
  manifest (3.12), so the creator does not need to understand FAT.

#### 3.9.2 Flow of the assistant

Changed in 0.8 (P-11, D-15): the assistant only asks about physical things. The step names stay, so references still work.

| Step | What happens | Text for non-experts (example) | Status 0.8 |
|---|---|---|---|
| S1 Language | language, keyboard and time zone from `vr/stick.json` (3.9.1) | "Welcome! Choose your language." | **only without `stick.json`** (stick written with another program), time zone then from the language. Since 0.18 the first screen |
| S2 Hardware check | count network cards (≥ 2 needed), memory, disks | "This computer has only one network port. A router needs at least two, for example with a USB network adapter." | **only visible when there is a problem**, otherwise silent |
| S3 Detect WAN | activate all ports, wait for link (see 3.9.3) | "Unplug all network cables. Now plug in **only** the cable from the modem or internet router." | stays |
| S4 Internet access | detect DHCP and PPPoE (without and with VLAN 7), record the result in `config.json` | "Your connection needs credentials. You will enter them shortly in your browser." | **no more input**, display only. Credentials in the web interface (F-UI-08) |
| S5 LAN | remaining ports become the home network, LAN network automatic | – | **dropped** as a step of its own, is in the summary |
| S6 Disk | choose the target, clear warning | "**All data on this disk will be erased.**" | stays, even with only one disk (deliberate confirmation) |
| S7 Name and time zone | defaults | – | **dropped**, default `router.home.arpa`, changeable in the web interface (F-UI-09) |
| S8 Installation | write the image, generate the configuration, boot loader | progress bar | stays |
| S9 Summary | setup code, address of the web interface, QR code | "Connect a device to the home network and open https://router.home.arpa, code: XXXX-XXXX" | stays, plus F-INST-17 |
| S10 Restart | unplug the stick, restart | "Unplug the stick and press Enter." | stays |

So a non-expert normally sees exactly three screens with an action: plug in the cable (S3), confirm the disk (S6), unplug the stick (S10).

Principles for the assistant:
- Operated with arrow keys and Enter only, no text input (P-11, F-INST-16).
- Each step can be repeated.
- Nothing is written to the disk before S8.
- The result ends up in a `config.json`. A prepared `config.json` for an unattended installation only exists for automated tests
  (F-INST-08), not in the stick creator.

#### 3.9.3 Detecting the WAN port (the core)

1. **Find ports:** `/sys/class/net/*` with type Ethernet (`type == 1`) and a real device (`device` link, not virtual). Wi-Fi
   (`wireless/`) is excluded. For each port, MAC, driver, bus (PCI/USB) and maximum speed are recorded.
2. **Activate all ports** (`ip link set up`), without addresses.
3. **Baseline:** ports that already have link are noted. If there are any, the user is asked to unplug all cables until no port has link.
4. **Wait for the event:** through netlink (`RTMGRP_LINK`), wait for `IFF_LOWER_UP` or carrier. The first port with link becomes the candidate.
5. **Confirm:** try DHCP (and PPPoE discovery in parallel) on the candidate. With an answer the port is certainly WAN. Without an answer
   the assistant asks ("Is this really the cable from the modem?").
6. **Ambiguity:** if two ports get link at the same time, the assistant asks again. As a help it makes the port's LED blink
   (`ethtool --identify`), if the driver can do that.
7. **Fallback:** manual choice from a list with driver, MAC and position (PCI path).

#### 3.9.4 Disk and boot

- **GPT** with two partitions:
  - EFI system partition 1 GiB (vfat)
  - The rest as btrfs with subvolumes `@` (`/`), `@log` (`/var/log`, not in the snapshot, so logs survive a rollback), `@snapshots` and
    optionally `@data` (for the NAS module, with snapshot rules of its own)
- **No swap partition**, zram instead (little RAM needed, spares the SSD).
- **Boot loader:** GRUB with signed shim (Ubuntu's default, works with Secure Boot) or systemd-boot (simpler, boot assessment built in).
  Depends on Q-03 and Q-16.
- **Smallest disks:** 16 GB (NF-01). From 64 GB the assistant offers to use the rest for data.

#### 3.9.5 Recovery for non-experts

The stick is also the rescue tool. If you start it on a router that is already set up, it recognizes the installation and offers:

| Action | Effect |
|---|---|
| Reset the web interface password | new setup code, all settings stay |
| Start the last working state | roll back to the last snapshot that passed the health check |
| Write diagnostics to the stick | logs, `config.json` without secrets, hardware list, for help in a forum |
| Reinstall, keep settings | rewrite the target system, take over `config.json` and secrets |
| Reset to factory state | erase everything, assistant from the start |

See F-INST-12 to F-INST-14.

#### 3.9.6 What the assistant does not do

- It does not ask about things that can be detected (connection type, LAN network, time zone from Windows or the language).
- It does not ask for credentials, names, networks or passwords. All of that goes into the web interface, where you can comfortably type
  and read help (P-11).
- It requires no Linux terms. "WAN" is called "internet port" in the text, "LAN" is called "home network".
- It writes nothing to the disk before S8 is confirmed.

### 3.10 Extensions (secondary roles)

Optional modules, installable later through the web interface: NAS (Samba with a file browser, trash folder, backup), containers (Docker or
Podman), VPN server (WireGuard), DNS filter (block lists), guest network (VLAN or a port of its own), Wi-Fi access point (F-WIFI). Modules must not endanger the router
function: own units, own firewall tables, resource limits through systemd.

### 3.11 Taking over the prototype's code

The routerui code is taken over into this repo in M2. What changes:

| Area in the prototype | What changes |
|---|---|
| `internal/conf` (state, validation rules, generators) | Expanded into the schema for `config.json` (WAN, LAN, IPv6, DHCP pool, updates). New generators for systemd-networkd and dnsmasq DHCP. The generators for isc-dhcp are dropped. |
| Fixed `10.0.0.0/8`, `10.0.0.1`, `wan0`/`lan0` in the code | Come from `config.json`, no more constants |
| `internal/helper` (fixed actions, dry run) | Stays. Actions for WAN settings and network rollback are added (F-UI-03). |
| Firewall files and units (`router-v4/v6-firewall`) | Generated from the configuration (`inet vr_filter`, D-09), one unit `vr-firewall.service` |
| `internal/files`, Samba status | Moves to the NAS module (F-ADD-01) |
| Docker views | Move to the container module (F-ADD-02) |
| Audit script (Bash) | Implemented in Go, so checks and the web interface use the same data (F-SEC-08) |
| Web interface texts (German, fixed in the code) | Moved to a translation table (F-UI-05, done with D-16) |
| Data and names from the founder's household | Removed, examples only with documentation addresses (RFC 5737, RFC 3849) |

### 3.12 Stick creator for Windows

New in 0.8 (D-14, D-15). A program of its own instead of a third-party writing program, so non-experts need to know nothing about images,
checksums or drives.

**From the user's point of view:**

1. Download and start `vanilla-router-stick.exe`. There is no installation. Windows asks once for administrator rights, because the program
   writes directly to the stick.
2. A window with a list of the plugged-in USB sticks (name, size). A stick is preselected if there is only one.
3. "Create stick" → warning "All data on *SanDisk Ultra (32 GB)* will be erased" → progress (downloading, verifying, writing, checking) →
   "Done. Unplug the stick and plug it into the computer that is to become your router." Plus a note on how to start from a USB stick on the
   computer (boot menu, typical keys F12, F11, Esc).

**Technology:**

- **Go, standard library only**, built with `GOOS=windows` (D-01, NF-03). The window uses Win32 controls through `syscall` (window, list, button,
  progress bar), without WebView and without third-party libraries. A single `.exe` with an embedded manifest (`requireAdministrator`, DPI
  awareness, Common Controls 6).
- **Removable disks only:** disks with bus type USB (`IOCTL_STORAGE_QUERY_PROPERTY`), never the system drive, disks over 256 GB only after an
  additional confirmation. Before writing, all volumes of the stick are locked and dismounted (`FSCTL_LOCK_VOLUME`, `FSCTL_DISMOUNT_VOLUME`).
- **Image:** the program stays small and downloads the current image (about 1.5–2 GB) over HTTPS. It checks a **signed manifest** (Ed25519, public
  key in the program) with version, size, SHA-256 and location of `vr/stick.json`. The image is cached in the user folder. Alternatively a
  downloaded image file can be chosen (offline), it too must match the manifest.
- **Defaults from Windows:** language (`GetUserDefaultLocaleName`), keyboard layout and time zone (Windows name → IANA through the CLDR table
  `windowsZones`) end up in `vr/stick.json` (3.9.1). This is no configuration for the user, just a better default.
- **Check:** after writing, the stick is read back and verified against the SHA-256.
- **No background service**, no registry entries except what Windows creates itself, no telemetry (P-09). The download only goes to the project's
  documented address.

**Implemented in 0.11** (T-M4-08): fetch manifest → check size → warning (a second one above 256 GB) → download → verify the image completely → lock
the stick → erase start and end → write, the first MiB (partition table) last, so Windows mounts nothing in the meantime → read back. Without a built-in
download address only the local file with `manifest.json` and `manifest.json.sig` next to it remains. `cmd/vr-sign` creates manifests.

**Open:** code signing against the SmartScreen warning (Q-17), versions for macOS and Linux (Q-18), location and key management of downloads (Q-19).

---

## 4. Requirements

Columns: ID · priority · requirement · status. "Prototype" means: exists in the prototype, must be taken over.

### 4.1 Installer (F-INST)

| ID | Prio | Requirement | Status |
|---|---|---|---|
| F-INST-01 | MUST | UEFI boot from a USB stick on x86-64, start of the assistant without input | open |
| F-INST-02 | MUST | Detection of all wired network ports including USB adapters, abort with a clear message if < 2 | open |
| F-INST-03 | MUST | WAN detection by plugging in the cable (3.9.3) with confirmation through DHCP/PPPoE | open |
| F-INST-04 | MUST | PPPoE with optional VLAN, credentials input with help on the provider's letter | rejected on 2026-11-04 (P-11): input moves to the web interface (F-UI-08), the assistant only detects (F-INST-15) |
| F-INST-05 | MUST | Choice of the target disk with size and model, double confirmation before erasing | open |
| F-INST-06 | MUST | Offline installation from an image on the stick | open |
| F-INST-07 | MUST | Show the setup code and address of the web interface at the end (text and QR) | open |
| F-INST-08 | SHOULD | Unattended installation with a prepared `config.json` on the stick. Since 0.8 only for automated tests, not in the stick creator (D-15) | open |
| F-INST-09 | SHOULD | Port identification by LED blinking (`ethtool --identify`) | open |
| F-INST-10 | SHOULD | German and English, default from `vr/stick.json` (3.9.1) | done (D-16), the language question comes first |
| F-INST-11 | MAY | Installation on an existing Debian/Ubuntu ("conversion" mode) | open |
| F-INST-12 | MUST | The stick recognizes an existing installation and offers rescue actions (3.9.5) | open |
| F-INST-13 | MUST | Reset the web interface password from the stick | open |
| F-INST-14 | SHOULD | Write a diagnostics report to the stick (without secrets) | open |
| F-INST-15 | MUST | Detect the connection type (DHCP, PPPoE without and with VLAN, no answer) and record it in `config.json` (`wan.detected`), so the web interface knows whether to ask for credentials | done (T-M4-05) |
| F-INST-16 | MUST | Operated with arrow keys and Enter only, no text input (P-11) | done (T-M4-05) |
| F-INST-17 | MUST | As long as the web interface is not set up, the router shows the address and setup code on the screen after every start (text and QR) | text done (T-M4-06), QR open |
| F-INST-18 | MUST | Steps without a problem are not displayed (S2), normally only S3, S6, S10 with an action (3.9.2) | done (T-M4-05) |

### 4.2 Network (F-NET)

| ID | Prio | Requirement | Status |
|---|---|---|---|
| F-NET-01 | MUST | Fixed interface names `wan0`/`lan0` by MAC, LAN as a bridge over all other ports | open |
| F-NET-02 | MUST | IPv4 NAT (masquerade) and forwarding LAN → WAN | prototype |
| F-NET-03 | MUST | LAN network automatically without overlap with the WAN network | open |
| F-NET-04 | MUST | DHCPv4 server with reservations, pools automatically around reservations | prototype |
| F-NET-05 | MUST | Local DNS with names under `home.arpa` (forward and reverse), search domain `home.arpa` | prototype (partly) |
| F-NET-06 | MUST | IPv6: DHCPv6 PD on WAN, RA/SLAAC in the LAN, ULA in addition | open |
| F-NET-07 | MUST | Pools never start at a network or broadcast address (L-12) | open |
| F-NET-08 | SHOULD | Detection of double NAT and a note on port forwards | prototype |
| F-NET-09 | SHOULD | DS-Lite | open |
| F-NET-10 | SHOULD | Guest network (own port or VLAN), isolated from the LAN | open |
| F-NET-11 | MAY | Several WANs (failover) | open |
| F-NET-12 | MUST | ULA prefix random at installation per RFC 4193, stable afterwards | open |
| F-NET-13 | MUST | PPPoE with provider templates (Telekom: VLAN 7, user name assembled from the fields of the letter) | open |

### 4.3 Security (F-SEC)

| ID | Prio | Requirement | Status |
|---|---|---|---|
| F-SEC-01 | MUST | Firewall IPv4 and IPv6, drop incoming on WAN by default | prototype |
| F-SEC-02 | MUST | The firewall is loaded before the network (`Before=network-pre.target`) | prototype |
| F-SEC-03 | MUST | Web interface only from the LAN, HTTPS, setup code, login throttle (in the prototype also fail2ban, in the target nft rate limiting instead of fail2ban, D-10) | prototype |
| F-SEC-04 | MUST | SSH off by default. If on, only with a key and only from the LAN | prototype (partly) |
| F-SEC-05 | MUST | Automatic security updates | prototype |
| F-SEC-06 | MUST | AppArmor on, state visible in the web interface (L-04) | prototype (shown in the audit) |
| F-SEC-07 | MUST | Privilege separation of the web interface (web server without root, helper with a fixed list of actions) | prototype |
| F-SEC-08 | SHOULD | Built-in self-check (audit) with comparison to the last run | prototype |
| F-SEC-09 | SHOULD | Warning about risky extensions (container with `docker.sock`, privileged, network in the LAN) | prototype |
| F-SEC-10 | SHOULD | TLS certificate that devices in the LAN can easily trust (Q-09) | open |
| F-SEC-11 | MAY | DNS over TLS to the upstream | open |

### 4.4 Web interface (F-UI)

| ID | Prio | Requirement | Status |
|---|---|---|---|
| F-UI-01 | MUST | All functions from 2.3 based on `config.json` | prototype (on state.json) |
| F-UI-02 | MUST | Setup mode after installation | open |
| F-UI-03 | MUST | Change WAN settings (type, credentials, IPv6) through the web interface, with rollback on connection loss | done (T-M4-03), hardware test open |
| F-UI-04 | SHOULD | Suitable for mobile devices | prototype (basics) |
| F-UI-05 | SHOULD | Two languages. Since D-16: English as the default, German as a translation (`internal/i18n/de.json`) | done (2026-11-06), a switch in the web interface is open: the language follows `system.locale` |
| F-UI-06 | MAY | Notifications (email, push) for outages, new devices, updates | open |
| F-UI-07 | MUST | "About" page with version, license (AGPL-3.0-or-later) and link to the source of exactly this version (AGPL §13, D-11). The link is a build parameter, so changed versions point to their own source | done |
| F-UI-08 | MUST | Setup step "internet access" after the first login, if the assistant detected PPPoE or no answer: credentials with provider templates (F-NET-13), VLAN, connection check (3.8) | done (T-M4-04) |
| F-UI-09 | MUST | Change router name, home network (address, DHCP range), time zone and language in the web interface, with rollback on connection loss (like F-UI-03) | done except for the language (F-UI-05) |

### 4.5 Updates (F-UPD)

| ID | Prio | Requirement | Status |
|---|---|---|---|
| F-UPD-01 | MUST | Security updates automatically, restart only in the maintenance window | prototype (without restart) |
| F-UPD-02 | MUST | Snapshot before each update and each package change (L-05, L-06) | done, untested on a real system (T-M5-01) |
| F-UPD-03 | MUST | Automatic rollback if the system does not come up healthy after an update | done, untested on a real system (T-M5-02) |
| F-UPD-04 | MUST | Updates for vanilla-router itself (own signed package source) | open, design in Q-22 |
| F-UPD-05 | MUST | Announcement of the end of support and guided release upgrade (L-03) | open |
| F-UPD-06 | SHOULD | Pre-check before upgrades (space, half-installed packages, known pitfalls like L-05) | open |
| F-UPD-07 | SHOULD | Choose a mirror with good speed (L-11) | open |

### 4.6 Extensions (F-ADD)

| ID | Prio | Requirement | Status |
|---|---|---|---|
| F-ADD-01 | SHOULD | NAS module (Samba, file browser, trash folder) | prototype |
| F-ADD-02 | SHOULD | Container module with warnings (F-SEC-09) | prototype (display) |
| F-ADD-03 | SHOULD | Backup module (versioned, e.g. restic, to USB or the cloud) | open |
| F-ADD-04 | SHOULD | VPN server (WireGuard) with QR codes for phones | open |
| F-ADD-05 | MAY | DNS filter with block lists | open |

### 4.7 Stick creator (F-STICK)

| ID | Prio | Requirement | Status |
|---|---|---|---|
| F-STICK-01 | MUST | One `.exe` for Windows 10 and 11 (x86-64), without installation, without a runtime, Go with the standard library only (3.12) | done (T-M4-08) |
| F-STICK-02 | MUST | No questions except choosing the stick and the confirmation before erasing | done (T-M4-08) |
| F-STICK-03 | MUST | Only show USB removable disks, never the system drive; disks over 256 GB only after an additional confirmation | done, plus the disk the program runs from (T-M4-08) |
| F-STICK-04 | MUST | Download the image and verify it against a signed manifest before writing (Ed25519, SHA-256) | done (T-M4-08), download address and key open (Q-19) |
| F-STICK-05 | MUST | Lock and dismount volumes, write the image raw, then read back and verify | done (T-M4-08), checked with a real stick (test image, 2026-11-05) |
| F-STICK-06 | MUST | Clear messages on errors (stick too small, write-protected, unplugged, download interrupted), cancel at any time | done (T-M4-08) |
| F-STICK-07 | SHOULD | Enter language, keyboard layout and time zone from Windows into `vr/stick.json` | done (T-M4-08) |
| F-STICK-08 | SHOULD | Cache the image, recognize a newer image; alternatively choose a local image file (offline) | done (T-M4-08) |
| F-STICK-09 | SHOULD | German and English depending on the Windows language | done (T-M4-08) |
| F-STICK-10 | SHOULD | A note at the end on how to start from a USB stick (boot menu, typical keys) | done (T-M4-08) |
| F-STICK-11 | MAY | Versions for macOS and Linux (Q-18) | open |

### 4.8 Non-functional (NF)

| ID | Prio | Requirement | Status |
|---|---|---|---|
| NF-01 | MUST | Runs on x86-64 with ≥ 2 GB RAM, ≥ 16 GB disk, ≥ 2 Ethernet ports | open |
| NF-02 | MUST | Routing with at least 1 Gbit/s on a current entry-level x86 (e.g. Intel N100) | open |
| NF-03 | MUST | Own code with the Go standard library only, frontend without dependencies (P-08) | prototype |
| NF-04 | MUST | All generated configurations are covered by tests (golden files) | prototype (partly) |
| NF-05 | MUST | Test environment with identical hardening to production (L-09) | prototype |
| NF-06 | SHOULD | Installation in under 10 minutes | open |
| NF-07 | SHOULD | Idle power consumption is not increased by vanilla-router (no constant load, polling only with the web interface open) | prototype |
| NF-08 | MUST | Every source file starts with `SPDX-License-Identifier: AGPL-3.0-or-later` (D-11) | open |
| NF-09 | MUST | Package dependencies of the base system only from Ubuntu *main* (D-10), checked by CI | open |

### 4.9 Wi-Fi (F-WIFI)

New in 0.21 (D-18, Q-06). "Untested on radio" means: tested with unit tests, golden files and the test instance, never with a real Wi-Fi adapter (T-M4-09).

| ID | Prio | Requirement | Status |
|---|---|---|---|
| F-WIFI-01 | MUST | A Wi-Fi adapter in the router runs as an access point through `hostapd` and is a port of the bridge `lan0`, so DHCP, DNS and firewall apply unchanged | done, untested on radio |
| F-WIFI-02 | MUST | The web interface sets network name, password, band, channel, channel width, country and on/off | done, untested on radio |
| F-WIFI-03 | MUST | The web interface shows whether the access point runs, on which channel, and the connected devices | done, untested on radio |
| F-WIFI-04 | MUST | Without Wi-Fi hardware the page does not appear in the navigation, and opened directly it explains why. A PCI Wi-Fi card without a working driver gets its own explanation (firmware comes with `vrctl complete`, D-17) | done, checked in the test instance |
| F-WIFI-05 | MUST | The password is a secret (`apply.DirSecrets`), not in `config.json`, and is never sent back to the browser. The generated `hostapd.conf` has mode 0600 | done |
| F-WIFI-06 | MUST | Changes go through `internal/apply`: backup, start, health check, confirmation within 5 minutes, otherwise automatic rollback. `hostapd` has no check mode, so a rejected configuration shows up at the start (D-18) | done, untested on radio |
| F-WIFI-07 | MUST | The Wi-Fi packages are only downloaded when an adapter exists and the owner asks for it. From *universe* only `hostapd` is reachable (apt pinning) | done, untested on an installed router |
| F-WIFI-08 | SHOULD | WPA2 and WPA3 at the same time, protected management frames where the device supports them | done, untested on radio |
| F-WIFI-09 | MAY | Automatic channel selection and DFS channels (5 GHz, 52–144) | open: not offered, because they cannot be tested without hardware |
| F-WIFI-10 | MAY | Wi-Fi 6 (802.11ax), guest network on its own SSID (with F-NET-10), several adapters at once | open |

---

## 5. Decisions

| ID | Decision | Reason | Status |
|---|---|---|---|
| D-01 | Own code in **Go, standard library only**, frontend in vanilla JS without a build step | One static binary, no supply chain, maintainable for a long time. Proven in the prototype (≈ 30 functions without one dependency). | done |
| D-02 | **One declarative file** as the source of truth, all service configurations are generated | Reproducible, testable, exportable. Proven in the prototype with `state.json`. | done |
| D-03 | **Own nft tables per purpose**, never `flush ruleset` | Docker and other services bring their own tables. A `flush ruleset` destroys them. | done |
| D-04 | **Privilege separation**: web server without root, helper with a fixed list of actions, file service as the share's user | A hole in the web server does not immediately become root. | done |
| D-05 | **dnsmasq for DNS and DHCPv4** (instead of isc-dhcp), RA and DHCPv6 through systemd-networkd or dnsmasq (Q-04) | isc-dhcp has not been maintained since the end of 2022 [S-12]. OPNsense recommends dnsmasq including DHCPv6 and RA for small networks, Kea only for large networks with high availability [S-13]. One service instead of two avoids errors like L-07. | open (proposal) |
| D-06 | **systemd-networkd directly** instead of netplan | One layer less, PD/RA/bridge natively. netplan brings no advantage on servers if the configuration is generated anyway. | open (proposal) |
| D-07 | **Image-based installation** (prebuilt root file system on the stick) instead of debootstrap at runtime | Offline, fast, identical for all users, testable. | open (proposal) |
| D-08 | **Debian stable** as the base instead of Ubuntu LTS | All router packages (dnsmasq, fail2ban, nftables, pppd) are in *main* and get security updates. On Ubuntu, dnsmasq and fail2ban are in *universe* [S-01], the same problem as isc-dhcp in the prototype. Plus no snap and a smaller base system. Downside: the prototype runs on Ubuntu, but experience largely carries over. | rejected on 2026-11-04 in favor of D-10 |
| D-09 | **One `inet` table for the filter** (IPv4 and IPv6 together), NAT separately in `ip vr_nat` | Shared rules cannot drift apart. The prototype had forgotten IPv6 because it needed a table of its own (L-01). | open (proposal) |
| D-10 | **Ubuntu LTS** as the base | The project owner's decision per P-10: Ubuntu is better known, has more guides and better support for new hardware, and the prototype already runs on it. The *universe* problem from D-08 is solved by dependencies coming only from *main*: `dnsmasq-base` instead of `dnsmasq`, no fail2ban requirement, Q-03 decides on snapper (3.2) [S-15]. | done (2026-11-04) |
| D-11 | License **AGPL-3.0-or-later** | The project owner's decision on 2026-11-04: the newest AGPL version (version 3), with "or later" for future versions. In addition to the GPL, the AGPL requires that users of a changed version operated over the network get the source. For the web interface this means F-UI-07. Text in `LICENSE`, every source file gets an SPDX line (NF-08). | done (2026-11-04) |
| D-12 | **Ubuntu Server** with the package set of `ubuntu-server`, remove nothing, only adapt through configuration (3.2) | The project owner's wish (P-10). A system that looks exactly like a normal Ubuntu Server is easiest for helpers and guides. Removing packages breaks the metapackages and thus release upgrades. The cost (a few hundred MB, a few disabled services) is irrelevant on PC hardware. | done (2026-11-04) |
| D-13 | **Rollback after updates without snapper**: btrfs snapshot before apt, GRUB entry from the snapshot, state (`vr_pending`, `vr_trial`) in a grubenv on the ESP, confirmation by `vr-health`, rollback becomes permanent by renaming subvolumes | snapper is in *universe* (D-10), grub-btrfs is missing in Ubuntu. GRUB cannot write to btrfs, but can to FAT. Kernel and initrd come from the snapshot itself, so they always match. Restart after an error only on an attempt after an update, otherwise a restart loop. | done (code), hardware test open |
| D-14 | **A stick creator of our own for Windows** in Go (Win32 through `syscall`), downloads a signed image, no third-party libraries (3.12) | Third-party writing programs are a hurdle for non-experts (which image, which drive, which settings). A program of our own can offer only USB sticks, verify the image itself and bring defaults from Windows. Go with `GOOS=windows` keeps the project at one language (D-01). A small `.exe` plus download instead of a 2 GB `.exe`, so the program does not have to be downloaded again with every new version. Win32 instead of WebView, because WebView would need third-party code. | open (proposal) |
| D-15 | **Flow from the user's point of view** (section 1): stick creator without questions, assistant only for physical things (WAN port, disk), everything else in the web interface (P-11) | The project owner's decision on 2026-11-04. In a browser it is more comfortable to type and read help than on a console with an unknown keyboard layout. Fewer questions mean fewer errors. Consequence: PPPoE input moves from the assistant to the web interface (F-INST-04 rejected, F-UI-08), F-UI-03 and T-M4-03 are prerequisites for the first version. | done (2026-11-04) |
| D-16 | **English everywhere, German as a translation**: code, comments, documentation, PLAN.md and everything on GitHub in English. The product is English by default, German can be chosen. One catalog `internal/i18n/de.json`, translated at the point of display (`internal/tui`, `h()` in the frontend), the language follows `system.locale` | The project owner's decision on 2026-11-05/06 (Q-07): the project is public and speaks English on GitHub, and contributors should be able to read everything. German stays selectable because the target group includes non-experts in Germany. Translating at the point of display keeps the call sites simple: error messages from the helper stay plain strings. | done (2026-11-06) |
| D-18 | **Wi-Fi access point through `hostapd`, as an optional extension** with a justified exception to NF-09: only `hostapd` comes from *universe* (F-WIFI). The adapter keeps its kernel name in `config.json` (`wifi.interface`), `hostapd` puts it into the bridge `lan0` itself. Downloaded through the web interface only if an adapter exists | The project owner's decision on 2026-11-06 (Q-06), replaces "external access points only" as the only option. Alternative `wpa_supplicant` in AP mode (in *main*) rejected: unusual, few guides (P-10), no DFS and no automatic channel selection as far as known, bridge behavior not verifiable without hardware. Why the exception is acceptable: `hostapd` and `wpasupplicant` (*main*) are built from the same source package `wpa`, and the security pockets of 22.04 and 24.04 carry identical versions of both [S-18]. That is observed practice, not a promise: Canonical's security commitment only covers *main*. The target image only has *main* as a source, so the download adds a *universe* source and pins it to `hostapd` (`/etc/apt/preferences.d/vr-wifi`). Routers without Wi-Fi never get the package, so the base system stays *main* only. Own unit `vr-hostapd.service`, the package's `hostapd.service` is disabled. No `.link` file, because renaming needs a restart and an adapter name like `wlx…` or `wlp…` is stable anyway. `hostapd` has no check mode (P-05): `config.Validate` restricts the values, and the health check after the start (`iw dev … info` reports an access point) triggers the rollback | done (code), hardware test open |

---

## 6. Open questions

| ID | Question | Options | Tendency |
|---|---|---|---|
| Q-01 | Base distribution | Debian stable · Ubuntu LTS | **done: Ubuntu LTS** (D-10, 2026-11-04) |
| Q-02 | Tool for the image | mkosi · live-build · debos | mkosi (reproducible, Debian and Ubuntu, close to systemd) |
| Q-03 | Rollback of updates | btrfs + snapper + grub-btrfs · A/B partitions · systemd-sysupdate | **implemented: a tool of our own** (btrfs snapshot + GRUB state on the ESP, D-13), hardware test open. Earlier assessment: btrfs snapshots fit P-01 best. snapper is in *universe* on Ubuntu, grub-btrfs is missing entirely [S-15]: either a small tool of our own (`btrfs subvolume snapshot` + apt hook) or snapper as a justified exception. Bootability of snapshots and the automatic rollback are unresolved (3.7), hence a spike in M5. |
| Q-04 | Who does IPv6 in the LAN (RA, DHCPv6)? | systemd-networkd · dnsmasq | **implemented: systemd-networkd** (RA, SLAAC, PD, one source), dnsmasq only DHCPv4 + DNS. Confirmation by the project owner open |
| Q-05 | Assistant as a text interface on the screen or as a web interface from a phone? | TUI on tty1 · web through a LAN port · both | **TUI on tty1**, since D-15 only three screens without text input. Everything with input runs through the web interface anyway |
| Q-06 | Wi-Fi access point in the router? | no (external APs) · hostapd for supported cards | **done: hostapd for supported cards, as an optional extension** (D-18, 2026-11-06). External APs keep working |
| Q-07 | Language of the project (code, documentation, interface) | German · English · both | **done: D-16** (2026-11-06) |
| Q-08 | ARM (Raspberry Pi 5 and the like)? | later · never | later |
| Q-09 | TLS for the web interface without a warning | self-signed + instructions · own local CA with an installation aid · HTTP only in the LAN | open |
| Q-10 | DNS over TLS | dnsmasq cannot do it, additional service (e.g. unbound, stubby) | later |
| Q-11 | License | GPL-3.0 · AGPL-3.0 · MIT · Apache-2.0 | **done: AGPL-3.0-or-later** (D-11, 2026-11-04) |
| Q-12 | Recognizing devices despite random MACs | DHCP client ID, host name, fingerprinting | open |
| Q-13 | Name | check "vanilla-router" (trademarks, existing projects, domains) | open. On GitHub there are several small JavaScript routing libraries with this name [S-14]. That is not a legal problem, but people searching end up there. Possible: keep the name and make it unique with an addition ("vanilla-router OS", short form `vr`). Trademark search (DPMA/EUIPO) before publication. As of 2026-11-05 [S-17]: no trademark "vanilla router". The EU trademark "VANILLA" (018129708, Cinnamon B.V.) in class 42 also covers software development and network services. Low risk for a free open source project, get legal advice before commercial offers under the name. The project owner's decision: the name stays for now. |
| Q-14 | Where does provider data come from (VLAN IDs, credential formats)? | own maintained list in the repo · community contributions | own list as a data file, contributions through pull requests |
| Q-15 | DS-Lite with systemd-networkd: is the AFTR address (DHCPv6 option 64) evaluated? | networkd · own helper that reads the option and creates the tunnel | open, check in the provider VM |
| Q-16 | Secure Boot | shim + GRUB (signed, Ubuntu's default) · systemd-boot (check signing) · Secure Boot off | open. Must work with Secure Boot, many non-experts' computers have it on. |
| Q-17 | Code signing of the stick creator | without (SmartScreen warns "unknown publisher", instructions needed) · OV/EV certificate (costs yearly) · signing service for open source (e.g. SignPath Foundation) | open. The warning is off-putting for non-experts, a signing service for open source would be ideal |
| Q-18 | Stick creator also for macOS and Linux? | Windows only · later all three | Windows first (the project owner's wish), build the core (download, verification, writing) so that only the window and drive access are new per system |
| Q-19 | Where do images come from, who signs? | GitHub Releases · own server · both | open. Keep the signing key offline, sign the manifest separately from the image |
| Q-20 | Reporting channel for security issues (SECURITY.md) | own address with PGP · the platform's reporting feature ("private vulnerability reporting") · both | done (2026-11-05): GitHub's reporting feature ("Private vulnerability reporting"), linked in SECURITY.md. An address of our own can be added later |
| Q-21 | The boot stick's build uses *universe*, because mkosi's default initrd needs `systemd-repart`. Is that acceptable? | accept for the stick only (it is not a long-lived system, no security updates apply to it) · own initrd from *main* (more work) | accept for the stick only, the target system stays *main* (NF-09). Decision by the project owner open |
| Q-22 | How does vanilla-router update itself (F-UPD-04)? | own signed apt repository (GitHub Pages or own server) · download of the `.deb` by an own updater · A/B images | recommendation: signed apt repository, because it is the ordinary Ubuntu way (P-01) and the snapshot hook and health check already wrap every apt run. Needed: repository and offline OpenPGP key, source entry and key in the target image, our origin in unattended-upgrades, service restarts in the package scripts (`postinst` only reloads systemd so far), migration of `config.json` on schema changes. Decision by the project owner open |

---

## 7. Roadmap

| ID | Milestone | Result | Acceptance |
|---|---|---|---|
| M0 | Project start | repo, planning document, agent guide | this document exists |
| M1 | Port detection | Go prototype of WAN detection (3.9.3), runs in the namespace lab with 3 veth ports (changed from VM to namespaces, see T-M1-01) | "plugging in" a cable is detected reliably, DHCP and PPPoE confirmation (with VLAN 7) work: `sudo test/lab/netns-lab.sh test` passes |
| M2 | Configuration core | `vrctl` + schema `config.json` + generators for networkd, nftables, dnsmasq; routerui code taken over after cleaning | golden file tests, VM with a generated configuration routes IPv4/IPv6 |
| M3 | Installer image | mkosi image with the assistant, installs a VM from scratch | VM boots after installation, LAN client gets IP, DNS and internet |
| M4 | Non-expert flow | web interface on `config.json`, stick creator for Windows (3.12), shortened assistant (3.9.2), setup mode with internet access (F-UI-08) | a test person without Linux knowledge manages the whole way from download to "internet works" without help |
| M5 | Updates and rollback | snapshots, health check, automatic rollback | a deliberately broken update is rolled back automatically |
| M6 | Hardware and beta | test matrix of real devices, documentation, first public version | ≥ 5 device models tested, documentation for non-experts exists |

### 7.1 Tasks for M1

As of 2026-11-04: code and unit tests finished (`go test ./...` passes, built with Go 1.26 on Ubuntu 26.04). `vr-detect -list` on real
hardware (prototype) recognizes both ports correctly. The lab run passed all scenarios on 2026-11-06, it needs root and the packages
`golang-go dnsmasq-base pppoe`.

| ID | Task | Result | Status |
|---|---|---|---|
| T-M1-01 | Test environment. **Changed:** instead of QEMU VMs, network namespaces with veth pairs (starts in seconds, needs no images, still tests real netlink, AF_PACKET and VLAN 7). "Plugging in a cable" = turning on the other side. QEMU only comes for the installer (M3) | `test/lab/netns-lab.sh` with 5 scenarios: DHCP, PPPoE over VLAN 7, unplug first, ambiguous, no answer + confirmation | done: 5 of 5 scenarios passed in WSL on 2026-11-06 |
| T-M1-02 | Go package `internal/netdetect`: find ports (sysfs), read properties (MAC, driver, bus, speed, link) | `ports.go`, tests with a fake sysfs tree. Built-in ports first, USB adapters afterwards, order by bus position (PCI hexadecimal, USB naturally sorted) | done |
| T-M1-03 | Link events through netlink (`RTMGRP_LINK`, standard library only) | `linkwatch.go`: parser tested with fake kernel messages, socket tested live (without privileges), `SetUp` through ioctl. Buffer overflow (`ENOBUFS`) → read the state again | done |
| T-M1-04 | Flow "all cables out → modem cable in" with ambiguity and timeout as a state machine | `internal/wandetect`: pure automaton (time is passed in), 12 tests, texts for non-experts without jargon (checked) | done |
| T-M1-05 | Confirmation through DHCP discover on the candidate port, without taking an address (raw socket) | `internal/probe`: frames built by hand (checksums tested), offer parsing, flow with a fake socket and an artificial clock | done (lab passed) |
| T-M1-06 | PPPoE discovery (send PADI, recognize PADO), with and without VLAN 7 | one AF_PACKET socket for everything, VLAN tag put back from `PACKET_AUXDATA` (drivers with VLAN offload), Host-Uniq matches responses | done (lab passed) |
| T-M1-07 | Small text interface as a demo | `cmd/vr-detect`: `-list` (without privileges), flow with input (number = port by hand, w = repeat, j = anyway), `-json` for the later `config.json` | done |
| T-M1-08 | Check script `test/check-main.sh`: resolve dependencies and report what does not come from Ubuntu *main* (NF-09), plus SPDX lines (NF-08) | Resolution like apt (only the first alternative, first provider of virtual packages): 325 packages, all *main*. Counter-test: `fail2ban` and a file without SPDX fail. Unknown packages count as errors | done |

### 7.2 Status M2, M3, M5 (2026-11-04)

Code and unit tests finished (10 packages, all pass on Ubuntu 26.04). Generated firewall and dnsmasq files for four example configurations pass `nft -c` and
`dnsmasq --test`. Units pass `systemd-analyze verify`. The package `vanilla-router_*.deb` builds, all 391 packages of the images come from *main*.
**Not yet applied or booted anywhere**, that is the hardware test (7.3).

| ID | Task | Result | Status |
|---|---|---|---|
| T-M2-01 | Schema `config.json` (version 1), strict loading, validation with all errors at once | `internal/config`, 30 error cases tested | done |
| T-M2-02 | Generators networkd, nftables, dnsmasq, pppd, sysctl, resolved, sshd, unattended-upgrades | `internal/gen`, golden files in `testdata/` (NF-04) | done |
| T-M2-03 | `vrctl apply`: check, back up, replace atomically, reload selectively, health check, rollback | `internal/apply`, confirmation through a systemd-run timer (survives the session aborting) | done |
| T-M2-04 | `vrctl setup`: adapt Ubuntu Server (D-12) | resolved stub, cloud-init, netplan, snapd, SSH off | done |
| T-M2-05 | Package and units | `packaging/build-deb.sh`, `vr-dnsmasq` (dnsmasq-base, hardened), `vr-firewall` (before the network), `vr-pppoe` | done |
| T-M3-01 | Find disks, exclude the stick, plan partitions | `internal/disk` | done |
| T-M3-02 | Installation: GPT, btrfs with @/@log/@snapshots, unpack the image, fstab, settings, shim+GRUB (also the standard path), services offline, user admin | `internal/install`, check before the first write | done (untested on hardware) |
| T-M3-03 | Assistant S1–S10 | `cmd/vr-install`, Telekom form, hidden password input, `--dry-run`, `--unattended` (F-INST-08) | done |
| T-M3-04 | Images | `image/target`, `image/stick`, `image/build.sh` (mkosi) | in-progress: built with mkosi 26, starts in VirtualBox up to the assistant (2026-11-05), the installation failed on missing `sfdisk` (L-19, fixed), cable detection and disk selection work in the VM (2026-11-06), see `image/README.md` |
| T-M4-01 | Take over the web interface from the prototype, switch to `config.json` (3.11) | `cmd/vr-webui` with helper; `internal/conf` only translates between API forms and `config.json`, validation through `config.Validate`, applying through `apply` (backup, rollback). dnsmasq leases, addresses and names from the configuration, own TLS certificate, setup code from the installer, extensions (containers, file share, audit) only visible when installed. Checked in the browser against a local test instance (all pages, reservation, DNS, port forward with confirmation, block, error messages) | done (untested on hardware) |
| T-M4-02 | "About" page with license and source link (F-UI-07) | sidebar and login page, `/api/about`, link through a build parameter | done |
| T-M4-03 | Settings for WAN type, PPPoE credentials, host name and home network in the web interface (F-UI-03, F-UI-09). Since D-15 a prerequisite for the first version | Page "Internet & home network" (`web/static/views/internet.js`), helper actions `settings.*`. Network changes through apply's confirmation (systemd timer: WAN 2 min, home network 5 min), check "internet reachable". Moving the home network moves the pool, reservations, DNS records and port forwards (`config.MoveLAN`), checks for overlap with the WAN network. New PPPoE password under a new reference, so a rollback matches the old one. apply takes over host name and time zone (`hostnamectl`, `timedatectl`) and restarts the web interface on a new name, new address or a switch between DHCP/PPPoE. Checked in the browser against the test instance: Telekom form with error message, confirming, deadline expiry with rollback, home network move, name and time zone | done (untested on hardware) |
| T-M4-04 | Setup step "internet access" after the first password (F-UI-08), provider templates as a data file (Q-14) | `wan.detected` in config.json (F-INST-15), after login automatically `#/internet?setup=1` prefilled with the detected PPPoE/VLAN, "Later" for one day, note on the overview. Provider templates so far only Telekom (fixed in the code), data file open | done except for the data file |
| T-M4-05 | Shorten the assistant (3.9.2): S1 only without `stick.json`, S2 only on a problem, S4 without input, S5 and S7 gone, only arrow keys and Enter (F-INST-15 to 18) | `cmd/vr-install`: new `internal/tui` (keys instead of lines, menus with ↑ ↓ Enter, `MenuAsync` next to the running detection), `internal/stick` (reads `vr/stick.json` under /efi, /boot/efi, /boot; otherwise a choice English, German DE/AT/CH), detection with menus instead of letters ("try again", "use anyway", "choose the port myself"), `wan.detected` is written, erasing through a menu with preselection "No, cancel". Dry run checked in WSL (cancel, confirmation, plan). `vr-detect` and lab script adapted | done (lab and hardware open) |
| T-M4-06 | Show the address and setup code on tty1 after every start, until the web interface is set up (F-INST-17) | `vr-webui prepare` writes `/run/issue.d/50-vanilla-router.issue` (agetty shows it before login), without a code after the first password (`console.setup_done`). QR code still missing | done except for QR |
| T-M4-07 | Image with reserved `vr/stick.json` and signed manifest (3.9.1, 3.12) | `image/build.sh`, signing tool | tool `cmd/vr-sign` done (keygen, manifest with search for the reservation, verify), image open |
| T-M4-08 | Stick creator for Windows (F-STICK-01 to 10) | `cmd/vr-stick`: window with Win32 through `syscall` (list, buttons, progress, file choice), manifest as a resource through `internal/winres` (own .syso, no third-party tool), `packaging/build-stick-exe.sh`. `internal/windisk` (USB bus only, system, program and cache drive excluded, lock and dismount volumes, NO_BUFFERING), `internal/stickwrite` (verify before writing, partition table last, enter `stick.json`, read back), `internal/manifest`, `internal/windefaults` (CLDR `windowsZones`). Download with resume, cache, offline fallback. Tests on Linux and Windows, window checked as a preview. Checked with a real stick and a signed test image (2026-11-05). Since 0.12 in the logo's style: header with logo, custom buttons and progress bar, logo as the program icon | done |
| T-M4-09 | Wi-Fi access point (F-WIFI, D-18) | `config.WiFi` (schema stays 1, the block is optional), `gen` (`/etc/vanilla-router/wifi/hostapd.conf`, networkd leaves the adapter alone), group `wifi` in `apply` with confirmation and health check, `apply.PrepareWiFiPackages`, helper actions `wifi.*`, `sys` reads adapters from sysfs and parses `iw`, page `web/static/views/wifi.js`, test instance with `VR_WIFI=1` and a simulated adapter. Checked: unit tests, golden files, apt pinning with `apt-cache policy`, the page in the browser against the test instance. **Not checked: anything with a real radio** (test plan 7.3, step 8) | done (untested on hardware) |
| T-M5-01 | Snapshot before each apt run, GRUB entry "state before the update", state on the ESP | `internal/snapshot`, `packaging/grub.d`, apt hook | done (untested on hardware) |
| T-M5-02 | Health check after start, automatic rollback | `vr-health.service`, `panic=10`, watchdog. Restart only on an attempt after an update (no restart loop) | done (untested on hardware) |

### 7.3 Test plan for real hardware

The order is chosen so that each step only needs the result of the previous one. Steps 2 to 6 can also be run in VirtualBox
([docs/development.md, 5.5](docs/development.md#55-real-hardware-and-virtual-machines)), except for PPPoE.

1. **Lab in WSL** (without hardware): `sudo test/lab/netns-lab.sh test` → 5 passed.
2. **Build the stick**: `image/build.sh` on an Ubuntu 26.04 with mkosi. From T-M4-08 on, write the stick with the stick creator on Windows.
3. **Boot the stick** (Secure Boot on): the assistant appears on tty1, dry run cancelled at S6.
4. **Detection**: all cables out, modem cable into any port → right port, DHCP detected. With a Telekom connection also: PPPoE over VLAN 7 detected.
5. **Installation** on the internal SSD, restart without the stick → boots, `lan0` has 192.168.42.1, a device in the home network gets an address, DNS and
   internet, IPv6 if the provider gives a prefix.
6. **vrctl apply --confirm 60s** with a port forward, do not confirm → back after 60 s. In the web interface: switch the home network to another address,
   reconnect the device, confirm at the new address. Second round without confirmation → the old network comes back, the web interface restarts at the
   old address. Check whether `networkctl reload` removes the old address from `lan0`. Change the name → new certificate, web interface reachable under
   the new name.
7. **Rollback after an update**: `vrctl snapshot pre-update`, then deliberately break `vr-dnsmasq` (e.g. `systemctl mask vr-dnsmasq`), restart → vr-health
   restarts, GRUB takes the snapshot, afterwards the old state runs and stays after another restart.
8. **Wi-Fi** (F-WIFI, needs a Wi-Fi adapter, a Hyper-V VM has none): the page appears, "Download Wi-Fi software" works and `apt-cache policy` shows only `hostapd` from *universe*. Switch on with 2.4 GHz → a phone connects with WPA2 and with WPA3, gets an address from the home network, appears in the list of devices, reaches the internet and a device on a cable, and a device block applies to it. 5 GHz with 80 MHz. Wrong channel for the country → error message and the old state. Change the password without confirming → the old one is back after 5 minutes. Reboot → the access point comes up by itself. Check whether the hardening of `vr-hostapd.service` is too tight.

---

## 8. Test strategy

- **Unit tests** for validation rules and generators (golden files: expected configuration per input).
- **VM tests** with QEMU/KVM: a "provider VM" simulates the connection (DHCPv4, DHCPv6 PD, PPPoE server, optionally DS-Lite), the router VM has 3 NICs, a client
  VM in the LAN checks DHCP, DNS, internet, IPv6. Plugging and unplugging cables through the link state of the virtual NIC (QEMU monitor
  `set_link <netdev> on|off`). Layout:

  ```
  [provider VM] ──net-wan── [router VM nic0]
                             [router VM nic1] ──net-lan── [client VM]
                             [router VM nic2]   (free, for ambiguity tests)
  ```
  The networks are QEMU `socket` or Linux bridge networks without a connection to the real network. For "internet", the provider VM is on a NAT network of its
  own. Scenarios as files (`test/scenarios/*.json`): connection type, expected detection, expected reachability.
- **Real hardware:** test matrix with mini PCs and typical USB network adapters (Realtek, ASIX, Intel) in M6.
- **Same hardening in tests as in production** (NF-05, L-09).
- **Self-check** (audit) runs in every test VM and must have no critical findings.

---

## 9. Risks

| ID | Risk | Countermeasure |
|---|---|---|
| R-01 | Variety of provider connections (PPPoE variants, VLAN, DS-Lite, CGNAT) | provider VM for each variant, list of tested providers, fallback to manual settings |
| R-02 | Network cards without free drivers or requiring firmware (some Realtek chips) | firmware packages in the image, hardware list, clear message in the assistant |
| R-03 | An update makes the router unusable | snapshots + automatic rollback (F-UPD-02/03) |
| R-04 | Non-experts lock themselves out (wrong WAN setting, forgotten password) | rollback on connection loss, reset path on the device (e.g. boot the stick again → "reset password") |
| R-05 | Security hole in the web interface | privilege separation, LAN only, no dependencies, audit, fast updates |
| R-06 | The project depends on one person | open license, good documentation, machine-readable planning document, tests |
| R-07 | The stick creator overwrites the wrong disk | USB bus only, never the system drive, size limit with additional confirmation, name and size in the warning (F-STICK-03) |
| R-08 | SmartScreen or virus scanners warn about the stick creator, non-experts give up | code signing (Q-17), reproducible build, checksums on the download page, instructions with screenshots |
| R-09 | The computer does not start from the stick (boot order, Secure Boot, old BIOS computers) | note on boot menu keys (F-STICK-10), shim for Secure Boot (Q-16), list of tested devices |
| R-10 | Wi-Fi adapters differ widely: many built-in cards refuse an access point on 5 GHz or do not support it at all, and nothing here has run on a real radio yet | The page shows what the adapter reports (`iw phy`), a failed start rolls back with the message from `hostapd`, external access points stay the recommended way for good coverage |

---

## 10. Repo structure

As of 0.18. What is where and how it fits together is explained in [docs/architecture.md](docs/architecture.md), how to work with it in
[docs/development.md](docs/development.md).

```
vanilla-router/
  PLAN.md              this planning document (authoritative)
  README.md            entry point for humans, guide to the documentation
  CONTRIBUTING.md      rules for code, tests, texts, commits
  SECURITY.md          reporting security issues (GitHub's reporting feature, Q-20)
  CLAUDE.md            working rules for AI agents
  docs/                architecture.md, development.md, logo/
  cmd/                 vrctl, vr-webui, vr-install, vr-detect, vr-stick (Windows), vr-sign
  internal/            one package per purpose (overview: docs/architecture.md, section 10)
  web/                 frontend (vanilla JS, embedded through go:embed)
  packaging/           .deb package, systemd units, stick creator
  image/               mkosi configuration for the stick and target system
  test/                check-all.sh (all checks), check-main.sh (NF-08, NF-09), lab/ (namespace lab),
                       dev/ (test instance of the web interface), stick/ (test image, preview of the stick creator)
```

---

## 11. Glossary

| Term | Meaning |
|---|---|
| WAN | connection toward the internet (modem or provider router) |
| LAN | home network |
| PD | prefix delegation: the provider assigns an IPv6 prefix for the home network through DHCPv6 |
| RA / SLAAC | router advertisement / automatic IPv6 addresses of devices |
| ULA | unique local address, private IPv6 addresses (`fd00::/8`) |
| PPPoE | dial-up protocol with DSL and some fiber connections |
| DS-Lite | connection with real IPv6 and IPv4 only through a tunnel to the provider |
| CGNAT | IPv4 NAT at the provider, incoming IPv4 connections impossible |
| home.arpa | reserved domain for home networks (RFC 8375) |

---

## 12. Sources

Research from 2026-11-04. Forums are marked as such and must be confirmed in the test lab before implementation.

| ID | Source | Content |
|---|---|---|
| S-01 | https://packages.ubuntu.com/resolute/dnsmasq · https://packages.ubuntu.com/resolute/fail2ban | dnsmasq and fail2ban are in *universe* in Ubuntu 26.04 |
| S-02 | https://www.debian.org/releases/stable/index.de.html | Debian 13: full support until 2028-08-09, LTS until 2030-06-30 |
| S-03 | https://wiki.debian.org/IPv6PrefixDelegation | PD with networkd: both interfaces must be managed by networkd |
| S-04 | https://major.io/p/dhcpv6-prefix-delegation-with-systemd-networkd | example PD + `IPv6SendRA` with networkd |
| S-05 | https://forum.netgate.com/post/949140 (forum) | Telekom: format of the PPPoE user name, VLAN 7, modem as a media converter |
| S-06 | https://telekomhilft.telekom.de/t5/Festnetz-Internet/Optic-fiber-connection-setup-with-Glasfaser-Modem-2-and-my-own/td-p/6566852 (Telekom community) | PPPoE over VLAN 7 on Telekom connections |
| S-07 | https://forum.vodafone.de/t5/Ger%C3%A4te/Eigener-Router-Bridge-Mode-DS-Lite/m-p/3055535 (forum) | Vodafone cable: bridge mode, DS-Lite or dual stack, can change |
| S-08 | https://dev.to/lyraalishaikh/stop-risky-linux-upgrades-from-becoming-outages-practical-rollbacks-with-btrfs-snapper-126h | btrfs + snapper for rollbacks after upgrades |
| S-09 | https://github.com/david-cortes/snapper-in-debian-guide | snapper + grub-btrfs + snapper-rollback on Debian, snapshot at each apt run |
| S-10 | https://parrot.sh/docs/configuration/btrfs-assistant-snapper-setup/ | grub-btrfs shows snapshots, but does not reliably boot them on Debian systems without adapting the initramfs |
| S-11 | https://wiki.archlinux.org/title/Mkosi | mkosi builds bootable Debian and Ubuntu images, writable to USB |
| S-12 | https://isc.org/dhcp/ | ISC DHCP unmaintained since the end of 2022, successor Kea |
| S-13 | https://docs.opnsense.org/manual/isc.html | OPNsense: switch to Kea or dnsmasq, dnsmasq with DHCPv6 and RA |
| S-14 | https://github.com/xan105/web-vanilla-router · https://awesome.ecosyste.ms/projects/github.com%2Frayc2045%2Fvanilla-router | JavaScript libraries with the same name |
| S-15 | `apt-cache show` on the prototype (Ubuntu 26.04, 2026-11-04) | *main*: dnsmasq-base, nftables, ppp, btrfs-progs, kea-dhcp4-server. *universe*: dnsmasq, fail2ban, snapper, mkosi. grub-btrfs: no package |
| S-16 | https://www.gnu.org/licenses/agpl-3.0.txt | license text AGPL version 3 (November 19, 2007), in the repo as `LICENSE` |
| S-17 | https://www.tmdn.org/tmview/ (search "vanilla", classes 9/38/42, offices DE/EM/WO/AT/CH/BX, 2026-11-05) · https://euipo.europa.eu/eSearch/#details/trademarks/018129708 | Trademark search for the name (Q-13): 18 hits, no trademark "vanilla router"; "VANILLA" by Cinnamon B.V. with class 42 |
| S-18 | `apt-cache policy` in Ubuntu 26.04 (2026-11-06) · http://archive.ubuntu.com/ubuntu/dists/noble-security/ · http://archive.ubuntu.com/ubuntu/dists/jammy-security/ | `hostapd` is in *universe*, `iw`, `wireless-regdb`, `rfkill` and `wpasupplicant` in *main*. `hostapd` and `wpasupplicant` share the source package `wpa`. Security pockets: noble both `2:2.10-21ubuntu0.2`, jammy both `2:2.10-6ubuntu2.2` |

---

## Change history

| Version | Date | Change |
|---|---|---|
| 0.1 | 2026-11-04 | First draft from the prototype and conversations |
| 0.2 | 2026-11-04 | Research incorporated: Debian instead of Ubuntu (D-08), networkd PD example, provider details (Telekom, Vodafone), status of snapshots and rollback, mkosi, name; Q-14, Q-15, sources (S-NN) |
| 0.3 | 2026-11-04 | Review: contradictions fixed (distribution, time service, package list), random ULA (F-NET-12), PPPoE templates (F-NET-13), firewall in one `inet` table (D-09), disk and boot (3.9.4), recovery (3.9.5, F-INST-12–14), taking over code (3.11), tasks for M1 (7.1), VM layout, Secure Boot (Q-16) |
| 0.4 | 2026-11-04 | The project owner's decisions: Ubuntu LTS (D-10, D-08 rejected, Q-01 done), license AGPL-3.0-or-later (D-11, Q-11 done), principle comfort over optimization (P-10). Package check for *main* [S-15]: dnsmasq-base instead of dnsmasq, no fail2ban, snapper open (Q-03). New: F-UI-07, NF-08, NF-09, T-M1-08 |
| 0.5 | 2026-11-04 | Ubuntu Server with the package set of `ubuntu-server` (D-12, table in 3.2: resolved, cloud-init, snapd …), chrony instead of timesyncd. M1 started: T-M1-02 to 08 done, T-M1-01 switched to the namespace lab (run pending). L-17 (locale, checks without data) |
| 0.6 | 2026-11-04 | M2 (configuration core), M3 (installer, images) and M5 (snapshots, rollback) finished in code, untested on hardware (7.2). Q-03 and Q-04 implemented, D-13. Test plan for hardware (7.3). M4 (web interface) open |
| 0.7 | 2026-11-04 | M4: web interface taken over and switched to `config.json` (T-M4-01, T-M4-02), F-UI-07 done, WAN settings in the web interface open (T-M4-03). L-18 |
| 0.8 | 2026-11-04 | Flow from the user's point of view (section 1, D-15) and principle P-11: stick creator for Windows (3.12, F-STICK, D-14), assistant shortened to physical things (3.9.2, F-INST-04 rejected, F-INST-15 to 18), `vr/stick.json`, credentials and settings in the web interface (3.8, F-UI-08/09). M4 expanded (T-M4-04 to 08), Q-17 to Q-19, R-07 to R-09 |
| 0.9 | 2026-11-04 | T-M4-03 and T-M4-04 done: page "Internet & home network", setup step internet access, `wan.detected`. F-UI-03, F-UI-08, F-UI-09 done (without hardware test), test plan 7.3 extended |
| 0.10 | 2026-11-05 | T-M4-05 and T-M4-06: assistant with arrow keys and Enter only, without credentials, name and time zone; `vr/stick.json`; address and code on the console. F-INST-15, 16, 18 done, F-INST-17 without QR |
| 0.11 | 2026-11-05 | T-M4-08: stick creator for Windows (`cmd/vr-stick`), F-STICK-01 to 10 done, test with a real stick open. `vr-sign` for signed manifests (part of T-M4-07) |
| 0.12 | 2026-11-05 | Project logo (vanilla ice cream, the vanilla pod as an antenna): `internal/logo` generates SVG, PNG and icon without third-party tools (`docs/logo/`, favicon of the web interface). Stick creator in the logo's style and with an icon (`internal/winres` can now do several resources), assistant with a text logo and colored headings. T-M4-08 checked with a real stick |
| 0.13 | 2026-11-05 | Simpler logo: only scoop and cone in two colors (vanilla, brown), without the Wi-Fi reference. Stick creator and assistant in these colors |
| 0.14 | 2026-11-05 | Logo as a pictogram: circle (scoop) above a triangle (cone), two colors, no pattern. Text logo in the assistant likewise |
| 0.15 | 2026-11-05 | Developer documentation: architecture (idea, programs, data flow, processes, packages, files, security model, reading order, hardware status), development (setup, testing, trying it out without a router, building, stick creator, common problems), `CONTRIBUTING.md`, `SECURITY.md`, README as a guide. Tools: `test/check-all.sh` (all checks with one command), `test/dev/webui.sh` (test instance without root), `test/stick/test-package.sh` and `preview.sh`. Q-20 new, section 10 updated to the actual layout |
| 0.16 | 2026-11-05 | First publication on GitHub (`jacob3141/vanilla-router`), pre-release 0.0.1. Q-20 done (GitHub's reporting feature), trademark search for Q-13 [S-17], the name stays for now |
| 0.17 | 2026-11-05 | English on GitHub (part of Q-07): `README.md`, `SECURITY.md` and all commit messages translated, history rewritten |
| 0.19 | 2026-11-06 | Installer: the final unmount no longer fails on the busy cgroup mount under /sys, the restart no longer needs programs from the unplugged stick (both found in the VM). First complete installation in a VM: the installed system boots and shows the address and setup code. Assistant: spinner with seconds while waiting and during installation (L-21), clean console (screen cleared, machine-id set on the stick so systemd no longer tries first-boot presets). Lab fixed for WSL (VLAN setup, FIFOs under /tmp, pure Go build), first complete run: 5 of 5 passed (T-M1-01). Translation: single-line patterns no longer match across lines |
| 0.18 | 2026-11-06 | Everything in English (D-16, Q-07 done): code, comments, documentation, this document (`docs/architecture.md`, `docs/development.md`). The product is English by default, German is selectable through `internal/i18n` (F-UI-05, F-INST-10). The language question comes first in the assistant. First build of the boot stick with mkosi, starts in VirtualBox (T-M3-04): fixes for the signed boot loader, services after the presets, `fdisk` on the stick and a tool check at build time (L-19, L-20). Q-21 new |
| 0.20 | 2026-11-06 | Pre-release 0.0.2. Tested on Hyper-V (Generation 2, Secure Boot on): the stick boots, cable detection, installation (17 s), restart without the stick and the start from the disk work without any workaround; the project owner set up the router and used the web interface by hand ("basically works", no systematic checklist). The source link of the web interface was still a placeholder, now GitHub, tagged per version (F-UI-07). F-UPD-02 and 03 statuses brought in line with T-M5. Q-22 (self-update), `CHANGELOG.md` |
| 0.21 | 2026-11-06 | Wi-Fi access point as an optional extension through `hostapd` (D-18, Q-06 done, F-WIFI, T-M4-09, R-10, S-18, non-goal in 1.4 narrowed). Exception to NF-09 for this one package, decided by the project owner. Done in code, not tested with a real radio |
