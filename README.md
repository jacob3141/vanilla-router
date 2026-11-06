<p align="center"><img src="docs/logo/logo.svg" alt="Logo: a scoop of vanilla ice cream in a cone" width="160"></p>

# vanilla-router

**A router built from a perfectly ordinary Linux, for any computer with two network ports, set up from a boot stick that guides you step by step.**

> “Now plug the cable from your modem into one of the network ports.”
> That port automatically becomes the internet connection, all others become your home network.

## Why

Router systems like OpenWrt are built for tiny devices with a few MB of storage and are therefore quite
limited. Yet a used office PC or a mini PC with several network ports costs only €100–200 today and draws
very little power. It runs a full Ubuntu LTS with all packages and security updates. vanilla-router turns
it into a router without requiring any Linux knowledge.

## How it is meant to work for users

1. **Create the stick:** a Windows program that needs no installation. Plug in a USB stick, select it,
   done. It takes language, keyboard layout and time zone from Windows.
2. **Install:** boot the future router from the stick. The assistant asks only two things: which cable
   goes to the modem (just plug it in, it detects it) and which disk it may use.
3. **Set up:** open `https://router.home.arpa` in a browser, enter the code shown on the router's screen
   and choose a password. Everything else, such as your provider's credentials, devices, port forwarding
   and parental controls, is done there.

## Principles

- **Unmodified Ubuntu LTS** instead of a custom router operating system
- **Comfort over optimization**: well-known tools and guides instead of the smallest possible system, but never at the expense of security
- **Secure by default**: firewall for IPv4 and IPv6, no open services, automatic security updates
- **One configuration file** as the single source of truth; everything else is generated from it and validated before it is applied
- **Rollback**: changes that break the network and updates after which the router no longer works are rolled back automatically
- **No dependencies**: Go with the standard library only, frontend without frameworks, packages only from Ubuntu *main*

## Status

In development, **not ready for production use**. The code for all parts is written and covered by unit
tests: configuration, generators, applying with rollback, web interface, setup assistant, WAN detection,
rollback after updates and the stick creator. A complete installation works in a virtual machine with Secure
Boot; on real hardware only port detection and the stick creator have been tested so far. An installed router
does not update vanilla-router itself yet, only Ubuntu's security updates. The exact status is in
[docs/architecture.md, section 15](docs/architecture.md#15-status-what-has-been-tested-for-real),
the plan in [PLAN.md](PLAN.md).

The web interface comes from a hand-built prototype that serves a real household as router and NAS.

## Documentation

| For | What | Where |
|---|---|---|
| Everyone | Goals, requirements, decisions, open questions, roadmap | [PLAN.md](PLAN.md) |
| Developers | Architecture, idea, data flow, security model, where to start | [docs/architecture.md](docs/architecture.md) |
| Developers | Development environment, testing, trying it out without a router, building | [docs/development.md](docs/development.md) |
| Contributors | Rules for code, tests, texts and commits | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Security researchers | Reporting vulnerabilities | [SECURITY.md](SECURITY.md) |
| Building the boot stick | Images with mkosi | [image/README.md](image/README.md) |
| Everyone | What changed in each release | [CHANGELOG.md](CHANGELOG.md) |

## Quick start for developers

On Ubuntu 26.04 (WSL2 works too):

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

This runs all checks and starts the web interface locally on http://127.0.0.1:9443, without root and
without changing anything on your machine. More in [docs/development.md](docs/development.md).

## License

[GNU Affero General Public License, version 3 or later](LICENSE) (`AGPL-3.0-or-later`).
If you run a modified version and make it available to others over a network, you must also give them
its source code.

## Contributing

The project is an early pre-release. Bug reports and questions are welcome as
[issues](https://github.com/jacob3141/vanilla-router/issues), in English or German. Please discuss larger
changes in an issue first. What contributions should look like is described in
[CONTRIBUTING.md](CONTRIBUTING.md). Please report security issues privately ([SECURITY.md](SECURITY.md)).
