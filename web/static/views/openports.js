// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, every, clear, icon, badge } from '../ui.js';
import { get } from '../api.js';

const SCOPE = { all: ['all addresses', 'warn'], lan: ['Home network', 'info'], local: ['local only', ''] };

export default {
  title: 'Open ports',
  subtitle: 'Services on the router waiting for connections',
  render(root) {
    const ports = h('tbody');
    root.append(h('section', { class: 'card' },
      h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('shield')), h('h2', null, 'Open ports of the router')),
      h('p', { class: 'muted small' }, 'None of them can be reached from the internet, the firewall drops incoming connections on the internet port for IPv4 and IPv6.'),
      h('div', { class: 'table-wrap' }, h('table', null, h('thead', null, h('tr', null, ['Port', 'Protocol', 'Address', 'Service', 'Reachable from'].map((x) => h('th', null, x)))), ports))));

    async function load() {
      let p;
      try { p = await get('/api/ports'); } catch { return; }
      clear(ports).append(...p.map((x) => h('tr', null,
        h('td', { class: 'mono' }, String(x.port)), h('td', null, x.proto.toUpperCase()), h('td', { class: 'mono small' }, x.addr),
        h('td', null, x.process || '–'), h('td', null, badge(...SCOPE[x.scope])))));
    }

    load();
    return every(30000, load);
  },
};
