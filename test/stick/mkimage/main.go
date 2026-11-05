// SPDX-License-Identifier: AGPL-3.0-or-later

// mkimage erzeugt ein kleines, nicht bootfähiges Test-Abbild für den Stick-Ersteller (siehe
// test/stick/test-package.sh): MBR mit einer FAT16-Partition ab 1 MiB, darin vr/stick.json (4096 Leerzeichen,
// Reservierung) und LIESMICH.TXT. Die Partition muss vorher mit mkfs.fat angelegt sein.
//
//	go run ./test/stick/mkimage part.img test.img
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf16"
)

const headBytes = 1 << 20

var le = binary.LittleEndian

func die(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...); os.Exit(1) }

func main() {
	if len(os.Args) != 3 {
		die("Aufruf: main part.img test.img")
	}
	p, err := os.ReadFile(os.Args[1])
	if err != nil {
		die("%v", err)
	}
	bps := int(le.Uint16(p[11:]))
	spc := int(p[13])
	reserved := int(le.Uint16(p[14:]))
	nFAT := int(p[16])
	rootEnt := int(le.Uint16(p[17:]))
	fatSz := int(le.Uint16(p[22:]))
	if bps != 512 || fatSz == 0 || string(p[54:59]) != "FAT16" {
		die("keine FAT16-Partition mit 512-Byte-Sektoren")
	}
	clus := bps * spc
	fatAt := reserved * bps
	rootAt := fatAt + nFAT*fatSz*bps
	dataAt := rootAt + rootEnt*32
	clusAt := func(n int) int { return dataAt + (n-2)*clus }
	setFAT := func(n, v int) {
		for i := 0; i < nFAT; i++ {
			le.PutUint16(p[fatAt+i*fatSz*bps+2*n:], uint16(v))
		}
	}
	next := 2
	alloc := func(size int) int {
		k := (size + clus - 1) / clus
		if k == 0 {
			k = 1
		}
		first := next
		for i := 0; i < k; i++ {
			v := next + 1
			if i == k-1 {
				v = 0xFFFF
			}
			setFAT(next, v)
			next++
		}
		return first
	}
	now := time.Now()
	fdate := uint16((now.Year()-1980)<<9 | int(now.Month())<<5 | now.Day())
	ftime := uint16(now.Hour()<<11 | now.Minute()<<5 | now.Second()/2)
	entry := func(name string, attr, ntres byte, cl, size int) []byte {
		e := make([]byte, 32)
		copy(e, name)
		e[11], e[12] = attr, ntres
		le.PutUint16(e[14:], ftime)
		le.PutUint16(e[16:], fdate)
		le.PutUint16(e[18:], fdate)
		le.PutUint16(e[22:], ftime)
		le.PutUint16(e[24:], fdate)
		le.PutUint16(e[26:], uint16(cl))
		le.PutUint32(e[28:], uint32(size))
		return e
	}
	lfn := func(long, short string) []byte {
		var sum byte
		for i := 0; i < 11; i++ {
			sum = (sum&1)<<7 + sum>>1 + short[i]
		}
		u := utf16.Encode([]rune(long))
		if len(u) > 13 {
			die("Name zu lang")
		}
		if len(u) < 13 {
			u = append(u, 0)
		}
		for len(u) < 13 {
			u = append(u, 0xFFFF)
		}
		e := make([]byte, 32)
		e[0], e[11], e[13] = 0x41, 0x0F, sum
		for i, at := range []int{1, 3, 5, 7, 9, 14, 16, 18, 20, 22, 24, 28, 30} {
			le.PutUint16(e[at:], u[i])
		}
		return e
	}
	rootFree := func() int {
		for i := 0; i < rootEnt; i++ {
			if p[rootAt+32*i] == 0 {
				return rootAt + 32*i
			}
		}
		die("Stammverzeichnis voll")
		return 0
	}
	const lowerName, lowerExt = 0x08, 0x10

	// vr/ mit stick.json
	vr := alloc(clus)
	copy(p[rootFree():], entry("VR         ", 0x10, lowerName, vr, 0))
	d := clusAt(vr)
	copy(p[d:], entry(".          ", 0x10, 0, vr, 0))
	copy(p[d+32:], entry("..         ", 0x10, 0, 0, 0))
	const short = "STICK~1 JSO"
	copy(p[d+64:], lfn("stick.json", short))
	sj := alloc(4096)
	copy(p[d+96:], entry(short, 0x20, 0, sj, 4096))
	copy(p[clusAt(sj):], strings.Repeat(" ", 4096))

	text := strings.ReplaceAll(`Test-Abbild für den Stick-Ersteller von vanilla-router.

Dieser Stick startet nichts, er dient nur zum Prüfen des Schreibvorgangs.
In vr\stick.json stehen nach dem Schreiben Sprache, Tastatur und Zeitzone,
die der Stick-Ersteller aus Windows übernommen hat.

Danach kann der Stick in Windows einfach neu formatiert werden.
`, "\n", "\r\n")
	lm := alloc(len(text))
	copy(p[rootFree():], entry("LIESMICH TXT", 0x20, lowerName|lowerExt, lm, len(text)))
	copy(p[clusAt(lm):], text)

	// MBR mit einer Partition (Typ 0x0E, FAT16 LBA) ab Sektor 2048.
	head := make([]byte, headBytes)
	le.PutUint32(head[0x1B8:], 0x56524D54)
	pe := head[0x1BE:]
	pe[1], pe[2], pe[3] = 0xFE, 0xFF, 0xFF
	pe[4] = 0x0E
	pe[5], pe[6], pe[7] = 0xFE, 0xFF, 0xFF
	le.PutUint32(pe[8:], headBytes/512)
	le.PutUint32(pe[12:], uint32(len(p)/512))
	head[510], head[511] = 0x55, 0xAA

	out := append(head, p...)
	if err := os.WriteFile(os.Args[2], out, 0o644); err != nil {
		die("%v", err)
	}
	fmt.Printf("%s: %d Bytes, Cluster %d Bytes, stick.json bei %d\n", os.Args[2], len(out), clus, headBytes+clusAt(sj))
}
