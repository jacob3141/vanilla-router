// SPDX-License-Identifier: AGPL-3.0-or-later
// Small UI library. All content is created through h(), text always as text nodes:
// device names and log lines come from the network and must never be interpreted as HTML.

import { t, locale } from './i18n.js';

const TEXT_ATTRS = new Set(['title', 'placeholder', 'aria-label', 'alt']);

export function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    // style only as an object (CSSOM): style attributes would be blocked by the CSP
    else if (k === 'style') Object.assign(el.style, v);
    else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2), v);
    else if (k === 'value') el.value = v;
    else if (k === 'checked') el.checked = !!v;
    else if (TEXT_ATTRS.has(k)) el.setAttribute(k, t(String(v)));
    else el.setAttribute(k, v === true ? '' : String(v));
  }
  append(el, children);
  return el;
}

function append(el, children) {
  for (const c of children) {
    // "" and false mean "nothing"; 0 as a number is shown deliberately (counters)
    if (c == null || c === false || c === '') continue;
    if (Array.isArray(c)) append(el, c);
    else if (c instanceof Node) el.appendChild(c);
    else el.appendChild(document.createTextNode(t(String(c))));
  }
}

// every calls fn regularly, but not while the tab is in the background; on returning
// once immediately. Returns the cleanup function for the view.
export function every(ms, fn) {
  const id = setInterval(() => { if (!document.hidden) fn(); }, ms);
  const onVis = () => { if (!document.hidden) fn(); };
  document.addEventListener('visibilitychange', onVis);
  return () => { clearInterval(id); document.removeEventListener('visibilitychange', onVis); };
}

export function clear(el) { while (el.firstChild) el.removeChild(el.firstChild); return el; }

// --- Icons (paths in the Lucide style, 24 grid) --------------------------------------
const ICONS = {
  wifi: 'M2 8.5a15 15 0 0 1 20 0M5 12a10.5 10.5 0 0 1 14 0M8.5 15.5a5.5 5.5 0 0 1 7 0M12 19h.01',
  dashboard: 'M3 13h8V3H3zM13 21h8V11h-8zM3 21h8v-6H3zM13 3v6h8V3z',
  devices: 'M4 6h16v10H4zM2 20h20M9 16v4M15 16v4',
  dns: 'M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zM2 12h20M12 2a15 15 0 0 1 0 20M12 2a15 15 0 0 0 0 20',
  shield: 'M12 2l8 4v6c0 5-3.5 8.5-8 10-4.5-1.5-8-5-8-10V6z',
  services: 'M4 4h16v6H4zM4 14h16v6H4zM8 7h.01M8 17h.01',
  container: 'M21 8l-9-5-9 5v8l9 5 9-5zM3 8l9 5 9-5M12 13v8',
  system: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z',
  tools: 'M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18l3 3 6.3-6.3a4 4 0 0 0 5.4-5.4l-2.5 2.5-2.4-.6-.6-2.4z',
  settings: 'M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6',
  logout: 'M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9',
  sun: 'M12 17a5 5 0 1 0 0-10 5 5 0 0 0 0 10zM12 1v2M12 21v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M1 12h2M21 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4',
  moon: 'M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z',
  menu: 'M3 6h18M3 12h18M3 18h18',
  router: 'M3 14h18v6H3zM7 17h.01M11 17h.01M8.5 10.5a5 5 0 0 1 7 0M6 8a8.5 8.5 0 0 1 12 0M12 14v-1',
  cpu: 'M6 6h12v12H6zM9 9h6v6H9zM9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4',
  memory: 'M3 7h18v10H3zM7 7v10M11 7v10M15 7v10M19 7v10',
  thermo: 'M14 14.8V4a2 2 0 1 0-4 0v10.8a4 4 0 1 0 4 0z',
  clock: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 6v6l4 2',
  globe: 'M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zM2 12h20M12 2a15 15 0 0 1 0 20M12 2a15 15 0 0 0 0 20',
  home: 'M3 10l9-7 9 7v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2zM9 22V12h6v10',
  activity: 'M22 12h-4l-3 9L9 3l-3 9H2',
  check: 'M20 6L9 17l-5-5',
  alert: 'M10.3 3.9L1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0zM12 9v4M12 17h.01',
  info: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 16v-4M12 8h.01',
  plus: 'M12 5v14M5 12h14',
  edit: 'M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z',
  trash: 'M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6',
  refresh: 'M23 4v6h-6M1 20v-6h6M3.5 9a9 9 0 0 1 14.9-3.4L23 10M1 14l4.6 4.4A9 9 0 0 0 20.5 15',
  play: 'M5 3l14 9-14 9z',
  stop: 'M5 5h14v14H5z',
  power: 'M18.4 6.6a9 9 0 1 1-12.8 0M12 2v10',
  lock: 'M5 11h14v10H5zM8 11V7a4 4 0 0 1 8 0v4',
  search: 'M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16zM21 21l-4.3-4.3',
  file: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8zM14 2v6h6',
  arrowRight: 'M5 12h14M12 5l7 7-7 7',
  pin: 'M12 22s7-7.5 7-12a7 7 0 1 0-14 0c0 4.5 7 12 7 12zM12 12.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5z',
  folder: 'M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
  download: 'M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3',
  drive: 'M22 12H2M5.5 5h13l3.5 7v6a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-6zM6 16h.01M10 16h.01',
};

export function icon(name, cls) {
  const NS = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('fill', 'none');
  svg.setAttribute('stroke', 'currentColor');
  svg.setAttribute('stroke-width', '2');
  svg.setAttribute('stroke-linecap', 'round');
  svg.setAttribute('stroke-linejoin', 'round');
  svg.setAttribute('aria-hidden', 'true');
  if (cls) svg.setAttribute('class', cls);
  const path = document.createElementNS(NS, 'path');
  path.setAttribute('d', ICONS[name] || ICONS.info);
  svg.appendChild(path);
  return svg;
}

// --- Formatting ---------------------------------------------------------------------
const nf1 = { format: (n) => new Intl.NumberFormat(locale(), { maximumFractionDigits: 1 }).format(n) };
const nf0 = { format: (n) => new Intl.NumberFormat(locale(), { maximumFractionDigits: 0 }).format(n) };

export const fmt = {
  bytes(n) {
    const u = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
    return `${i ? nf1.format(n) : nf0.format(n)} ${u[i]}`;
  },
  // bytes/s → bits/s, as is usual for line speeds
  rate(bytesPerSec) {
    let n = bytesPerSec * 8;
    const u = ['bit/s', 'kbit/s', 'Mbit/s', 'Gbit/s'];
    let i = 0;
    while (n >= 1000 && i < u.length - 1) { n /= 1000; i++; }
    return `${n >= 100 || i === 0 ? nf0.format(n) : nf1.format(n)} ${u[i]}`;
  },
  duration(s) {
    const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60);
    if (d) return `${d} d ${h} h`;
    if (h) return `${h} h ${m} min`;
    return `${m} min`;
  },
  pct(n) { return `${nf0.format(n)} %`; },
  num(n, digits = 1) { return new Intl.NumberFormat(locale(), { maximumFractionDigits: digits }).format(n); },
  date(iso) {
    if (!iso) return '–';
    return new Intl.DateTimeFormat(locale(), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso));
  },
};

// --- Messages and dialogs ------------------------------------------------------------
let toastBox;
export function toast(msg, kind = 'ok') {
  if (!toastBox) { toastBox = h('div', { class: 'toasts', role: 'status', 'aria-live': 'polite' }); document.body.appendChild(toastBox); }
  const t = h('div', { class: `toast ${kind}` }, icon(kind === 'ok' ? 'check' : kind === 'crit' ? 'alert' : 'info'), h('div', null, msg));
  toastBox.appendChild(t);
  setTimeout(() => t.remove(), kind === 'crit' ? 8000 : 4000);
}

// Confirmation dialog. Returns true if confirmed.
export function confirmDialog({ title, text, confirm = 'Confirm', danger = false, details }) {
  return new Promise((resolve) => {
    const dlg = h('dialog', null,
      h('div', { class: 'dialog-body' }, h('h2', null, title), text && h('p', null, text), details),
      h('div', { class: 'dialog-foot' },
        h('button', { class: 'btn', value: 'cancel', onclick: () => dlg.close('cancel') }, 'Cancel'),
        h('button', { class: `btn ${danger ? 'danger solid' : 'primary'}`, onclick: () => dlg.close('ok') }, confirm)));
    dlg.addEventListener('close', () => { resolve(dlg.returnValue === 'ok'); dlg.remove(); });
    document.body.appendChild(dlg);
    dlg.showModal();
  });
}

// Form dialog: fields = [{name, label, value, hint, placeholder, type, options, required}]
// onSubmit(values) throws on errors; the message appears in the dialog.
export function formDialog({ title, text, fields, submit = 'Save', onSubmit, wide }) {
  return new Promise((resolve) => {
    const err = h('div', { class: 'form-error hidden' });
    const inputs = {};
    const body = fields.map((f) => {
      if (f.type === 'section') return h('h3', { class: 'sub form-section' }, f.label);
      if (f.type === 'custom') return h('div', { class: 'field' }, h('label', null, f.label), f.element);
      let input;
      if (f.type === 'select') {
        input = h('select', { class: 'input', name: f.name }, f.options.map((o) => h('option', { value: o.value, selected: o.value === f.value }, o.label)));
      } else if (f.type === 'textarea') {
        input = h('textarea', { class: 'input', name: f.name, rows: f.rows || 3, placeholder: f.placeholder, spellcheck: 'false' }, f.value ?? '');
      } else if (f.type === 'checkbox') {
        input = h('input', { type: 'checkbox', name: f.name, checked: f.value });
      } else {
        input = h('input', { class: 'input', name: f.name, value: f.value ?? '', placeholder: f.placeholder, type: f.type || 'text', autocomplete: 'off', spellcheck: 'false', required: f.required });
      }
      inputs[f.name] = input;
      return h('div', { class: 'field' }, h('label', null, f.label), input, f.hint && h('div', { class: 'hint' }, f.hint));
    });
    const save = h('button', { class: 'btn primary', type: 'submit' }, submit);
    const form = h('form', { method: 'dialog' },
      h('div', { class: 'dialog-body' }, h('h2', null, title), text && h('p', null, text), h('div', { class: 'form-fields' }, err, body)),
      h('div', { class: 'dialog-foot' }, h('button', { class: 'btn', type: 'button', onclick: () => dlg.close() }, 'Cancel'), save));
    const dlg = h('dialog', { class: wide ? 'wide' : null }, form);
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const values = {};
      for (const [k, el] of Object.entries(inputs)) values[k] = el.type === 'checkbox' ? el.checked : el.value.trim();
      save.disabled = true;
      err.classList.add('hidden');
      try {
        const res = await onSubmit(values);
        dlg.close();
        resolve(res ?? true);
      } catch (ex) {
        err.textContent = t(ex.message);
        err.classList.remove('hidden');
      } finally {
        save.disabled = false;
      }
    });
    dlg.addEventListener('close', () => { dlg.remove(); resolve(false); });
    document.body.appendChild(dlg);
    dlg.showModal();
    Object.values(inputs)[0]?.focus();
  });
}

// Log viewer: load() returns the text, "Reload" calls it again.
export function logDialog(title, load) {
  const pre = h('pre', { class: 'code log' }, 'Loading …');
  const refresh = async () => {
    pre.textContent = t('Loading …');
    try { pre.textContent = (await load()) || t('(no entries)'); } catch (e) { pre.textContent = t(e.message); }
    pre.scrollTop = pre.scrollHeight;
  };
  const dlg = h('dialog', { class: 'wide' },
    h('div', { class: 'dialog-body' }, h('h2', null, title), pre),
    h('div', { class: 'dialog-foot' },
      h('button', { class: 'btn', onclick: refresh }, icon('refresh'), 'Reload'),
      h('button', { class: 'btn primary', onclick: () => dlg.close() }, 'Close')));
  dlg.addEventListener('close', () => dlg.remove());
  document.body.appendChild(dlg);
  dlg.showModal();
  refresh();
}

export function badge(text, kind) { return h('span', { class: `badge ${kind || ''}` }, text); }

export function stateKind(state) {
  if (state === 'active' || state === 'running') return 'ok';
  if (state === 'failed' || state === 'exited' || state === 'dead') return 'crit';
  if (state === 'activating' || state === 'reloading' || state === 'restarting') return 'warn';
  return '';
}

export const STATE_LABEL = {
  active: 'running', inactive: 'stopped', failed: 'failed', activating: 'starting',
  deactivating: 'stopping', reloading: 'reloading', running: 'running', exited: 'stopped',
  created: 'created', paused: 'paused', restarting: 'restarting', dead: 'tot',
};
