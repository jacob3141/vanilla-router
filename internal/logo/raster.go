// SPDX-License-Identifier: AGPL-3.0-or-later

package logo

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"sort"
)

// Ein kleiner Rasterer: Pfade werden in Polygone zerlegt, Konturen in Vierecke je Strecke plus Kreise
// an den Punkten (runde Enden und Ecken). Die Deckung je Pixel wird mit 16 Unterzeilen und exakter
// waagerechter Überdeckung berechnet (Nonzero-Regel).

const subRows = 16

type poly []Pt

// flatten zerlegt einen Pfad in Linienzüge; closed sagt, ob der Zug mit Z endet.
func (pa Path) flatten(px float64) (lines []poly, closed []bool) {
	var cur poly
	flush := func(c bool) {
		if len(cur) > 0 {
			lines = append(lines, cur)
			closed = append(closed, c)
		}
		cur = nil
	}
	for _, s := range pa {
		switch s.Op {
		case 'M':
			flush(false)
			cur = poly{tr(s.Pts[0], px)}
		case 'L':
			cur = append(cur, tr(s.Pts[0], px))
		case 'C':
			p0 := cur[len(cur)-1]
			c1, c2, p3 := tr(s.Pts[0], px), tr(s.Pts[1], px), tr(s.Pts[2], px)
			const n = 24
			for i := 1; i <= n; i++ {
				t := float64(i) / n
				u := 1 - t
				cur = append(cur, Pt{
					u*u*u*p0.X + 3*u*u*t*c1.X + 3*u*t*t*c2.X + t*t*t*p3.X,
					u*u*u*p0.Y + 3*u*u*t*c1.Y + 3*u*t*t*c2.Y + t*t*t*p3.Y,
				})
			}
		case 'Z':
			flush(true)
		}
	}
	flush(false)
	return
}

func area(q poly) float64 {
	var a float64
	for i := range q {
		j := (i + 1) % len(q)
		a += q[i].X*q[j].Y - q[j].X*q[i].Y
	}
	return a / 2
}

// oriented dreht ein Polygon in eine einheitliche Richtung, damit sich Teile einer Kontur bei der
// Nonzero-Regel nicht gegenseitig aufheben.
func oriented(q poly) poly {
	if area(q) < 0 {
		r := make(poly, len(q))
		for i := range q {
			r[i] = q[len(q)-1-i]
		}
		return r
	}
	return q
}

func strokePolys(pa Path, width, px float64) []poly {
	w := width * scale * px / Size / 2
	lines, closed := pa.flatten(px)
	var out []poly
	circle := func(c Pt) poly {
		const n = 32
		q := make(poly, n)
		for i := range q {
			a := 2 * math.Pi * float64(i) / n
			q[i] = Pt{c.X + w*math.Cos(a), c.Y + w*math.Sin(a)}
		}
		return q
	}
	for k, l := range lines {
		if closed[k] {
			l = append(l, l[0])
		}
		for i, a := range l {
			out = append(out, circle(a))
			if i == 0 {
				continue
			}
			b := l[i-1]
			dx, dy := a.X-b.X, a.Y-b.Y
			d := math.Hypot(dx, dy)
			if d == 0 {
				continue
			}
			nx, ny := -dy/d*w, dx/d*w
			out = append(out, oriented(poly{{b.X + nx, b.Y + ny}, {a.X + nx, a.Y + ny}, {a.X - nx, a.Y - ny}, {b.X - nx, b.Y - ny}}))
		}
	}
	for i := range out {
		out[i] = oriented(out[i])
	}
	return out
}

func fillPolys(pa Path, px float64) []poly {
	lines, _ := pa.flatten(px)
	return lines
}

type edge struct {
	x0, y0, x1, y1 float64
	dir            int
}

// coverage berechnet die Deckung je Pixel (0…1) für n × n Pixel.
func coverage(ps []poly, n int) []float64 {
	var es []edge
	for _, q := range ps {
		for i := range q {
			a, b := q[i], q[(i+1)%len(q)]
			if a.Y == b.Y {
				continue
			}
			if a.Y < b.Y {
				es = append(es, edge{a.X, a.Y, b.X, b.Y, 1})
			} else {
				es = append(es, edge{b.X, b.Y, a.X, a.Y, -1})
			}
		}
	}
	cov := make([]float64, n*n)
	type cross struct {
		x   float64
		dir int
	}
	var xs []cross
	for py := 0; py < n; py++ {
		row := cov[py*n : (py+1)*n]
		for j := 0; j < subRows; j++ {
			y := float64(py) + (float64(j)+0.5)/subRows
			xs = xs[:0]
			for _, e := range es {
				if y >= e.y0 && y < e.y1 {
					xs = append(xs, cross{e.x0 + (y-e.y0)/(e.y1-e.y0)*(e.x1-e.x0), e.dir})
				}
			}
			if len(xs) == 0 {
				continue
			}
			sort.Slice(xs, func(a, b int) bool { return xs[a].x < xs[b].x })
			wind, start := 0, 0.0
			for _, c := range xs {
				was := wind
				wind += c.dir
				if was == 0 && wind != 0 {
					start = c.x
				} else if was != 0 && wind == 0 {
					addSpan(row, start, c.x)
				}
			}
		}
	}
	for i := range cov {
		cov[i] = math.Min(1, cov[i])
	}
	return cov
}

func addSpan(row []float64, xa, xb float64) {
	n := len(row)
	xa, xb = math.Max(0, xa), math.Min(float64(n), xb)
	if xb <= xa {
		return
	}
	for x := int(xa); x < n && float64(x) < xb; x++ {
		o := math.Min(xb, float64(x+1)) - math.Max(xa, float64(x))
		if o > 0 {
			row[x] += o / subRows
		}
	}
}

// Render zeichnet das Logo in n × n Pixel.
func Render(n int) *image.NRGBA {
	px := float64(n)
	// vormultiplizierte Farbe
	buf := make([][4]float64, n*n)
	paint := func(cov []float64, c color.NRGBA) {
		for i, v := range cov {
			a := v * float64(c.A) / 255
			if a == 0 {
				continue
			}
			d := &buf[i]
			d[0] = float64(c.R)/255*a + d[0]*(1-a)
			d[1] = float64(c.G)/255*a + d[1]*(1-a)
			d[2] = float64(c.B)/255*a + d[2]*(1-a)
			d[3] = a + d[3]*(1-a)
		}
	}
	for _, l := range Layers() {
		var clip []float64
		if l.Clip != nil {
			clip = coverage(fillPolys(l.Clip, px), n)
		}
		apply := func(cov []float64) []float64 {
			if clip != nil {
				for i := range cov {
					cov[i] *= clip[i]
				}
			}
			return cov
		}
		if l.Fill.A != 0 {
			paint(apply(coverage(fillPolys(l.Path, px), n)), l.Fill)
		}
		if l.Stroke.A != 0 {
			paint(apply(coverage(strokePolys(l.Path, l.Width, px), n)), l.Stroke)
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for i, d := range buf {
		if d[3] == 0 {
			continue
		}
		c := color.NRGBA{uint8(math.Round(d[0] / d[3] * 255)), uint8(math.Round(d[1] / d[3] * 255)),
			uint8(math.Round(d[2] / d[3] * 255)), uint8(math.Round(d[3] * 255))}
		img.SetNRGBA(i%n, i/n, c)
	}
	return img
}

// PNG liefert das Logo als PNG mit n × n Pixeln.
func PNG(n int) []byte {
	var b bytes.Buffer
	png.Encode(&b, Render(n))
	return b.Bytes()
}

// IconSizes sind die Größen im Windows-Icon (Taskleiste, Explorer, Kopf des Stick-Erstellers bis 200 % DPI).
var IconSizes = []int{16, 20, 24, 32, 40, 48, 64, 96, 128, 256}

// IconImage liefert ein Bild für eine .ico-Datei oder RT_ICON: bis 128 als 32-Bit-DIB, 256 als PNG.
func IconImage(n int) []byte {
	if n >= 256 {
		return PNG(n)
	}
	img := Render(n)
	var b bytes.Buffer
	le := binary.LittleEndian
	binary.Write(&b, le, struct {
		Size                  uint32
		Width, Height         int32
		Planes, BitCount      uint16
		Compression, ImgSize  uint32
		XPels, YPels          int32
		ClrUsed, ClrImportant uint32
	}{40, int32(n), int32(2 * n), 1, 32, 0, 0, 0, 0, 0, 0})
	for y := n - 1; y >= 0; y-- { // von unten nach oben
		for x := 0; x < n; x++ {
			c := img.NRGBAAt(x, y)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	mask := (n + 31) / 32 * 4 // UND-Maske, 1 Bit je Pixel, Zeilen auf 4 Byte aufgefüllt
	for y := n - 1; y >= 0; y-- {
		row := make([]byte, mask)
		for x := 0; x < n; x++ {
			if img.NRGBAAt(x, y).A == 0 {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
		b.Write(row)
	}
	return b.Bytes()
}

// ICO liefert eine .ico-Datei mit allen IconSizes.
func ICO() []byte {
	le := binary.LittleEndian
	var head, data bytes.Buffer
	binary.Write(&head, le, [3]uint16{0, 1, uint16(len(IconSizes))})
	off := 6 + 16*len(IconSizes)
	for _, n := range IconSizes {
		img := IconImage(n)
		head.Write([]byte{byte(n), byte(n), 0, 0}) // 256 wird als 0 geschrieben
		binary.Write(&head, le, [2]uint16{1, 32})
		binary.Write(&head, le, [2]uint32{uint32(len(img)), uint32(off + data.Len())})
		data.Write(img)
	}
	return append(head.Bytes(), data.Bytes()...)
}
