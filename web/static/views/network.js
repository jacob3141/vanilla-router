// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, every, clear, icon, fmt, badge } from '../ui.js';
import { get } from '../api.js';

const SCOPE = { all: ['all addresses', 'warn'], lan: ['Home network', 'info'], local: ['local only', ''] };
const NEIGH = { REACHABLE: 'ok', STALE: '', DELAY: 'warn', PROBE: 'warn', FAILED: 'crit', INCOMPLETE: 'warn', PERMANENT: 'info', NOARP: '' };

export default {
  title: 'Network status',
  subtitle: 'Interfaces, routes, neighbors, IPv6 and open ports',
  render(root) {
    const ifaces = h('div', { class: 'grid cols-2' });
    const routes = h('div');
    const ipv6 = h('div');
    const ports = h('tbody');
    const neigh = h('tbody');
    const dhcp = h('dl', { class: 'kv' });
    get('/api/dhcp/settings').then((d) => {
      const s = d.settings;
      const lease = (v) => (v ? fmt.duration(+v) : '–');
      const rows = [
        ['Network', s.subnet || '–'],
        ['Gateway', s['option routers'] || '–'],
        ['DNS-Server', s['option domain-name-servers'] || '–'],
        ['Search domain', s['option domain-name'] || '–'],
        ['Dynamic range', `${d.pool.start} – ${d.pool.end}`],
        ['Fixed addresses', String(d.reservations)],
        ['Lease duration', `${lease(s['default-lease-time'])} (at most ${lease(s['max-lease-time'])})`],
        ['Authoritative', s.authoritative !== undefined ? 'yes' : 'no'],
      ];
      clear(dhcp).append(...rows.flatMap(([k, v]) => [h('dt', null, k), h('dd', null, v)]));
    }).catch(() => {});

    root.append(
      ifaces,
      h('div', { class: 'grid cols-2 mt' },
        card('home', 'DHCP settings', dhcp),
        card('globe', 'IPv6', ipv6),
        card('shield', 'Open ports of the router', h('p', { class: 'muted small' }, 'Services on the router waiting for connections. None of them can be reached from the internet, the firewall drops incoming connections on wan0 for IPv4 and IPv6.'),
          h('div', { class: 'table-wrap' }, h('table', null, h('thead', null, h('tr', null, ['Port', 'Protocol', 'Address', 'Service', 'Reachable from'].map((t) => h('th', null, t)))), ports))),
        h('section', { class: 'card span-2' }, head('arrowRight', 'Routes'), routes),
        h('section', { class: 'card span-2' }, head('devices', 'Neighbor table (ARP/NDP)'),
          h('div', { class: 'table-wrap' }, h('table', null, h('thead', null, h('tr', null, ['Address', 'MAC', 'Interface', 'State'].map((t) => h('th', null, t)))), neigh)))));

    async function load() {
      const [n, p] = await Promise.all([get('/api/network').catch(() => null), get('/api/ports').catch(() => [])]);
      if (!n) return;
      const addrs = Object.fromEntries(n.addrs.map((a) => [a.ifname, a]));
      clear(ifaces);
      for (const l of n.links.filter((x) => ['wan0', 'lan0'].includes(x.ifname) || (x.ifname !== 'lo' && x.operstate === 'UP' && !x.ifname.startsWith('veth')))) {
        const a = addrs[l.ifname]?.addr_info || [];
        const st = l.stats64 || {};
        const rows = [
          ['State', badge(l.operstate === 'UP' ? 'connected' : l.operstate, l.operstate === 'UP' ? 'ok' : 'crit')],
          ['MAC', h('span', { class: 'mono' }, l.address || '–')],
          ['MTU', String(l.mtu)],
          ['Addresses', a.length ? a.map((x) => h('div', { class: 'mono small' }, `${x.local}/${x.prefixlen}`, x.scope === 'link' ? h('span', { class: 'muted' }, ' (link-local)') : '', x.dynamic ? h('span', { class: 'muted' }, ' (dynamic)') : '')) : '–'],
          ['Received', `${fmt.bytes(st.rx?.bytes || 0)} · ${fmt.num(st.rx?.packets || 0, 0)} packets`],
          ['Sent', `${fmt.bytes(st.tx?.bytes || 0)} · ${fmt.num(st.tx?.packets || 0, 0)} packets`],
          ['Errors', errCell(st)],
        ];
        ifaces.append(h('section', { class: 'card' }, head(l.ifname === 'wan0' ? 'globe' : l.ifname === 'lan0' ? 'home' : 'devices', l.ifname === 'wan0' ? 'wan0 · Internet' : l.ifname === 'lan0' ? 'lan0 · home network' : l.ifname),
          h('dl', { class: 'kv' }, rows.flatMap(([k, v]) => [h('dt', null, k), h('dd', null, v)]))));
      }

      clear(routes).append(h('div', { class: 'grid cols-2' },
        routeTable('IPv4', n.routes4), routeTable('IPv6', n.routes6.filter((r) => !r.dst?.startsWith('fe80') && !r.dst?.startsWith('ff00')))));

      const wanV6 = (addrs.wan0?.addr_info || []).filter((x) => x.family === 'inet6' && x.scope === 'global');
      const lanV6 = (addrs.lan0?.addr_info || []).filter((x) => x.family === 'inet6' && x.scope === 'global');
      const def6 = n.routes6.find((r) => r.dst === 'default');
      clear(ipv6).append(h('dl', { class: 'kv' },
        h('dt', null, 'WAN addresses'), h('dd', null, wanV6.length ? wanV6.map((x) => h('div', { class: 'mono small' }, `${x.local}/${x.prefixlen}`)) : '–'),
        h('dt', null, 'Standard-Route'), h('dd', null, def6 ? h('span', { class: 'mono small' }, `via ${def6.gateway} (${def6.dev})`) : badge('none', 'warn')),
        h('dt', null, 'Forwarding'), h('dd', null, n.sysctl['net.ipv6.conf.all.forwarding'] === '1' ? badge('an', 'ok') : badge('aus')),
        h('dt', null, 'LAN prefix'), h('dd', null, lanV6.length ? lanV6.map((x) => h('div', { class: 'mono small' }, `${x.local}/${x.prefixlen}`)) : [badge('none', 'warn'), h('div', { class: 'muted small' }, 'Devices in the home network get no public IPv6 addresses. For that the upstream router would have to pass a prefix to this router through prefix delegation.')])));

      clear(ports).append(...p.map((x) => h('tr', null,
        h('td', { class: 'mono' }, String(x.port)), h('td', null, x.proto.toUpperCase()), h('td', { class: 'mono small' }, x.addr),
        h('td', null, x.process || '–'), h('td', null, badge(...SCOPE[x.scope])))));

      clear(neigh).append(...n.neighbors.filter((x) => x.lladdr).sort((a, b) => (a.dev + a.dst).localeCompare(b.dev + b.dst, 'de', { numeric: true })).map((x) => h('tr', null,
        h('td', { class: 'mono small' }, x.dst), h('td', { class: 'mono small' }, x.lladdr), h('td', null, x.dev),
        h('td', null, badge((x.state || []).join(' ') || '–', NEIGH[(x.state || [])[0]] || '')))));
    }

    load();
    return every(30000, load);
  },
};

function head(ic, title) { return h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon(ic)), h('h2', null, title)); }
function card(ic, title, ...body) { return h('section', { class: 'card' }, head(ic, title), ...body); }
function errCell(st) {
  const e = (st.rx?.errors || 0) + (st.tx?.errors || 0);
  const d = (st.rx?.dropped || 0) + (st.tx?.dropped || 0);
  return [badge(`${fmt.num(e, 0)} errors`, e ? 'warn' : 'ok'), ' ', badge(`${fmt.num(d, 0)} dropped`, d ? '' : 'ok')];
}
function routeTable(title, list) {
  return h('div', null, h('h3', { class: 'sub' }, title), h('div', { class: 'table-wrap flat' }, h('table', null,
    h('thead', null, h('tr', null, ['Target', 'via', 'Interface', 'Source'].map((t) => h('th', null, t)))),
    h('tbody', null, list.map((r) => h('tr', null,
      h('td', { class: 'mono small' }, r.dst === 'default' ? 'Standard' : r.dst), h('td', { class: 'mono small' }, r.gateway || '–'),
      h('td', null, r.dev || '–'), h('td', { class: 'muted small' }, r.protocol || '–')))))));
}
