# Images (M3)

| Folder | Result | Content |
|---|---|---|
| `target/` | `rootfs.tar.zst` | Ubuntu Server 26.04 (package set of `ubuntu-server`, D-12) with vanilla-router, kernel, shim + GRUB |
| `stick/` | `vanilla-router-stick.raw` | a small Ubuntu, assistant `vr-install` on tty1, containing `rootfs.tar.zst` |

Building: `image/build.sh [version]` (mkosi, root or user namespaces, internet). It needs a real Linux file system
(not `/mnt/c` or `/mnt/e` in WSL) and about 10 GB of free space, plus `mkosi`, `dpkg-dev`, `dosfstools`, `mtools`,
`zstd`, `systemd-repart` and `btrfs-progs` on the build machine.

**Status 2026-11-06:** built with mkosi 26 on Ubuntu 26.04. In a Hyper-V Generation 2 VM with Secure Boot on, the stick
boots, the installation completes and the installed system starts from its disk (Ubuntu's signed shim + GRUB, fstab
with subvolumes). Still to check: real hardware.

Notes from the first build:

- mkosi's default initrd needs `systemd-repart`, which Ubuntu has in *universe*. That is why the stick (not the
  target system) also uses *universe* at build time (see Q-21 in PLAN.md).
- mkosi's presets enable all services of the package. `stick/mkosi.finalize.chroot` runs after them and turns the
  network services off again. It also fails the build if the stick lacks a tool the installer needs.

The steps themselves (partitions, file systems, unpacking, boot loader, services) are tested in `internal/install`
with a fake system. The test on hardware shows the real run.
