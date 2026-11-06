// SPDX-License-Identifier: AGPL-3.0-or-later
import { t } from '../i18n.js';
import { h, clear, icon } from '../ui.js';
import { get, post } from '../api.js';

function authPage(app, title, lead, form) {
  const name = h('div', { class: 'brand-name' }, 'vanilla-router');
  const foot = h('p', { class: 'muted small' });
  clear(app).append(h('div', { class: 'auth-page' },
    h('div', { class: 'card auth-card' },
      h('div', { class: 'brand' },
        h('div', { class: 'brand-logo' }, icon('router')),
        h('div', null, name, h('div', { class: 'brand-sub' }, 'Administration'))),
      h('h1', null, title),
      h('p', { class: 'lead' }, lead),
      form, foot)));
  // Router name and note about the license and source (F-UI-07, AGPL §13), also without login
  get('/api/state').then((s) => {
    if (s.name) name.textContent = s.name;
    clear(foot).append('vanilla-router ', s.version || '', ' · free software (AGPL-3.0) · ',
      h('a', { href: s.source || '#', target: '_blank', rel: 'noopener noreferrer' }, 'Source code'));
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
  const btn = h('button', { class: 'btn primary', type: 'submit' }, 'Set password');
  const form = h('form', null, err,
    field('Setup code', code, 'Shown on the screen at the end of the installation. Forgotten? On the router: sudo vr-webui setup-code'),
    field('New password', pw1, 'At least 12 characters. A sentence of several words is easy to remember.'),
    field('Repeat password', pw2),
    h('div', { class: 'row end' }, btn));
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    err.classList.add('hidden');
    if (pw1.value !== pw2.value) { show(err, 'The two passwords are not the same.'); return; }
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
  authPage(app, 'Setup', 'Set the password for the web interface. So that not just any device on the network can do this, you need the setup code from the router.', form);
  code.focus();
}

export function renderLogin(app, done) {
  const err = h('div', { class: 'form-error hidden' });
  const pw = h('input', { class: 'input', type: 'password', autocomplete: 'current-password', required: true });
  const btn = h('button', { class: 'btn primary', type: 'submit' }, icon('lock'), 'Sign in');
  const form = h('form', null, err, field('Password', pw), h('div', { class: 'row end' }, btn));
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
  authPage(app, 'Sign in', 'Sign in to manage the router.', form);
  pw.focus();
}

function show(el, msg) { el.textContent = t(msg); el.classList.remove('hidden'); }
