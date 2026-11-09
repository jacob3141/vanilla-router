// SPDX-License-Identifier: AGPL-3.0-or-later
// A terminal in a dialog. The project has no third-party code, so this is a small emulator: what shells and
// everyday full-screen programs need from a VT100, with colors. The router's output only ever becomes text.
import { h, toast } from '../ui.js';
import { post } from '../api.js';

const ROWS = 28;
const COLS = 110;
const SCROLLBACK = 2000;
const blank = () => ({ c: ' ', fg: 0, bg: 0, b: false, r: false });
const emptyLine = () => Array.from({ length: COLS }, blank);

export class Screen {
  constructor() {
    this.lines = Array.from({ length: ROWS }, emptyLine);
    this.history = [];
    this.x = 0; this.y = 0;
    this.top = 0; this.bottom = ROWS - 1;
    this.attr = { fg: 0, bg: 0, b: false, r: false };
    this.state = 'text'; this.params = '';
    this.cursor = true;
    this.saved = null; this.alt = null; this.sx = 0; this.sy = 0;
  }

  scrollUp(n = 1) {
    for (let i = 0; i < n; i++) {
      const gone = this.lines.splice(this.top, 1)[0];
      if (this.top === 0 && !this.alt) { this.history.push(gone); if (this.history.length > SCROLLBACK) this.history.shift(); }
      this.lines.splice(this.bottom, 0, emptyLine());
    }
  }

  scrollDown(n = 1) { for (let i = 0; i < n; i++) { this.lines.splice(this.bottom, 1); this.lines.splice(this.top, 0, emptyLine()); } }

  newline() { if (this.y === this.bottom) this.scrollUp(); else if (this.y < ROWS - 1) this.y++; }

  put(c) {
    if (this.x >= COLS) { this.x = 0; this.newline(); }
    this.lines[this.y][this.x] = { c, ...this.attr };
    this.x++;
  }

  erase(line, from, to) { for (let i = from; i < to; i++) this.lines[line][i] = blank(); }

  sgr(p) {
    const list = p.length ? p : [0];
    for (let i = 0; i < list.length; i++) {
      const n = list[i];
      if (n === 0) this.attr = { fg: 0, bg: 0, b: false, r: false };
      else if (n === 1) this.attr.b = true;
      else if (n === 22) this.attr.b = false;
      else if (n === 7) this.attr.r = true;
      else if (n === 27) this.attr.r = false;
      else if (n >= 30 && n <= 37) this.attr.fg = n - 29;
      else if (n === 39) this.attr.fg = 0;
      else if (n >= 40 && n <= 47) this.attr.bg = n - 39;
      else if (n === 49) this.attr.bg = 0;
      else if (n >= 90 && n <= 97) this.attr.fg = n - 81;
      else if (n >= 100 && n <= 107) this.attr.bg = n - 91;
      else if (n === 38 || n === 48) i += list[i + 1] === 5 ? 2 : 4; // 256 colors and true color are shown in the default color
    }
  }

  csi(final) {
    const priv = this.params.startsWith('?');
    const p = (priv ? this.params.slice(1) : this.params).split(';').filter((x, i, a) => x !== '' || a.length > 1).map((x) => (x === '' ? 0 : +x));
    const n = (i = 0, d = 1) => p[i] || d;
    switch (final) {
      case 'A': this.y = Math.max(this.top, this.y - n()); break;
      case 'B': this.y = Math.min(this.bottom, this.y + n()); break;
      case 'C': this.x = Math.min(COLS - 1, this.x + n()); break;
      case 'D': this.x = Math.max(0, this.x - n()); break;
      case 'G': this.x = Math.min(COLS - 1, n() - 1); break;
      case 'd': this.y = Math.min(ROWS - 1, n() - 1); break;
      case 'H': case 'f': this.y = Math.min(ROWS - 1, n(0) - 1); this.x = Math.min(COLS - 1, n(1) - 1); break;
      case 'J': {
        const m = p[0] || 0;
        if (m === 0) { this.erase(this.y, this.x, COLS); for (let l = this.y + 1; l < ROWS; l++) this.erase(l, 0, COLS); }
        if (m === 1) { this.erase(this.y, 0, this.x + 1); for (let l = 0; l < this.y; l++) this.erase(l, 0, COLS); }
        if (m >= 2) for (let l = 0; l < ROWS; l++) this.erase(l, 0, COLS);
        break;
      }
      case 'K': {
        const m = p[0] || 0;
        this.erase(this.y, m === 0 ? this.x : 0, m === 1 ? this.x + 1 : COLS);
        break;
      }
      case 'L': { const b = this.top; this.top = this.y; this.scrollDown(n()); this.top = b; break; }
      case 'M': { const b = this.top; this.top = this.y; this.scrollUp(n()); this.top = b; break; }
      case 'P': { const l = this.lines[this.y]; l.splice(this.x, n()); while (l.length < COLS) l.push(blank()); break; }
      case '@': { const l = this.lines[this.y]; for (let i = 0; i < n(); i++) l.splice(this.x, 0, blank()); l.length = COLS; break; }
      case 'X': this.erase(this.y, this.x, Math.min(COLS, this.x + n())); break;
      case 'S': this.scrollUp(n()); break;
      case 'T': this.scrollDown(n()); break;
      case 'm': this.sgr(p); break;
      case 'r': this.top = Math.max(0, n(0) - 1); this.bottom = Math.min(ROWS - 1, n(1, ROWS) - 1); this.x = 0; this.y = 0; break;
      case 's': this.sx = this.x; this.sy = this.y; break;
      case 'u': this.x = this.sx; this.y = this.sy; break;
      case 'h': case 'l':
        if (priv && p.includes(25)) this.cursor = final === 'h';
        if (priv && (p.includes(1049) || p.includes(47))) this.setAlt(final === 'h');
        break;
      default:
    }
  }

  setAlt(on) {
    if (on && !this.alt) { this.alt = { lines: this.lines, x: this.x, y: this.y }; this.lines = Array.from({ length: ROWS }, emptyLine); this.x = 0; this.y = 0; }
    if (!on && this.alt) { ({ lines: this.lines, x: this.x, y: this.y } = this.alt); this.alt = null; }
  }

  feed(text) {
    for (const ch of text) {
      const code = ch.codePointAt(0);
      if (this.state === 'esc') {
        if (ch === '[') { this.state = 'csi'; this.params = ''; continue; }
        if (ch === ']') { this.state = 'osc'; continue; }
        if ('()*+'.includes(ch)) { this.state = 'skip'; continue; }
        this.state = 'text';
        if (ch === 'M') { if (this.y === this.top) this.scrollDown(); else if (this.y > 0) this.y--; }
        if (ch === 'D') this.newline();
        if (ch === 'E') { this.x = 0; this.newline(); }
        if (ch === '7') { this.sx = this.x; this.sy = this.y; }
        if (ch === '8') { this.x = this.sx; this.y = this.sy; }
        continue;
      }
      if (this.state === 'skip') { this.state = 'text'; continue; }
      if (this.state === 'osc') { if (code === 7) this.state = 'text'; else if (code === 27) this.state = 'oscEsc'; continue; }
      if (this.state === 'oscEsc') { this.state = 'text'; continue; }
      if (this.state === 'csi') {
        if (code >= 0x40 && code <= 0x7e) { this.state = 'text'; this.csi(ch); } else this.params += ch;
        continue;
      }
      if (code === 27) { this.state = 'esc'; continue; }
      if (code === 13) { this.x = 0; continue; }
      if (code === 10 || code === 11 || code === 12) { this.newline(); continue; }
      if (code === 8) { this.x = Math.max(0, this.x - 1); continue; }
      if (code === 9) { this.x = Math.min(COLS - 1, (Math.floor(this.x / 8) + 1) * 8); continue; }
      if (code < 32 || code === 127) continue;
      this.put(ch);
    }
  }

  // Lines of runs with the same look: [[text, cssClass], …]
  runs(line, cursorAt) {
    const out = [];
    let cur = null;
    line.forEach((c, i) => {
      const inverse = c.r !== (i === cursorAt);
      const cls = [c.fg && `t-f${c.fg}`, c.bg && `t-b${c.bg}`, c.b && 't-bold', inverse && 't-rev'].filter(Boolean).join(' ');
      if (cur && cur[1] === cls) cur[0] += c.c; else { cur = [c.c, cls]; out.push(cur); }
    });
    return out;
  }
}

const KEYS = { Enter: '\r', Backspace: '\x7f', Tab: '\t', Escape: '\x1b', ArrowUp: '\x1b[A', ArrowDown: '\x1b[B', ArrowRight: '\x1b[C', ArrowLeft: '\x1b[D', Home: '\x1b[H', End: '\x1b[F', Delete: '\x1b[3~', PageUp: '\x1b[5~', PageDown: '\x1b[6~', Insert: '\x1b[2~' };

export function keyToBytes(e) {
  if (e.ctrlKey && !e.altKey && e.key.length === 1) {
    const k = e.key.toUpperCase().charCodeAt(0);
    if (k >= 64 && k <= 95) return String.fromCharCode(k - 64);
  }
  if (KEYS[e.key]) return KEYS[e.key];
  if (e.key.length === 1 && !e.metaKey && !e.ctrlKey) return (e.altKey ? '\x1b' : '') + e.key;
  return '';
}

const fromB64 = (s) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
const toB64 = (s) => { let b = ''; new TextEncoder().encode(s).forEach((x) => { b += String.fromCharCode(x); }); return btoa(b); };

export async function openTerminal(name, mode, shell) {
  let id;
  try { id = (await post('/api/containers/term/open', { name, mode, shell: shell || '' })).data.id; } catch (e) { toast(e.message, 'crit'); return; }
  const screen = new Screen();
  const view = h('pre', { class: 'term', tabindex: '0' });
  const status = h('span', { class: 'muted small spacer' }, mode === 'attach' ? 'Attached. Closing does not stop the container.' : 'Ctrl+C and friends go to the container.');
  const dlg = h('dialog', { class: 'wide term-dialog' },
    h('div', { class: 'dialog-body' }, h('h2', null, `${mode === 'attach' ? 'Attach' : 'Console'}: ${name}`), view),
    h('div', { class: 'dialog-foot' }, status, h('button', { class: 'btn', onclick: () => dlg.close() }, 'Close')));

  let open = true;
  let queue = '';
  let sending = false;
  let drawn = false;
  function render() {
    drawn = false;
    const atBottom = view.scrollTop + view.clientHeight >= view.scrollHeight - 4;
    const line = (l, cursorAt) => h('div', { class: 't-line' }, screen.runs(l, cursorAt).map(([t, c]) => h('span', { class: c || null }, t)));
    view.replaceChildren(...screen.history.slice(-300).map((l) => line(l, -1)), ...screen.lines.map((l, i) => line(l, screen.cursor && i === screen.y ? screen.x : -1)));
    if (atBottom) view.scrollTop = view.scrollHeight;
  }
  const schedule = () => { if (!drawn) { drawn = true; requestAnimationFrame(render); } };

  async function flush() {
    if (sending || !queue) return;
    sending = true;
    while (queue && open) {
      const data = queue;
      queue = '';
      try { await post('/api/containers/term/write', { id, data: toB64(data) }); } catch { open = false; }
    }
    sending = false;
  }
  const send = (s) => { queue += s; flush(); };

  view.addEventListener('keydown', (e) => {
    if (e.ctrlKey && e.shiftKey && (e.key === 'C' || e.key === 'V')) return; // left to the browser
    const s = keyToBytes(e);
    if (s) { e.preventDefault(); send(s); }
  });
  view.addEventListener('paste', (e) => { e.preventDefault(); send(e.clipboardData.getData('text')); });

  dlg.addEventListener('close', () => {
    open = false;
    dlg.remove();
    post('/api/containers/term/close', { id }).catch(() => {});
  });
  document.body.appendChild(dlg);
  dlg.showModal();
  render();
  view.focus();
  post('/api/containers/term/resize', { id, rows: ROWS, cols: COLS }).catch(() => {});

  const decoder = new TextDecoder();
  while (open) {
    let r;
    try { r = (await post('/api/containers/term/read', { id })).data; } catch (e) { status.textContent = e.message; break; }
    if (r.data) { screen.feed(decoder.decode(fromB64(r.data), { stream: true })); schedule(); }
    if (!r.alive) { status.textContent = 'The terminal has ended.'; break; }
  }
}
