// SPDX-License-Identifier: AGPL-3.0-or-later

package probe

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

// DHCP constants (RFC 2131, RFC 2132).
const (
	dhcpServerPort = 67
	dhcpClientPort = 68
	bootRequest    = 1
	bootReply      = 2

	optPad       = 0
	optSubnet    = 1
	optRouter    = 3
	optDNS       = 6
	optLease     = 51
	optMsgType   = 53
	optServerID  = 54
	optParamList = 55
	optEnd       = 255

	dhcpDiscover = 1
	dhcpOffer    = 2
)

var dhcpMagic = []byte{99, 130, 83, 99}

// Offer is a parsed DHCPOFFER.
type Offer struct {
	ServerMAC net.HardwareAddr
	YourIP    net.IP
	Mask      net.IPMask
	Router    net.IP
	DNS       []net.IP
	ServerID  net.IP
	Lease     time.Duration
}

// String for display, e.g. "192.0.2.10/24 via 192.0.2.1".
func (o Offer) String() string {
	s := o.YourIP.String()
	if o.Mask != nil {
		ones, _ := o.Mask.Size()
		s += fmt.Sprintf("/%d", ones)
	}
	if o.Router != nil {
		s += " via " + o.Router.String()
	}
	return s
}

// buildDiscover builds the BOOTP payload of a DHCPDISCOVER. The broadcast flag is set,
// because the port has no address and unicast responses to the offered address would otherwise
// not arrive. Deliberately without host name and vendor ID (P-09).
func buildDiscover(mac net.HardwareAddr, xid uint32) []byte {
	b := make([]byte, 236, 300)
	b[0] = bootRequest
	b[1] = 1 // Ethernet
	b[2] = 6 // MAC length
	binary.BigEndian.PutUint32(b[4:], xid)
	binary.BigEndian.PutUint16(b[10:], 0x8000) // Broadcast
	copy(b[28:34], mac)
	b = append(b, dhcpMagic...)
	b = append(b, optMsgType, 1, dhcpDiscover)
	b = append(b, optParamList, 4, optSubnet, optRouter, optDNS, optLease)
	b = append(b, optEnd)
	for len(b) < 300 { // some servers expect the BOOTP minimum length
		b = append(b, optPad)
	}
	return b
}

// BuildDiscoverFrame builds the complete Ethernet frame (broadcast, 0.0.0.0:68 → 255.255.255.255:67).
func BuildDiscoverFrame(mac net.HardwareAddr, xid uint32, ipID uint16) []byte {
	ip := buildIPv4UDP(net.IPv4zero, net.IPv4bcast, dhcpClientPort, dhcpServerPort, ipID, buildDiscover(mac, xid))
	return buildFrame(broadcastMAC, mac, 0, etherTypeIPv4, ip)
}

var errNotOffer = errors.New("no matching DHCPOFFER")

// ParseOfferFrame checks whether a frame is a DHCPOFFER for our request (xid, MAC).
func ParseOfferFrame(frame []byte, mac net.HardwareAddr, xid uint32) (Offer, error) {
	f, err := parseFrame(frame)
	if err != nil || f.EtherType != etherTypeIPv4 || f.VLAN != 0 {
		return Offer{}, errNotOffer
	}
	sport, dport, payload, err := parseIPv4UDP(f.Payload)
	if err != nil || sport != dhcpServerPort || dport != dhcpClientPort {
		return Offer{}, errNotOffer
	}
	o, err := parseOffer(payload, mac, xid)
	if err != nil {
		return Offer{}, err
	}
	o.ServerMAC = append(net.HardwareAddr(nil), f.Src...)
	return o, nil
}

func parseOffer(b []byte, mac net.HardwareAddr, xid uint32) (Offer, error) {
	if len(b) < 240 || b[0] != bootReply || b[1] != 1 || b[2] != 6 {
		return Offer{}, errNotOffer
	}
	if binary.BigEndian.Uint32(b[4:8]) != xid || net.HardwareAddr(b[28:34]).String() != mac.String() {
		return Offer{}, errNotOffer
	}
	if string(b[236:240]) != string(dhcpMagic) {
		return Offer{}, errNotOffer
	}
	o := Offer{YourIP: net.IP(append([]byte(nil), b[16:20]...))}
	var msgType byte
	opts := b[240:]
	for len(opts) > 0 {
		code := opts[0]
		if code == optEnd {
			break
		}
		if code == optPad {
			opts = opts[1:]
			continue
		}
		if len(opts) < 2 || len(opts) < 2+int(opts[1]) {
			return Offer{}, errors.New("DHCP option truncated")
		}
		v := opts[2 : 2+int(opts[1])]
		opts = opts[2+int(opts[1]):]
		switch code {
		case optMsgType:
			if len(v) == 1 {
				msgType = v[0]
			}
		case optSubnet:
			if len(v) == 4 {
				o.Mask = net.IPMask(append([]byte(nil), v...))
			}
		case optRouter:
			if len(v) >= 4 {
				o.Router = net.IP(append([]byte(nil), v[:4]...))
			}
		case optDNS:
			for i := 0; i+4 <= len(v); i += 4 {
				o.DNS = append(o.DNS, net.IP(append([]byte(nil), v[i:i+4]...)))
			}
		case optServerID:
			if len(v) == 4 {
				o.ServerID = net.IP(append([]byte(nil), v...))
			}
		case optLease:
			if len(v) == 4 {
				o.Lease = time.Duration(binary.BigEndian.Uint32(v)) * time.Second
			}
		}
	}
	if msgType != dhcpOffer {
		return Offer{}, errNotOffer
	}
	return o, nil
}
