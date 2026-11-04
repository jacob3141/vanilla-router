// SPDX-License-Identifier: AGPL-3.0-or-later

package probe

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
)

// PPPoE-Discovery (RFC 2516).
const (
	pppoeVerType = 0x11
	codePADI     = 0x09
	codePADO     = 0x07

	tagEndOfList   = 0x0000
	tagServiceName = 0x0101
	tagACName      = 0x0102
	tagHostUniq    = 0x0103
	tagServiceErr  = 0x0201
	tagACSysErr    = 0x0202
	tagGenericErr  = 0x0203
)

// PADO ist das zerlegte Angebot eines PPPoE-Zugangsservers.
type PADO struct {
	ACMAC   net.HardwareAddr
	VLAN    int
	ACName  string
	Service []string
	Error   string // Fehler-Tag des Servers, falls gesetzt
}

// BuildPADIFrame baut eine PADI-Anfrage (Broadcast), mit VLAN-Tag wenn vlan > 0.
// Host-Uniq ordnet die Antworten unserer Anfrage zu.
func BuildPADIFrame(mac net.HardwareAddr, vlan int, hostUniq []byte) []byte {
	var tags []byte
	tags = appendTag(tags, tagServiceName, nil) // beliebiger Dienst
	tags = appendTag(tags, tagHostUniq, hostUniq)
	p := make([]byte, 6, 6+len(tags))
	p[0] = pppoeVerType
	p[1] = codePADI
	binary.BigEndian.PutUint16(p[4:], uint16(len(tags)))
	p = append(p, tags...)
	return buildFrame(broadcastMAC, mac, vlan, etherTypePPPoEDisc, p)
}

func appendTag(b []byte, typ uint16, v []byte) []byte {
	b = binary.BigEndian.AppendUint16(b, typ)
	b = binary.BigEndian.AppendUint16(b, uint16(len(v)))
	return append(b, v...)
}

var errNotPADO = errors.New("kein passendes PADO")

// ParsePADOFrame prüft, ob ein Rahmen ein PADO an uns mit unserem Host-Uniq ist.
func ParsePADOFrame(frame []byte, mac net.HardwareAddr, hostUniq []byte) (PADO, error) {
	f, err := parseFrame(frame)
	if err != nil || f.EtherType != etherTypePPPoEDisc || !bytes.Equal(f.Dst, mac) {
		return PADO{}, errNotPADO
	}
	p := f.Payload
	if len(p) < 6 || p[0] != pppoeVerType || p[1] != codePADO {
		return PADO{}, errNotPADO
	}
	n := int(binary.BigEndian.Uint16(p[4:6]))
	if len(p) < 6+n {
		return PADO{}, errShort
	}
	out := PADO{ACMAC: append(net.HardwareAddr(nil), f.Src...), VLAN: f.VLAN}
	matched := false
	tags := p[6 : 6+n]
	for len(tags) >= 4 {
		typ := binary.BigEndian.Uint16(tags[0:2])
		l := int(binary.BigEndian.Uint16(tags[2:4]))
		if len(tags) < 4+l {
			return PADO{}, errShort
		}
		v := tags[4 : 4+l]
		tags = tags[4+l:]
		switch typ {
		case tagEndOfList:
			tags = nil
		case tagACName:
			out.ACName = string(v)
		case tagServiceName:
			out.Service = append(out.Service, string(v))
		case tagHostUniq:
			matched = bytes.Equal(v, hostUniq)
		case tagServiceErr, tagACSysErr, tagGenericErr:
			out.Error = string(v)
			if out.Error == "" {
				out.Error = "Fehler ohne Text"
			}
		}
	}
	if !matched {
		return PADO{}, errNotPADO
	}
	return out, nil
}
