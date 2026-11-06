#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Project checks without root (T-M1-08):
#   1. NF-08: every source file (Go, shell, JS, CSS) has an SPDX line in the first 5 lines.
#   2. NF-09: all packages from image/packages.txt and their recursive dependencies come from Ubuntu main.
#      Runs on an Ubuntu of the same version as the target system (WSL is enough), uses apt-cache.
# Usage: test/check-main.sh [--no-apt]
set -euo pipefail
cd "$(dirname "$0")/.."
FAIL=0

echo "== SPDX lines (NF-08)"
missing=0
while IFS= read -r -d '' f; do
	if ! head -n 5 "$f" | grep -q 'SPDX-License-Identifier: AGPL-3.0-or-later'; then
		echo "  missing: $f"
		missing=$((missing + 1))
	fi
done < <(find . -path ./.git -prune -o -path '*/work' -prune -o -type f \
	\( -name '*.go' -o -name '*.sh' -o -name '*.js' -o -name '*.css' \) -print0)
if [ "$missing" = 0 ]; then echo "  OK"; else FAIL=1; fi

[ "${1:-}" = "--no-apt" ] && exit $FAIL

echo "== Package sources (NF-09)"
command -v apt-cache >/dev/null || { echo "  apt-cache is missing, skipped"; exit $FAIL; }
export LC_ALL=C # apt otherwise translates "Depends:" and the evaluation finds nothing
. /etc/os-release
echo "  checked against: $PRETTY_NAME"
mapfile -t roots < <(cat image/packages*.txt | grep -v '^\s*#' | awk 'NF {print $1}' | sort -u)
mapfile -t allow < <(grep -v '^\s*#' test/universe-allow.txt | awk 'NF {print $1}')

bad=0
# Packages apt does not know are an error: otherwise a universe package could slip through unnoticed
# on a system without universe in its sources.
for r in "${roots[@]}"; do
	if ! apt-cache show --no-all-versions "$r" >/dev/null 2>&1; then
		echo "  UNKNOWN: $r (not in the package sources, probably universe or a typo)"
		bad=$((bad + 1))
	fi
done

# Resolve dependencies (Depends, PreDepends, without Recommends) level by level. Like apt, for
# "a | b" only the first alternative is followed and for virtual packages (<x>) the first provider.
# `apt-cache depends --recurse` would instead follow all alternatives (e.g. every pinentry-*).
#
# Format of apt-cache depends:
#   package
#    |Depends: a        ← "|": alternative, the next line belongs to the same group
#     Depends: b        ← last alternative of the group
#     Depends: <virt>
#       provider1       ← provider of the virtual package
first_deps() {
	apt-cache depends --no-recommends --no-suggests --no-conflicts --no-breaks --no-replaces \
		--no-enhances "$@" 2>/dev/null | awk '
	function take(name) { if (name != "") print name }
	/^[^ ]/ { if (pending_virtual) take(""); in_group = 0; pending_virtual = 0; next }
	/^    / { if (pending_virtual) { sub(/^ +/, ""); take($0); pending_virtual = 0 } ; next }
	/^ [ |](Pre)?Depends: / {
		alt = ($0 ~ /^ \|/)
		name = $0; sub(/^ [ |](Pre)?Depends: /, "", name)
		pending_virtual = 0
		if (!in_group) {
			if (name ~ /^</) pending_virtual = 1; else take(name)
		}
		in_group = alt
		next
	}
	{ in_group = 0 }'
}

declare -A seen=()
queue=("${roots[@]}")
for r in "${roots[@]}"; do seen[$r]=1; done
while [ ${#queue[@]} -gt 0 ]; do
	next=()
	while IFS= read -r d; do
		d=${d%%:*} # remove the architecture suffix (:any)
		[ -n "$d" ] && [ -z "${seen[$d]:-}" ] || continue
		seen[$d]=1
		next+=("$d")
	done < <(first_deps "${queue[@]}")
	queue=("${next[@]}")
done
mapfile -t pkgs < <(printf '%s\n' "${!seen[@]}" | sort)
echo "  ${#pkgs[@]} packages including dependencies"

# Component from the pool path, first entry per package (candidate from the current sources)
while IFS=$'\t' read -r pkg comp; do
	[ "$comp" = main ] && continue
	if printf '%s\n' "${allow[@]}" | grep -qx -- "$pkg"; then
		echo "  allowed ($comp): $pkg"
		continue
	fi
	echo "  NICHT main ($comp): $pkg"
	bad=$((bad + 1))
done < <(apt-cache show --no-all-versions "${pkgs[@]}" 2>/dev/null | awk '
	/^Package: / { p = $2 }
	/^Filename: / { split($2, a, "/"); if (!(p in seen)) { seen[p] = 1; print p "\t" a[2] } }')
if [ "$bad" = 0 ]; then echo "  OK"; else FAIL=1; fi
exit $FAIL
