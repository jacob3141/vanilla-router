// SPDX-License-Identifier: AGPL-3.0-or-later

package probe

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

var (
	ourMAC   = net.HardwareAddr{0x02, 0, 0, 0, 0, 0x01}
	modemMAC = net.HardwareAddr{0x02, 0, 0, 0, 0, 0xfe}
	acMAC    = net.HardwareAddr{0x02, 0, 0, 0, 0, 0xac}
)

// --- Gegenstelle für Tests: baut Antworten wie ein Modem bzw. ein PPPoE-Server ---

func buildOfferFrame(t *testing.T, discover []byte, vlan int) []byte {
	t.Helper()
	f, err := parseFrame(discover)
	if err != nil {
		t.Fatal(err)
	}
	_, _, req, err := parseIPv4UDP(f.Payload)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 240)
	b[0], b[1], b[2] = bootReply, 1, 6
	copy(b[4:8], req[4:8])     // xid
	copy(b[28:34], req[28:34]) // chaddr
	copy(b[16:20], net.IPv4(192, 0, 2, 10).To4())
	copy(b[236:240], dhcpMagic)
	b = append(b, optPad, optMsgType, 1, dhcpOffer)
	b = append(b, optSubnet, 4, 255, 255, 255, 0)
	b = append(b, optRouter, 4, 192, 0, 2, 1)
	b = append(b, optDNS, 8, 192, 0, 2, 1, 198, 51, 100, 53)
	b = append(b, optServerID, 4, 192, 0, 2, 1)
	b = append(b, optLease, 4, 0, 0, 0x0e, 0x10) // 3600 s
	b = append(b, optEnd)
	ip := buildIPv4UDP(net.IPv4(192, 0, 2, 1), net.IPv4bcast, dhcpServerPort, dhcpClientPort, 7, b)
	return buildFrame(ourMAC, modemMAC, vlan, etherTypeIPv4, ip)
}

func buildPADOFrame(t *testing.T, padi []byte, acName string, errTag bool) []byte {
	t.Helper()
	f, err := parseFrame(padi)
	if err != nil {
		t.Fatal(err)
	}
	var hostUniq []byte
	tags := f.Payload[6:]
	for len(tags) >= 4 {
		typ := binary.BigEndian.Uint16(tags)
		l := int(binary.BigEndian.Uint16(tags[2:]))
		if typ == tagHostUniq {
			hostUniq = tags[4 : 4+l]
		}
		tags = tags[4+l:]
	}
	var out []byte
	out = appendTag(out, tagACName, []byte(acName))
	out = appendTag(out, tagServiceName, nil)
	if errTag {
		out = appendTag(out, tagGenericErr, nil)
	}
	out = appendTag(out, tagHostUniq, hostUniq)
	p := []byte{pppoeVerType, codePADO, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(p[4:], uint16(len(out)))
	return buildFrame(f.Src, acMAC, f.VLAN, etherTypePPPoEDisc, append(p, out...))
}

// --- Pakete ---

func TestChecksumGueltig(t *testing.T) {
	fr := BuildDiscoverFrame(ourMAC, 0x01020304, 99)
	f, _ := parseFrame(fr)
	if checksum(f.Payload[:20]) != 0 {
		t.Error("IPv4-Kopf-Prüfsumme ungültig")
	}
	udpLen := int(binary.BigEndian.Uint16(f.Payload[24:26]))
	pseudo := append([]byte{0, 0, 0, 0, 255, 255, 255, 255, 0, 17}, f.Payload[24:26]...)
	pseudo = append(pseudo, f.Payload[20:20+udpLen]...)
	if checksum(pseudo) != 0 {
		t.Error("UDP-Prüfsumme ungültig")
	}
}

func TestDiscoverAufbau(t *testing.T) {
	fr := BuildDiscoverFrame(ourMAC, 0xdeadbeef, 1)
	f, err := parseFrame(fr)
	if err != nil || !bytes.Equal(f.Dst, broadcastMAC) || !bytes.Equal(f.Src, ourMAC) || f.EtherType != etherTypeIPv4 || f.VLAN != 0 {
		t.Fatalf("Rahmen: %+v %v", f, err)
	}
	sp, dp, b, err := parseIPv4UDP(f.Payload)
	if err != nil || sp != 68 || dp != 67 {
		t.Fatalf("UDP %d→%d %v", sp, dp, err)
	}
	if len(b) < 300 || b[0] != bootRequest || binary.BigEndian.Uint32(b[4:]) != 0xdeadbeef ||
		binary.BigEndian.Uint16(b[10:]) != 0x8000 || !bytes.Equal(b[28:34], ourMAC) {
		t.Errorf("BOOTP-Kopf falsch: % x", b[:40])
	}
	if !bytes.Equal(b[236:240], dhcpMagic) || b[240] != optMsgType || b[242] != dhcpDiscover {
		t.Errorf("Optionen falsch: % x", b[236:250])
	}
}

func TestOfferZerlegen(t *testing.T) {
	disc := BuildDiscoverFrame(ourMAC, 42, 1)
	off, err := ParseOfferFrame(buildOfferFrame(t, disc, 0), ourMAC, 42)
	if err != nil {
		t.Fatal(err)
	}
	if off.String() != "192.0.2.10/24 über 192.0.2.1" || len(off.DNS) != 2 || off.Lease != time.Hour ||
		!off.ServerID.Equal(net.IPv4(192, 0, 2, 1)) || !bytes.Equal(off.ServerMAC, modemMAC) {
		t.Errorf("Offer: %+v (%s)", off, off)
	}
	if _, err := ParseOfferFrame(buildOfferFrame(t, disc, 0), ourMAC, 43); err == nil {
		t.Error("fremde xid angenommen")
	}
	if _, err := ParseOfferFrame(buildOfferFrame(t, disc, 0), modemMAC, 42); err == nil {
		t.Error("fremde MAC angenommen")
	}
	if _, err := ParseOfferFrame(buildOfferFrame(t, disc, 7), ourMAC, 42); err == nil {
		t.Error("Offer auf VLAN angenommen")
	}
}

func TestPADIAufbau(t *testing.T) {
	hu := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	f, err := parseFrame(BuildPADIFrame(ourMAC, 7, hu))
	if err != nil || f.VLAN != 7 || f.EtherType != etherTypePPPoEDisc || !bytes.Equal(f.Dst, broadcastMAC) {
		t.Fatalf("%+v %v", f, err)
	}
	if f.Payload[0] != 0x11 || f.Payload[1] != codePADI || binary.BigEndian.Uint16(f.Payload[2:]) != 0 {
		t.Errorf("PPPoE-Kopf: % x", f.Payload[:6])
	}
	if l := binary.BigEndian.Uint16(f.Payload[4:]); l != 4+4+8 {
		t.Errorf("Länge der Tags %d", l)
	}
}

func TestPADOZerlegen(t *testing.T) {
	hu := []byte{9, 9, 9, 9, 1, 2, 3, 4}
	padi := BuildPADIFrame(ourMAC, 7, hu)
	p, err := ParsePADOFrame(buildPADOFrame(t, padi, "AC-TEST-1", false), ourMAC, hu)
	if err != nil || p.ACName != "AC-TEST-1" || p.VLAN != 7 || !bytes.Equal(p.ACMAC, acMAC) || p.Error != "" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := ParsePADOFrame(buildPADOFrame(t, padi, "x", false), ourMAC, []byte{0}); err == nil {
		t.Error("fremdes Host-Uniq angenommen")
	}
	p, err = ParsePADOFrame(buildPADOFrame(t, padi, "x", true), ourMAC, hu)
	if err != nil || p.Error == "" {
		t.Errorf("Fehler-Tag nicht erkannt: %+v %v", p, err)
	}
}

func TestVLANWiederEinfuegen(t *testing.T) {
	hu := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	tagged := buildPADOFrame(t, BuildPADIFrame(ourMAC, 7, hu), "AC", false)
	// so liefert ein Treiber mit VLAN-Offload den Rahmen: Tag entfernt, steht in PACKET_AUXDATA
	stripped := append(append([]byte(nil), tagged[:12]...), tagged[16:]...)
	p, err := ParsePADOFrame(insertVLAN(stripped, 7), ourMAC, hu)
	if err != nil || p.VLAN != 7 {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestVlanFromAux(t *testing.T) {
	data := make([]byte, 20)
	binary.NativeEndian.PutUint32(data[0:], tpStatusVLANValid)
	binary.NativeEndian.PutUint16(data[16:], 7)
	oob := make([]byte, 0, 64)
	hdr := make([]byte, 16) // cmsghdr: len (8), level (4), type (4) auf amd64/arm64
	binary.NativeEndian.PutUint64(hdr[0:], uint64(16+len(data)))
	binary.NativeEndian.PutUint32(hdr[8:], solPacket)
	binary.NativeEndian.PutUint32(hdr[12:], packetAuxdata)
	oob = append(append(oob, hdr...), data...)
	for len(oob)%8 != 0 {
		oob = append(oob, 0)
	}
	if tci, ok := vlanFromAux(oob); !ok || tci != 7 {
		t.Errorf("tci=%d ok=%v", tci, ok)
	}
	binary.NativeEndian.PutUint32(oob[16:], 0)
	if _, ok := vlanFromAux(oob); ok {
		t.Error("VLAN ohne Gültig-Bit angenommen")
	}
}

// Kein Absturz bei abgeschnittenen oder zufälligen Daten.
func TestRobustGegenMuell(t *testing.T) {
	hu := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	samples := [][]byte{
		buildOfferFrame(t, BuildDiscoverFrame(ourMAC, 1, 1), 0),
		buildPADOFrame(t, BuildPADIFrame(ourMAC, 0, hu), "AC", false),
	}
	for _, s := range samples {
		for i := 0; i <= len(s); i++ {
			ParseOfferFrame(s[:i], ourMAC, 1)
			ParsePADOFrame(s[:i], ourMAC, hu)
		}
		for i := 14; i < len(s); i++ { // Längenfelder verfälschen
			m := append([]byte(nil), s...)
			m[i] = 0xff
			ParseOfferFrame(m, ourMAC, 1)
			ParsePADOFrame(m, ourMAC, hu)
		}
	}
}

// --- Ablauf mit nachgebautem Socket und künstlicher Uhr ---

type fakeConn struct {
	t       *testing.T
	clock   time.Time
	sent    [][]byte
	queue   [][]byte
	respond func(sent []byte) [][]byte
	delay   int // Antworten erst ab dem n-ten gesendeten Rahmen
}

func (c *fakeConn) now() time.Time { return c.clock }
func (c *fakeConn) Send(b []byte) error {
	c.sent = append(c.sent, b)
	if len(c.sent) > c.delay && c.respond != nil {
		c.queue = append(c.queue, c.respond(b)...)
	}
	return nil
}
func (c *fakeConn) Recv(d time.Duration) ([]byte, error) {
	if len(c.queue) > 0 {
		f := c.queue[0]
		c.queue = c.queue[1:]
		c.clock = c.clock.Add(10 * time.Millisecond)
		return f, nil
	}
	c.clock = c.clock.Add(d)
	return nil, nil
}
func (c *fakeConn) Close() error { return nil }

func TestRunDHCP(t *testing.T) {
	c := &fakeConn{t: t, clock: time.Unix(0, 0), delay: 3}
	c.respond = func(b []byte) [][]byte {
		f, _ := parseFrame(b)
		if f.EtherType == etherTypeIPv4 {
			return [][]byte{BuildPADIFrame(modemMAC, 0, nil), buildOfferFrame(t, b, 0)} // Rauschen + Antwort
		}
		return nil
	}
	o := Options{}.withDefaults()
	res, err := run(context.Background(), c, ourMAC, o, c.now)
	if err != nil || res.DHCP == nil || len(res.PPPoE) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if el := c.clock.Sub(time.Unix(0, 0)); el > 2*time.Second {
		t.Errorf("nach der Antwort nicht zügig beendet: %v", el)
	}
	// erste Runde: Discover + 2 PADI, zweite Runde: Discover beantwortet
	if len(c.sent) != 6 {
		t.Errorf("%d Rahmen gesendet, erwartet 6", len(c.sent))
	}
}

func TestRunPPPoEVLAN7(t *testing.T) {
	c := &fakeConn{t: t, clock: time.Unix(0, 0)}
	c.respond = func(b []byte) [][]byte {
		f, _ := parseFrame(b)
		if f.EtherType == etherTypePPPoEDisc && f.VLAN == 7 {
			return [][]byte{buildPADOFrame(t, b, "AC-7", false)}
		}
		return nil
	}
	res, err := run(context.Background(), c, ourMAC, Options{}.withDefaults(), c.now)
	if err != nil || res.DHCP != nil || len(res.PPPoE) != 1 || res.PPPoE[0].VLAN != 7 || res.PPPoE[0].ACName != "AC-7" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestRunOhneAntwort(t *testing.T) {
	c := &fakeConn{t: t, clock: time.Unix(0, 0)}
	o := Options{Timeout: 3 * time.Second}.withDefaults()
	res, err := run(context.Background(), c, ourMAC, o, c.now)
	if err != nil || res.Answered() {
		t.Fatalf("%+v %v", res, err)
	}
	if el := c.clock.Sub(time.Unix(0, 0)); el != 3*time.Second {
		t.Errorf("Dauer %v, erwartet 3 s", el)
	}
	if len(c.sent) != 9 { // 3 Runden à Discover + 2 PADI
		t.Errorf("%d Rahmen gesendet, erwartet 9", len(c.sent))
	}
}

func TestRunAbbruch(t *testing.T) {
	c := &fakeConn{t: t, clock: time.Unix(0, 0)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := run(ctx, c, ourMAC, Options{}.withDefaults(), c.now); err == nil {
		t.Error("Abbruch nicht gemeldet")
	}
}
