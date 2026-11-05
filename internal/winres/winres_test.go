// SPDX-License-Identifier: AGPL-3.0-or-later

package winres

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"testing"
)

func TestManifestObjekt(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><assembly/>`)
	f, err := pe.NewFile(bytes.NewReader(Manifest(xml)))
	if err != nil {
		t.Fatal(err)
	}
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || len(f.Sections) != 1 {
		t.Fatalf("%+v", f.FileHeader)
	}
	s := f.Sections[0]
	if s.Name != ".rsrc" || len(s.Relocs) != 1 || s.Relocs[0].VirtualAddress != 72 || s.Relocs[0].Type != relAddr32NB {
		t.Fatalf("%+v %+v", s.SectionHeader, s.Relocs)
	}
	data, err := s.Data()
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	// Typ → Name → Sprache → Dateneintrag
	if le.Uint16(data[14:]) != 1 || le.Uint32(data[16:]) != rtManifest || le.Uint32(data[20:]) != 0x80000000|24 {
		t.Error("Typ-Ebene")
	}
	if le.Uint32(data[64:]) != langEnUS || le.Uint32(data[68:]) != 72 {
		t.Error("Sprach-Ebene")
	}
	if le.Uint32(data[72:]) != 88 || le.Uint32(data[76:]) != uint32(len(xml)) {
		t.Error("Dateneintrag")
	}
	if !bytes.Equal(data[88:88+len(xml)], xml) || len(data)%8 != 0 {
		t.Error("Daten")
	}
	if len(f.Symbols) != 1 || f.Symbols[0].Name != ".rsrc" || f.Symbols[0].SectionNumber != 1 {
		t.Errorf("%+v", f.Symbols)
	}
}

// fakeICO baut eine .ico-Datei mit zwei Bildern (Inhalt beliebig).
func fakeICO() []byte {
	le := binary.LittleEndian
	var b bytes.Buffer
	binary.Write(&b, le, [3]uint16{0, 1, 2})
	imgs := [][]byte{[]byte("klein"), []byte("groesseres Bild")}
	off := 6 + 16*len(imgs)
	for i, img := range imgs {
		b.Write([]byte{byte(16 * (i + 1)), byte(16 * (i + 1)), 0, 0})
		binary.Write(&b, le, [2]uint16{1, 32})
		binary.Write(&b, le, [2]uint32{uint32(len(img)), uint32(off)})
		off += len(img)
	}
	for _, img := range imgs {
		b.Write(img)
	}
	return b.Bytes()
}

// TestIconUndManifest geht den Ressourcenbaum durch, wie Windows ihn liest.
func TestIconUndManifest(t *testing.T) {
	xml := []byte(`<assembly/>`)
	res := append(Icon(fakeICO(), 1, 1), Resource{rtManifest, 1, xml})
	f, err := pe.NewFile(bytes.NewReader(Build(res)))
	if err != nil {
		t.Fatal(err)
	}
	s := f.Sections[0]
	data, _ := s.Data()
	if len(s.Relocs) != len(res) {
		t.Fatalf("%d Relokationen", len(s.Relocs))
	}
	le := binary.LittleEndian
	type found struct {
		typ, id uint32
		data    []byte
	}
	var got []found
	entries := func(at uint32) [][2]uint32 {
		n := uint32(le.Uint16(data[at+12:])) + uint32(le.Uint16(data[at+14:]))
		var out [][2]uint32
		for i := uint32(0); i < n; i++ {
			e := at + 16 + 8*i
			out = append(out, [2]uint32{le.Uint32(data[e:]), le.Uint32(data[e+4:])})
		}
		return out
	}
	relocs := map[uint32]bool{}
	for _, r := range s.Relocs {
		relocs[r.VirtualAddress] = true
	}
	for _, te := range entries(0) {
		for _, ie := range entries(te[1] &^ highBit) {
			le2 := entries(ie[1] &^ highBit)
			if len(le2) != 1 || le2[0][0] != langEnUS || le2[0][1]&highBit != 0 {
				t.Fatal("Sprach-Ebene")
			}
			d := le2[0][1]
			if !relocs[d] {
				t.Errorf("keine Relokation für Dateneintrag bei %d", d)
			}
			off, size := le.Uint32(data[d:]), le.Uint32(data[d+4:])
			if off%8 != 0 {
				t.Errorf("Daten bei %d nicht ausgerichtet", off)
			}
			got = append(got, found{te[0], ie[0], data[off : off+size]})
		}
	}
	if len(got) != 4 || got[0].typ != rtIcon || string(got[1].data) != "groesseres Bild" ||
		got[2].typ != rtGroupIcon || got[3].typ != rtManifest || !bytes.Equal(got[3].data, xml) {
		t.Fatalf("%+v", got)
	}
	grp := got[2].data
	if le.Uint16(grp[4:]) != 2 || len(grp) != 6+14*2 || le.Uint16(grp[6+12:]) != 1 || le.Uint16(grp[6+14+12:]) != 2 ||
		le.Uint32(grp[6+14+8:]) != uint32(len("groesseres Bild")) || grp[6+14] != 32 {
		t.Errorf("Gruppe: % x", grp)
	}
}
