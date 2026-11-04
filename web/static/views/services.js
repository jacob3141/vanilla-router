// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, every, clear, icon, badge, toast, confirmDialog, logDialog, stateKind, STATE_LABEL, fmt } from '../ui.js';
import { get, post } from '../api.js';

const WARN = {
  docker: 'Alle Container werden dabei neu gestartet und sind kurz nicht erreichbar.',
  'vr-dnsmasq': 'DNS ist für ein bis zwei Sekunden weg, Geräte bekommen kurz keine neue Adresse. Bestehende Adressen bleiben gültig.',
  ssh: 'Bestehende SSH-Verbindungen bleiben bestehen.',
  'systemd-networkd': 'Das Netzwerk wird neu eingerichtet. Internet und Heimnetz können kurz ausfallen.',
  'vr-firewall': 'Die Firewall wird neu geladen (IPv4 und IPv6).',
  'vr-pppoe': 'Die Internet-Verbindung wird neu aufgebaut. Das dauert einige Sekunden, die IPv4-Adresse kann sich ändern.',
};

export default {
  title: 'Dienste',
  subtitle: 'Systemdienste des Routers, Neustart und Logs',
  render(root) {
    const tbody = h('tbody');
    const banner = h('div');
    root.append(banner, h('section', { class: 'card' },
      h('div', { class: 'table-wrap' }, h('table', null,
        h('thead', null, h('tr', null, ['', 'Dienst', 'Status', 'Seit', 'Autostart', ''].map((t) => h('th', null, t)))),
        tbody))));

    let last = '';
    async function load() {
      let list;
      try { list = await get('/api/services'); } catch (e) { clear(banner).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      clear(banner);
      // Nur neu zeichnen, wenn sich etwas geändert hat, sonst gehen Klicks während des Aufbaus verloren
      const key = JSON.stringify(list);
      if (key === last) return;
      last = key;
      clear(tbody).append(...list.map((u) => h('tr', null,
        h('td', null, h('span', { class: `dot ${stateKind(u.active)}` })),
        h('td', null, h('div', { class: 'cell-main mono' }, u.name), h('div', { class: 'muted small' }, u.description)),
        h('td', null, badge(STATE_LABEL[u.active] || u.active, stateKind(u.active)), u.sub && !['running', 'exited', 'dead', u.active].includes(u.sub) && h('span', { class: 'muted small' }, ` ${u.sub}`)),
        h('td', { class: 'muted small' }, sinceText(u.since)),
        h('td', null, u.enabled === 'enabled' ? badge('an', 'ok') : badge(u.enabled || '–')),
        h('td', { class: 'actions' },
          h('button', { class: 'btn sm', onclick: () => logDialog(`Logs: ${u.name}`, async () => (await get(`/api/services/logs?unit=${encodeURIComponent(u.name)}&lines=300`)).text) }, icon('file'), 'Logs'),
          u.restartable && h('button', { class: 'btn sm', onclick: () => restart(u) }, icon('refresh'), 'Neu starten')))));
    }

    // systemctl liefert "@<unix-sekunden>" (Option --timestamp=unix im Helper)
    function sinceText(v) {
      const m = /^@(\d+)/.exec(v || '');
      return m && m[1] !== '0' ? fmt.date(new Date(+m[1] * 1000).toISOString()) : '–';
    }

    async function restart(u) {
      if (!(await confirmDialog({ title: `${u.name} neu starten?`, text: WARN[u.name] || 'Der Dienst wird kurz unterbrochen.', confirm: 'Neu starten' }))) return;
      try { toast((await post('/api/services/restart', { unit: u.name })).message); } catch (e) { toast(e.message, 'crit'); }
      last = '';
      load();
    }

    load();
    return every(15000, load);
  },
};
