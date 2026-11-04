// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"encoding/binary"
	"testing"
)

// attr baut ein Netlink-Attribut (Typ, Inhalt), auf 4 Byte aufgefüllt.
func attr(typ uint16, data []byte, nested bool) []byte {
	l := 4 + len(data)
	b := make([]byte, (l+3)&^3)
	binary.LittleEndian.PutUint16(b[0:2], uint16(l))
	if nested {
		typ |= nlaFNested
	}
	binary.LittleEndian.PutUint16(b[2:4], typ)
	copy(b[4:], data)
	return b
}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func be16(v uint16) []byte { b := make([]byte, 2); binary.BigEndian.PutUint16(b, v); return b }
func be32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }

func tupleAttr(typ uint16, src, dst []byte, proto uint8, sport, dport uint16) []byte {
	return attr(typ, cat(
		attr(ctaTupleIP, cat(attr(ctaIPv4Src, src, false), attr(ctaIPv4Dst, dst, false)), true),
		attr(ctaTupleProto, cat(attr(ctaProtoNum, []byte{proto}, false), attr(ctaProtoSrc, be16(sport), false), attr(ctaProtoDst, be16(dport), false)), true),
	), true)
}

func TestParseCTMessage(t *testing.T) {
	// Portweiterleitung: von außen auf 198.51.100.178:8080, Antwort von 192.168.42.20:80
	msg := cat([]byte{2, 0, 0, 0}, // nfgenmsg: AF_INET
		tupleAttr(ctaTupleOrig, []byte{203, 0, 113, 5}, []byte{198, 51, 100, 178}, 6, 51000, 8080),
		tupleAttr(ctaTupleReply, []byte{192, 168, 42, 20}, []byte{203, 0, 113, 5}, 6, 80, 51000),
		attr(ctaStatus, be32(ipsDstNAT|ipsAssured), false),
		attr(ctaTimeout, be32(431990), false),
		attr(ctaProtoinfo, attr(ctaProtoinfoTCP, attr(ctaProtoinfoTCPState, []byte{3}, false), true), true),
	)
	c, ok := parseCTMessage(msg)
	if !ok {
		t.Fatal("nicht gelesen")
	}
	want := Conn{Proto: "tcp", Src: "203.0.113.5", Dst: "198.51.100.178", SrcPort: 51000, DstPort: 8080,
		ReplySrc: "192.168.42.20", State: "ESTABLISHED", Timeout: 431990, DNAT: true, Assured: true}
	if c != want {
		t.Errorf("\n got %+v\nwant %+v", c, want)
	}
	if _, ok := parseCTMessage([]byte{2, 0}); ok {
		t.Error("kaputte Nachricht akzeptiert")
	}
	// abgeschnittenes Attribut darf nicht abstürzen
	parseCTMessage(cat([]byte{2, 0, 0, 0}, []byte{0xff, 0x00, 0x01, 0x80, 1, 2}))
}
