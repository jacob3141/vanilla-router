// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"testing"
	"time"
)

func TestMagicPacket(t *testing.T) {
	p, err := magicPacket("00:00:5e:00:53:42")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 102 || !bytes.Equal(p[:6], bytes.Repeat([]byte{0xff}, 6)) || !bytes.Equal(p[96:], []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x42}) {
		t.Errorf("magic packet wrong: %x", p)
	}
}

func TestDeviceStoreBaselineAndNew(t *testing.T) {
	dir := t.TempDir()
	s := NewDeviceStore(dir)
	t0 := time.Date(2026, 11, 4, 2, 0, 0, 0, time.UTC)
	s.Seen([]string{"aa:aa:aa:aa:aa:01"}, t0) // first pass: baseline
	s.Seen([]string{"aa:aa:aa:aa:aa:01", "aa:aa:aa:aa:aa:02"}, t0.Add(2*time.Minute))
	if n := s.NewSince(t0.Add(-time.Hour)); n != 1 {
		t.Errorf("%d new devices, expected 1 (the known one belongs to the baseline)", n)
	}
	s.SetMeta("aa:aa:aa:aa:aa:02", "Printer", "in the basement")
	// reload: details stay, no new baseline
	s2 := NewDeviceStore(dir)
	if m := s2.Get("aa:aa:aa:aa:aa:02"); m.Alias != "Printer" || m.Baseline || !m.FirstSeen.Equal(t0.Add(2*time.Minute)) {
		t.Errorf("not saved: %+v", m)
	}
	s2.Seen([]string{"aa:aa:aa:aa:aa:03"}, t0.Add(time.Hour))
	if m := s2.Get("aa:aa:aa:aa:aa:03"); m.Baseline {
		t.Error("after reloading a new device counts as baseline")
	}
}
