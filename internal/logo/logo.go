// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logo beschreibt das Projektlogo (eine Kugel Vanilleeis im Hörnchen, flach in zwei Farben)
// als Geometrie und erzeugt daraus SVG, PNG und das Windows-Icon. So gibt
// es nur eine Quelle und keine fremden Grafikwerkzeuge (D-01). Die Dateien unter docs/ und das
// Favicon der Oberfläche werden mit „go run ./internal/logo/mklogo“ erzeugt, ein Test prüft, dass sie
// zum Code passen.
package logo

import (
	"fmt"
	"image/color"
	"strings"
)

// Die zwei Farben des Logos.
var (
	Vanilla = color.NRGBA{0xF0, 0xC8, 0x6A, 0xFF} // Eiskugel, Waffelmuster
	Brown   = color.NRGBA{0x8B, 0x5A, 0x2B, 0xFF} // Hörnchen; auf Weiß 5,8:1, taugt auch für Text
)

// Abgeleitete Farben für Oberflächen im Stil des Logos (Stick-Ersteller).
var (
	Dark  = color.NRGBA{0x4A, 0x30, 0x18, 0xFF} // Text
	Cream = color.NRGBA{0xFD, 0xF3, 0xDC, 0xFF} // helle Flächen
)

// Pt ist ein Punkt im Entwurfsraum.
type Pt struct{ X, Y float64 }

// Seg ist ein Pfadbefehl: 'M', 'L', 'C' (drei Punkte) oder 'Z'.
type Seg struct {
	Op  byte
	Pts []Pt
}

// Path ist ein Pfad aus Segmenten.
type Path []Seg

// Layer ist eine Ebene: Füllung und/oder Kontur, wahlweise beschnitten auf die Fläche von Clip.
type Layer struct {
	Path   Path
	Fill   color.NRGBA // A == 0: keine Füllung
	Stroke color.NRGBA // A == 0: keine Kontur
	Width  float64
	Clip   Path
	ClipID string
}

// Size ist die Kantenlänge der Zeichenfläche. Der Entwurf wird mit Scale und Offset hineingesetzt.
const Size = 256

const (
	scale = 1.08
	offX  = 128 - 128*scale // Entwurf x 64…192 mittig
	offY  = 128 - 139*scale // Entwurf y 34…244 mittig
)

func p(x, y float64) Pt { return Pt{x, y} }

// ParsePath liest die Kurzschreibweise „M x y L x y C x y x y x y Z“ (nur absolute Befehle).
func ParsePath(s string) Path {
	f := strings.Fields(strings.ReplaceAll(s, ",", " "))
	var out Path
	num := func(i int) float64 {
		var v float64
		if _, err := fmt.Sscan(f[i], &v); err != nil {
			panic("logo: " + f[i])
		}
		return v
	}
	for i := 0; i < len(f); {
		op := f[i][0]
		i++
		n := map[byte]int{'M': 1, 'L': 1, 'C': 3, 'Z': 0}[op]
		s := Seg{Op: op}
		for k := 0; k < n; k++ {
			s.Pts = append(s.Pts, p(num(i), num(i+1)))
			i += 2
		}
		out = append(out, s)
	}
	return out
}

// Layers liefert das Logo von hinten nach vorn: Hörnchen mit Waffelmuster, darüber die Kugel.
// Nur die zwei Farben Vanilla und Brown.
func Layers() []Layer {
	cone := ParsePath("M 80 124 L 176 124 L 132 236 C 130 241 126 241 124 236 Z")
	var hatch Path
	for i := -3; i <= 4; i++ {
		x := 64 + float64(i)*32
		hatch = append(hatch, Seg{'M', []Pt{p(x, 120)}}, Seg{'L', []Pt{p(x+64, 244)}})
		hatch = append(hatch, Seg{'M', []Pt{p(x+64, 120)}}, Seg{'L', []Pt{p(x, 244)}})
	}
	scoop := ParsePath(`M 64 116 C 58 66 94 34 128 34 C 162 34 198 66 192 116
		C 192 134 166 138 160 126 C 154 140 134 140 128 126 C 122 140 102 140 96 126
		C 90 138 64 134 64 116 Z`)
	return []Layer{
		{Path: cone, Fill: Brown},
		{Path: hatch, Stroke: Vanilla, Width: 6, Clip: cone, ClipID: "cone"},
		{Path: scoop, Fill: Vanilla},
	}
}

// tr setzt einen Entwurfspunkt auf die Zeichenfläche der Größe px.
func tr(q Pt, px float64) Pt {
	f := px / Size
	return p((q.X*scale+offX)*f, (q.Y*scale+offY)*f)
}

func hexColor(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

func (pa Path) svg() string {
	var b strings.Builder
	num := func(v float64) string {
		s := fmt.Sprintf("%.2f", v)
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		if s == "-0" {
			s = "0"
		}
		return s
	}
	for i, s := range pa {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte(s.Op)
		for _, q := range s.Pts {
			b.WriteString(" " + num(q.X) + " " + num(q.Y))
		}
	}
	return b.String()
}

// SVG liefert das Logo als SVG (256 × 256, transparenter Hintergrund).
func SVG() []byte {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256">` + "\n")
	b.WriteString("<!-- vanilla-router, SPDX-License-Identifier: AGPL-3.0-or-later, erzeugt von internal/logo -->\n")
	ls := Layers()
	for _, l := range ls {
		if l.ClipID != "" {
			fmt.Fprintf(&b, `<defs><clipPath id="%s"><path d="%s"/></clipPath></defs>`+"\n", l.ClipID, l.Clip.svg())
		}
	}
	fmt.Fprintf(&b, `<g transform="translate(%.2f %.2f) scale(%.2f)" stroke-linecap="round" stroke-linejoin="round">`+"\n", offX, offY, scale)
	for _, l := range ls {
		attr := `fill="none"`
		if l.Fill.A != 0 {
			attr = `fill="` + hexColor(l.Fill) + `"`
		}
		if l.Stroke.A != 0 {
			attr += fmt.Sprintf(` stroke="%s" stroke-width="%g"`, hexColor(l.Stroke), l.Width)
			if l.Stroke.A != 0xFF {
				attr += fmt.Sprintf(` stroke-opacity="%.2f"`, float64(l.Stroke.A)/255)
			}
		}
		if l.ClipID != "" {
			attr += ` clip-path="url(#` + l.ClipID + `)"`
		}
		fmt.Fprintf(&b, `<path d="%s" %s/>`+"\n", l.Path.svg(), attr)
	}
	b.WriteString("</g>\n</svg>\n")
	return []byte(b.String())
}
