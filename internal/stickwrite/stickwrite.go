// SPDX-License-Identifier: AGPL-3.0-or-later

// Package stickwrite checks a stick image against its manifest, writes it raw to a disk,
// adds vr/stick.json and reads the result back for checking (F-STICK-04, F-STICK-05,
// PLAN.md 3.12). The package is platform-independent, a Device provides access to the stick.
package stickwrite

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"unsafe"

	"vanilla-router/internal/manifest"
	"vanilla-router/internal/stick"
)

// Device is the disk. Reading and writing always happen in whole sectors at
// sector boundaries, with buffers aligned to Align (Windows: FILE_FLAG_NO_BUFFERING).
type Device interface {
	io.WriterAt
	io.ReaderAt
	Size() int64
	SectorSize() int
}

// Syncer flushes caches to the disk (optional).
type Syncer interface{ Sync() error }

// Phase is the work step for the progress display.
type Phase int

const (
	PhaseCheck  Phase = iota // verify image
	PhaseWrite               // write to the stick
	PhaseVerify              // read back and compare
)

// Progress reports the progress in bytes.
type Progress func(p Phase, done, total int64)

// Errors that the window translates into plain language (F-STICK-06).
var (
	ErrTooSmall = errors.New("the stick is too small")
	ErrCorrupt  = errors.New("the image is damaged")
	ErrNotBlank = errors.New("the reservation for stick.json is missing in the image")
	ErrVerify   = errors.New("the check after writing failed")
)

const (
	// Align is the alignment of the buffers in memory and the largest supported sector size.
	Align = 4096
	chunk = 4 << 20
	// head is the start of the disk with the partition table. It is erased first and
	// written last, so that the operating system finds and mounts no partitions
	// during writing.
	head = 1 << 20
)

// Open opens the downloaded file (as described in the manifest under file).
type Open func() (io.ReadCloser, error)

// stream reads the file, checks the file's checksum on the way and returns the unpacked image.
type stream struct {
	f       io.ReadCloser
	tee     io.Reader
	img     io.Reader
	fileSum hash.Hash
	fileN   *countWriter
	imgSum  hash.Hash
	gz      *gzip.Reader
}

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

func openStream(open Open, m *manifest.Manifest) (*stream, error) {
	f, err := open()
	if err != nil {
		return nil, err
	}
	s := &stream{f: f, fileSum: sha256.New(), fileN: &countWriter{}, imgSum: sha256.New()}
	// Never read more than announced, a longer file comes to light in the size check.
	s.tee = io.TeeReader(io.LimitReader(f, m.File.Size+1), io.MultiWriter(s.fileSum, s.fileN))
	tee := s.tee
	if m.File.Compression == manifest.Gzip {
		gz, err := gzip.NewReader(tee)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		gz.Multistream(false)
		s.gz = gz
		s.img = io.LimitReader(gz, m.Image.Size+1)
	} else {
		s.img = tee
	}
	return s, nil
}

// finish reads the rest of the file and compares both checksums.
func (s *stream) finish(m *manifest.Manifest, imgN int64) error {
	if _, err := io.Copy(io.Discard, s.img); err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if s.gz != nil {
		if err := s.gz.Close(); err != nil {
			return fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
	}
	// Nothing may come after the end of the gzip stream.
	if _, err := io.Copy(io.Discard, s.tee); err != nil {
		return err
	}
	switch {
	case s.fileN.n != m.File.Size:
		return fmt.Errorf("%w: file has %d instead of %d bytes", ErrCorrupt, s.fileN.n, m.File.Size)
	case manifest.HexSum(s.fileSum.Sum(nil)) != m.File.SHA256:
		return fmt.Errorf("%w: file checksum wrong", ErrCorrupt)
	case imgN != m.Image.Size:
		return fmt.Errorf("%w: image has %d instead of %d bytes", ErrCorrupt, imgN, m.Image.Size)
	case manifest.HexSum(s.imgSum.Sum(nil)) != m.Image.SHA256:
		return fmt.Errorf("%w: image checksum wrong", ErrCorrupt)
	}
	return nil
}

// Check verifies the file completely against the manifest before anything is written (F-STICK-04),
// and that the place for stick.json is an empty reservation.
func Check(ctx context.Context, open Open, m *manifest.Manifest, prog Progress) error {
	s, err := openStream(open, m)
	if err != nil {
		return err
	}
	defer s.f.Close()
	buf := make([]byte, chunk)
	reserved := make([]byte, 0, stick.Size)
	var pos int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := io.ReadFull(s.img, buf)
		if n > 0 {
			s.imgSum.Write(buf[:n])
			reserved = appendRegion(reserved, buf[:n], pos, m.StickJSON.Offset, m.StickJSON.Size)
			pos += int64(n)
			if prog != nil {
				prog(PhaseCheck, min(pos, m.Image.Size), m.Image.Size)
			}
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("%w: %v", ErrCorrupt, rerr)
		}
	}
	if err := s.finish(m, pos); err != nil {
		return err
	}
	if !stick.Blank(reserved) {
		return ErrNotBlank
	}
	return nil
}

// appendRegion appends the part of chunk in [off, off+size) to dst.
func appendRegion(dst, chunk []byte, pos, off, size int64) []byte {
	lo, hi := max(off, pos), min(off+size, pos+int64(len(chunk)))
	if lo < hi {
		dst = append(dst, chunk[lo-pos:hi-pos]...)
	}
	return dst
}

// overlay replaces the part of chunk in [off, off+len(data)) with data.
func overlay(chunk []byte, pos, off int64, data []byte) {
	lo, hi := max(off, pos), min(off+int64(len(data)), pos+int64(len(chunk)))
	if lo < hi {
		copy(chunk[lo-pos:hi-pos], data[lo-off:hi-off])
	}
}

// aligned returns a buffer whose start is aligned to Align. Go's memory is not
// moved, so the alignment stays.
func aligned(n int) []byte {
	b := make([]byte, n+Align)
	off := int(uintptr(unsafe.Pointer(&b[0])) & (Align - 1))
	if off != 0 {
		off = Align - off
	}
	return b[off : off+n : off+n]
}

func roundUp(n, to int) int { return (n + to - 1) / to * to }

// Write writes the image to dev and checks it. stickJSON (exactly stick.Size bytes) is added at the
// place given in the manifest. Check must have succeeded beforehand, but Write verifies the
// checksums a second time, so a file changed in the meantime comes to light.
func Write(ctx context.Context, open Open, m *manifest.Manifest, dev Device, stickJSON []byte, prog Progress) error {
	if len(stickJSON) != stick.Size {
		return errors.New("stick.json has the wrong size")
	}
	sector := dev.SectorSize()
	if sector < 512 || sector > Align || sector&(sector-1) != 0 {
		return fmt.Errorf("sector size %d is not supported", sector)
	}
	devSize := dev.Size() / int64(sector) * int64(sector)
	if devSize < (m.Image.Size+int64(sector)-1)/int64(sector)*int64(sector) || devSize < 2*head {
		return ErrTooSmall
	}

	// Erase old partition tables (at the front and the GPT backup at the back).
	zero := aligned(head)
	if _, err := dev.WriteAt(zero, 0); err != nil {
		return err
	}
	if _, err := dev.WriteAt(zero, devSize-head); err != nil {
		return err
	}

	s, err := openStream(open, m)
	if err != nil {
		return err
	}
	defer s.f.Close()
	out := sha256.New()
	buf := aligned(chunk)
	headBuf := aligned(head)
	headLen := 0
	var pos int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := io.ReadFull(s.img, buf)
		if int64(n) > m.Image.Size-pos {
			return fmt.Errorf("%w: image longer than announced", ErrCorrupt)
		}
		if n > 0 {
			data := buf[:n]
			s.imgSum.Write(data)
			overlay(data, pos, m.StickJSON.Offset, stickJSON)
			out.Write(data)
			// Hold back the start (partition table).
			skip := 0
			if pos < head {
				skip = int(min(int64(n), head-pos))
				copy(headBuf[pos:], data[:skip])
				headLen = int(pos) + skip
			}
			if skip < n {
				w := buf[skip:roundUp(n, sector)]
				clear(buf[n : len(w)+skip])
				if _, err := dev.WriteAt(w, pos+int64(skip)); err != nil {
					return err
				}
			}
			pos += int64(n)
			if prog != nil {
				prog(PhaseWrite, pos, m.Image.Size)
			}
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("%w: %v", ErrCorrupt, rerr)
		}
	}
	if err := s.finish(m, pos); err != nil {
		return err
	}
	if headLen > 0 {
		w := headBuf[:roundUp(headLen, sector)]
		clear(w[headLen:])
		if _, err := dev.WriteAt(w, 0); err != nil {
			return err
		}
	}
	if sy, ok := dev.(Syncer); ok {
		if err := sy.Sync(); err != nil {
			return err
		}
	}
	return verify(ctx, dev, m.Image.Size, out.Sum(nil), sector, prog)
}

// FindReservation looks for the empty reservation for vr/stick.json in the image: a run of at least
// stick.Size spaces from a sector boundary behind the first MiB. There must be exactly one, otherwise
// the location is not unique (tool for creating the manifest, T-M4-07).
func FindReservation(r io.Reader) (int64, error) {
	const sec = 512
	buf := make([]byte, chunk)
	full := bytes.Repeat([]byte{' '}, sec)
	var pos, runStart, runLen int64
	var found []int64
	end := func() {
		if runLen >= stick.Size {
			found = append(found, runStart)
		}
		runLen = 0
	}
	for {
		n, err := io.ReadFull(r, buf)
		for i := 0; i+sec <= n; i += sec {
			off := pos + int64(i)
			if off >= head && bytes.Equal(buf[i:i+sec], full) {
				if runLen == 0 {
					runStart = off
				}
				runLen += sec
			} else {
				end()
			}
		}
		pos += int64(n)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	end()
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return 0, ErrNotBlank
	}
	return 0, fmt.Errorf("reservation not unique (%d places)", len(found))
}

// verify reads the image back from the disk and compares the checksum.
func verify(ctx context.Context, dev io.ReaderAt, size int64, want []byte, sector int, prog Progress) error {
	h := sha256.New()
	buf := aligned(chunk)
	for pos := int64(0); pos < size; {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := int(min(int64(chunk), size-pos))
		r := buf[:roundUp(n, sector)]
		if _, err := dev.ReadAt(r, pos); err != nil && !(errors.Is(err, io.EOF) && n == len(r)) {
			return err
		}
		h.Write(r[:n])
		pos += int64(n)
		if prog != nil {
			prog(PhaseVerify, pos, size)
		}
	}
	if !bytes.Equal(h.Sum(nil), want) {
		return ErrVerify
	}
	return nil
}
