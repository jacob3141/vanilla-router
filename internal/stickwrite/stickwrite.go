// SPDX-License-Identifier: AGPL-3.0-or-later

// Package stickwrite prüft ein Stick-Abbild gegen sein Manifest, schreibt es roh auf einen Datenträger,
// trägt dabei vr/stick.json ein und liest das Ergebnis zur Kontrolle zurück (F-STICK-04, F-STICK-05,
// PLAN.md 3.12). Das Paket ist plattformunabhängig, den Zugriff auf den Stick liefert ein Device.
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

// Device ist der Datenträger. Lesen und Schreiben geschehen immer in ganzen Sektoren an
// Sektorgrenzen, mit Puffern, die auf Align ausgerichtet sind (Windows: FILE_FLAG_NO_BUFFERING).
type Device interface {
	io.WriterAt
	io.ReaderAt
	Size() int64
	SectorSize() int
}

// Syncer schreibt Zwischenspeicher auf den Datenträger (optional).
type Syncer interface{ Sync() error }

// Phase ist der Arbeitsschritt für die Fortschrittsanzeige.
type Phase int

const (
	PhaseCheck  Phase = iota // Abbild prüfen
	PhaseWrite               // auf den Stick schreiben
	PhaseVerify              // zurücklesen und vergleichen
)

// Progress meldet den Fortschritt in Bytes.
type Progress func(p Phase, done, total int64)

// Fehler, die die Oberfläche in Laien-Text übersetzt (F-STICK-06).
var (
	ErrTooSmall = errors.New("der Stick ist zu klein")
	ErrCorrupt  = errors.New("das Abbild ist beschädigt")
	ErrNotBlank = errors.New("im Abbild fehlt die Reservierung für stick.json")
	ErrVerify   = errors.New("die Kontrolle nach dem Schreiben ist fehlgeschlagen")
)

const (
	// Align ist die Ausrichtung der Puffer im Speicher und die größte unterstützte Sektorgröße.
	Align = 4096
	chunk = 4 << 20
	// head ist der Anfang des Datenträgers mit der Partitionstabelle. Er wird zuerst gelöscht und
	// zuletzt geschrieben, damit das Betriebssystem während des Schreibens keine Partitionen findet
	// und einhängt.
	head = 1 << 20
)

// Open öffnet die heruntergeladene Datei (so wie im Manifest unter file beschrieben).
type Open func() (io.ReadCloser, error)

// stream liest die Datei, prüft dabei die Prüfsumme der Datei und liefert das entpackte Abbild.
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
	// Nie mehr lesen als angekündigt, eine längere Datei fällt bei der Größenprüfung auf.
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

// finish liest den Rest der Datei und vergleicht beide Prüfsummen.
func (s *stream) finish(m *manifest.Manifest, imgN int64) error {
	if _, err := io.Copy(io.Discard, s.img); err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if s.gz != nil {
		if err := s.gz.Close(); err != nil {
			return fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
	}
	// Nach dem Ende des gzip-Stroms darf nichts mehr kommen.
	if _, err := io.Copy(io.Discard, s.tee); err != nil {
		return err
	}
	switch {
	case s.fileN.n != m.File.Size:
		return fmt.Errorf("%w: Datei hat %d statt %d Bytes", ErrCorrupt, s.fileN.n, m.File.Size)
	case manifest.HexSum(s.fileSum.Sum(nil)) != m.File.SHA256:
		return fmt.Errorf("%w: Prüfsumme der Datei falsch", ErrCorrupt)
	case imgN != m.Image.Size:
		return fmt.Errorf("%w: Abbild hat %d statt %d Bytes", ErrCorrupt, imgN, m.Image.Size)
	case manifest.HexSum(s.imgSum.Sum(nil)) != m.Image.SHA256:
		return fmt.Errorf("%w: Prüfsumme des Abbilds falsch", ErrCorrupt)
	}
	return nil
}

// Check prüft die Datei vollständig gegen das Manifest, bevor etwas geschrieben wird (F-STICK-04),
// und dass die Stelle für stick.json eine leere Reservierung ist.
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

// appendRegion hängt den Teil von chunk an dst, der in [off, off+size) liegt.
func appendRegion(dst, chunk []byte, pos, off, size int64) []byte {
	lo, hi := max(off, pos), min(off+size, pos+int64(len(chunk)))
	if lo < hi {
		dst = append(dst, chunk[lo-pos:hi-pos]...)
	}
	return dst
}

// overlay ersetzt in chunk den Teil, der in [off, off+len(data)) liegt, durch data.
func overlay(chunk []byte, pos, off int64, data []byte) {
	lo, hi := max(off, pos), min(off+int64(len(data)), pos+int64(len(chunk)))
	if lo < hi {
		copy(chunk[lo-pos:hi-pos], data[lo-off:hi-off])
	}
}

// aligned liefert einen Puffer, dessen Anfang auf Align ausgerichtet ist. Der Speicher von Go wird
// nicht verschoben, die Ausrichtung bleibt also erhalten.
func aligned(n int) []byte {
	b := make([]byte, n+Align)
	off := int(uintptr(unsafe.Pointer(&b[0])) & (Align - 1))
	if off != 0 {
		off = Align - off
	}
	return b[off : off+n : off+n]
}

func roundUp(n, to int) int { return (n + to - 1) / to * to }

// Write schreibt das Abbild auf dev und kontrolliert es. stickJSON (genau stick.Size Bytes) wird an der
// Stelle aus dem Manifest eingetragen. Vorher muss Check erfolgreich gewesen sein, Write prüft die
// Prüfsummen aber ein zweites Mal, damit eine inzwischen veränderte Datei auffällt.
func Write(ctx context.Context, open Open, m *manifest.Manifest, dev Device, stickJSON []byte, prog Progress) error {
	if len(stickJSON) != stick.Size {
		return errors.New("stick.json hat die falsche Größe")
	}
	sector := dev.SectorSize()
	if sector < 512 || sector > Align || sector&(sector-1) != 0 {
		return fmt.Errorf("Sektorgröße %d wird nicht unterstützt", sector)
	}
	devSize := dev.Size() / int64(sector) * int64(sector)
	if devSize < (m.Image.Size+int64(sector)-1)/int64(sector)*int64(sector) || devSize < 2*head {
		return ErrTooSmall
	}

	// Alte Partitionstabellen (vorne und die GPT-Sicherung hinten) löschen.
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
			return fmt.Errorf("%w: Abbild länger als angekündigt", ErrCorrupt)
		}
		if n > 0 {
			data := buf[:n]
			s.imgSum.Write(data)
			overlay(data, pos, m.StickJSON.Offset, stickJSON)
			out.Write(data)
			// Den Anfang (Partitionstabelle) zurückhalten.
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

// FindReservation sucht im Abbild die leere Reservierung für vr/stick.json: eine Folge von mindestens
// stick.Size Leerzeichen ab einer Sektorgrenze hinter dem ersten MiB. Es muss genau eine geben, sonst
// ist die Lage nicht eindeutig (Werkzeug zum Erzeugen des Manifests, T-M4-07).
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
	return 0, fmt.Errorf("Reservierung nicht eindeutig (%d Stellen)", len(found))
}

// verify liest das Abbild vom Datenträger zurück und vergleicht die Prüfsumme.
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
