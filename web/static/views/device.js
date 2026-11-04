// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, clear, icon, fmt, badge, toast, confirmDialog } from '../ui.js';
import { get, post } from '../api.js';
import { displayName, blockText, reservationDialog, deleteReservation, metaDialog, blockDialog } from './devices.js';

// Detailseite eines Geräts: #/device?mac=aa:bb:…
export default {
  title: 'Gerät',
  subtitle: 'Alle Angaben und Aktionen zu einem Gerät',
  hidden: true,
  render(root) {
    const mac = new URLSearchParams(location.hash.split('?')[1] || '').get('mac') || '';
    const body = h('div');
    root.append(h('a', { href: '#/devices', class: 'back-link' }, icon('arrowRight', 'rot-left'), 'Alle Geräte'), body);
    let pool = null;

    async function load() {
      let all;
      try { all = await get('/api/devices'); } catch (e) { clear(body).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      pool = all.pool;
      const d = all.devices.find((x) => x.mac === mac);
      if (!d) { clear(body).append(h('div', { class: 'card empty' }, `Kein Gerät mit der MAC-Adresse ${mac}.`)); return; }
      document.querySelector('.topbar h1').textContent = displayName(d) || d.mac;
      const rows = [
        ['Status', d.online ? badge('online', 'ok') : badge('offline')],
        ['Eigener Name', d.alias || h('span', { class: 'muted' }, '–')],
        ['Vom Gerät gemeldet', d.hostname || h('span', { class: 'muted' }, '–')],
        ['IP-Adresse', h('span', { class: 'mono' }, d.ip || '–')],
        ['DNS-Name', d.dns_name ? h('span', { class: 'mono' }, d.dns_name) : h('span', { class: 'muted' }, '–')],
        ['MAC-Adresse', [h('span', { class: 'mono' }, d.mac), d.random_mac && h('div', { class: 'muted small' }, 'Lokal verwaltete Adresse: oft zufällig erzeugt (z. B. „private WLAN-Adresse“ bei Handys). Kann sich ändern, dann greifen Reservierung und Sperre nicht mehr.')]],
        ['Adresse', d.reservation ? [badge('fest', 'info'), ` ${d.reservation.ip}`] : d.lease_state === 'active' ? badge('dynamisch') : '–'],
        ['Lease bis', d.lease_ends && !d.reservation ? fmt.date(d.lease_ends) : '–'],
        ['Zuerst gesehen', d.first_seen ? [fmt.date(d.first_seen), d.new && h('span', { class: 'badge info small-badge' }, 'neu')] : '–'],
        ['Zuletzt gesehen', d.online ? 'jetzt' : d.last_seen ? fmt.date(d.last_seen) : '–'],
        ['Internet', d.block ? badge(blockText(d.block), d.block.enabled ? 'crit' : '') : badge('erlaubt', 'ok')],
        ['Notiz', d.note || h('span', { class: 'muted' }, '–')],
      ];
      clear(body).append(h('div', { class: 'grid cols-2' },
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('devices')), h('h2', null, 'Angaben')),
          h('dl', { class: 'kv' }, rows.flatMap(([k, v]) => [h('dt', null, k), h('dd', null, v)]))),
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('settings')), h('h2', null, 'Aktionen')),
          h('div', { class: 'action-list' },
            action('edit', 'Name und Notiz', 'Nur für diese Übersicht', async () => { if (await metaDialog(d)) { toast('Gespeichert.'); load(); } }),
            action('pin', d.reservation ? 'Feste Adresse bearbeiten' : 'Feste Adresse vergeben', 'Immer gleiche IP und Name unter home.arpa', async () => { if (await reservationDialog(d, pool)) { toast('Gespeichert.'); load(); } }),
            d.reservation && action('trash', 'Feste Adresse entfernen', d.reservation.ip, async () => { if (await deleteReservation(d)) load(); }, 'danger'),
            action('power', 'Aufwecken (Wake-on-LAN)', 'Schickt ein Weckpaket ins Heimnetz', wake),
            action('lock', d.block ? 'Sperre ändern' : 'Internet sperren', d.block ? blockText(d.block) : 'Immer oder zu bestimmten Zeiten', async () => { if (await blockDialog(d)) { toast('Sperre gespeichert.'); load(); } }),
            d.block && action('check', 'Sperre aufheben', 'Internet wieder erlauben', unblock))),
        d.ip && activity(d.ip)));

      async function wake() {
        try { toast((await post('/api/devices/wake', { mac: d.mac })).message); } catch (e) { toast(e.message, 'crit'); }
      }
      async function unblock() {
        if (!(await confirmDialog({ title: 'Sperre aufheben?', text: `${displayName(d) || d.mac} darf dann wieder ins Internet.`, confirm: 'Aufheben' }))) return;
        try { toast((await post('/api/blocks/delete', { mac: d.mac })).message); } catch (e) { toast(e.message, 'crit'); }
        load();
      }
    }

    load();
  },
};

// Aktivität: Verbindungen und häufigste DNS-Namen des Geräts (24 Std)
function activity(ip) {
  const conns = h('span', { class: 'muted' }, '…');
  const domains = h('tbody', null, h('tr', null, h('td', { class: 'muted' }, 'Lädt …')));
  get(`/api/connections?ip=${encodeURIComponent(ip)}&limit=1`).then((c) => {
    const dev = c.devices.find((x) => x.ip === ip);
    conns.textContent = dev ? `${dev.total} aktiv (${dev.tcp} TCP, ${dev.udp} UDP)` : 'keine';
  }).catch(() => { conns.textContent = '–'; });
  get(`/api/dns/stats?hours=24&client=${encodeURIComponent(ip)}`).then((s) => {
    clear(domains).append(...(s.top_domains.length ? s.top_domains.slice(0, 12).map((r) => h('tr', null, h('td', { class: 'mono small' }, r.name), h('td', { class: 'num-col' }, String(r.count))))
      : [h('tr', null, h('td', { class: 'muted' }, 'Keine DNS-Anfragen in den letzten 24 Std.'))]));
  }).catch((e) => clear(domains).append(h('tr', null, h('td', { class: 'muted' }, e.message))));
  return h('section', { class: 'card span-2' },
    h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('activity')), h('h2', null, 'Aktivität'),
      h('a', { class: 'btn sm', href: `#/connections?ip=${encodeURIComponent(ip)}` }, 'Verbindungen ansehen')),
    h('dl', { class: 'kv' }, h('dt', null, 'Verbindungen'), h('dd', null, conns)),
    h('h3', { class: 'sub mt' }, 'Häufigste DNS-Namen (24 Std)'),
    h('div', { class: 'table-wrap flat' }, h('table', null, domains)));
}

function action(ic, title, sub, onclick, kind) {
  return h('button', { class: `action ${kind || ''}`, onclick }, h('span', { class: 'icon-badge' }, icon(ic)), h('div', null, h('div', { class: 'cell-main' }, title), h('div', { class: 'muted small' }, sub)), icon('arrowRight'));
}
