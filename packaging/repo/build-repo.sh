#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Builds or extends the signed apt repository of vanilla-router (F-UPD-04, Q-22). Routers find it through
# /etc/apt/sources.list.d/vanilla-router.sources from the package and install updates like any Ubuntu update,
# guarded by the snapshot hook and the health check (D-13).
#
# Usage: packaging/repo/build-repo.sh <repo folder> <key fingerprint> <package.deb>...
#   The repo folder keeps older packages (pool/), so it is the checked-out content of the published repository.
#   Signs with gpg; GNUPGHOME selects the keyring. The private key never belongs in this repository.
# Needs: apt-utils (apt-ftparchive), gnupg.
set -euo pipefail
[ $# -ge 3 ] || { sed -n '6,10p' "$0" >&2; exit 2; }
REPO=$1 KEY=$2; shift 2
# gpg asks for the passphrase in this terminal
if [ -t 0 ]; then export GPG_TTY=$(tty); fi
SUITE=stable

mkdir -p "$REPO/pool/main/v/vanilla-router" "$REPO/dists/$SUITE/main/binary-amd64"
for deb in "$@"; do
	[ "$(dpkg-deb -f "$deb" Package)" = vanilla-router ] || { echo "not a vanilla-router package: $deb" >&2; exit 1; }
	cp --update=none "$deb" "$REPO/pool/main/v/vanilla-router/"
done

cd "$REPO"
apt-ftparchive packages pool >"dists/$SUITE/main/binary-amd64/Packages"
gzip -9nkf "dists/$SUITE/main/binary-amd64/Packages"
# Origin and suite are what unattended-upgrades matches on (internal/gen, 52vr-unattended-upgrades).
apt-ftparchive \
	-o APT::FTPArchive::Release::Origin=vanilla-router \
	-o APT::FTPArchive::Release::Label=vanilla-router \
	-o APT::FTPArchive::Release::Suite=$SUITE \
	-o APT::FTPArchive::Release::Codename=$SUITE \
	-o APT::FTPArchive::Release::Architectures=amd64 \
	-o APT::FTPArchive::Release::Components=main \
	-o APT::FTPArchive::Release::Description="vanilla-router updates" \
	release "dists/$SUITE" >"dists/$SUITE/Release"
rm -f "dists/$SUITE/InRelease" "dists/$SUITE/Release.gpg"
gpg --batch --yes --local-user "$KEY" --digest-algo SHA512 --clearsign -o "dists/$SUITE/InRelease" "dists/$SUITE/Release"
gpg --batch --yes --local-user "$KEY" --digest-algo SHA512 -abs -o "dists/$SUITE/Release.gpg" "dists/$SUITE/Release"
# GitHub Pages would otherwise treat folders with an underscore as Jekyll files
touch .nojekyll
echo "repository ready: $REPO"
awk '/^Version:/ {print "  " $2}' "dists/$SUITE/main/binary-amd64/Packages" | sort -V
