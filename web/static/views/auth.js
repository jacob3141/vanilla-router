// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, clear, icon } from '../ui.js';
import { get, post } from '../api.js';

function authPage(app, title, lead, form) {
  const name = h('div', { class: 'brand-name' }, 'vanilla-router');
  const foot = h('p', { class: 'muted small' });
  clear(app).append(h('div', { class: 'auth-page' },
    h('div', { class: 'card auth-card' },
      h('div', { class: 'brand' },
        h('div', { class: 'brand-logo' }, icon('router')),
        h('div', null, name, h('div', { class: 'brand-sub' }, 'Verwaltung'))),
      h('h1', null, title),
      h('p', { class: 'lead' }, lead),
      form, foot)));
  // Name des Routers und Hinweis auf Lizenz und Quelltext (F-UI-07, AGPL §13), auch ohne Anmeldung
  get('/api/state').then((s) => {
    if (s.name) name.textContent = s.name;
    clear(foot).append('vanilla-router ', s.version || '', ' · freie Software (AGPL-3.0) · ',
      h('a', { href: s.source || '#', target: '_blank', rel: 'noopener noreferrer' }, 'Quelltext'));
  }).catch(() => {});
}

function field(label, input, hint) {
  return h('div', { class: 'field' }, h('label', null, label), input, hint && h('div', { class: 'hint' }, hint));
}

export function renderSetup(app, done) {
  const err = h('div', { class: 'form-error hidden' });
  const code = h('input', { class: 'input mono', autocomplete: 'off', spellcheck: 'false', placeholder: 'XXXX-XXXX', required: true });
  const pw1 = h('input', { class: 'input', type: 'password', autocomplete: 'new-password', required: true, minlength: 12 });
  const pw2 = h('input', { class: 'input', type: 'password', autocomplete: 'new-password', required: true });
  const btn = h('button', { class: 'btn primary', type: 'submit' }, 'Passwort festlegen');
  const form = h('form', null, err,
    field('Einrichtungscode', code, 'Stand am Ende der Installation auf dem Bildschirm. Vergessen? Auf dem Router: sudo vr-webui setup-code'),
    field('Neues Passwort', pw1, 'Mindestens 12 Zeichen. Ein Satz aus mehreren Wörtern ist gut zu merken.'),
    field('Passwort wiederholen', pw2),
    h('div', { class: 'row end' }, btn));
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    err.classList.add('hidden');
    if (pw1.value !== pw2.value) { show(err, 'Die beiden Passwörter sind nicht gleich.'); return; }
    btn.disabled = true;
    try {
      await post('/api/setup', { code: code.value, password: pw1.value });
      done();
    } catch (ex) {
      show(err, ex.message);
    } finally {
      btn.disabled = false;
    }
  });
  authPage(app, 'Einrichtung', 'Lege das Passwort für die Weboberfläche fest. Damit nicht irgendein Gerät im Netz das tun kann, brauchst du den Einrichtungscode vom Router.', form);
  code.focus();
}

export function renderLogin(app, done) {
  const err = h('div', { class: 'form-error hidden' });
  const pw = h('input', { class: 'input', type: 'password', autocomplete: 'current-password', required: true });
  const btn = h('button', { class: 'btn primary', type: 'submit' }, icon('lock'), 'Anmelden');
  const form = h('form', null, err, field('Passwort', pw), h('div', { class: 'row end' }, btn));
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    err.classList.add('hidden');
    btn.disabled = true;
    try {
      await post('/api/login', { password: pw.value });
      done();
    } catch (ex) {
      show(err, ex.message);
      pw.select();
    } finally {
      btn.disabled = false;
    }
  });
  authPage(app, 'Anmelden', 'Melde dich an, um den Router zu verwalten.', form);
  pw.focus();
}

function show(el, msg) { el.textContent = msg; el.classList.remove('hidden'); }
