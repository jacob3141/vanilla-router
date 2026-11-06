#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Test lab for WAN detection (T-M1-01) made of network namespaces and veth pairs.
# No QEMU needed, runs in seconds, but tests real netlink, real raw sockets and VLAN 7.
#
#   Namespace vrlab-router            Namespace vrlab-provider
#   r0  <──veth──>  pdhcp      dnsmasq as a DHCP server (192.0.2.0/24, cable/fiber modem)
#   r1  <──veth──>  ppppoe     pppoe-server on ppppoe.7 (VLAN 7, like Telekom)
#   r2  <──veth──>  plan       nothing (a cable into the home network)
#
# "Plugging in a cable" = turning on the other side (ip link set <peer> up), then r* reports carrier.
#
# Usage (root needed, e.g. in WSL):
#   sudo test/lab/netns-lab.sh test      all scenarios, exit code 0 if all passed
#   sudo test/lab/netns-lab.sh up        build the lab and leave it standing (to try things out)
#   sudo test/lab/netns-lab.sh plug <pdhcp|ppppoe|plan> / unplug <…>
#   sudo test/lab/netns-lab.sh down      remove everything
# Dependencies: iproute2, dnsmasq-base, pppoe (pppoe-server), golang-go
set -euo pipefail

NS_R=vrlab-router
NS_P=vrlab-provider
HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../.." && pwd)
# FIFOs do not work reliably on Windows drives in WSL (/mnt/c), so the work folder is under /tmp.
WORK="${TMPDIR:-/tmp}/vrlab-work"
BIN="$WORK/vr-detect"

die() { echo "ERROR: $*" >&2; exit 1; }
inr() { ip netns exec "$NS_R" "$@"; }
inp() { ip netns exec "$NS_P" "$@"; }

need() {
	[ "$(id -u)" = 0 ] || die "root needed (sudo)"
	local miss=()
	command -v ip >/dev/null || miss+=(iproute2)
	command -v dnsmasq >/dev/null || miss+=(dnsmasq-base)
	command -v pppoe-server >/dev/null || miss+=(pppoe)
	command -v go >/dev/null || [ -x "$BIN" ] || miss+=(golang-go)
	[ ${#miss[@]} = 0 ] || die "missing packages: ${miss[*]}  →  sudo apt install ${miss[*]}"
}

build() {
	mkdir -p "$WORK"
	local u=${SUDO_USER:-root}
	if command -v go >/dev/null; then
		echo "building vr-detect …"
		(cd "$REPO" && sudo -u "$u" env GOFLAGS=-buildvcs=false CGO_ENABLED=0 go build -o "$BIN" ./cmd/vr-detect) || die "build failed"
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
		inr ip link set "$r" down # vr-detect turns it on itself
		inp ip link set "$p" down # "cable not plugged in"
	done

	# Provider A: DHCP (documentation network RFC 5737)
	inp ip addr add 192.0.2.1/24 dev pdhcp
	inp dnsmasq --conf-file=/dev/null --keep-in-foreground --port=0 --interface=pdhcp \
		--dhcp-range=192.0.2.100,192.0.2.150,255.255.255.0,1h --dhcp-option=3,192.0.2.1 \
		--dhcp-option=6,192.0.2.1 --leasefile-ro --pid-file= --log-facility=- \
		>"$WORK/dnsmasq.log" 2>&1 &
	echo $! >>"$WORK/$NS_P.pids"

	# Provider B: PPPoE over VLAN 7
	# A VLAN only comes up while its parent is up, so the "cable" is plugged in briefly for this.
	inp ip link add link ppppoe name ppppoe.7 type vlan id 7
	inp ip link set ppppoe up
	inp ip link set ppppoe.7 up
	inp pppoe-server -F -I ppppoe.7 -C AC-LAB-7 -L 198.51.100.1 -R 198.51.100.10 -N 4 \
		>"$WORK/pppoe-server.log" 2>&1 &
	echo $! >>"$WORK/$NS_P.pids"
	sleep 0.5
	inp ip link set ppppoe down
	for pid in $(cat "$WORK/$NS_P.pids"); do
		kill -0 "$pid" 2>/dev/null || die "service in the provider namespace did not start, see $WORK/*.log"
	done
	echo "Lab is up: ports r0 (DHCP), r1 (PPPoE VLAN 7), r2 (home network) in namespace $NS_R"
}

plug() { inp ip link set "$1" up; }
unplug() { inp ip link set "$1" down; }

reset_cables() {
	local p
	for p in pdhcp ppppoe plan; do unplug "$p"; done
	inr sh -c 'for i in r0 r1 r2; do ip link set $i down; done'
}

# start_detect <name>: starts vr-detect in the router namespace, input through a FIFO.
start_detect() {
	local name=$1
	rm -f "$WORK/$name.in"
	mkfifo "$WORK/$name.in"
	inr "$BIN" -include-virtual -ports r0,r1,r2 -json -probe-timeout 2s -retry-delay 1s \
		<"$WORK/$name.in" >"$WORK/$name.json" 2>"$WORK/$name.log" &
	DETECT_PID=$!
	exec 3>"$WORK/$name.in" # keep the FIFO open, otherwise vr-detect sees EOF immediately
}

PLUG_MSG="Now plug only the cable"

# wait_count <name> <text> <count> <seconds>: waits until text is in the log at least count times.
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
		echo "  PASSED     $1"
		PASS=$((PASS + 1))
	else
		echo "  FAILED     $1  (log: $WORK/$1.log, result: $WORK/$1.json)"
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
	expect_json $n '"wan": "r0"' '"method": "dhcp"' '"dhcp_offer": "192.0.2.1[0-9][0-9]/24 via 192.0.2.1"'
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

scenario_unplug_first() {
	local n=unplug-first
	reset_cables
	plug plan
	plug pdhcp
	start_detect $n
	wait_text $n "Still plugged in: Port 1, Port 3" 5 || return 1
	unplug pdhcp
	unplug plan
	wait_text $n "$PLUG_MSG" 5 || return 1
	plug ppppoe
	finish_detect || return 1
	expect_json $n '"wan": "r1"' '"method": "pppoe"'
}

scenario_ambiguous() {
	local n=ambiguous
	reset_cables
	start_detect $n
	wait_text $n "$PLUG_MSG" 5 || return 1
	plug plan
	plug pdhcp
	wait_text $n "Several cables" 6 || { kill "$DETECT_PID"; return 1; }
	unplug plan
	unplug pdhcp
	wait_count $n "$PLUG_MSG" 2 5 || return 1
	plug pdhcp
	finish_detect || return 1
	expect_json $n '"wan": "r0"' '"method": "dhcp"'
}

scenario_no_answer() {
	local n=no-answer
	reset_cables
	start_detect $n
	wait_text $n "$PLUG_MSG" 5 || return 1
	plug plan
	wait_text $n "no modem is answering" 25 || { kill "$DETECT_PID"; return 1; }
	# Menu: ↓ to "Use this port anyway", Enter (P-11: only arrow keys and Enter)
	printf '\033[B\n' >&3
	finish_detect || return 1
	expect_json $n '"wan": "r2"' '"method": "link"'
}

run_tests() {
	need
	build
	[ -x "$BIN" ] || die "$BIN is missing"
	lab_up
	trap lab_down EXIT
	echo "Scenarios:"
	local s
	for s in dhcp pppoe_vlan7 unplug_first ambiguous no_answer; do
		local rc=0
		"scenario_$s" || rc=$?
		result "${s//_/-}" $rc
	done
	echo "$PASS passed, $FAIL failed"
	[ "$FAIL" = 0 ]
}

case "${1:-}" in
test) run_tests ;;
up) need; build; lab_up ;;
down) lab_down; echo "Lab removed" ;;
plug) plug "${2:?other side's port: pdhcp, ppppoe or plan}" ;;
unplug) unplug "${2:?other side's port: pdhcp, ppppoe or plan}" ;;
*) sed -n '3,19p' "$0"; exit 2 ;;
esac
