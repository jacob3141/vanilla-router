# Contributing

Thanks for wanting to contribute. This document says what a contribution looks like that can be taken over
without much back and forth.

> **Status:** early pre-release. Bug reports and questions are welcome as issues, in English or German. Please
> discuss larger changes in an issue first, so nobody works for nothing. The rules below apply to everyone,
> also to AI agents.

## Before you start

1. [README.md](README.md) for the goal, [docs/architecture.md](docs/architecture.md) for the structure,
   [docs/development.md](docs/development.md) for the setup. `test/check-all.sh` must pass on your machine
   before you change anything.
2. [PLAN.md](PLAN.md) is authoritative. Check whether there is already a requirement (`F-…`), a decision
   (`D-…`) or an open question (`Q-…`) for what you plan.
3. Discuss larger changes first (issue), especially anything that touches a decision. Decisions only change
   with a reason. The project owner decides open questions.

## Guideline

**Comfort over optimization, but never at the expense of security** (P-10). When in doubt, what wins is what
is understandable for a non-expert and hard for an attacker, not what uses the least memory.

## Rules for code

| Rule | Why | ID |
|---|---|---|
| **Go with the standard library only**, no external modules | no supply chain, one static binary, maintainable for a long time | D-01, NF-03 |
| **Frontend without frameworks, build step or CDN** | the same reasons, and the web interface also works offline | D-01 |
| **SPDX line** `SPDX-License-Identifier: AGPL-3.0-or-later` at the start of every source file | clear license per file | NF-08 |
| **Packages only from Ubuntu *main*** (`dnsmasq-base` instead of `dnsmasq`) | only *main* gets guaranteed security updates | D-10, NF-09 |
| **One source of truth** `config.json`. Service files are always generated completely, never edited by text search | reproducible, testable | D-02 |
| **Check before applying**: generate → check with the service's tool → replace atomically → reload → health check → on failure restore | an error must not take the router down | P-05 |
| **nftables:** own tables per purpose, replace with `table x; delete table x; table x { … }`, **never `flush ruleset`** | Docker and others bring their own tables | D-03 |
| **Privilege separation:** the web server never runs as root. Anything privileged only through a fixed helper action that checks its parameters itself | a hole in the web server is not immediately root | D-04 |
| **Do not start programs with file capabilities** (e.g. `ping`) from hardened services, prefer native code | they fail because of the hardening | L-09 |
| **Frontend:** DOM only through `h()`, never `innerHTML` with data. No `style` attributes, only `el.style` | XSS, strict CSP | |
| **Data from the network** (host names, log lines, file names) is not trustworthy | | |
| Evaluate tool output (apt, ip …) only with `LC_ALL=C`. If data is missing, the check fails | translations change the output | L-17 |

## Tests

- **Every validation rule and every generator** gets unit tests, generators also golden files (NF-04).
- **Time and calendar logic always with tests** (L-16), time is passed in, not read.
- Whatever touches the system sits behind an interface and is replaced in tests (see
  [development.md, 5.1](docs/development.md#51-unit-tests)). `go test ./...` needs neither root nor network.
- The test environment has the same systemd hardening as production (NF-05).

## Texts

- Code, comments, documentation and everything on GitHub (commit messages, issues, pull requests, releases) are
  in **English**.
- The product is English by default, with German as a selectable language. **Add a new user-visible text** in
  English in the code and its German to [internal/i18n/de.json](internal/i18n/de.json)
  ([architecture.md, section 14](docs/architecture.md#14-languages)).
- Texts for non-experts **without jargon**: "internet port" instead of "WAN", "home network" instead of "LAN".
- Examples only with documentation addresses: IPv4 `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`
  (RFC 5737), IPv6 `2001:db8::/32` (RFC 3849), domain `home.arpa` or `example.org`, MACs starting with `02:`.

## Privacy in the repo

The project is public. **Never** in the repo: real MAC, IP or IPv6 addresses, host names, credentials, keys,
backups, audit results or logs from a real network. Test passwords and test keys belong in `dev/` (excluded by
`.gitignore`).

## Commits

Before every commit:

```bash
test/check-all.sh
```

- **One thing per commit.** Code, tests and the corresponding change to PLAN.md belong together.
- **Name IDs** where it fits: in the subject or text (`F-INST-03`, `D-05`), likewise in comments and tests.
- If a commit changes the status of a task, it updates the status in PLAN.md in the same commit, plus `version`,
  `updated` and the change history at the bottom of PLAN.md.
- A short subject in **English**, e.g. `M4: setup assistant with arrow keys and Enter only`, with a list of
  changes below it if needed.

## Maintaining PLAN.md

- IDs (`P-`, `L-`, `F-…-`, `NF-`, `D-`, `Q-`, `R-`, `M`, `T-M…-`, `S-`) are stable. Never renumber, never
  reassign. What is dropped gets the status `rejected` and stays.
- New research results with a source under `S-NN`. Mark forum sources as such.
- Open questions: suggest a tendency, but do not implement it on your own.

## License of your contributions

Contributions are under the same license as the project, [AGPL-3.0-or-later](LICENSE).

## Security issues

Do not report them as a public issue, see [SECURITY.md](SECURITY.md).
