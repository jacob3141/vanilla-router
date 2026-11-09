// SPDX-License-Identifier: AGPL-3.0-or-later
import { t, setLang } from './i18n.js';
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
import internet, { SETUP_LATER_KEY } from './views/internet.js';
import wifi from './views/wifi.js';
import wireless from './views/wireless.js';
import home from './views/home.js';
import { setupPalette } from './palette.js';

// Navigation: section → views. Each view exports { title, subtitle, render(container) }
// and may return a cleanup function (e.g. to close a live stream).
const NAV = [
  { section: null, items: [{ path: 'dashboard', label: 'Overview', icon: 'dashboard', view: dashboard }] },
  { section: 'Smart Home', items: [
    { path: 'home', label: 'Rooms and devices', icon: 'home', view: home, feature: 'home' },
  ] },
  { section: 'Network', items: [
    { path: 'internet', label: 'Internet & home network', icon: 'globe', view: internet },
    // Only with Wi-Fi hardware (F-WIFI-04). Opened directly without hardware, the page explains why.
    { path: 'wifi', label: 'Wi-Fi', icon: 'wifi', view: wifi, feature: 'wifi' },
    { path: 'wireless', label: 'Access points', icon: 'router', view: wireless },
    { path: 'devices', label: 'Devices', icon: 'devices', view: devices },
    { path: 'monitor', label: 'Online-Monitor', icon: 'activity', view: monitor },
    { path: 'dns', label: 'DNS', icon: 'dns', view: dns },
    { path: 'firewall', label: 'Firewall', icon: 'shield', view: firewall },
    { path: 'connections', label: 'Connections', icon: 'arrowRight', view: connections },
    { path: 'network', label: 'Network status', icon: 'globe', view: network },
  ] },
  { section: 'Services', items: [
    { path: 'services', label: 'Services', icon: 'services', view: services },
    // Extensions: only visible when installed (PLAN.md 3.8, /api/state → features)
    { path: 'containers', label: 'Containers', icon: 'container', view: containers, feature: 'containers' },
    { path: 'files', label: 'File share', icon: 'drive', view: files, feature: 'files' },
  ] },
  { section: 'System', items: [
    { path: 'system', label: 'System', icon: 'system', view: system },
    { path: 'logs', label: 'Logs', icon: 'file', view: logs },
    { path: 'tools', label: 'Tools', icon: 'tools', view: tools },
    { path: 'settings', label: 'Settings', icon: 'settings', view: settings },
  ] },
];

const ROUTES = Object.fromEntries(NAV.flatMap((s) => s.items).map((i) => [i.path, i]));
const openPalette = setupPalette(NAV.flatMap((s) => s.items.map((i) => ({ ...i, section: s.section }))));
// Pages without their own menu item; the parent entry is highlighted
const HIDDEN = { device: { path: 'devices', view: device } };

const app = document.getElementById('app');
let cleanup = null;

// --- Color scheme ----------------------------------------------------------------------
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
const themeBtn = h('button', { class: 'btn ghost icon', title: 'Toggle light/dark', onclick: toggleTheme });
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
    clear(app).append(h('div', { class: 'auth-page' }, h('div', { class: 'card auth-card' }, h('h1', null, 'Not reachable'), h('p', { class: 'lead' }, e.message))));
    return;
  }
  await setLang(state.lang);
  app.className = '';
  if (!state.configured) return renderSetup(app, boot);
  if (!state.authenticated) return renderLogin(app, boot);
  renderShell(state);
}

setUnauthorizedHandler(() => {
  if (cleanup) { cleanup(); cleanup = null; }
  toast('The session has expired. Please sign in again.', 'warn');
  boot();
});

// --- Frame with navigation -----------------------------------------------------------
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
        h('div', null, h('div', { class: 'brand-name' }, routerName), h('div', { class: 'brand-sub' }, state.dev ? 'Test instance' : 'Administration'))),
      nav,
      // F-UI-07 (AGPL §13): license and source always reachable
      h('div', { class: 'sidebar-foot' },
        h('a', { href: state.source || '#', target: '_blank', rel: 'noopener noreferrer', title: 'Free software under the GNU AGPL, version 3 or later' },
          `vanilla-router ${state.version || ''} · AGPL-3.0 · source code`))),
    h('div', { class: 'main' },
      h('header', { class: 'topbar' },
        h('button', { class: 'btn ghost icon menu-btn', title: 'Menu', onclick: () => shell.classList.toggle('nav-open') }, icon('menu')),
        h('div', { class: 'title' }, titleEl, subtitleEl),
        h('button', { class: 'btn search-btn', title: 'Quick search (Ctrl+K)', onclick: () => openPalette() }, icon('search'), h('span', { class: 'hide-sm' }, 'Search'), h('kbd', { class: 'hide-sm' }, 'Ctrl K')),
        themeBtn,
        h('button', { class: 'btn ghost', title: 'Sign out', onclick: logout }, icon('logout'), h('span', { class: 'hide-sm' }, 'Sign out'))),
      contentEl));
  shell.addEventListener('click', (e) => { if (e.target === shell) shell.classList.remove('nav-open'); });
  clear(app).append(shell);
  route();
  offerInternetSetup();
}

// Setup step "internet access" (F-UI-08): if the assistant detected PPPoE or no answer,
// the web interface leads there once after login. "Later" applies for one day, the overview
// keeps reminding.
async function offerInternetSetup() {
  let s;
  try { s = await get('/api/settings'); } catch { return; }
  if (!s.needs_setup || location.hash.startsWith('#/internet')) return;
  let later = 0;
  try { later = +localStorage.getItem(SETUP_LATER_KEY) || 0; } catch { /* privater Modus */ }
  if (Date.now() - later > 24 * 3600 * 1000) location.hash = '#/internet?setup=1';
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
  titleEl.textContent = t(entry.view.title);
  subtitleEl.textContent = t(entry.view.subtitle || '');
  document.title = `${t(entry.view.title)} · ${routerName}`;
  clear(contentEl);
  const res = entry.view.render(contentEl);
  if (typeof res === 'function') cleanup = res;
}

window.addEventListener('hashchange', route);
boot();
