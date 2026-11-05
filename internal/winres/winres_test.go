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
