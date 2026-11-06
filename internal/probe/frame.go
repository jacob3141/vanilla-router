// SPDX-License-Identifier: AGPL-3.0-or-later

// Package probe asks on a port whether a modem or provider answers there, without taking an
// address or changing the system: DHCP discover (T-M1-05) and PPPoE discovery with and
// without VLAN (T-M1-06). Packets are built by hand and sent over an AF_PACKET socket.
// Building and parsing packets is separate from the socket level and testable without privileges.
package probe

import (
	"encoding/binary"
	"errors"
	"net"
)

const (
	etherTypeIPv4      = 0x0800
	etherTypeVLAN      = 0x8100
	etherTypePPPoEDisc = 0x8863
)

var broadcastMAC = net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

// Frame is a parsed Ethernet frame.
type Frame struct {
	Dst, Src  net.HardwareAddr
	VLAN      int // 0 = without a VLAN tag
	EtherType uint16
	Payload   []byte
}

var errShort = errors.New("packet too short")

// buildFrame assembles an Ethernet frame, with a VLAN tag if vlan > 0.
func buildFrame(dst, src net.HardwareAddr, vlan int, etherType uint16, payload []byte) []byte {
	b := make([]byte, 0, 18+len(payload))
	b = append(b, dst...)
	b = append(b, src...)
	if vlan > 0 {
		b = binary.BigEndian.AppendUint16(b, etherTypeVLAN)
		b = binary.BigEndian.AppendUint16(b, uint16(vlan&0x0fff))
	}
	b = binary.BigEndian.AppendUint16(b, etherType)
	b = append(b, payload...)
	// Minimum length 60 bytes without checksum, otherwise some peers discard the frame.
	for len(b) < 60 {
		b = append(b, 0)
	}
	return b
}

// parseFrame parses an Ethernet frame. A VLAN tag is evaluated (only one, no QinQ).
func parseFrame(b []byte) (Frame, error) {
	if len(b) < 14 {
		return Frame{}, errShort
	}
	f := Frame{Dst: net.HardwareAddr(b[0:6]), Src: net.HardwareAddr(b[6:12])}
	et := binary.BigEndian.Uint16(b[12:14])
	rest := b[14:]
	if et == etherTypeVLAN {
		if len(rest) < 4 {
			return Frame{}, errShort
		}
		f.VLAN = int(binary.BigEndian.Uint16(rest[0:2]) & 0x0fff)
		et = binary.BigEndian.Uint16(rest[2:4])
		rest = rest[4:]
	}
	f.EtherType = et
	f.Payload = rest
	return f, nil
}

// insertVLAN puts a VLAN tag removed by the driver back into the frame, so that parseFrame
// treats all frames the same (see PACKET_AUXDATA in rawsock_linux.go).
func insertVLAN(b []byte, tci uint16) []byte {
	if len(b) < 12 {
		return b
	}
	out := make([]byte, 0, len(b)+4)
	out = append(out, b[:12]...)
	out = binary.BigEndian.AppendUint16(out, etherTypeVLAN)
	out = binary.BigEndian.AppendUint16(out, tci)
	return append(out, b[12:]...)
}

// checksum is the internet checksum (RFC 1071).
func checksum(b []byte) uint16 {
	var s uint32
	for i := 0; i+1 < len(b); i += 2 {
		s += uint32(binary.BigEndian.Uint16(b[i:]))
	}
	if len(b)%2 == 1 {
		s += uint32(b[len(b)-1]) << 8
	}
	for s>>16 != 0 {
		s = s&0xffff + s>>16
	}
	return ^uint16(s)
}

// buildIPv4UDP builds the IPv4 and UDP header around payload. The UDP checksum is calculated.
func buildIPv4UDP(src, dst net.IP, sport, dport uint16, id uint16, payload []byte) []byte {
	udpLen := 8 + len(payload)
	ip := make([]byte, 20, 20+udpLen)
	ip[0] = 0x45 // version 4, header 20 bytes
	binary.BigEndian.PutUint16(ip[2:], uint16(20+udpLen))
	binary.BigEndian.PutUint16(ip[4:], id)
	ip[8] = 64 // TTL
	ip[9] = 17 // UDP
	copy(ip[12:16], src.To4())
	copy(ip[16:20], dst.To4())
	binary.BigEndian.PutUint16(ip[10:], checksum(ip[:20]))

	udp := make([]byte, 8, udpLen)
	binary.BigEndian.PutUint16(udp[0:], sport)
	binary.BigEndian.PutUint16(udp[2:], dport)
	binary.BigEndian.PutUint16(udp[4:], uint16(udpLen))
	udp = append(udp, payload...)
	pseudo := make([]byte, 0, 12+udpLen)
	pseudo = append(pseudo, src.To4()...)
	pseudo = append(pseudo, dst.To4()...)
	pseudo = append(pseudo, 0, 17)
	pseudo = binary.BigEndian.AppendUint16(pseudo, uint16(udpLen))
	pseudo = append(pseudo, udp...)
	cs := checksum(pseudo)
	if cs == 0 {
		cs = 0xffff
	}
	binary.BigEndian.PutUint16(udp[6:], cs)
	return append(ip, udp...)
}

// parseIPv4UDP returns the ports and payload of an IPv4/UDP packet. Checksums are not
// checked (many cards only calculate them by offload when sending, own frames are anyway
// filtered out).
func parseIPv4UDP(b []byte) (sport, dport uint16, payload []byte, err error) {
	if len(b) < 20 || b[0]>>4 != 4 {
		return 0, 0, nil, errors.New("not IPv4")
	}
	ihl := int(b[0]&0x0f) * 4
	total := int(binary.BigEndian.Uint16(b[2:4]))
	if ihl < 20 || total < ihl+8 || len(b) < total {
		return 0, 0, nil, errShort
	}
	if b[9] != 17 {
		return 0, 0, nil, errors.New("not UDP")
	}
	if binary.BigEndian.Uint16(b[6:8])&0x3fff != 0 {
		return 0, 0, nil, errors.New("fragmented")
	}
	u := b[ihl:total]
	ul := int(binary.BigEndian.Uint16(u[4:6]))
	if ul < 8 || ul > len(u) {
		return 0, 0, nil, errShort
	}
	return binary.BigEndian.Uint16(u[0:2]), binary.BigEndian.Uint16(u[2:4]), u[8:ul], nil
}
