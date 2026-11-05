#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Startet eine Testinstanz der Weboberfläche auf dem Entwicklungsrechner, ohne Root und ohne etwas am
# System zu ändern:
#   - Helper im Probelauf: schreibt erzeugte Dateien nur unter $WORK/root, lädt und startet nichts.
#   - Webserver mit --dev: HTTP statt HTTPS, nur auf 127.0.0.1.
#   - config.json aus `vrctl default` mit Beispiel-MACs (02:00:… = lokal verwaltet, keine echten Geräte).
#   - Testpasswort zufällig, liegt in dev/test-credentials.txt (per .gitignore ausgeschlossen).
#
# Aufruf: test/dev/webui.sh [port]          Vorgabe 9443, dann http://127.0.0.1:9443 öffnen
#         VR_DETECTED=pppoe test/dev/webui.sh   so tun, als hätte der Assistent PPPoE über VLAN 7 erkannt
# Beenden mit Strg+C. Arbeitsordner: test/dev/work (wird bei jedem Start neu angelegt).
set -euo pipefail
cd "$(dirname "$0")/../.."
PORT=${1:-9443}
WORK=test/dev/work
export GOFLAGS=${GOFLAGS:--buildvcs=false}
command -v go >/dev/null || { echo "go fehlt, siehe docs/entwicklung.md" >&2; exit 1; }

rm -rf "$WORK"
mkdir -p "$WORK/bin" "$WORK/root/etc/vanilla-router" "$WORK/data" dev
echo "baue vr-webui und vrctl …"
go build -o "$WORK/bin/" ./cmd/vr-webui ./cmd/vrctl

CFG="$WORK/root/etc/vanilla-router/config.json"
"$WORK/bin/vrctl" default --wan-mac 02:00:00:00:00:01 --lan-mac 02:00:00:00:00:02,02:00:00:00:00:03 >"$CFG"
if [ "${VR_DETECTED:-}" = pppoe ]; then
	# Ergebnis der Erkennung wie vom Assistenten (F-INST-15): Die Oberfläche fragt dann nach Zugangsdaten
	sed -i '0,/"type": "dhcp",/s//"type": "dhcp", "detected": {"method": "pppoe", "vlan": 7},/' "$CFG"
fi

PW_FILE=dev/test-credentials.txt
if [ ! -s "$PW_FILE" ]; then
	(umask 077; head -c 18 /dev/urandom | base64 | tr -d '/+=\n' >"$PW_FILE")
fi
{ cat "$PW_FILE"; echo; } | "$WORK/bin/vr-webui" set-password --data "$WORK/data" 2>/dev/null

cleanup() { kill $(jobs -p) 2>/dev/null || true; }
trap cleanup EXIT INT TERM

"$WORK/bin/vr-webui" helper --dry-run --root "$WORK/root" --allow-user "$(id -un)" \
	--socket "$WORK/helper.sock" >"$WORK/helper.log" 2>&1 &
for _ in $(seq 50); do [ -S "$WORK/helper.sock" ] && break; sleep 0.1; done
[ -S "$WORK/helper.sock" ] || { cat "$WORK/helper.log"; exit 1; }

"$WORK/bin/vr-webui" serve --dev --config "$CFG" --data "$WORK/data" --helper "$WORK/helper.sock" \
	--files "$WORK/files.sock" --listen "127.0.0.1:$PORT" --redirect aus \
	--origins "http://127.0.0.1:$PORT,http://localhost:$PORT" >"$WORK/serve.log" 2>&1 &

for _ in $(seq 50); do
	if (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null; then break; fi
	sleep 0.1
done
cat <<EOF

Testinstanz läuft: http://127.0.0.1:$PORT
  Passwort:          steht in $PW_FILE (nur für diese Testinstanz)
  Erzeugte Dateien:  $WORK/root/etc/…  (der Helper schreibt nur dorthin)
  Protokolle:        $WORK/helper.log, $WORK/serve.log
Beenden mit Strg+C.
EOF
wait
