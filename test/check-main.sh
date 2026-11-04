#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Projektprüfungen ohne Root-Rechte (T-M1-08):
#   1. NF-08: Jede Quelldatei (Go, Shell, JS, CSS) trägt eine SPDX-Zeile in den ersten 5 Zeilen.
#   2. NF-09: Alle Pakete aus image/packages.txt samt rekursiven Abhängigkeiten kommen aus Ubuntu main.
#      Läuft auf einem Ubuntu derselben Version wie das Zielsystem (WSL reicht), nutzt apt-cache.
# Aufruf: test/check-main.sh [--no-apt]
set -euo pipefail
cd "$(dirname "$0")/.."
FAIL=0

echo "== SPDX-Zeilen (NF-08)"
missing=0
while IFS= read -r -d '' f; do
	if ! head -n 5 "$f" | grep -q 'SPDX-License-Identifier: AGPL-3.0-or-later'; then
		echo "  fehlt: $f"
		missing=$((missing + 1))
	fi
done < <(find . -path ./.git -prune -o -path '*/work' -prune -o -type f \
	\( -name '*.go' -o -name '*.sh' -o -name '*.js' -o -name '*.css' \) -print0)
if [ "$missing" = 0 ]; then echo "  in Ordnung"; else FAIL=1; fi

[ "${1:-}" = "--no-apt" ] && exit $FAIL

echo "== Paketquellen (NF-09)"
command -v apt-cache >/dev/null || { echo "  apt-cache fehlt, übersprungen"; exit $FAIL; }
export LC_ALL=C # apt übersetzt sonst „Depends:“ (z. B. „Hängt ab von:“) und die Auswertung findet nichts
. /etc/os-release
echo "  geprüft gegen: $PRETTY_NAME"
mapfile -t roots < <(cat image/packages*.txt | grep -v '^\s*#' | awk 'NF {print $1}' | sort -u)
mapfile -t allow < <(grep -v '^\s*#' test/universe-allow.txt | awk 'NF {print $1}')

bad=0
# Pakete, die apt nicht kennt, sind ein Fehler: Sonst würde ein universe-Paket auf einem System
# ohne universe in den Quellen unbemerkt durchrutschen.
for r in "${roots[@]}"; do
	if ! apt-cache show --no-all-versions "$r" >/dev/null 2>&1; then
		echo "  UNBEKANNT: $r (nicht in den Paketquellen, vermutlich universe oder Tippfehler)"
		bad=$((bad + 1))
	fi
done

# Abhängigkeiten (Depends, PreDepends, ohne Recommends) Ebene für Ebene auflösen. Wie apt wird bei
# „a | b“ nur die erste Alternative verfolgt und bei virtuellen Paketen (<x>) der erste Anbieter.
# `apt-cache depends --recurse` würde dagegen allen Alternativen folgen (z. B. jedem pinentry-*).
#
# Format von apt-cache depends:
#   paket
#    |Depends: a        ← „|“: Alternative, die nächste Zeile gehört zur selben Gruppe
#     Depends: b        ← letzte Alternative der Gruppe
#     Depends: <virt>
#       anbieter1       ← Anbieter des virtuellen Pakets
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
		d=${d%%:*} # Architektur-Zusatz (:any) entfernen
		[ -n "$d" ] && [ -z "${seen[$d]:-}" ] || continue
		seen[$d]=1
		next+=("$d")
	done < <(first_deps "${queue[@]}")
	queue=("${next[@]}")
done
mapfile -t pkgs < <(printf '%s\n' "${!seen[@]}" | sort)
echo "  ${#pkgs[@]} Pakete einschließlich Abhängigkeiten"

# Komponente aus dem Pool-Pfad, erster Eintrag je Paket (Kandidat der aktuellen Quellen)
while IFS=$'\t' read -r pkg comp; do
	[ "$comp" = main ] && continue
	if printf '%s\n' "${allow[@]}" | grep -qx -- "$pkg"; then
		echo "  erlaubt ($comp): $pkg"
		continue
	fi
	echo "  NICHT main ($comp): $pkg"
	bad=$((bad + 1))
done < <(apt-cache show --no-all-versions "${pkgs[@]}" 2>/dev/null | awk '
	/^Package: / { p = $2 }
	/^Filename: / { split($2, a, "/"); if (!(p in seen)) { seen[p] = 1; print p "\t" a[2] } }')
if [ "$bad" = 0 ]; then echo "  in Ordnung"; else FAIL=1; fi
exit $FAIL
