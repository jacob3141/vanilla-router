// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"encoding/binary"
	"net/netip"
	"syscall"
)

// Read the conntrack table directly via netlink (NETLINK_NETFILTER, ctnetlink). The router has
// neither the conntrack program nor /proc/net/nf_conntrack.

const (
	netlinkNetfilter    = 12
	nfnlSubsysCTNetlink = 1
	ipctnlMsgCTGet      = 1 // IPCTNL_MSG_CT_NEW = 0, IPCTNL_MSG_CT_GET = 1
	nlaFNested          = 0x8000
	nlaTypeMask         = 0x3fff

	ctaTupleOrig  = 1
	ctaTupleReply = 2
	ctaStatus     = 3
	ctaProtoinfo  = 4
	ctaTimeout    = 7

	ctaTupleIP    = 1
	ctaTupleProto = 2
	ctaIPv4Src    = 1
	ctaIPv4Dst    = 2
	ctaIPv6Src    = 3
	ctaIPv6Dst    = 4
	ctaProtoNum   = 1
	ctaProtoSrc   = 2
	ctaProtoDst   = 3

	ctaProtoinfoTCP      = 1
	ctaProtoinfoTCPState = 1

	ipsDstNAT  = 1 << 5
	ipsAssured = 1 << 2
)

var tcpStates = []string{"NONE", "SYN_SENT", "SYN_RECV", "ESTABLISHED", "FIN_WAIT", "CLOSE_WAIT", "LAST_ACK", "TIME_WAIT", "CLOSE", "LISTEN", "SYN_SENT2"}

type Conn struct {
	Proto    string `json:"proto"`
	Src      string `json:"src"`
	Dst      string `json:"dst"`
	SrcPort  uint16 `json:"sport,omitempty"`
	DstPort  uint16 `json:"dport,omitempty"`
	ReplySrc string `json:"reply_src,omitempty"` // with DNAT: the actual target in the LAN
	State    string `json:"state,omitempty"`
	Timeout  uint32 `json:"timeout"`
	DNAT     bool   `json:"dnat,omitempty"`
	Assured  bool   `json:"assured,omitempty"`
}

type nlattr struct {
	typ  uint16
	data []byte
}

func parseAttrs(b []byte) []nlattr {
	var out []nlattr
	for len(b) >= 4 {
		l := int(binary.LittleEndian.Uint16(b[0:2]))
		t := binary.LittleEndian.Uint16(b[2:4])
		if l < 4 || l > len(b) {
			break
		}
		out = append(out, nlattr{t & nlaTypeMask, b[4:l]})
		adv := (l + 3) &^ 3 // attributes are aligned to 4 bytes
		if adv > len(b) {
			break
		}
		b = b[adv:]
	}
	return out
}

func find(attrs []nlattr, typ uint16) []byte {
	for _, a := range attrs {
		if a.typ == typ {
			return a.data
		}
	}
	return nil
}

type tuple struct {
	src, dst     netip.Addr
	proto        uint8
	sport, dport uint16
}

func parseTuple(b []byte) tuple {
	var t tuple
	attrs := parseAttrs(b)
	if ip := find(attrs, ctaTupleIP); ip != nil {
		ia := parseAttrs(ip)
		for _, k := range []uint16{ctaIPv4Src, ctaIPv6Src} {
			if v := find(ia, k); v != nil {
				t.src, _ = netip.AddrFromSlice(v)
			}
		}
		for _, k := range []uint16{ctaIPv4Dst, ctaIPv6Dst} {
			if v := find(ia, k); v != nil {
				t.dst, _ = netip.AddrFromSlice(v)
			}
		}
	}
	if p := find(attrs, ctaTupleProto); p != nil {
		pa := parseAttrs(p)
		if v := find(pa, ctaProtoNum); len(v) >= 1 {
			t.proto = v[0]
		}
		if v := find(pa, ctaProtoSrc); len(v) >= 2 {
			t.sport = binary.BigEndian.Uint16(v)
		}
		if v := find(pa, ctaProtoDst); len(v) >= 2 {
			t.dport = binary.BigEndian.Uint16(v)
		}
	}
	return t
}

func protoName(p uint8) string {
	switch p {
	case 6:
		return "tcp"
	case 17:
		return "udp"
	case 1:
		return "icmp"
	case 58:
		return "icmpv6"
	}
	return "other"
}

// parseCTMessage reads a single ctnetlink message (without the netlink header, from nfgenmsg).
func parseCTMessage(b []byte) (Conn, bool) {
	if len(b) < 4 {
		return Conn{}, false
	}
	attrs := parseAttrs(b[4:])
	orig := find(attrs, ctaTupleOrig)
	if orig == nil {
		return Conn{}, false
	}
	o := parseTuple(orig)
	c := Conn{Proto: protoName(o.proto), Src: o.src.String(), Dst: o.dst.String(), SrcPort: o.sport, DstPort: o.dport}
	if r := find(attrs, ctaTupleReply); r != nil {
		rt := parseTuple(r)
		if rt.src.IsValid() && rt.src != o.dst {
			c.ReplySrc = rt.src.String()
		}
	}
	if v := find(attrs, ctaStatus); len(v) >= 4 {
		st := binary.BigEndian.Uint32(v)
		c.DNAT = st&ipsDstNAT != 0
		c.Assured = st&ipsAssured != 0
	}
	if v := find(attrs, ctaTimeout); len(v) >= 4 {
		c.Timeout = binary.BigEndian.Uint32(v)
	}
	if pi := find(attrs, ctaProtoinfo); pi != nil {
		if tcp := find(parseAttrs(pi), ctaProtoinfoTCP); tcp != nil {
			if v := find(parseAttrs(tcp), ctaProtoinfoTCPState); len(v) >= 1 && int(v[0]) < len(tcpStates) {
				c.State = tcpStates[v[0]]
			}
		}
	}
	return c, true
}

// dumpConntrack returns all connections of an address family (AF_INET or AF_INET6).
func dumpConntrack(family uint8, limit int) ([]Conn, error) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, netlinkNetfilter)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(fd)
	if err := syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return nil, err
	}
	tv := syscall.Timeval{Sec: 5}
	syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)

	// nlmsghdr (16) + nfgenmsg (4)
	req := make([]byte, 20)
	binary.LittleEndian.PutUint32(req[0:4], 20)
	binary.LittleEndian.PutUint16(req[4:6], nfnlSubsysCTNetlink<<8|ipctnlMsgCTGet)
	binary.LittleEndian.PutUint16(req[6:8], syscall.NLM_F_REQUEST|syscall.NLM_F_DUMP)
	binary.LittleEndian.PutUint32(req[8:12], 1)
	req[16] = family
	if err := syscall.Sendto(fd, req, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return nil, err
	}

	var out []Conn
	buf := make([]byte, 1<<16)
	for {
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			return out, err
		}
		msgs, err := syscall.ParseNetlinkMessage(buf[:n])
		if err != nil {
			return out, err
		}
		for _, m := range msgs {
			switch m.Header.Type {
			case syscall.NLMSG_DONE:
				return out, nil
			case syscall.NLMSG_ERROR:
				if len(m.Data) >= 4 {
					if code := int32(binary.LittleEndian.Uint32(m.Data[0:4])); code != 0 {
						return out, syscall.Errno(-code)
					}
				}
				return out, nil
			}
			if c, ok := parseCTMessage(m.Data); ok && len(out) < limit {
				out = append(out, c)
			}
		}
	}
}
