// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, every, clear, icon, badge } from '../ui.js';
import { get } from '../api.js';

const NEIGH = { REACHABLE: 'ok', STALE: '', DELAY: 'warn', PROBE: 'warn', FAILED: 'crit', INCOMPLETE: 'warn', PERMANENT: 'info', NOARP: '' };

// The Linux names mean nothing to the user
function port(dev) {
  if (dev === 'wan0') return 'WAN';
  if (dev === 'ppp0') return 'WAN (PPPoE)';
  if (dev === 'lan0') return 'LAN';
  const m = /^lanp(\d+)$/.exec(dev || '');
  return m ? `LAN ${m[1]}` : dev || '–';
}

export default {
  title: 'Routes',
  subtitle: 'Where the router sends traffic, and its neighbors in the network',
  render(root) {
    const routes = h('div');
    const ipv6 = h('div');
    const neigh = h('tbody');
    root.append(
      h('section', { class: 'card' }, head('arrowRight', 'Routes'), routes),
      h('section', { class: 'card mt' }, head('globe', 'IPv6'), ipv6),
      h('section', { class: 'card mt' }, head('devices', 'Neighbor table (ARP/NDP)'),
        h('div', { class: 'table-wrap' }, h('table', null, h('thead', null, h('tr', null, ['Address', 'MAC', 'Port', 'State'].map((x) => h('th', null, x)))), neigh))));

    async function load() {
      let n;
      try { n = await get('/api/network'); } catch { return; }
      const addrs = Object.fromEntries(n.addrs.map((a) => [a.ifname, a]));
      clear(routes).append(h('div', { class: 'grid cols-2' },
        routeTable('IPv4', n.routes4), routeTable('IPv6', n.routes6.filter((r) => !r.dst?.startsWith('fe80') && !r.dst?.startsWith('ff00')))));

      const global6 = (dev) => (addrs[dev]?.addr_info || []).filter((x) => x.family === 'inet6' && x.scope === 'global');
      const list = (a) => a.map((x) => h('div', { class: 'mono small' }, `${x.local}/${x.prefixlen}`));
      const wanV6 = global6('wan0');
      const lanV6 = global6('lan0');
      const def6 = n.routes6.find((r) => r.dst === 'default');
      clear(ipv6).append(h('dl', { class: 'kv' },
        h('dt', null, 'WAN addresses'), h('dd', null, wanV6.length ? list(wanV6) : '–'),
        h('dt', null, 'Default route'), h('dd', null, def6 ? h('span', { class: 'mono small' }, `via ${def6.gateway} (${port(def6.dev)})`) : badge('none', 'warn')),
        h('dt', null, 'Forwarding'), h('dd', null, n.sysctl['net.ipv6.conf.all.forwarding'] === '1' ? badge('on', 'ok') : badge('off')),
        h('dt', null, 'LAN prefix'), h('dd', null, lanV6.length ? list(lanV6) : [badge('none', 'warn'), h('div', { class: 'muted small' }, 'Devices in the home network get no public IPv6 addresses. For that the upstream router would have to pass a prefix to this router through prefix delegation.')])));

      clear(neigh).append(...n.neighbors.filter((x) => x.lladdr).sort((a, b) => (a.dev + a.dst).localeCompare(b.dev + b.dst, 'en', { numeric: true })).map((x) => h('tr', null,
        h('td', { class: 'mono small' }, x.dst), h('td', { class: 'mono small' }, x.lladdr), h('td', null, port(x.dev)),
        h('td', null, badge((x.state || []).join(' ') || '–', NEIGH[(x.state || [])[0]] || '')))));
    }

    load();
    return every(30000, load);
  },
};

function head(ic, title) { return h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon(ic)), h('h2', null, title)); }

function routeTable(title, list) {
  return h('div', null, h('h3', { class: 'sub' }, title), h('div', { class: 'table-wrap flat' }, h('table', null,
    h('thead', null, h('tr', null, ['Target', 'via', 'Port', 'Source'].map((x) => h('th', null, x)))),
    h('tbody', null, list.map((r) => h('tr', null,
      h('td', { class: 'mono small' }, r.dst === 'default' ? 'Default' : r.dst), h('td', { class: 'mono small' }, r.gateway || '–'),
      h('td', null, port(r.dev)), h('td', { class: 'muted small' }, r.protocol || '–')))))));
}
