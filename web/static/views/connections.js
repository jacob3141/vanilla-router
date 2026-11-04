// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, every, clear, icon, badge, fmt } from '../ui.js';
import { get } from '../api.js';

const WELL_KNOWN = { 53: 'DNS', 80: 'HTTP', 443: 'HTTPS', 123: 'NTP', 22: 'SSH', 445: 'SMB', 853: 'DNS über TLS', 1883: 'MQTT', 8883: 'MQTT/TLS', 5353: 'mDNS', 3478: 'STUN', 993: 'IMAPS', 587: 'SMTP', 5223: 'Apple Push', 8080: 'HTTP-Alt', 19302: 'Google STUN' };

// Aktive Verbindungen aus der Conntrack-Tabelle, wie unter LuCI „Verbindungen“
export default {
  title: 'Verbindungen',
  subtitle: 'Aktive Verbindungen durch den Router (Conntrack)',
  render(root) {
    let ip = new URLSearchParams(location.hash.split('?')[1] || '').get('ip') || '';
    const summary = h('div', { class: 'muted small' });
    const devs = h('div', { class: 'chip-list' });
    const tbody = h('tbody');
    root.append(
      h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('devices')), h('h2', null, 'Geräte mit den meisten Verbindungen'), summary), devs),
      h('section', { class: 'card mt' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('arrowRight')), h('h2', null, 'Verbindungen')),
        h('div', { class: 'table-wrap' }, h('table', null,
          h('thead', null, h('tr', null, ['Protokoll', 'Von', 'Nach', 'Dienst', 'Zustand', 'läuft ab in'].map((t) => h('th', null, t)))), tbody))));

    async function load() {
      let d;
      try { d = await get(`/api/connections${ip ? `?ip=${encodeURIComponent(ip)}` : ''}`); } catch (e) { clear(tbody).append(h('tr', null, h('td', { colspan: 6, class: 'empty' }, e.message))); return; }
      const name = (a) => d.names[a];
      summary.textContent = `${fmt.num(d.total, 0)} Verbindungen insgesamt`;
      clear(devs).append(
        h('button', { class: `chip ${ip ? '' : 'active'}`, onclick: () => { ip = ''; load(); } }, 'Alle'),
        ...d.devices.slice(0, 24).map((x) => h('button', { class: `chip ${ip === x.ip ? 'active' : ''}`, onclick: () => { ip = x.ip; load(); } },
          x.name || x.ip, h('span', { class: 'chip-count' }, String(x.total)))));
      clear(tbody);
      if (!d.connections.length) tbody.append(h('tr', null, h('td', { colspan: 6, class: 'empty' }, 'Keine Verbindungen.')));
      for (const c of d.connections) {
        const dst = c.dnat && c.reply_src ? `${c.reply_src}` : c.dst;
        tbody.append(h('tr', null,
          h('td', null, badge(c.proto.toUpperCase())),
          h('td', null, endpoint(c.src, c.sport, name(c.src))),
          h('td', null, endpoint(dst, c.dport, name(dst)), c.dnat && h('div', { class: 'muted small' }, `Portweiterleitung von ${c.dst}`)),
          h('td', { class: 'muted small' }, WELL_KNOWN[c.dport] || ''),
          h('td', null, c.state ? badge(c.state === 'ESTABLISHED' ? 'verbunden' : c.state.toLowerCase(), c.state === 'ESTABLISHED' ? 'ok' : '') : c.assured ? badge('aktiv', 'ok') : badge('neu')),
          h('td', { class: 'muted small num' }, fmt.duration(c.timeout))));
      }
    }

    load();
    return every(15000, load);
  },
};

function endpoint(addr, port, name) {
  return h('div', null, h('span', { class: 'mono small' }, port ? `${addr.includes(':') ? `[${addr}]` : addr}:${port}` : addr), name && h('div', { class: 'muted small' }, name));
}
