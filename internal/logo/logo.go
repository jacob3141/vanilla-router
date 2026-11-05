// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logo beschreibt das Projektlogo (ein Vanilleeis im Hörnchen, die Vanilleschote ragt wie die
// Antenne eines Routers heraus) als Geometrie und erzeugt daraus SVG, PNG und das Windows-Icon. So gibt
// es nur eine Quelle und keine fremden Grafikwerkzeuge (D-01). Die Dateien unter docs/ und das
// Favicon der Oberfläche werden mit „go run ./internal/logo/mklogo“ erzeugt, ein Test prüft, dass sie
// zum Code passen.
package logo

import (
	"fmt"
	"image/color"
	"math"
	"strings"
)

// Farben des Logos, auch für Oberflächen im selben Stil (Stick-Ersteller, Installer).
var (
	Outline = color.NRGBA{0x4E, 0x34, 0x23, 0xFF} // dunkle Vanilleschote
	Cream   = color.NRGBA{0xFF, 0xF5, 0xDD, 0xFF} // Vanilleeis
	Waffle  = color.NRGBA{0xE5, 0xA8, 0x5A, 0xFF} // Hörnchen
	Caramel = color.NRGBA{0xC5, 0x85, 0x42, 0xFF} // Waffelmuster, Funkbögen, Akzent
	Speck   = color.NRGBA{0x7A, 0x52, 0x38, 0xFF} // Vanillekörnchen
	White   = color.NRGBA{0xFF, 0xFF, 0xFF, 0xE6}
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
	scale = 0.83
	offX  = 128 - 150*scale // Mitte des Entwurfs (x 58…242) auf die Mitte der Fläche
	offY  = 8 + 50*scale    // Oberkante des Entwurfs (y −50, mit Konturbreite) auf 8
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

// arc liefert einen Kreisbogen um c als Bézierkurven (je höchstens 45°).
func arc(c Pt, r, from, to float64) Path {
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	at := func(a float64) Pt { return p(c.X+r*math.Cos(a), c.Y+r*math.Sin(a)) }
	n := int(math.Ceil(math.Abs(to-from) / 45))
	step := rad(to-from) / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	a := rad(from)
	path := Path{{'M', []Pt{at(a)}}}
	for i := 0; i < n; i++ {
		b := a + step
		p0, p3 := at(a), at(b)
		c1 := p(p0.X-k*r*math.Sin(a), p0.Y+k*r*math.Cos(a))
		c2 := p(p3.X+k*r*math.Sin(b), p3.Y-k*r*math.Cos(b))
		path = append(path, Seg{'C', []Pt{c1, c2, p3}})
		a = b
	}
	return path
}

// Layers liefert das Logo von hinten nach vorn.
func Layers() []Layer {
	cone := ParsePath("M 80 122 L 176 122 L 133 232 C 131 239 125 239 123 232 Z")
	scoop := ParsePath(`M 66 112 C 60 70 92 40 128 40 C 164 40 196 70 190 112
		C 196 126 184 140 172 134 C 166 150 150 152 146 138 C 140 147 130 147 126 137
		C 120 162 100 162 102 137 C 94 145 80 143 80 132 C 66 134 60 124 66 112 Z`)
	var ls []Layer
	// Vanilleschote als Antenne, mit zwei Funkbögen an der Spitze
	tip := p(200, -8)
	ls = append(ls, Layer{Path: ParsePath("M 146 64 C 166 40 186 18 200 -8"), Stroke: Outline, Width: 15})
	ls = append(ls,
		Layer{Path: arc(tip, 19, -100, -8), Stroke: Caramel, Width: 8},
		Layer{Path: arc(tip, 36, -102, -6), Stroke: Caramel, Width: 8})
	// Hörnchen mit Waffelmuster
	ls = append(ls, Layer{Path: cone, Fill: Waffle})
	var hatch Path
	for i := -4; i <= 6; i++ {
		x := 60 + float64(i)*22
		hatch = append(hatch, Seg{'M', []Pt{p(x, 118)}}, Seg{'L', []Pt{p(x+70, 240)}})
		hatch = append(hatch, Seg{'M', []Pt{p(x+70, 118)}}, Seg{'L', []Pt{p(x, 240)}})
	}
	ls = append(ls, Layer{Path: hatch, Stroke: Caramel, Width: 5, Clip: cone, ClipID: "cone"})
	ls = append(ls, Layer{Path: cone, Stroke: Outline, Width: 8})
	// Eiskugel mit Glanz und Vanillekörnchen
	ls = append(ls, Layer{Path: scoop, Fill: Cream, Stroke: Outline, Width: 8})
	ls = append(ls, Layer{Path: ParsePath("M 84 98 C 82 84 88 68 102 60"), Stroke: White, Width: 9})
	for _, s := range [][4]float64{{112, 72, 116, 68}, {146, 62, 150, 66}, {164, 92, 168, 89},
		{122, 104, 125, 108}, {98, 116, 102, 113}, {142, 120, 146, 121}, {136, 88, 139, 85}} {
		ls = append(ls, Layer{Path: Path{{'M', []Pt{p(s[0], s[1])}}, {'L', []Pt{p(s[2], s[3])}}}, Stroke: Speck, Width: 6})
	}
	return ls
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
