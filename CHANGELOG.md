# Changelog

Pre-releases: **not ready for production use**. See [the status](docs/architecture.md#15-status-what-has-been-tested-for-real).

## 0.0.6 (2026-11-08)

### Added

- SSH access can be switched on in the web interface (Settings), with public keys for the user admin. Without it an installed router had no way to a shell.

## 0.0.5 (2026-11-08)

**Untested pre-release.** It was published to get a working update path for bug fixes. Expect errors. The new access point feature has never run on a real radio.

### Added

- Stick creator for Windows: a program that downloads the image, checks it and writes the boot stick.
- Several access points with the router as their center (`docs/sdn-concept.md`): a new page "Access points" for Wi-Fi networks, pairing by comparing a code, a central channel plan, and a preview of what each access point receives. An access point is a PC with Ubuntu Server, this package and `vrctl ap enable`.
- Segments: guest and older-devices networks as VLANs with their own DHCP, DNS and firewall rules. Your switches must pass VLAN tags. A router without segments generates the same files as before.
- New services `vr-apd` on the router (TLS 1.3 with pinned keys, home network only) and `vr-ap` on an access point (rollback timer and health check after each change).
- Own signed package source for updates of vanilla-router itself (D-19).

### Changed

- English only for now: the assistant no longer asks for a language, the stick creator no longer follows the Windows language. A language setting comes to the web interface later.
- The image is a zip file.

### Fixed

- Online step: only Ubuntu's sources are read, an unreachable vanilla-router repository no longer blocks it.
- Installed system: package sources, working DNS and DHCP, settings survive a rollback.

### Known limits

- Access points: never tested with a real radio, a real access point or a VLAN switch. See `docs/webui-sdn-log.md`.
- The assistant does not detect the access point role yet, and the router's own Wi-Fi card is not part of the roaming network.
- The image and the package are **not signed**. Compare them with `SHA256SUMS` from this release page.
- Never tested on real router hardware, nor with a real PPPoE line.

## 0.0.2 (2026-11-06)

First release with a boot stick image and a complete installation, tested in a virtual machine. The stick holds a small base system of 1.2 GB, and the router fetches the rest of Ubuntu Server by itself once it is online.

### Added

- English as the source language everywhere, German selectable. The language follows the choice made in the assistant.
- Activity indicator with elapsed seconds whenever the assistant waits or installs.
- Boot stick image built with mkosi, `Bootloader=grub-signed` for Secure Boot.
- Small base system on the stick that also works without internet, for PPPoE lines. `vrctl complete` fetches the rest.
- Updates of vanilla-router itself from its own signed package source, installed automatically like Ubuntu's security updates, with a snapshot before and a health check after.
- Password, configuration and secrets survive the rollback of a broken update.
- Wi-Fi access point as an optional extension (F-WIFI, D-18): a Wi-Fi adapter in the router joins the home network, so addresses, names and firewall work as on a cable. The new page sets network name, password, band, channel, channel width, country and on/off, and shows the connected devices. Changes roll back without confirmation within 5 minutes. **Never tested with a real radio so far.**
- `hostapd` comes from Ubuntu's *universe* and is downloaded on request only if an adapter exists. Apt is limited to this one package from *universe*.

### Fixed

- `dnsmasq` did not start on an installed router, so there was no DHCP and no DNS.
- The installed router had no package sources, so it received no security updates.
- Installer: the final unmount failed with "target is busy" on the cgroup mount below `/sys`.
- Installer: the restart failed after unplugging the stick, because it needed `systemctl` from the stick.
- Boot stick: `sfdisk` was missing. The build now fails if a tool the installer needs is absent.
- Web interface and package: the source link pointed to a placeholder (AGPL section 13). It now points to the source of exactly this version.

### Changed

- Defaults are `en_US.UTF-8`, the `us` keyboard layout and UTC, until a language is chosen.
- Renamed to English: the file share's `.trash` folder and the `line_id`, `access_number` and `co_user` keys of the PPPoE settings in `config.json`. A configuration from 0.0.1 with these keys has to be entered again.

### Known limits

- The image and the package are **not signed**. Compare them with `SHA256SUMS` from this release page.
- Updates have been tested in a container and the rollback in a virtual machine, not yet an update from the published package source on a running router.
- Rollback after a broken update has only been tested with a fake system.
- Never tested on real router hardware, nor with a real PPPoE line.
- The stick creator for Windows is not part of this release: its download address and signing key are not decided yet. Write the image with Rufus, Etcher or `dd`.

## 0.0.1 (2026-11-05)

First publication: source and `.deb` package.
