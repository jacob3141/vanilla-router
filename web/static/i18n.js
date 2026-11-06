// SPDX-License-Identifier: AGPL-3.0-or-later
// Translation for display (Q-07), same rules as internal/i18n: the code contains English, the catalog
// maps it to German, {} stands for a value filled in at runtime. h() in ui.js translates all text it
// creates. Unknown texts stay as they are.

let lang = 'en';
let exact = null;
let patterns = [];
const cache = new Map();

export async function setLang(next) {
  if (next === lang || next !== 'de') return;
  const res = await fetch(`/api/i18n?lang=${next}`, { credentials: 'same-origin' });
  const raw = await res.json();
  exact = new Map();
  patterns = [];
  for (const k of Object.keys(raw).sort((a, b) => b.length - a.length || (a < b ? -1 : 1))) {
    if (!k.includes('{}')) {
      exact.set(k, raw[k]);
      if (k.trim() !== k && !(k.trim() in raw)) exact.set(k.trim(), raw[k].trim());
      continue;
    }
    // Patterns are matched against trimmed text, translate() keeps the surrounding whitespace.
    const src = k.trim().split('{}').map((p) => p.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('(.*?)');
    // Only a pattern that spans lines itself may match across lines.
    patterns.push([new RegExp(`^${src}$`, k.trim().includes('\n') ? 's' : ''), raw[k].trim()]);
  }
  lang = next;
  cache.clear();
  document.documentElement.lang = next;
}

// locale for Intl: undefined is the browser's own.
export function locale() { return lang === 'de' ? 'de-DE' : undefined; }

function lookup(s, depth) {
  if (exact.has(s)) return exact.get(s);
  for (const [re, val] of patterns) {
    const m = re.exec(s);
    if (!m) continue;
    let out = val;
    for (const v of m.slice(1)) out = out.replace('{}', () => translate(v, depth + 1));
    return out;
  }
  return null;
}

function translate(s, depth) {
  if (exact.has(s)) return exact.get(s);
  const trimmed = s.trim();
  if (!trimmed || depth > 5) return s;
  const start = s.indexOf(trimmed);
  const lead = s.slice(0, start);
  const tail = s.slice(start + trimmed.length);
  const v = lookup(trimmed, depth);
  if (v !== null) return lead + v + tail;
  for (const sep of ['\n', ', ']) {
    if (trimmed.includes(sep)) return lead + trimmed.split(sep).map((p) => translate(p, depth + 1)).join(sep) + tail;
  }
  const field = /^[a-z0-9_.[\]-]+: /.exec(trimmed);
  if (field) return lead + field[0] + translate(trimmed.slice(field[0].length), depth + 1) + tail;
  return s;
}

export function t(s) {
  if (!exact || typeof s !== 'string' || !/[A-Za-z]/.test(s)) return s;
  let r = cache.get(s);
  if (r === undefined) {
    if (cache.size > 5000) cache.clear();
    r = translate(s, 0);
    cache.set(s, r);
  }
  return r;
}
