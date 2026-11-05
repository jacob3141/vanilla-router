// SPDX-License-Identifier: AGPL-3.0-or-later

// Package winres erzeugt eine COFF-Objektdatei (.syso) mit einem Windows-Anwendungsmanifest als
// Ressource. Der Go-Linker bindet .syso-Dateien im Paketordner automatisch ein, so braucht der
// Stick-Ersteller kein fremdes Ressourcen-Werkzeug (D-01, PLAN.md 3.12).
//
// Aufbau nach „PE Format“ (Microsoft): ein Abschnitt .rsrc mit dem Ressourcenbaum
// Typ (RT_MANIFEST = 24) → Name (1) → Sprache (0x0409) → Dateneintrag. Der Dateneintrag enthält eine
// RVA, deshalb gibt es eine Relokation (IMAGE_REL_AMD64_ADDR32NB) auf das Abschnittssymbol.
package winres

import (
	"bytes"
	"encoding/binary"
)

const (
	machineAMD64     = 0x8664
	rtManifest       = 24
	langEnUS         = 0x0409
	relAddr32NB      = 3
	scnInitData      = 0x00000040
	scnMemRead       = 0x40000000
	symClassStatic   = 3
	fileHeaderSize   = 20
	sectionHeaderLen = 40
	relocLen         = 10
)

type dirTable struct {
	Characteristics, TimeDateStamp uint32
	Major, Minor                   uint16
	NamedEntries, IDEntries        uint16
}

type dirEntry struct{ NameOrID, Offset uint32 }

type dataEntry struct{ OffsetToData, Size, CodePage, Reserved uint32 }

// Manifest liefert die .syso-Datei für amd64 mit dem Manifest als Ressource 1.
func Manifest(xml []byte) []byte {
	le := binary.LittleEndian
	var rsrc bytes.Buffer
	const highBit = 0x80000000
	// Drei Verzeichnisebenen mit je einem Eintrag: 16 + 8 Bytes je Ebene.
	binary.Write(&rsrc, le, dirTable{IDEntries: 1})
	binary.Write(&rsrc, le, dirEntry{rtManifest, highBit | 24})
	binary.Write(&rsrc, le, dirTable{IDEntries: 1})
	binary.Write(&rsrc, le, dirEntry{1, highBit | 48})
	binary.Write(&rsrc, le, dirTable{IDEntries: 1})
	binary.Write(&rsrc, le, dirEntry{langEnUS, 72})
	const dataEntryAt, dataAt = 72, 88
	binary.Write(&rsrc, le, dataEntry{OffsetToData: dataAt, Size: uint32(len(xml))})
	rsrc.Write(xml)
	for rsrc.Len()%8 != 0 {
		rsrc.WriteByte(0)
	}

	raw := rsrc.Bytes()
	rawAt := uint32(fileHeaderSize + sectionHeaderLen)
	relAt := rawAt + uint32(len(raw))
	symAt := relAt + relocLen

	var out bytes.Buffer
	binary.Write(&out, le, struct {
		Machine, Sections               uint16
		Time, SymbolTable, Symbols      uint32
		OptionalHeader, Characteristics uint16
	}{machineAMD64, 1, 0, symAt, 1, 0, 0})
	var name [8]byte
	copy(name[:], ".rsrc")
	binary.Write(&out, le, struct {
		Name                                      [8]byte
		VirtualSize, VirtualAddress, SizeOfRaw    uint32
		PointerToRaw, PointerToRelocs, PointerToL uint32
		Relocs, Linenumbers                       uint16
		Characteristics                           uint32
	}{name, 0, 0, uint32(len(raw)), rawAt, relAt, 0, 1, 0, scnInitData | scnMemRead})
	out.Write(raw)
	binary.Write(&out, le, struct {
		VirtualAddress, Symbol uint32
		Type                   uint16
	}{dataEntryAt, 0, relAddr32NB})
	binary.Write(&out, le, struct {
		Name          [8]byte
		Value         uint32
		Section       int16
		Type          uint16
		Class, NumAux uint8
	}{name, 0, 1, 0, symClassStatic, 0})
	binary.Write(&out, le, uint32(4)) // leere Stringtabelle
	return out.Bytes()
}
