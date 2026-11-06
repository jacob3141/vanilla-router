// SPDX-License-Identifier: AGPL-3.0-or-later

// Package winres creates a COFF object file (.syso) with Windows resources (application manifest,
// icon). The Go linker automatically includes .syso files in the package folder, so the
// stick creator needs no third-party resource tool (D-01, PLAN.md 3.12).
//
// Layout per "PE Format" (Microsoft): a .rsrc section with the resource tree
// type → name (ID) → language → data entry. Each data entry contains an RVA, so there is one
// relocation (IMAGE_REL_AMD64_ADDR32NB) per entry to the section symbol.
package winres

import (
	"bytes"
	"encoding/binary"
	"sort"
)

const (
	machineAMD64     = 0x8664
	rtIcon           = 3
	rtGroupIcon      = 14
	rtManifest       = 24
	langEnUS         = 0x0409
	relAddr32NB      = 3
	scnInitData      = 0x00000040
	scnMemRead       = 0x40000000
	symClassStatic   = 3
	fileHeaderSize   = 20
	sectionHeaderLen = 40
	relocLen         = 10
	highBit          = 0x80000000
)

// Resource is a resource with a numeric type and numeric ID (language en-US).
type Resource struct {
	Type, ID uint16
	Data     []byte
}

type dirTable struct {
	Characteristics, TimeDateStamp uint32
	Major, Minor                   uint16
	NamedEntries, IDEntries        uint16
}

type dirEntry struct{ NameOrID, Offset uint32 }

type dataEntry struct{ OffsetToData, Size, CodePage, Reserved uint32 }

// Build returns the .syso file for amd64 with the resources.
func Build(res []Resource) []byte {
	le := binary.LittleEndian
	res = append([]Resource(nil), res...)
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].Type != res[j].Type {
			return res[i].Type < res[j].Type
		}
		return res[i].ID < res[j].ID
	})
	var types []uint16
	for i, r := range res {
		if i == 0 || r.Type != res[i-1].Type {
			types = append(types, r.Type)
		}
	}
	// Space of the levels: root, one table per type, one language table per resource, then the
	// data entries and finally the data.
	const tbl, ent = 16, 8
	typeAt := make(map[uint16]int)
	off := tbl + ent*len(types)
	for _, t := range types {
		typeAt[t] = off
		n := 0
		for _, r := range res {
			if r.Type == t {
				n++
			}
		}
		off += tbl + ent*n
	}
	langAt := make([]int, len(res))
	for i := range res {
		langAt[i] = off
		off += tbl + ent
	}
	entryAt := make([]int, len(res))
	for i := range res {
		entryAt[i] = off
		off += 16
	}
	dataAt := make([]int, len(res))
	for i, r := range res {
		off = (off + 7) &^ 7
		dataAt[i] = off
		off += len(r.Data)
	}

	var rsrc bytes.Buffer
	binary.Write(&rsrc, le, dirTable{IDEntries: uint16(len(types))})
	for _, t := range types {
		binary.Write(&rsrc, le, dirEntry{uint32(t), highBit | uint32(typeAt[t])})
	}
	for _, t := range types {
		var idx []int
		for i, r := range res {
			if r.Type == t {
				idx = append(idx, i)
			}
		}
		binary.Write(&rsrc, le, dirTable{IDEntries: uint16(len(idx))})
		for _, i := range idx {
			binary.Write(&rsrc, le, dirEntry{uint32(res[i].ID), highBit | uint32(langAt[i])})
		}
	}
	for i := range res {
		binary.Write(&rsrc, le, dirTable{IDEntries: 1})
		binary.Write(&rsrc, le, dirEntry{langEnUS, uint32(entryAt[i])})
	}
	for i, r := range res {
		binary.Write(&rsrc, le, dataEntry{OffsetToData: uint32(dataAt[i]), Size: uint32(len(r.Data))})
	}
	for i, r := range res {
		for rsrc.Len() < dataAt[i] {
			rsrc.WriteByte(0)
		}
		rsrc.Write(r.Data)
	}
	for rsrc.Len()%8 != 0 {
		rsrc.WriteByte(0)
	}

	raw := rsrc.Bytes()
	rawAt := uint32(fileHeaderSize + sectionHeaderLen)
	relAt := rawAt + uint32(len(raw))
	symAt := relAt + relocLen*uint32(len(res))

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
	}{name, 0, 0, uint32(len(raw)), rawAt, relAt, 0, uint16(len(res)), 0, scnInitData | scnMemRead})
	out.Write(raw)
	for i := range res {
		binary.Write(&out, le, struct {
			VirtualAddress, Symbol uint32
			Type                   uint16
		}{uint32(entryAt[i]), 0, relAddr32NB})
	}
	binary.Write(&out, le, struct {
		Name          [8]byte
		Value         uint32
		Section       int16
		Type          uint16
		Class, NumAux uint8
	}{name, 0, 1, 0, symClassStatic, 0})
	binary.Write(&out, le, uint32(4)) // empty string table
	return out.Bytes()
}

// Manifest returns the .syso file with the manifest as resource 1.
func Manifest(xml []byte) []byte { return Build([]Resource{{rtManifest, 1, xml}}) }

// Icon returns the resources for an icon from an .ico file: one RT_ICON per image (IDs from first)
// and an RT_GROUP_ICON with the ID group. Explorer takes the icon with the smallest ID for the exe.
func Icon(ico []byte, group, first uint16) []Resource {
	le := binary.LittleEndian
	n := int(le.Uint16(ico[4:]))
	var grp bytes.Buffer
	grp.Write(ico[:6])
	var out []Resource
	for i := 0; i < n; i++ {
		e := ico[6+16*i : 6+16*(i+1)]
		size, off := le.Uint32(e[8:]), le.Uint32(e[12:])
		id := first + uint16(i)
		out = append(out, Resource{rtIcon, id, ico[off : off+size]})
		grp.Write(e[:12]) // width … bytes as in the .ico file, instead of the offset the ID
		binary.Write(&grp, le, id)
	}
	return append(out, Resource{rtGroupIcon, group, grp.Bytes()})
}
