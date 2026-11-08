#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Tests the own package source (F-UPD-04) without network and without the real key: a throwaway key signs a
# repository with two versions, an unpacked target system (rootfs.tar.zst from image/build.sh) installs the older
# one, finds the newer one through apt and unattended-upgrades, and refuses a tampered repository.
#
# Usage (root, Linux): test/repo/test-repo.sh [rootfs.tar.zst]
set -euo pipefail
# All mounts in a private namespace: unmounting a bind of /dev in the shared one also takes the host's /dev/pts.
[ -n "${VR_TEST_NS:-}" ] || VR_TEST_NS=1 exec unshare --mount --propagation private "$0" "$@"
cd "$(dirname "$0")/../.."
ROOTFS=${1:-dist/stick-extra/usr/share/vanilla-router/rootfs.tar.zst}
[ -f "$ROOTFS" ] || { echo "missing $ROOTFS (image/build.sh)" >&2; exit 2; }
T=$(mktemp -d)
R=$T/root
cleanup() {
	local rc=$?
	for m in srv/repo dev proc; do if mountpoint -q "$R/$m"; then umount -R "$R/$m" || umount -R -l "$R/$m"; fi; done
	# Never delete through a mount: with /dev still bound, rm would delete the host's device nodes.
	if findmnt -rn -o TARGET | grep -q "^$R"; then
		echo "mounts left under $R, not deleting $T" >&2
	else
		rm -rf --one-file-system "$T"
	fi
	[ $rc -eq 0 ] || echo "FAILED (exit $rc)" >&2
	exit $rc
}
trap cleanup EXIT
fail() { echo "FAILED: $*" >&2; exit 1; }

echo "== throwaway key"
export GNUPGHOME=$T/gnupg; mkdir -m 700 "$GNUPGHOME"
gpg --batch --passphrase '' --quick-generate-key "vanilla-router test key" ed25519 sign never 2>/dev/null
FPR=$(gpg --list-keys --with-colons | awk -F: '/^fpr/ {print $10; exit}')
gpg --export "$FPR" >"$T/key.gpg"

echo "== two package versions"
mkdir -p "$T/debs"
for v in 0.0.90~test1 0.0.90~test2; do
	VR_ARCHIVE_KEYRING=$T/key.gpg OUT=dist bash packaging/build-deb.sh "$v" >/dev/null 2>&1
	mv "dist/vanilla-router_${v}_amd64.deb" "$T/debs/"
done
CONTENT=$(dpkg-deb -c "$T/debs/vanilla-router_0.0.90~test1_amd64.deb")
grep -q 'etc/apt/sources.list.d/vanilla-router.sources' <<<"$CONTENT" || fail "package lacks the source entry"
grep -q 'usr/share/keyrings/vanilla-router-archive-keyring.gpg' <<<"$CONTENT" || fail "package lacks the key"

echo "== repository with both"
bash packaging/repo/build-repo.sh "$T/repo" "$FPR" "$T"/debs/*.deb >/dev/null
gpgv --keyring "$T/key.gpg" "$T/repo/dists/stable/InRelease" 2>/dev/null || fail "InRelease not signed"

echo "== target system with the older version"
mkdir -p "$R"
tar --zstd -xpf "$ROOTFS" -C "$R" --numeric-owner
mount --bind /proc "$R/proc"; mount --rbind /dev "$R/dev"
mkdir -p "$R/srv/repo"; mount --bind "$T/repo" "$R/srv/repo"
rm -f "$R/etc/apt/sources.list.d/ubuntu.sources" # no network in this test
cp "$T/debs/vanilla-router_0.0.90~test1_amd64.deb" "$R/tmp/"
chroot "$R" dpkg -i /tmp/vanilla-router_0.0.90~test1_amd64.deb >/dev/null 2>&1 || chroot "$R" dpkg -i /tmp/vanilla-router_0.0.90~test1_amd64.deb
grep -q 'URIs: https://jacob3141.github.io/vanilla-router/apt' "$R/etc/apt/sources.list.d/vanilla-router.sources" ||
	fail "source entry not installed"
sed -i 's|^URIs: .*|URIs: file:///srv/repo|' "$R/etc/apt/sources.list.d/vanilla-router.sources"
go run ./cmd/vrctl render --out "$T/render" >/dev/null 2>&1 || true
cp internal/gen/testdata/dhcp/out/etc/apt/apt.conf.d/52vr-unattended-upgrades "$R/etc/apt/apt.conf.d/"

echo "== apt sees the update"
chroot "$R" apt-get update -o APT::Update::Error-Mode=any >/dev/null || fail "apt-get update"
POLICY=$(chroot "$R" apt-cache policy vanilla-router)
grep -q 'Candidate: 0.0.90~test2' <<<"$POLICY" || { echo "$POLICY"; fail "no candidate"; }

echo "== unattended-upgrades would install it"
UU=$(chroot "$R" unattended-upgrade --dry-run --debug 2>&1 || true)
grep -E 'Packages that will be upgraded:.*vanilla-router' <<<"$UU" ||
	{ grep -i -E 'allowed origins|vanilla|error' <<<"$UU" | head -12; fail "not picked by unattended-upgrades"; }

echo "== update"
chroot "$R" env DEBIAN_FRONTEND=noninteractive apt-get install -y vanilla-router >/dev/null 2>&1 || fail "install"
chroot "$R" dpkg-query -W -f '${Version}' vanilla-router | grep -qx '0.0.90~test2' || fail "version after update"

echo "== tampered repository is refused"
sed -i 's/^Description: .*/Description: tampered/' "$T/repo/dists/stable/InRelease"
rm -rf "$R/var/lib/apt/lists/"*
if chroot "$R" apt-get update -o APT::Update::Error-Mode=any >/dev/null 2>&1; then fail "tampered InRelease accepted"; fi

echo "ALL PASSED"
