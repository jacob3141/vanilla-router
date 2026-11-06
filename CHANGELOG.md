# Changelog

Pre-releases: **not ready for production use**. See [the status](docs/architecture.md#15-status-what-has-been-tested-for-real).

## 0.0.2 (2026-11-06)

First release with a boot stick image and a complete installation, tested in a virtual machine.

### Added

- English as the source language everywhere, German selectable. The language follows the choice made in the assistant.
- Activity indicator with elapsed seconds whenever the assistant waits or installs.
- Boot stick image built with mkosi, `Bootloader=grub-signed` for Secure Boot.

### Fixed

- Installer: the final unmount failed with "target is busy" on the cgroup mount below `/sys`.
- Installer: the restart failed after unplugging the stick, because it needed `systemctl` from the stick.
- Boot stick: `sfdisk` was missing. The build now fails if a tool the installer needs is absent.
- Web interface and package: the source link pointed to a placeholder (AGPL section 13). It now points to the source of exactly this version.

### Changed

- Defaults are `en_US.UTF-8`, the `us` keyboard layout and UTC, until a language is chosen.
- Renamed to English: the file share's `.trash` folder and the `line_id`, `access_number` and `co_user` keys of the PPPoE settings in `config.json`. A configuration from 0.0.1 with these keys has to be entered again.

### Known limits

- The image and the package are **not signed**. Compare them with `SHA256SUMS` from this release page.
- An installed router cannot update vanilla-router itself yet. Ubuntu's security updates do work.
- Rollback after a broken update has only been tested with a fake system.
- Never tested on real router hardware, nor with a real PPPoE line.
- The stick creator for Windows is not part of this release: its download address and signing key are not decided yet. Write the image with Rufus, Etcher or `dd`.

## 0.0.1 (2026-11-05)

First publication: source and `.deb` package.
