#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Testlabor für die WAN-Erkennung (T-M1-01) aus Network Namespaces und veth-Paaren.
# Kein QEMU nötig, läuft in Sekunden, testet aber echtes Netlink, echte Rohsockets und VLAN 7.
#
#   Namespace vrlab-router            Namespace vrlab-provider
#   r0  <──veth──>  pdhcp      dnsmasq als DHCP-Server (192.0.2.0/24, Kabel-/Glasfasermodem)
#   r1  <──veth──>  ppppoe     pppoe-server auf ppppoe.7 (VLAN 7, wie Telekom)
#   r2  <──veth──>  plan       nichts (ein Kabel ins Heimnetz)
#
# „Kabel einstecken“ = Gegenseite einschalten (ip link set <peer> up), dann meldet r* Carrier.
#
# Aufruf (Root nötig, z. B. in WSL):
#   sudo test/lab/netns-lab.sh test      alle Szenarien, Exit-Code 0 wenn alle bestanden
#   sudo test/lab/netns-lab.sh up        Labor aufbauen und stehen lassen (zum Ausprobieren)
#   sudo test/lab/netns-lab.sh plug <pdhcp|ppppoe|plan> / unplug <…>
#   sudo test/lab/netns-lab.sh down      alles entfernen
# Abhängigkeiten: iproute2, dnsmasq-base, pppoe (pppoe-server), golang-go
set -euo pipefail

NS_R=vrlab-router
NS_P=vrlab-provider
HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../.." && pwd)
WORK="$HERE/work"
BIN="$WORK/vr-detect"

die() { echo "FEHLER: $*" >&2; exit 1; }
inr() { ip netns exec "$NS_R" "$@"; }
inp() { ip netns exec "$NS_P" "$@"; }

need() {
	[ "$(id -u)" = 0 ] || die "Root-Rechte nötig (sudo)"
	local miss=()
	command -v ip >/dev/null || miss+=(iproute2)
	command -v dnsmasq >/dev/null || miss+=(dnsmasq-base)
	command -v pppoe-server >/dev/null || miss+=(pppoe)
	command -v go >/dev/null || [ -x "$BIN" ] || miss+=(golang-go)
	[ ${#miss[@]} = 0 ] || die "fehlende Pakete: ${miss[*]}  →  sudo apt install ${miss[*]}"
}

build() {
	mkdir -p "$WORK"
	local u=${SUDO_USER:-root}
	if command -v go >/dev/null; then
		echo "baue vr-detect …"
		(cd "$REPO" && sudo -u "$u" env GOFLAGS=-buildvcs=false go build -o "$BIN" ./cmd/vr-detect) || die "Build fehlgeschlagen"
	fi
}

lab_down() {
	for ns in "$NS_R" "$NS_P"; do
		if [ -f "$WORK/$ns.pids" ]; then
			xargs -r kill <"$WORK/$ns.pids" 2>/dev/null || true
			rm -f "$WORK/$ns.pids"
		fi
		ip netns del "$ns" 2>/dev/null || true
	done
}

lab_up() {
	lab_down
	mkdir -p "$WORK"
	ip netns add "$NS_R"
	ip netns add "$NS_P"
	inr ip link set lo up
	inp ip link set lo up
	local pair
	for pair in r0:pdhcp r1:ppppoe r2:plan; do
		local r=${pair%%:*} p=${pair##*:}
		ip link add "$r" netns "$NS_R" type veth peer name "$p" netns "$NS_P"
		inr ip link set "$r" down # vr-detect schaltet selbst ein
		inp ip link set "$p" down # „Kabel steckt nicht“
	done

	# Anbieter A: DHCP (Dokumentationsnetz RFC 5737)
	inp ip addr add 192.0.2.1/24 dev pdhcp
	inp dnsmasq --conf-file=/dev/null --keep-in-foreground --port=0 --interface=pdhcp \
		--dhcp-range=192.0.2.100,192.0.2.150,255.255.255.0,1h --dhcp-option=3,192.0.2.1 \
		--dhcp-option=6,192.0.2.1 --leasefile-ro --pid-file= --log-facility=- \
		>"$WORK/dnsmasq.log" 2>&1 &
	echo $! >>"$WORK/$NS_P.pids"

	# Anbieter B: PPPoE über VLAN 7
	inp ip link add link ppppoe name ppppoe.7 type vlan id 7
	inp ip link set ppppoe.7 up
	inp pppoe-server -F -I ppppoe.7 -C AC-LAB-7 -L 198.51.100.1 -R 198.51.100.10 -N 4 \
		>"$WORK/pppoe-server.log" 2>&1 &
	echo $! >>"$WORK/$NS_P.pids"
	sleep 0.5
	for pid in $(cat "$WORK/$NS_P.pids"); do
		kill -0 "$pid" 2>/dev/null || die "Dienst im Provider-Namespace nicht gestartet, siehe $WORK/*.log"
	done
	echo "Labor steht: Ports r0 (DHCP), r1 (PPPoE VLAN 7), r2 (Heimnetz) im Namespace $NS_R"
}

plug() { inp ip link set "$1" up; }
unplug() { inp ip link set "$1" down; }

reset_cables() {
	local p
	for p in pdhcp ppppoe plan; do unplug "$p"; done
	inr sh -c 'for i in r0 r1 r2; do ip link set $i down; done'
}

# start_detect <name>: startet vr-detect im Router-Namespace, Eingaben über eine FIFO.
start_detect() {
	local name=$1
	rm -f "$WORK/$name.in"
	mkfifo "$WORK/$name.in"
	inr "$BIN" -include-virtual -ports r0,r1,r2 -json -probe-timeout 2s -retry-delay 1s \
		<"$WORK/$name.in" >"$WORK/$name.json" 2>"$WORK/$name.log" &
	DETECT_PID=$!
	exec 3>"$WORK/$name.in" # FIFO offen halten, sonst sieht vr-detect sofort EOF
}

PLUG_MSG="Stecken Sie jetzt nur das Kabel"

# wait_count <name> <text> <anzahl> <sekunden>: wartet, bis text mindestens anzahl-mal im Protokoll steht.
wait_count() {
	local i c
	for ((i = 0; i < $4 * 10; i++)); do
		c=$(grep -c -- "$2" "$WORK/$1.log" 2>/dev/null || true)
		[ "${c:-0}" -ge "$3" ] && return 0
		sleep 0.1
	done
	return 1
}

wait_text() { wait_count "$1" "$2" 1 "$3"; }

finish_detect() {
	local i
	for ((i = 0; i < 300; i++)); do
		kill -0 "$DETECT_PID" 2>/dev/null || break
		sleep 0.1
	done
	if kill -0 "$DETECT_PID" 2>/dev/null; then
		kill "$DETECT_PID"
		exec 3>&-
		return 1
	fi
	exec 3>&-
	wait "$DETECT_PID"
}

PASS=0
FAIL=0
result() { # result <name> <ok?>
	if [ "$2" = 0 ]; then
		echo "  BESTANDEN  $1"
		PASS=$((PASS + 1))
	else
		echo "  FEHLER     $1  (Protokoll: $WORK/$1.log, Ergebnis: $WORK/$1.json)"
		FAIL=$((FAIL + 1))
	fi
}

expect_json() { # expect_json <name> <muster>…
	local n=$1
	shift
	local m
	for m in "$@"; do grep -q -- "$m" "$WORK/$n.json" || return 1; done
}

scenario_dhcp() {
	local n=dhcp
	reset_cables
	start_detect $n
	wait_text $n "$PLUG_MSG" 5 || return 1
	plug pdhcp
	finish_detect || return 1
	expect_json $n '"wan": "r0"' '"method": "dhcp"' '"dhcp_offer": "192.0.2.1[0-9][0-9]/24 über 192.0.2.1"'
}

scenario_pppoe_vlan7() {
	local n=pppoe-vlan7
	reset_cables
	start_detect $n
	wait_text $n "$PLUG_MSG" 5 || return 1
	plug ppppoe
	finish_detect || return 1
	expect_json $n '"wan": "r1"' '"method": "pppoe"' '"vlan": 7' '"ac_name": "AC-LAB-7"'
}

scenario_erst_abziehen() {
	local n=erst-abziehen
	reset_cables
	plug plan
	plug pdhcp
	start_detect $n
	wait_text $n "Noch eingesteckt: Anschluss 1, Anschluss 3" 5 || return 1
	unplug pdhcp
	unplug plan
	wait_text $n "$PLUG_MSG" 5 || return 1
	plug ppppoe
	finish_detect || return 1
	expect_json $n '"wan": "r1"' '"method": "pppoe"'
}

scenario_mehrdeutig() {
	local n=mehrdeutig
	reset_cables
	start_detect $n
	wait_text $n "$PLUG_MSG" 5 || return 1
	plug plan
	plug pdhcp
	wait_text $n "mehrere Kabel" 6 || { kill "$DETECT_PID"; return 1; }
	unplug plan
	unplug pdhcp
	wait_count $n "$PLUG_MSG" 2 5 || return 1
	plug pdhcp
	finish_detect || return 1
	expect_json $n '"wan": "r0"' '"method": "dhcp"'
}

scenario_keine_antwort() {
	local n=keine-antwort
	reset_cables
	start_detect $n
	wait_text $n "$PLUG_MSG" 5 || return 1
	plug plan
	wait_text $n "antwortet kein Modem" 25 || { kill "$DETECT_PID"; return 1; }
	# Menü: ↓ auf „Diesen Anschluss trotzdem verwenden“, Enter (P-11: nur Pfeiltasten und Enter)
	printf '\033[B\n' >&3
	finish_detect || return 1
	expect_json $n '"wan": "r2"' '"method": "link"'
}

run_tests() {
	need
	build
	[ -x "$BIN" ] || die "$BIN fehlt"
	lab_up
	trap lab_down EXIT
	echo "Szenarien:"
	local s
	for s in dhcp pppoe_vlan7 erst_abziehen mehrdeutig keine_antwort; do
		local rc=0
		"scenario_$s" || rc=$?
		result "${s//_/-}" $rc
	done
	echo "$PASS bestanden, $FAIL fehlgeschlagen"
	[ "$FAIL" = 0 ]
}

case "${1:-}" in
test) run_tests ;;
up) need; build; lab_up ;;
down) lab_down; echo "Labor entfernt" ;;
plug) plug "${2:?Port der Gegenseite: pdhcp, ppppoe oder plan}" ;;
unplug) unplug "${2:?Port der Gegenseite: pdhcp, ppppoe oder plan}" ;;
*) sed -n '3,19p' "$0"; exit 2 ;;
esac
