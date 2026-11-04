#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Baut das Paket vanilla-router_<version>_amd64.deb (ohne Root, braucht go und dpkg-deb).
# Abhängigkeiten kommen aus image/packages.txt, damit Paket und Prüfung (NF-09) übereinstimmen.
# Aufruf: packaging/build-deb.sh [version]   → dist/vanilla-router_<version>_amd64.deb
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION=${1:-0.0.0~dev$(date -u +%Y%m%d%H%M)}
OUT=dist
umask 022
STAGE=$(mktemp -d)
chmod 755 "$STAGE" # mktemp legt 0700 an, das würde zur Wurzel / im Paket
trap 'rm -rf "$STAGE"' EXIT

echo "baue Programme …"
mkdir -p "$STAGE/usr/sbin"
for cmd in vrctl vr-detect vr-install; do
	[ -d "cmd/$cmd" ] || continue
	CGO_ENABLED=0 GOFLAGS=-buildvcs=false go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
		-o "$STAGE/usr/sbin/$cmd" "./cmd/$cmd"
done

install -Dm644 -t "$STAGE/usr/lib/systemd/system" packaging/systemd/*
install -Dm644 packaging/sysusers.d/vanilla-router.conf "$STAGE/usr/lib/sysusers.d/vanilla-router.conf"
install -Dm644 packaging/tmpfiles.d/vanilla-router.conf "$STAGE/usr/lib/tmpfiles.d/vanilla-router.conf"
install -Dm644 LICENSE "$STAGE/usr/share/doc/vanilla-router/copyright"

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
Homepage: https://example.org/vanilla-router
Description: Router aus einem unveränderten Ubuntu Server
 Erzeugt Netzwerk, Firewall, DNS und DHCP aus einer einzigen Konfigurationsdatei
 (/etc/vanilla-router/config.json) und spielt Änderungen geprüft und mit Rücknahme ein.
EOF
cat >"$STAGE/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if [ "$1" = configure ]; then
	systemd-sysusers vanilla-router.conf || true
	systemd-tmpfiles --create vanilla-router.conf || true
	systemctl daemon-reload || true
fi
EOF
chmod 755 "$STAGE/DEBIAN/postinst"

mkdir -p "$OUT"
DEB="$OUT/vanilla-router_${VERSION}_amd64.deb"
dpkg-deb --root-owner-group -Zxz --build "$STAGE" "$DEB" >/dev/null
echo "fertig: $DEB"
dpkg-deb --info "$DEB" | sed -n '/Package:/,/Depends:/p'
