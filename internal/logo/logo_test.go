// SPDX-License-Identifier: AGPL-3.0-or-later

package logo

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Die Dateien im Repo müssen zum Code passen (sonst: go run ./internal/logo/mklogo).
func TestDateienAktuell(t *testing.T) {
	for _, f := range Files() {
		got, err := os.ReadFile(filepath.Join("..", "..", f.Path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, f.Data()) {
			t.Errorf("%s ist veraltet: go run ./internal/logo/mklogo", f.Path)
		}
	}
}

func TestSVGWohlgeformt(t *testing.T) {
	d := xml.NewDecoder(bytes.NewReader(SVG()))
	for {
		_, err := d.Token()
		if err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			break
		}
	}
}

func TestRender(t *testing.T) {
	img := Render(64)
	// Ecken durchsichtig, Mitte der Eiskugel cremefarben, Hörnchen unten golden
	if img.NRGBAAt(0, 0).A != 0 || img.NRGBAAt(63, 63).A != 0 {
		t.Error("Ecken nicht durchsichtig")
	}
	at := func(x, y float64) Pt { return tr(Pt{x, y}, 64) }
	q := at(150, 80)
	if c := img.NRGBAAt(int(q.X), int(q.Y)); c != Cream {
		t.Errorf("Eiskugel: %v", c)
	}
	q = at(128, 200)
	if c := img.NRGBAAt(int(q.X), int(q.Y)); c.A != 0xFF || c.R < 0xC0 || c.B > 0x70 {
		t.Errorf("Hörnchen: %v", c)
	}
	if _, err := png.Decode(bytes.NewReader(PNG(32))); err != nil {
		t.Fatal(err)
	}
}

func TestICO(t *testing.T) {
	b := ICO()
	le := binary.LittleEndian
	if le.Uint16(b[2:]) != 1 || int(le.Uint16(b[4:])) != len(IconSizes) {
		t.Fatal("Kopf")
	}
	for i, n := range IconSizes {
		e := b[6+16*i:]
		size, off := le.Uint32(e[8:]), le.Uint32(e[12:])
		img := b[off : off+size]
		if n == 256 {
			if e[0] != 0 || !bytes.HasPrefix(img, []byte("\x89PNG")) {
				t.Errorf("%d: kein PNG", n)
			}
			continue
		}
		if int(e[0]) != n || le.Uint32(img) != 40 || int(le.Uint32(img[8:])) != 2*n {
			t.Errorf("%d: DIB-Kopf", n)
		}
		if want := 40 + 4*n*n + (n+31)/32*4*n; len(img) != want {
			t.Errorf("%d: %d Bytes, erwartet %d", n, len(img), want)
		}
	}
}
