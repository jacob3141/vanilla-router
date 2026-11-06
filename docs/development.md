# Development

Everything you need to work on vanilla-router: setting up, building, testing, trying it out. How it all
fits together is in [architecture.md](architecture.md). The rules for contributions are in
[CONTRIBUTING.md](../CONTRIBUTING.md).

Every command in this document is meant exactly as written: from the repo's root folder, without root
unless it starts with `sudo`. None of it changes the computer outside the repo, unless it says so.

## Contents

1. [Short version](#1-short-version)
2. [Prerequisites](#2-prerequisites)
3. [First check](#3-first-check)
4. [What is where](#4-what-is-where)
5. [Testing](#5-testing)
6. [Trying it out without a router](#6-trying-it-out-without-a-router)
7. [Building](#7-building)
8. [Stick creator (Windows)](#8-stick-creator-windows)
9. [Common problems](#9-common-problems)

## 1. Short version

On Ubuntu 26.04 (also in WSL2 on Windows):

```bash
sudo apt install golang-go git
```

```bash
git clone https://github.com/jacob3141/vanilla-router.git && cd vanilla-router
```

```bash
test/check-all.sh
```

```bash
test/dev/webui.sh
```

The last command starts the web interface on http://127.0.0.1:9443. The password is then in
`dev/test-credentials.txt`.

## 2. Prerequisites

### Required

| What | Version | Where from |
|---|---|---|
| Linux, x86-64 | **Ubuntu 26.04 LTS** recommended, the same version as the target system | also in WSL2 on Windows 11 |
| Go | at least the version in [go.mod](../go.mod) (currently 1.26) | Ubuntu 26.04: `sudo apt install golang-go`, otherwise https://go.dev/dl |
| git | any | `sudo apt install git` |

There are **no** other dependencies: no `go mod download`, no npm, no Docker. The project only uses the Go
standard library (D-01). A fresh clone builds without network access once Go is installed.

Other distributions work the same for building and unit tests. Only the package source check
(`test/check-main.sh`) needs the `apt-cache` of an Ubuntu 26.04, and it is skipped otherwise.

### Depending on the task

| Task | Additionally | Section |
|---|---|---|
| Really check the generated firewall and dnsmasq configuration | `nftables`, `dnsmasq-base` | [5.3](#53-checking-generated-files-with-the-real-tools) |
| Namespace lab of the WAN detection | root, `iproute2`, `dnsmasq-base`, `pppoe` | [5.4](#54-namespace-lab-wan-detection) |
| Build the `.deb` package | `dpkg-dev` | [7](#7-building) |
| Build the boot stick | `mkosi` ≥ 25, root or user namespaces, internet | [7](#7-building) |
| Test package for the stick creator | `dosfstools` | [8](#8-stick-creator-windows) |
| Test the stick creator on Windows | Windows 10/11, Go for Windows | [8](#8-stick-creator-windows) |

### Editor

Any editor with Go support (gopls). Only two things matter:

- **gofmt on save.** `test/check-all.sh` rejects unformatted code.
- **LF line endings.** [.gitattributes](../.gitattributes) enforces this on check-in, also on Windows. Shell
  scripts with CRLF do not run on Linux.

On Windows: clone the repo in WSL2 (e.g. `~/vanilla-router`) and open it with an editor that knows WSL. A
clone on a Windows drive (`/mnt/c/…`) also works, but is much slower.

## 3. First check

```bash
test/check-all.sh
```

This is the same script that must pass before every commit. It runs:

| Step | Checks |
|---|---|
| `gofmt -l .` | formatting |
| `go vet ./...` | typical errors, for Linux |
| `GOOS=windows go vet …` | the same for the stick creator (also runs on Linux) |
| `go test ./...` | all unit and golden file tests |
| `test/check-main.sh` | SPDX line in every source file (NF-08), all packages from Ubuntu *main* (NF-09) |

The first run takes longer because Go builds its cache, after that it is fast. `test/check-all.sh --quick`
leaves out the Windows check and the package sources.

It ends with `ALL PASSED`. Two tests skip themselves in some environments, and that is not an error: the
netlink test (without netlink, e.g. in containers) and the ICMP test (if `net.ipv4.ping_group_range` forbids
unprivileged ICMP sockets).

## 4. What is where

```
cmd/            programs: vrctl, vr-webui, vr-install, vr-detect, vr-stick (Windows), vr-sign
internal/       all packages, one folder per purpose (overview: docs/architecture.md, section 10)
web/static/     frontend (vanilla JS, no build step), in the binary through go:embed
packaging/      .deb package, systemd units, stick creator (.exe)
image/          mkosi configuration for the target system and boot stick
test/           check scripts, lab, test instance, test package for the stick creator
docs/           these guides, logo
PLAN.md         planning document: requirements, decisions, open questions, status (authoritative)
CLAUDE.md       working rules, also for AI agents
```

Not in the repo (through [.gitignore](../.gitignore)): `dist/` (build results), `dev/` (test password, test
key), `test/dev/work/`, `test/lab/work/`.

## 5. Testing

### 5.1 Unit tests

```bash
go test ./...
```

```bash
go test ./internal/gen -run TestGolden -v
```

The tests need neither root nor network nor installed services. Whatever touches the system sits behind an
interface (`apply.Runner`, `install.Runner`, fake sysfs, artificial clock) and is replaced in tests. New tests
follow the same pattern. A test that needs root or sends packets to the network belongs in the lab (5.4), not
in `go test`. Exception: the helper's settings tests call the real `dnsmasq --test` as apply does, so they need
`dnsmasq-base`.

### 5.2 Golden files

The generators (`internal/gen`) are compared with expected outputs: for each case under
`internal/gen/testdata/<case>/` there is a `config.json` and next to it, under `out/`, every generated file.

After a **deliberate** change to a generator:

```bash
go test ./internal/gen -update
```

```bash
git diff internal/gen/testdata
```

Read the difference carefully: it is exactly what will change on every router. To add a new case, create a
folder with a `config.json` and run `-update`.

### 5.3 Checking generated files with the real tools

The golden files only prove that nothing changes unintentionally. Whether nftables and dnsmasq accept the files
is checked by their own tools. This needs `nftables` and `dnsmasq-base`:

```bash
for f in internal/gen/testdata/*/out/etc/vanilla-router/dnsmasq.conf; do dnsmasq --test --conf-file="$f" || echo "FAILED: $f"; done
```

```bash
for f in internal/gen/testdata/*/out/etc/vanilla-router/nft/vr.nft; do sudo unshare --net nft -c -f "$f" || echo "FAILED: $f"; done
```

`nft -c` needs root. `unshare --net` runs it in an empty network namespace, so the computer's firewall stays
untouched. `-c` only checks and loads nothing.

### 5.4 Namespace lab (WAN detection)

Detecting the internet port works with netlink and raw sockets. For this the lab builds a network of network
namespaces and veth pairs: dnsmasq as a DHCP server, pppoe-server on VLAN 7, a free port. "Plugging in a cable"
there means "turning on the other side".

```bash
sudo apt install iproute2 dnsmasq-base pppoe
```

```bash
sudo test/lab/netns-lab.sh test
```

The lab only creates the namespaces `vrlab-router` and `vrlab-provider` and removes them at the end
(`sudo test/lab/netns-lab.sh down` if a run aborts). It is meant for WSL2 or a Linux computer. Further commands
(`up`, `plug`, `unplug`) are at the top of the script.

> **Status:** all 5 scenarios passed in WSL2 on 2026-11-06 (PLAN.md, T-M1-01).

### 5.5 Real hardware and virtual machines

The test plan for real hardware is in [PLAN.md, section 7.3](../PLAN.md#73-test-plan-for-real-hardware).
`vr-detect` without `-list` turns on all ports and sends DHCP and PPPoE requests. So: **never on a running
router**, only on an installation machine or in the lab.

**Hyper-V is the recommended VM on Windows**, since WSL2 already needs the Windows hypervisor. A Generation 2 VM with
the Secure Boot template `MicrosoftUEFICertificateAuthority` also tests Secure Boot. Adapter one on the `Default Switch`
(the "modem", with DHCP and NAT), adapter two on an internal switch (the "home network", with `MacAddressSpoofing On`,
because the bridge `lan0` has a MAC of its own). The stick becomes a second SCSI disk:
`VBoxManage convertfromraw` or `qemu-img` to VHD, `Convert-VHD` to VHDX. `Disconnect-VMNetworkAdapter` and
`Connect-VMNetworkAdapter` are the cables, `Remove-VMHardDiskDrive` unplugs the stick while the VM runs.

Before connecting the home network adapter, give the Windows side of the internal switch a fixed address **without a
gateway** (`192.168.42.2/24`, needs an elevated shell). Otherwise Windows gets a lease and a default route from the VM,
and because that adapter often has the lower metric, the PC's own traffic goes through the test router.

The boot stick can also be tried in VirtualBox: a VM with EFI, a 32 GB SATA disk and two network adapters, the
first as NAT (the "modem"), the second as host-only (the "home network", without VirtualBox's DHCP server, with
`--nicpromisc2 allow-all`). Convert
the stick with `VBoxManage convertfromraw dist/vanilla-router-stick.raw stick.vdi --format VDI` and attach it to a
USB storage controller. Start with both cables unplugged; `VBoxManage controlvm <vm> setlinkstate1 on` plugs one
in. Do not use a bridged adapter for the home network: the VM would hand out DHCP addresses in your real network.
PPPoE cannot be tested this way.

**VirtualBox while Hyper-V or WSL2 is active on Windows:** VirtualBox then runs through the Windows hypervisor and
loses timer interrupts. An idle guest stands still until the next interrupt: no message appears after plugging in the
cable, and the spinner's seconds stop. It looks like a hang in the assistant, but pressing any key wakes the VM, and the
kernel reports a `soft lockup`. Other settings (`--cpus 1`, `--paravirt-provider hyperv|minimal|none`) made it worse: the
VM did not even boot. What works is a small loop on the host that taps Shift once a second:
`VBoxManage controlvm <vm> keyboardputscancode 2a aa`. It types nothing, but delivers an interrupt. That is enough to get
through the assistant and the installation, with stalls of several minutes, but not for using the web interface.

The home network adapter needs `--nicpromisc2 allow-all`: the bridge `lan0` has a MAC of its own, and VirtualBox otherwise
only passes frames for the adapter's MAC. ARP answers then arrive, but nothing else does.

**Wi-Fi** needs a real adapter, and neither Hyper-V nor the WSL kernel offer one (the WSL kernel has no `mac80211_hwsim`). The
test plan is step 8 in PLAN.md 7.3. What works without a radio: `VR_WIFI=1 test/dev/webui.sh` for the page, and the real `hostapd`
for the syntax of a generated file. It reports unknown options first and only then fails at the missing adapter:

```bash
hostapd internal/gen/testdata/wifi-5-80/out/etc/vanilla-router/wifi/hostapd.conf
```

## 6. Trying it out without a router

### 6.1 Web interface

```bash
test/dev/webui.sh
```

The script builds `vr-webui` and `vrctl`, creates an example configuration under `test/dev/work/` and starts:

- the **helper in dry-run mode**: it only writes generated files under `test/dev/work/root/`, loads no service
  and restarts nothing,
- the **web server with `--dev`**: HTTP instead of HTTPS, only on 127.0.0.1.

Then open http://127.0.0.1:9443 and sign in with the password from `dev/test-credentials.txt`. The password is
randomly created at the first start and only applies to this test instance.

| Goal | How |
|---|---|
| another port | `test/dev/webui.sh 9500` |
| test setup after detected PPPoE | `VR_DETECTED=pppoe test/dev/webui.sh` |
| see the German translation | `VR_LOCALE=de_DE.UTF-8 test/dev/webui.sh` |
| Wi-Fi page without hardware | `VR_WIFI=1 test/dev/webui.sh` (simulated adapter, before and after the download, with two devices) |
| see what a change generates | `test/dev/work/root/etc/…` after saving in the web interface |
| debugging | `test/dev/work/helper.log`, `test/dev/work/serve.log` |
| frontend changed | restart the script (the files are embedded in the binary) |

Run the script from a Linux file system when you save passwords: on a Windows drive under WSL (`/mnt/e`) every file
has mode 777, and secrets are rejected for that.

Live values (traffic, devices) come from the development machine itself. Interfaces like `wan0` do not exist
there, so those displays stay empty. That is not an error.

### 6.2 Configuration and generators

```bash
go run ./cmd/vrctl default --wan-mac 02:00:00:00:00:01 --lan-mac 02:00:00:00:00:02 > /tmp/vr-config.json
```

```bash
go run ./cmd/vrctl validate /tmp/vr-config.json
```

```bash
go run ./cmd/vrctl render --config /tmp/vr-config.json --secrets /tmp/vr-secrets --out /tmp/vr-out
```

Afterwards `/tmp/vr-out` contains everything a router with this configuration would get. MACs starting with `02:`
are locally administered addresses and belong to no real device. `vrctl apply` without `--dry-run` needs root and
changes the system. That only belongs on a test router.

### 6.3 Installation assistant

```bash
go run ./cmd/vr-install --dry-run
```

Without root, the dry run shows the whole flow with fake ports and a fake disk, and at the end prints the plan it
would install. Nothing is written. It works as on the router: arrow keys and Enter.

### 6.4 Ports of the computer

```bash
go run ./cmd/vr-detect -list
```

Shows the Ethernet ports the way the assistant sees them (name, MAC, driver, bus, link). Changes nothing and sends
nothing.

## 7. Building

| Result | Command | Needs |
|---|---|---|
| a single program | `go build -o dist/ ./cmd/vrctl` | Go |
| package `dist/vanilla-router_<version>_amd64.deb` | `packaging/build-deb.sh [version]` | `dpkg-dev` |
| boot stick `dist/vanilla-router-stick.raw` | `image/build.sh [version]` | `mkosi`, root or user namespaces, internet |
| stick creator `dist/vanilla-router-stick.exe` | `packaging/build-stick-exe.sh [version]` | Go (cross-compile, also on Linux) |
| logo files | `go run ./internal/logo/mklogo` | Go |

Notes:

- All programs are static (`CGO_ENABLED=0`). A version is set with `-ldflags "-X main.version=…"`, the scripts do
  this themselves.
- The boot stick build needs a real Linux file system, not `/mnt/c` or `/mnt/e` in WSL, and about 10 GB of free
  space. See [image/README.md](../image/README.md).
- For a release, the stick creator is built with a download URL and key:
  `VR_MANIFEST_URL=… VR_PUBLIC_KEYS=… packaging/build-stick-exe.sh`. Both are still open (Q-19).
- The logo is code ([internal/logo](../internal/logo/logo.go)). Anyone changing it regenerates the files. A test
  fails as long as `docs/logo/` and the favicon do not match the code.

## 8. Stick creator (Windows)

The stick creator is built and tested on Linux like everything else. Only the window and drive access need Windows.

**View the window without administrator rights** (for working on texts and appearance):

```bash
test/stick/preview.sh
```

Start `dist/vr-stick-preview.exe` on Windows. Writing does not work with the preview.

**Try writing without a real image:**

```bash
sudo apt install dosfstools
```

```bash
test/stick/test-package.sh
```

This creates a test key under `dev/` and builds in `dist/stick-test/`:

- a test exe with this key,
- a 64 MB test image (not bootable, FAT16 with `vr/stick.json`),
- the signed manifest.

On Windows: start the exe, "Choose image file …", take the `.img.gz`. **The chosen stick is completely erased.**
Afterwards `vr\stick.json` on the stick contains what was taken over from Windows.

**Tests on Windows** (Go for Windows installed, in PowerShell in the repo):

```
go test ./cmd/vr-stick ./internal/windisk ./internal/winres ./internal/windefaults
```

`internal/windisk` only reads the list of drives and writes nothing. If a `.syso` is still in `cmd/vr-stick` (see
[9](#9-common-problems)), the test asks for administrator rights.

## 9. Common problems

| Message | Cause and solution |
|---|---|
| `go.mod requires go >= 1.26` | Go too old. Ubuntu 26.04: `golang-go`, otherwise https://go.dev/dl |
| `gofmt` lists files | `gofmt -w .` |
| Golden test fails | generator changed: `go test ./internal/gen -update`, then read `git diff` (5.2) |
| `TestFilesUpToDate` (logo) fails | `go run ./internal/logo/mklogo` |
| Helper settings tests: `dnsmasq: executable file not found` | `sudo apt install dnsmasq-base` |
| `check-main.sh`: `missing` | every new `.go`, `.sh`, `.js`, `.css` needs `SPDX-License-Identifier: AGPL-3.0-or-later` in its first lines |
| `check-main.sh`: `UNIVERSE` or `UNKNOWN` | a new package in `image/packages*.txt` does not come from Ubuntu *main* (NF-09). Exceptions only with a justified decision in PLAN.md |
| `check-main.sh`: `apt-cache is missing` | not Ubuntu: package sources are skipped, SPDX is still checked |
| Test instance: `address already in use` | another port: `test/dev/webui.sh 9500` |
| Windows: `go test ./cmd/vr-stick` asks for administrator rights | a `.syso` is in `cmd/vr-stick` (from `go generate`): delete `cmd/vr-stick/*.syso` |
| Script: `bad interpreter: /usr/bin/env: 'bash\r'` | the file has CRLF line endings (usually from an editor). Fix with `sed -i 's/\r$//' <file>`, set the editor to LF |
| Lab: `root needed` | `sudo test/lab/netns-lab.sh test` |
