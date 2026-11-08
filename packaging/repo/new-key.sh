#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Creates the signing key of the apt repository, once, by the project owner (Q-22). gpg asks for a passphrase.
# Writes the PUBLIC key to packaging/repo/vanilla-router-archive-keyring.gpg, which goes into the package and the
# repository. The private key stays in GNUPGHOME (default ~/.gnupg-vanilla-router): back it up offline, never commit
# it. Anyone holding it can install software on every router.
#
# Usage: packaging/repo/new-key.sh
set -euo pipefail
cd "$(dirname "$0")"
export GNUPGHOME=${GNUPGHOME:-$HOME/.gnupg-vanilla-router}
mkdir -p "$GNUPGHOME"; chmod 700 "$GNUPGHOME"
OUT=vanilla-router-archive-keyring.gpg
[ ! -e "$OUT" ] || { echo "$OUT exists already. A new key would cut off all installed routers." >&2; exit 1; }
gpg --quick-generate-key "vanilla-router archive signing key" ed25519 sign 5y
FPR=$(gpg --list-keys --with-colons "vanilla-router archive signing key" | awk -F: '/^fpr/ {print $10; exit}')
gpg --export "$FPR" >"$OUT"
echo
echo "Fingerprint: $FPR"
echo "Public key:  packaging/repo/$OUT (commit this file)"
echo "Private key: $GNUPGHOME"
echo "Back it up offline now, for example:"
echo "  GNUPGHOME=$GNUPGHOME gpg --export-secret-keys --armor $FPR > /media/<offline stick>/vanilla-router-signing-key.asc"
