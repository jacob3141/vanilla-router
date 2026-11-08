// SPDX-License-Identifier: AGPL-3.0-or-later

package stickwrite

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"testing"
	"unsafe"

	"vanilla-router/internal/manifest"
	"vanilla-router/internal/stick"
)

// memDev is a disk in memory that checks the rules of FILE_FLAG_NO_BUFFERING.
type memDev struct {
	t      *testing.T
	b      []byte
	sector int
	writes []int64
	flip   int64 // > 0: this byte is corrupted on writing
}

func (d *memDev) check(p []byte, off int64) {
	d.t.Helper()
	if off%int64(d.sector) != 0 || len(p)%d.sector != 0 {
		d.t.Fatalf("not aligned: off %d len %d", off, len(p))
	}
	if uintptr(unsafe.Pointer(&p[0]))%uintptr(d.sector) != 0 {
		d.t.Fatalf("buffer not aligned")
	}
	if off+int64(len(p)) > int64(len(d.b)) {
		d.t.Fatalf("past the end: off %d len %d", off, len(p))
	}
}

func (d *memDev) WriteAt(p []byte, off int64) (int, error) {
	d.check(p, off)
	d.writes = append(d.writes, off)
	copy(d.b[off:], p)
	if d.flip > 0 && d.flip >= off && d.flip < off+int64(len(p)) {
		d.b[d.flip] ^= 0xff
	}
	return len(p), nil
}

func (d *memDev) ReadAt(p []byte, off int64) (int, error) {
	d.check(p, off)
	return copy(p, d.b[off:]), nil
}

func (d *memDev) Size() int64     { return int64(len(d.b)) }
func (d *memDev) SectorSize() int { return d.sector }

const offset = 2 << 20

func testImage(size int) []byte {
	img := make([]byte, size)
	rand.New(rand.NewSource(1)).Read(img)
	copy(img[offset:], bytes.Repeat([]byte{' '}, stick.Size))
	return img
}

func sum(b []byte) string { s := sha256.Sum256(b); return manifest.HexSum(s[:]) }

// build returns the manifest and download file.
func build(t *testing.T, img []byte, compression string) (*manifest.Manifest, []byte) {
	t.Helper()
	file := img
	if compression == manifest.Gzip {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		zw.Write(img)
		zw.Close()
		file = buf.Bytes()
	}
	if compression == manifest.Zip {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("stick.img")
		w.Write(img)
		zw.Close()
		file = buf.Bytes()
	}
	m := &manifest.Manifest{Schema: 1, Name: "vanilla-router-stick", Version: "0.1.0",
		File:      manifest.File{Name: "stick.img", Size: int64(len(file)), SHA256: sum(file), Compression: compression},
		Image:     manifest.Image{Size: int64(len(img)), SHA256: sum(img)},
		StickJSON: manifest.StickJSON{Offset: offset, Size: stick.Size}}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	return m, file
}

func opener(b []byte) Open {
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
}

func stickJSON(t *testing.T) []byte {
	b, err := stick.Info{Schema: 1, Keymap: "de", Timezone: "Europe/Vienna"}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWriteAndVerify(t *testing.T) {
	for _, tc := range []struct {
		comp   string
		size   int
		sector int
	}{
		{manifest.None, 9<<20 + 512*3, 512},
		{manifest.Gzip, 9<<20 + 512*3, 4096},
		{manifest.Gzip, 3 << 20, 512},
		{manifest.Zip, 9<<20 + 512*3, 512},
		{manifest.Zip, 3 << 20, 4096},
	} {
		t.Run(fmt.Sprintf("%s-%d-%d", tc.comp, tc.size, tc.sector), func(t *testing.T) {
			img := testImage(tc.size)
			m, file := build(t, img, tc.comp)
			dev := &memDev{t: t, b: bytes.Repeat([]byte{0xaa}, 16<<20), sector: tc.sector}
			var phases []Phase
			prog := func(p Phase, done, total int64) {
				if done > total {
					t.Fatalf("progress %d > %d", done, total)
				}
				if len(phases) == 0 || phases[len(phases)-1] != p {
					phases = append(phases, p)
				}
			}
			ctx := context.Background()
			if err := Check(ctx, opener(file), m, prog); err != nil {
				t.Fatal(err)
			}
			sj := stickJSON(t)
			if err := Write(ctx, opener(file), m, dev, sj, prog); err != nil {
				t.Fatal(err)
			}
			want := append([]byte(nil), img...)
			copy(want[offset:], sj)
			if !bytes.Equal(dev.b[:len(img)], want) {
				t.Fatal("wrong content")
			}
			// The old GPT backup at the end is erased.
			if !bytes.Equal(dev.b[len(dev.b)-head:], make([]byte, head)) {
				t.Error("end not erased")
			}
			// First erase the start and end, write the partition table last.
			if dev.writes[0] != 0 || dev.writes[1] != int64(len(dev.b)-head) || dev.writes[len(dev.writes)-1] != 0 {
				t.Errorf("order %v", dev.writes)
			}
			for _, w := range dev.writes[2 : len(dev.writes)-1] {
				if w < head {
					t.Errorf("start written too early: %d", w)
				}
			}
			if fmt.Sprint(phases) != fmt.Sprint([]Phase{PhaseCheck, PhaseWrite, PhaseVerify}) {
				t.Errorf("phases %v", phases)
			}
		})
	}
}

func TestDamagedFile(t *testing.T) {
	img := testImage(3 << 20)
	for _, comp := range []string{manifest.None, manifest.Gzip} {
		m, file := build(t, img, comp)
		bad := append([]byte(nil), file...)
		bad[len(bad)/2] ^= 1
		if err := Check(context.Background(), opener(bad), m, nil); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: %v", comp, err)
		}
		if err := Check(context.Background(), opener(file[:len(file)-1]), m, nil); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s shortened: %v", comp, err)
		}
		if err := Check(context.Background(), opener(append(append([]byte(nil), file...), 0)), m, nil); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s lengthened: %v", comp, err)
		}
		// A file changed in the meantime also comes to light on writing.
		dev := &memDev{t: t, b: make([]byte, 8<<20), sector: 512}
		if err := Write(context.Background(), opener(bad), m, dev, stickJSON(t), nil); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s Write: %v", comp, err)
		}
	}
}

func TestReservationMissing(t *testing.T) {
	img := testImage(3 << 20)
	img[offset+100] = 'x'
	m, file := build(t, img, manifest.None)
	if err := Check(context.Background(), opener(file), m, nil); !errors.Is(err, ErrNotBlank) {
		t.Fatal(err)
	}
}

func TestStickTooSmall(t *testing.T) {
	img := testImage(3 << 20)
	m, file := build(t, img, manifest.None)
	dev := &memDev{t: t, b: make([]byte, 3<<20-512), sector: 512}
	if err := Write(context.Background(), opener(file), m, dev, stickJSON(t), nil); !errors.Is(err, ErrTooSmall) {
		t.Fatal(err)
	}
	if len(dev.writes) != 0 {
		t.Error("nothing may be written on a stick that is too small")
	}
}

func TestVerifyDetectsErrors(t *testing.T) {
	img := testImage(3 << 20)
	m, file := build(t, img, manifest.Gzip)
	dev := &memDev{t: t, b: make([]byte, 8<<20), sector: 512, flip: 2<<20 + 77}
	if err := Write(context.Background(), opener(file), m, dev, stickJSON(t), nil); !errors.Is(err, ErrVerify) {
		t.Fatal(err)
	}
}

func TestAbort(t *testing.T) {
	img := testImage(9 << 20)
	m, file := build(t, img, manifest.None)
	ctx, cancel := context.WithCancel(context.Background())
	dev := &memDev{t: t, b: make([]byte, 16<<20), sector: 512}
	err := Write(ctx, opener(file), m, dev, stickJSON(t), func(p Phase, done, total int64) {
		if done > 4<<20 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestFindReservation(t *testing.T) {
	img := testImage(5 << 20)
	if off, err := FindReservation(bytes.NewReader(img)); err != nil || off != offset {
		t.Fatalf("%d %v", off, err)
	}
	// Spaces in the first MiB do not count.
	copy(img[4096:], bytes.Repeat([]byte{' '}, stick.Size))
	if off, err := FindReservation(bytes.NewReader(img)); err != nil || off != offset {
		t.Fatalf("%d %v", off, err)
	}
	copy(img[4<<20:], bytes.Repeat([]byte{' '}, stick.Size))
	if _, err := FindReservation(bytes.NewReader(img)); err == nil {
		t.Error("two places must fail")
	}
	if _, err := FindReservation(bytes.NewReader(make([]byte, 3<<20))); !errors.Is(err, ErrNotBlank) {
		t.Error(err)
	}
}

func TestOverlayAndRegion(t *testing.T) {
	c := []byte("0123456789")
	overlay(c, 10, 8, []byte("abcd")) // data 8..12, block 10..20
	if string(c) != "cd23456789" {
		t.Error(string(c))
	}
	if r := appendRegion(nil, []byte("0123456789"), 10, 18, 4); string(r) != "89" {
		t.Error(string(r))
	}
}
