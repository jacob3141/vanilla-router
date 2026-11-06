# vanilla-router: guide for AI agents

A router built from an unmodified Ubuntu LTS on standard hardware, with an installer that guides non-experts step
by step. **[PLAN.md](PLAN.md) is authoritative.** Read it before any larger task.

License: **AGPL-3.0-or-later** (D-11). Guideline: **comfort over optimization, but never at the expense of
security** (P-10).

## Working with PLAN.md

- IDs (`P-`, `L-`, `F-…-`, `NF-`, `D-`, `Q-`, `R-`, `M`, `T-M…-`, `S-`) are stable. Never renumber, never
  reassign. What is dropped gets the status `rejected` and stays.
- Refer to IDs in commits, code comments and tests, e.g. `F-INST-03`, `D-05`.
- If a task changes the status, update the status in PLAN.md in the same commit, plus `version`, `updated` and
  the change history.
- Change decisions (`D-`) only with a reason. The project owner decides open questions (`Q-`). Suggest a
  tendency, but do not implement it on your own.
- Record new research results with a source under `S-NN`. Mark forum sources as such.

## Basic rules for code

- **Go with the standard library only** (D-01, NF-03). No external modules. Frontend without frameworks, without
  a build step, without a CDN.
- **Every new source file** starts with `SPDX-License-Identifier: AGPL-3.0-or-later` in the language's comment
  style (NF-08).
- **Package dependencies only from Ubuntu *main*** (D-10, NF-09). So `dnsmasq-base` instead of `dnsmasq`, no
  `fail2ban`. Exceptions from *universe* only for extensions and only with a justified decision.
- **One source of truth** (`config.json`, D-02). Service configuration files are always generated completely,
  never edited by text search.
- **Check before applying** (P-05): generate → check with the service's tool → replace atomically → reload →
  health check → on failure restore.
- **nftables:** own tables per purpose, replace with `table x; delete table x; table x { … }`, **never `flush
  ruleset`** (D-03). Always compare rule sets with `nft -s` (without counters, L-10).
- **Privilege separation** (D-04): the web server never runs as root. Everything privileged goes through the helper
  with a fixed list of actions. Each action checks its parameters itself, regardless of what the web server
  checked.
- **Do not start external programs with file capabilities** (e.g. `ping`) from hardened services, prefer native
  code (L-09).
- **Frontend:** DOM only through the `h()` helper, never `innerHTML` with data. No `style` attributes (CSP), only
  `el.style`. Data from the network (host names, log lines) is not trustworthy.
- **Tests:**
  - Validation rules and generators with unit tests and golden files (NF-04).
  - Time and calendar logic always with tests (L-16).
  - The test environment has the same systemd hardening as production (NF-05).

## Documentation for humans

[docs/architecture.md](docs/architecture.md) (structure), [docs/development.md](docs/development.md) (setup,
testing, building), [CONTRIBUTING.md](CONTRIBUTING.md) (rules, in detail with reasons). Anyone changing programs,
packages, commands or flows keeps these documents up to date in the same commit.

## Check before every commit

```
test/check-all.sh               # gofmt, go vet (Linux + Windows), go test, SPDX (NF-08), packages from main (NF-09)
sudo test/lab/netns-lab.sh test # WAN detection in the namespace lab (root, see the top of the script)
```

Evaluate tool output (apt, ip, …) only with `LC_ALL=C`, checks fail on missing data (L-17).

## Language and texts

- **Everything is in English:** code, comments, documentation, PLAN.md and everything on GitHub (commit messages,
  issues, pull requests, releases, repo description). Q-07.
- **The product is English by default, German is selectable.** New user-visible texts: English in the code, German
  in `internal/i18n/de.json` ([docs/architecture.md, section 14](docs/architecture.md#14-languages)). The stick
  creator has its own table in `cmd/vr-stick/texts.go`.
- Texts for non-experts without jargon: "internet port" instead of "WAN", "home network" instead of "LAN".
- Examples only with documentation addresses: IPv4 `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24` (RFC 5737),
  IPv6 `2001:db8::/32` (RFC 3849), domain `home.arpa` or `example.org`.

## Privacy in the repo

The project is public (GitHub). **No** real MAC, IP or IPv6 addresses, host names, credentials, keys, backups, audit
results or logs from a real network in the repo. The prototype is in a private repo; whatever is taken over from
there is cleaned first (PLAN.md 3.11).

## Local notes

Environment-specific things (tools on the development machine, access to the prototype) are in `CLAUDE.local.md`.
The file is excluded by `.gitignore` and never committed.
