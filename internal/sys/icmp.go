// SPDX-License-Identifier: AGPL-3.0-or-later

package sys

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"time"
)

// Ping without an external program: ICMP echo over a datagram socket (like `ping` without root,
// allowed by net.ipv4.ping_group_range). The ping program itself cannot be started under the
// hardened unit: it has the file capability cap_net_raw, which the
// CapabilityBoundingSet does not contain, so the kernel refuses to start it.

var pingSeq atomic.Uint32

func icmpEcho(id, seq uint16, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	b[0] = 8 // Echo Request
	binary.BigEndian.PutUint16(b[4:6], id)
	binary.BigEndian.PutUint16(b[6:8], seq)
	copy(b[8:], payload)
	binary.BigEndian.PutUint16(b[2:4], icmpChecksum(b))
	return b
}

func icmpChecksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i:]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}

// icmpPing sends an echo and waits for the response. Returns the response time in ms.
func icmpPing(ctx context.Context, addr string, timeout time.Duration) (float64, bool) {
	ip := net.ParseIP(addr).To4()
	if ip == nil {
		return 0, false
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, syscall.IPPROTO_ICMP)
	if err != nil {
		return 0, false
	}
	f := os.NewFile(uintptr(fd), "icmp")
	conn, err := net.FilePacketConn(f)
	f.Close()
	if err != nil {
		return 0, false
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)

	seq := uint16(pingSeq.Add(1))
	start := time.Now()
	// The kernel sets the ID itself on datagram sockets, the sequence number is compared
	if _, err := conn.WriteTo(icmpEcho(0, seq, []byte("routerui")), &net.UDPAddr{IP: ip}); err != nil {
		return 0, false
	}
	buf := make([]byte, 1500)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return 0, false
		}
		if n < 8 || buf[0] != 0 || binary.BigEndian.Uint16(buf[6:8]) != seq {
			continue // no response to our echo
		}
		if ua, ok := from.(*net.UDPAddr); ok && !ua.IP.Equal(ip) {
			continue
		}
		return float64(time.Since(start).Microseconds()) / 1000, true
	}
}
