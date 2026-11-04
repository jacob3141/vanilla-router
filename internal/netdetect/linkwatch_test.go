// SPDX-License-Identifier: AGPL-3.0-or-later

package netdetect

import (
	"encoding/binary"
	"syscall"
	"testing"
	"time"
)

// buildLinkMessage erzeugt eine RTM_NEWLINK/RTM_DELLINK-Nachricht wie vom Kernel.
// carrier < 0 lässt IFLA_CARRIER weg.
func buildLinkMessage(typ uint16, index int32, flags uint32, name string, carrier int) []byte {
	var attrs []byte
	addAttr := func(t uint16, v []byte) {
		l := 4 + len(v)
		a := make([]byte, (l+3)&^3)
		binary.NativeEndian.PutUint16(a[0:], uint16(l))
		binary.NativeEndian.PutUint16(a[2:], t)
		copy(a[4:], v)
		attrs = append(attrs, a...)
	}
	addAttr(iflaIfname, append([]byte(name), 0))
	if carrier >= 0 {
		addAttr(iflaCarrier, []byte{byte(carrier)})
	}
	addAttr(iflaOperstate, []byte{6})
	info := make([]byte, syscall.SizeofIfInfomsg)
	info[0] = syscall.AF_UNSPEC
	binary.NativeEndian.PutUint16(info[2:], 1) // ARPHRD_ETHER
	binary.NativeEndian.PutUint32(info[4:], uint32(index))
	binary.NativeEndian.PutUint32(info[8:], flags)
	body := append(info, attrs...)
	msg := make([]byte, syscall.SizeofNlMsghdr+len(body))
	binary.NativeEndian.PutUint32(msg[0:], uint32(len(msg)))
	binary.NativeEndian.PutUint16(msg[4:], typ)
	copy(msg[syscall.SizeofNlMsghdr:], body)
	return msg
}

func TestParseLinkMessages(t *testing.T) {
	var buf []byte
	buf = append(buf, buildLinkMessage(syscall.RTM_NEWLINK, 3, iffUp|iffLowerUp, "enp2s0", 1)...)
	buf = append(buf, buildLinkMessage(syscall.RTM_NEWLINK, 4, iffUp, "eno1", -1)...)
	buf = append(buf, buildLinkMessage(syscall.RTM_NEWADDR, 4, 0, "x", -1)...) // fremder Typ
	buf = append(buf, buildLinkMessage(syscall.RTM_DELLINK, 5, iffUp|iffLowerUp, "enx2", 1)...)
	buf = append(buf, buildLinkMessage(syscall.RTM_NEWLINK, 6, iffUp|iffLowerUp, "enx3", 0)...)

	evs, err := ParseLinkMessages(buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 4 {
		t.Fatalf("%d Ereignisse, erwartet 4: %+v", len(evs), evs)
	}
	e := evs[0]
	if e.Index != 3 || e.Name != "enp2s0" || !e.Up || !e.LowerUp || e.Carrier != Yes || e.OperState != "up" || !e.HasLink() {
		t.Errorf("Ereignis 1: %+v", e)
	}
	if evs[1].Carrier != Unknown || evs[1].HasLink() {
		t.Errorf("Ereignis 2 ohne IFLA_CARRIER und ohne LOWER_UP: %+v", evs[1])
	}
	if !evs[2].Removed || evs[2].HasLink() {
		t.Errorf("Ereignis 3 (DELLINK): %+v", evs[2])
	}
	if evs[3].HasLink() {
		t.Errorf("Ereignis 4: IFLA_CARRIER=0 muss Vorrang vor LOWER_UP haben: %+v", evs[3])
	}
}

func TestParseLinkMessagesKaputt(t *testing.T) {
	msg := buildLinkMessage(syscall.RTM_NEWLINK, 3, 0, "x", 1)
	if _, err := ParseLinkMessages(msg[:syscall.SizeofNlMsghdr+4]); err == nil {
		t.Error("abgeschnittene Nachricht ohne Fehler angenommen")
	}
}

// TestLinkWatcherLive öffnet den echten Socket. Ereignisse werden nicht erwartet, nur dass
// Öffnen, Warten mit Zeitlimit und Schließen ohne Rechte funktionieren.
func TestLinkWatcherLive(t *testing.T) {
	w, err := NewLinkWatcher()
	if err != nil {
		t.Skipf("kein Netlink verfügbar: %v", err)
	}
	defer w.Close()
	start := time.Now()
	if _, err := w.Read(150 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Zeitlimit nicht eingehalten: %v", d)
	}
}
