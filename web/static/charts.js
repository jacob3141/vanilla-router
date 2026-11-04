// SPDX-License-Identifier: AGPL-3.0-or-later
// Einfache SVG-Liniendiagramme. Zeitachse in Unix-Sekunden, Breite passt sich dem Element an.

const NS = 'http://www.w3.org/2000/svg';

function el(tag, attrs = {}, text) {
  const e = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
  if (text != null) e.textContent = text;
  return e;
}

export function chartElement(height = 190) {
  const svg = el('svg', { class: 'chart', viewBox: `0 0 600 ${height}` });
  svg.dataset.h = height;
  return svg;
}

export function niceMax(v, min = 1) {
  if (!(v > 0)) return min;
  const exp = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 2, 2.5, 5, 10]) if (m * exp >= v) return Math.max(min, m * exp);
  return Math.max(min, 10 * exp);
}

/**
 * series: [{ points: [[t, v], …], cls: 'rx-line', area: 'rx-area' }]
 * opts: { t0, t1, yMax, yFmt(v), tFmt(t), gaps: [[t0,t1],…] (markiert, z. B. Ausfälle), gapSec }
 * Fehlende Werte (null) unterbrechen die Linie.
 */
export function drawLines(svg, series, opts) {
  while (svg.firstChild) svg.removeChild(svg.firstChild);
  const H = +svg.dataset.h || 190;
  const W = Math.max(300, Math.round(svg.clientWidth || 600));
  svg.setAttribute('viewBox', `0 0 ${W} ${H}`);
  const padT = 10, padB = 18;
  const { t0, t1 } = opts;
  const all = series.flatMap((s) => s.points.map((p) => p[1]).filter((v) => v != null));
  const yMax = opts.yMax ?? niceMax(Math.max(0, ...all) * 1.1, opts.yMin ?? 1);
  const x = (t) => (W * (t - t0)) / Math.max(1, t1 - t0);
  const y = (v) => padT + (H - padT - padB) * (1 - Math.min(v, yMax) / yMax);

  for (let g = 0; g <= 4; g++) {
    const yy = padT + ((H - padT - padB) * g) / 4;
    svg.appendChild(el('line', { x1: 0, x2: W, y1: yy, y2: yy, class: 'grid-line' }));
  }
  for (const [a, b] of opts.gaps || []) {
    svg.appendChild(el('rect', { x: x(a), y: padT, width: Math.max(2, x(b) - x(a)), height: H - padT - padB, class: 'gap' }));
  }
  const gapSec = opts.gapSec || Infinity;
  for (const s of series) {
    // Linie in Stücke teilen: bei null oder Zeitlücken größer gapSec
    const segs = [];
    let cur = [];
    let lastT = null;
    for (const [t, v] of s.points) {
      if (v == null || (lastT != null && t - lastT > gapSec)) {
        if (cur.length) segs.push(cur);
        cur = [];
      }
      if (v != null) cur.push([t, v]);
      lastT = t;
    }
    if (cur.length) segs.push(cur);
    for (const seg of segs) {
      const d = seg.map(([t, v], i) => `${i ? 'L' : 'M'}${x(t).toFixed(1)},${y(v).toFixed(1)}`).join('');
      if (s.area && seg.length > 1) {
        svg.appendChild(el('path', { d: `${d}L${x(seg[seg.length - 1][0]).toFixed(1)},${y(0)}L${x(seg[0][0]).toFixed(1)},${y(0)}Z`, class: s.area }));
      }
      svg.appendChild(el('path', { d: seg.length > 1 ? d : `${d}l0.1,0`, class: s.cls }));
    }
  }
  svg.appendChild(el('text', { x: W - 4, y: padT + 11, 'text-anchor': 'end' }, opts.yFmt ? opts.yFmt(yMax) : String(yMax)));
  svg.appendChild(el('text', { x: 2, y: H - 3 }, opts.tFmt(t0)));
  svg.appendChild(el('text', { x: W - 4, y: H - 3, 'text-anchor': 'end' }, opts.tFmt(t1)));
}

export function clockFmt(t) {
  return new Intl.DateTimeFormat('de-DE', { hour: '2-digit', minute: '2-digit' }).format(new Date(t * 1000));
}
