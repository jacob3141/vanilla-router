// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, clear, icon, toast } from './ui.js';
import { get, post, setUnauthorizedHandler } from './api.js';
import { renderSetup, renderLogin } from './views/auth.js';
import dashboard from './views/dashboard.js';
import devices from './views/devices.js';
import dns from './views/dns.js';
import firewall from './views/firewall.js';
import services from './views/services.js';
import containers from './views/containers.js';
import system from './views/system.js';
import tools from './views/tools.js';
import settings from './views/settings.js';
import files from './views/files.js';
import monitor from './views/monitor.js';
import device from './views/device.js';
import connections from './views/connections.js';
import network from './views/network.js';
import logs from './views/logs.js';
import { setupPalette } from './palette.js';

// Navigation: Bereich → Ansichten. Jede Ansicht exportiert { title, subtitle, render(container) }
// und darf eine Aufräumfunktion zurückgeben (z. B. um einen Live-Stream zu schließen).
const NAV = [
  { section: null, items: [{ path: 'dashboard', label: 'Übersicht', icon: 'dashboard', view: dashboard }] },
  { section: 'Netzwerk', items: [
    { path: 'devices', label: 'Geräte', icon: 'devices', view: devices },
    { path: 'monitor', label: 'Online-Monitor', icon: 'activity', view: monitor },
    { path: 'dns', label: 'DNS', icon: 'dns', view: dns },
    { path: 'firewall', label: 'Firewall', icon: 'shield', view: firewall },
    { path: 'connections', label: 'Verbindungen', icon: 'arrowRight', view: connections },
    { path: 'network', label: 'Netzwerk-Status', icon: 'globe', view: network },
  ] },
  { section: 'Dienste', items: [
    { path: 'services', label: 'Dienste', icon: 'services', view: services },
    // Erweiterungen: nur sichtbar, wenn installiert (PLAN.md 3.8, /api/state → features)
    { path: 'containers', label: 'Container', icon: 'container', view: containers, feature: 'containers' },
    { path: 'files', label: 'Dateiablage', icon: 'drive', view: files, feature: 'files' },
  ] },
  { section: 'System', items: [
    { path: 'system', label: 'System', icon: 'system', view: system },
    { path: 'logs', label: 'Protokolle', icon: 'file', view: logs },
    { path: 'tools', label: 'Werkzeuge', icon: 'tools', view: tools },
    { path: 'settings', label: 'Einstellungen', icon: 'settings', view: settings },
  ] },
];

const ROUTES = Object.fromEntries(NAV.flatMap((s) => s.items).map((i) => [i.path, i]));
const openPalette = setupPalette(NAV.flatMap((s) => s.items.map((i) => ({ ...i, section: s.section }))));
// Seiten ohne eigenen Menüpunkt; markiert wird der übergeordnete Eintrag
const HIDDEN = { device: { path: 'devices', view: device } };

const app = document.getElementById('app');
let cleanup = null;

// --- Farbschema ----------------------------------------------------------------------
function storedTheme() { try { return localStorage.getItem('theme'); } catch { return null; } }
function applyTheme(t) {
  const theme = t || (matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark');
  document.documentElement.dataset.theme = theme;
  return theme;
}
applyTheme(storedTheme());

function toggleTheme() {
  const next = document.documentElement.dataset.theme === 'light' ? 'dark' : 'light';
  try { localStorage.setItem('theme', next); } catch { /* privater Modus */ }
  applyTheme(next);
  renderThemeButton();
}
const themeBtn = h('button', { class: 'btn ghost icon', title: 'Hell/Dunkel umschalten', onclick: toggleTheme });
function renderThemeButton() {
  clear(themeBtn).appendChild(icon(document.documentElement.dataset.theme === 'light' ? 'moon' : 'sun'));
}
renderThemeButton();

// --- Start ---------------------------------------------------------------------------
async function boot() {
  let state;
  try {
    state = await get('/api/state');
  } catch (e) {
    clear(app).append(h('div', { class: 'auth-page' }, h('div', { class: 'card auth-card' }, h('h1', null, 'Nicht erreichbar'), h('p', { class: 'lead' }, e.message))));
    return;
  }
  app.className = '';
  if (!state.configured) return renderSetup(app, boot);
  if (!state.authenticated) return renderLogin(app, boot);
  renderShell(state);
}

setUnauthorizedHandler(() => {
  if (cleanup) { cleanup(); cleanup = null; }
  toast('Die Sitzung ist abgelaufen. Bitte neu anmelden.', 'warn');
  boot();
});

// --- Gerüst mit Navigation -----------------------------------------------------------
let shell, contentEl, titleEl, subtitleEl, navLinks = {};
let routerName = 'vanilla-router';

function renderShell(state) {
  navLinks = {};
  if (state.name) routerName = state.name;
  const has = (i) => !i.feature || (state.features && state.features[i.feature]);
  const nav = h('nav', { class: 'nav' }, NAV.map((s) => [
    s.section && h('div', { class: 'nav-section' }, s.section),
    s.items.filter(has).map((i) => (navLinks[i.path] = h('a', { href: `#/${i.path}` }, icon(i.icon), i.label))),
  ]));
  titleEl = h('h1');
  subtitleEl = h('p');
  contentEl = h('div', { class: 'content' });
  shell = h('div', { class: 'shell' },
    h('aside', { class: 'sidebar' },
      h('div', { class: 'brand' },
        h('div', { class: 'brand-logo' }, icon('router')),
        h('div', null, h('div', { class: 'brand-name' }, routerName), h('div', { class: 'brand-sub' }, state.dev ? 'Testinstanz' : 'Verwaltung'))),
      nav,
      // F-UI-07 (AGPL §13): Lizenz und Quelltext immer erreichbar
      h('div', { class: 'sidebar-foot' },
        h('a', { href: state.source || '#', target: '_blank', rel: 'noopener noreferrer', title: 'Freie Software unter der GNU AGPL, Version 3 oder später' },
          `vanilla-router ${state.version || ''} · AGPL-3.0 · Quelltext`))),
    h('div', { class: 'main' },
      h('header', { class: 'topbar' },
        h('button', { class: 'btn ghost icon menu-btn', title: 'Menü', onclick: () => shell.classList.toggle('nav-open') }, icon('menu')),
        h('div', { class: 'title' }, titleEl, subtitleEl),
        h('button', { class: 'btn search-btn', title: 'Schnellsuche (Strg+K)', onclick: () => openPalette() }, icon('search'), h('span', { class: 'hide-sm' }, 'Suchen'), h('kbd', { class: 'hide-sm' }, 'Strg K')),
        themeBtn,
        h('button', { class: 'btn ghost', title: 'Abmelden', onclick: logout }, icon('logout'), h('span', { class: 'hide-sm' }, 'Abmelden'))),
      contentEl));
  shell.addEventListener('click', (e) => { if (e.target === shell) shell.classList.remove('nav-open'); });
  clear(app).append(shell);
  route();
}

async function logout() {
  try { await post('/api/logout'); } catch { /* egal */ }
  if (cleanup) { cleanup(); cleanup = null; }
  boot();
}

function route() {
  if (!contentEl) return;
  const path = location.hash.replace(/^#\/?/, '').split('?')[0] || 'dashboard';
  const entry = HIDDEN[path] || ROUTES[path] || ROUTES.dashboard;
  if (cleanup) { cleanup(); cleanup = null; }
  for (const [p, a] of Object.entries(navLinks)) a.classList.toggle('active', p === entry.path);
  shell.classList.remove('nav-open');
  titleEl.textContent = entry.view.title;
  subtitleEl.textContent = entry.view.subtitle || '';
  document.title = `${entry.view.title} · ${routerName}`;
  clear(contentEl);
  const res = entry.view.render(contentEl);
  if (typeof res === 'function') cleanup = res;
}

window.addEventListener('hashchange', route);
boot();
