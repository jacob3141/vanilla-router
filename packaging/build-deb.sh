#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Builds the package vanilla-router_<version>_amd64.deb (no root, needs go and dpkg-deb).
# Dependencies come from image/packages.txt, so the package and the check (NF-09) agree.
# Usage: packaging/build-deb.sh [version]   → dist/vanilla-router_<version>_amd64.deb
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION=${1:-0.0.0~dev$(date -u +%Y%m%d%H%M)}
OUT=dist
# Source of exactly this version (AGPL §13, F-UI-07), for a development build the repository itself
SOURCE_URL=https://github.com/jacob3141/vanilla-router
case "$VERSION" in *~*) ;; *) SOURCE_URL="$SOURCE_URL/tree/v$VERSION" ;; esac
umask 022
STAGE=$(mktemp -d)
chmod 755 "$STAGE" # mktemp creates 0700, that would become the root / in the package
trap 'rm -rf "$STAGE"' EXIT

echo "building programs …"
mkdir -p "$STAGE/usr/sbin"
for cmd in vrctl vr-detect vr-install vr-webui; do
	[ -d "cmd/$cmd" ] || continue
	CGO_ENABLED=0 GOFLAGS=-buildvcs=false go build -trimpath -ldflags "-s -w -X main.version=$VERSION -X main.sourceURL=$SOURCE_URL" \
		-o "$STAGE/usr/sbin/$cmd" "./cmd/$cmd"
done

install -Dm644 -t "$STAGE/usr/lib/systemd/system" packaging/systemd/*
install -Dm644 packaging/sysusers.d/vanilla-router.conf "$STAGE/usr/lib/sysusers.d/vanilla-router.conf"
install -Dm644 packaging/tmpfiles.d/vanilla-router.conf "$STAGE/usr/lib/tmpfiles.d/vanilla-router.conf"
install -Dm644 LICENSE "$STAGE/usr/share/doc/vanilla-router/copyright"
# Docker comes from universe (D-20), limited to its own packages. Its configuration keeps the firewall ours.
install -Dm644 packaging/docker/daemon.json "$STAGE/etc/docker/daemon.json"
install -Dm644 packaging/docker/vr-docker.sources "$STAGE/etc/apt/sources.list.d/vr-docker.sources"
install -Dm644 packaging/docker/vr-docker.pref "$STAGE/etc/apt/preferences.d/vr-docker"
# Own package source (F-UPD-04): public key and source entry. VR_ARCHIVE_KEYRING replaces the key for tests.
KEYRING=${VR_ARCHIVE_KEYRING:-packaging/repo/vanilla-router-archive-keyring.gpg}
if [ -f "$KEYRING" ]; then
	install -Dm644 "$KEYRING" "$STAGE/usr/share/keyrings/vanilla-router-archive-keyring.gpg"
	install -Dm644 packaging/repo/vanilla-router.sources "$STAGE/etc/apt/sources.list.d/vanilla-router.sources"
else
	case "$VERSION" in
	*~*) echo "note: no signing key yet ($KEYRING), the package gets no update source" >&2 ;;
	*) echo "a release needs $KEYRING (packaging/repo/new-key.sh)" >&2; exit 1 ;;
	esac
fi
mkdir -p "$STAGE/usr/share/vanilla-router"
grep -v '^\s*#' image/packages-online.txt >"$STAGE/usr/share/vanilla-router/online-packages"
install -Dm644 packaging/system.conf.d/vanilla-router.conf "$STAGE/usr/lib/systemd/system.conf.d/vanilla-router.conf"
# Under /etc: configuration files of the package (conffiles), may be adapted
install -Dm755 packaging/grub.d/42_vanilla_router "$STAGE/etc/grub.d/42_vanilla_router"
install -Dm644 packaging/default-grub.d/vanilla-router.cfg "$STAGE/etc/default/grub.d/vanilla-router.cfg"
install -Dm644 packaging/apt/80vr-snapshot "$STAGE/etc/apt/apt.conf.d/80vr-snapshot"

DEPENDS=$(grep -v '^\s*#' image/packages.txt | awk 'NF {print $1}' | paste -sd, - | sed 's/,/, /g')
mkdir -p "$STAGE/DEBIAN"
cat >"$STAGE/DEBIAN/control" <<EOF
Package: vanilla-router
Version: $VERSION
Architecture: amd64
Maintainer: vanilla-router <vanilla-router@invalid>
Depends: $DEPENDS
Section: net
Priority: optional
Homepage: https://github.com/jacob3141/vanilla-router
Description: Router built from an unmodified Ubuntu Server
 Generates network, firewall, DNS and DHCP from a single configuration file
 (/etc/vanilla-router/config.json) and applies changes checked and with rollback.
EOF
cat >"$STAGE/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if [ "$1" = configure ]; then
	systemd-sysusers vanilla-router.conf || true
	systemd-tmpfiles --create vanilla-router.conf || true
	systemctl daemon-reload || true
	# vr-smbd.service replaces the services of the package samba
	systemctl disable --now smbd.service nmbd.service samba-ad-dc.service 2>/dev/null || true
	# Update ($2 = old version): run the new programs. Sessions of the web interface end, the setup stays (F-UPD-08).
	if [ -n "$2" ]; then
		systemctl try-restart vr-webui-helper.service vr-webui.service vr-apd.service vr-ap.service vr-dnsmasq.service || true
	fi
fi
EOF
chmod 755 "$STAGE/DEBIAN/postinst"
(cd "$STAGE" && find etc -type f | sed 's|^|/|') >"$STAGE/DEBIAN/conffiles"

mkdir -p "$OUT"
DEB="$OUT/vanilla-router_${VERSION}_amd64.deb"
dpkg-deb --root-owner-group -Zxz --build "$STAGE" "$DEB" >/dev/null
echo "done: $DEB"
dpkg-deb --info "$DEB" | sed -n '/Package:/,/Depends:/p'
