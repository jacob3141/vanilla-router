# Security

vanilla-router sits between the internet and a home network, so we take security issues very seriously.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through GitHub instead:
[report a vulnerability](https://github.com/jacob3141/vanilla-router/security/advisories/new)
(“Security” tab → “Report a vulnerability”). Only the project maintainers can see the report. We will
reply there, coordinate the fix with you and publish an advisory only afterwards. Reports in English or
German are welcome.

Helpful details:

- affected version or commit,
- which part (web interface, helper, firewall, installer, stick creator …),
- steps to reproduce and what an attacker could achieve.

## What counts as a vulnerability

- Access to the router or the home network **from the internet** that the configuration does not allow
  (firewall, IPv4 and IPv6).
- Access to the web interface **without a valid login**, or bypassing CSRF protection, login rate
  limiting or session expiry.
- A path from the web server (user `vr-webui`) to **root**, for example through a helper action that does
  not validate its parameters sufficiently.
- The file service exposes files outside the share.
- The stick creator writes an image **without a valid signature**, or writes to a disk other than a
  selected USB stick.
- Secrets (PPPoE password, password hash) become readable by unauthorized users or end up in an export.

Not a security issue: anything someone can do with the web interface password or with root access to the
router. That is what those credentials are for.

## How the project is protected

The security model with all its boundaries is described in
[docs/architecture.md, section 12](docs/architecture.md#12-security-model). In short: a
firewall in front of the network covering IPv4 and IPv6 in one table, a web server that never runs as
root and has a hardened systemd unit, privileged actions only through a helper with a fixed list of
actions, no third-party libraries, packages only from Ubuntu *main* with automatic security updates, and
snapshots with automatic rollback after updates.
