// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, every, clear, icon, fmt, badge } from '../ui.js';
import { get } from '../api.js';

const SCOPE = { all: ['alle Adressen', 'warn'], lan: ['Heimnetz', 'info'], local: ['nur lokal', ''] };
const NEIGH = { REACHABLE: 'ok', STALE: '', DELAY: 'warn', PROBE: 'warn', FAILED: 'crit', INCOMPLETE: 'warn', PERMANENT: 'info', NOARP: '' };

export default {
  title: 'Netzwerk-Status',
  subtitle: 'Schnittstellen, Routen, Nachbarn, IPv6 und offene Ports',
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
        ['Netz', s.subnet || '–'],
        ['Gateway', s['option routers'] || '–'],
        ['DNS-Server', s['option domain-name-servers'] || '–'],
        ['Suchdomain', s['option domain-name'] || '–'],
        ['Dynamischer Bereich', `${d.pool.start} – ${d.pool.end}`],
        ['Feste Adressen', String(d.reservations)],
        ['Lease-Dauer', `${lease(s['default-lease-time'])} (höchstens ${lease(s['max-lease-time'])})`],
        ['Autoritativ', s.authoritative !== undefined ? 'ja' : 'nein'],
      ];
      clear(dhcp).append(...rows.flatMap(([k, v]) => [h('dt', null, k), h('dd', null, v)]));
    }).catch(() => {});

    root.append(
      ifaces,
      h('div', { class: 'grid cols-2 mt' },
        card('home', 'DHCP-Einstellungen', dhcp),
        card('globe', 'IPv6', ipv6),
        card('shield', 'Offene Ports des Routers', h('p', { class: 'muted small' }, 'Dienste, die auf dem Router auf Verbindungen warten. Aus dem Internet ist keiner davon erreichbar, die Firewall verwirft eingehende Verbindungen auf wan0 für IPv4 und IPv6.'),
          h('div', { class: 'table-wrap' }, h('table', null, h('thead', null, h('tr', null, ['Port', 'Protokoll', 'Adresse', 'Dienst', 'Erreichbar aus'].map((t) => h('th', null, t)))), ports))),
        h('section', { class: 'card span-2' }, head('arrowRight', 'Routen'), routes),
        h('section', { class: 'card span-2' }, head('devices', 'Nachbartabelle (ARP/NDP)'),
          h('div', { class: 'table-wrap' }, h('table', null, h('thead', null, h('tr', null, ['Adresse', 'MAC', 'Schnittstelle', 'Zustand'].map((t) => h('th', null, t)))), neigh)))));

    async function load() {
      const [n, p] = await Promise.all([get('/api/network').catch(() => null), get('/api/ports').catch(() => [])]);
      if (!n) return;
      const addrs = Object.fromEntries(n.addrs.map((a) => [a.ifname, a]));
      clear(ifaces);
      for (const l of n.links.filter((x) => ['wan0', 'lan0'].includes(x.ifname) || (x.ifname !== 'lo' && x.operstate === 'UP' && !x.ifname.startsWith('veth')))) {
        const a = addrs[l.ifname]?.addr_info || [];
        const st = l.stats64 || {};
        const rows = [
          ['Zustand', badge(l.operstate === 'UP' ? 'verbunden' : l.operstate, l.operstate === 'UP' ? 'ok' : 'crit')],
          ['MAC', h('span', { class: 'mono' }, l.address || '–')],
          ['MTU', String(l.mtu)],
          ['Adressen', a.length ? a.map((x) => h('div', { class: 'mono small' }, `${x.local}/${x.prefixlen}`, x.scope === 'link' ? h('span', { class: 'muted' }, ' (link-local)') : '', x.dynamic ? h('span', { class: 'muted' }, ' (dynamisch)') : '')) : '–'],
          ['Empfangen', `${fmt.bytes(st.rx?.bytes || 0)} · ${fmt.num(st.rx?.packets || 0, 0)} Pakete`],
          ['Gesendet', `${fmt.bytes(st.tx?.bytes || 0)} · ${fmt.num(st.tx?.packets || 0, 0)} Pakete`],
          ['Fehler', errCell(st)],
        ];
        ifaces.append(h('section', { class: 'card' }, head(l.ifname === 'wan0' ? 'globe' : l.ifname === 'lan0' ? 'home' : 'devices', l.ifname === 'wan0' ? 'wan0 · Internet' : l.ifname === 'lan0' ? 'lan0 · Heimnetz' : l.ifname),
          h('dl', { class: 'kv' }, rows.flatMap(([k, v]) => [h('dt', null, k), h('dd', null, v)]))));
      }

      clear(routes).append(h('div', { class: 'grid cols-2' },
        routeTable('IPv4', n.routes4), routeTable('IPv6', n.routes6.filter((r) => !r.dst?.startsWith('fe80') && !r.dst?.startsWith('ff00')))));

      const wanV6 = (addrs.wan0?.addr_info || []).filter((x) => x.family === 'inet6' && x.scope === 'global');
      const lanV6 = (addrs.lan0?.addr_info || []).filter((x) => x.family === 'inet6' && x.scope === 'global');
      const def6 = n.routes6.find((r) => r.dst === 'default');
      clear(ipv6).append(h('dl', { class: 'kv' },
        h('dt', null, 'WAN-Adressen'), h('dd', null, wanV6.length ? wanV6.map((x) => h('div', { class: 'mono small' }, `${x.local}/${x.prefixlen}`)) : '–'),
        h('dt', null, 'Standard-Route'), h('dd', null, def6 ? h('span', { class: 'mono small' }, `über ${def6.gateway} (${def6.dev})`) : badge('keine', 'warn')),
        h('dt', null, 'Weiterleitung'), h('dd', null, n.sysctl['net.ipv6.conf.all.forwarding'] === '1' ? badge('an', 'ok') : badge('aus')),
        h('dt', null, 'LAN-Präfix'), h('dd', null, lanV6.length ? lanV6.map((x) => h('div', { class: 'mono small' }, `${x.local}/${x.prefixlen}`)) : [badge('keins', 'warn'), h('div', { class: 'muted small' }, 'Geräte im Heimnetz bekommen keine öffentlichen IPv6-Adressen. Dafür müsste der vorgeschaltete Router per Prefix Delegation ein Präfix an diesen Router weitergeben.')])));

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
  return [badge(`${fmt.num(e, 0)} Fehler`, e ? 'warn' : 'ok'), ' ', badge(`${fmt.num(d, 0)} verworfen`, d ? '' : 'ok')];
}
function routeTable(title, list) {
  return h('div', null, h('h3', { class: 'sub' }, title), h('div', { class: 'table-wrap flat' }, h('table', null,
    h('thead', null, h('tr', null, ['Ziel', 'über', 'Schnittstelle', 'Herkunft'].map((t) => h('th', null, t)))),
    h('tbody', null, list.map((r) => h('tr', null,
      h('td', { class: 'mono small' }, r.dst === 'default' ? 'Standard' : r.dst), h('td', { class: 'mono small' }, r.gateway || '–'),
      h('td', null, r.dev || '–'), h('td', { class: 'muted small' }, r.protocol || '–')))))));
}
