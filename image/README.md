# Images (M3)

| Ordner | Ergebnis | Inhalt |
|---|---|---|
| `target/` | `rootfs.tar.zst` | Ubuntu Server 26.04 (Paketumfang `ubuntu-server`, D-12) mit vanilla-router, Kernel, shim + GRUB |
| `stick/` | `vanilla-router-stick.raw` | kleines Ubuntu, Assistent `vr-install` auf tty1, darin `rootfs.tar.zst` |

Bauen: `image/build.sh [version]` (mkosi, Root oder User-Namespaces, Internet).

**Stand 2026-11-04: ungetestet.** Die mkosi-Konfiguration ist nach Dokumentation geschrieben, aber noch
nicht gebaut worden (auf dem Entwicklungsrechner fehlen Root-Rechte in WSL). Zu prüfen beim ersten Bau:

- Heißen die Schlüssel in der installierten mkosi-Version so (`ShimBootloader=`, `PackageDirectories=`, `Keymap=`)?
- Bootet der Stick mit eingeschaltetem Secure Boot (signierter shim + GRUB von Ubuntu)?
- Findet `vr-install` den Stick als Systemdatenträger (`disk.BootDisk`) und bietet ihn nicht als Ziel an?
- Bootet das installierte System (fstab mit Subvolumes, `rootflags=subvol=@` durch `update-grub`)?

Die Schritte selbst (Partitionen, Dateisysteme, Entpacken, Bootloader, Dienste) sind in
`internal/install` getestet, mit nachgebautem System. Den echten Durchlauf zeigt der Test auf Hardware.
