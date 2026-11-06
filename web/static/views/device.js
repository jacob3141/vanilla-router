// SPDX-License-Identifier: AGPL-3.0-or-later
import { t } from '../i18n.js';
import { h, clear, icon, fmt, badge, toast, confirmDialog } from '../ui.js';
import { get, post } from '../api.js';
import { displayName, blockText, reservationDialog, deleteReservation, metaDialog, blockDialog } from './devices.js';

// Detail page of a device: #/device?mac=aa:bb:…
export default {
  title: 'Device',
  subtitle: 'All details and actions for a device',
  hidden: true,
  render(root) {
    const mac = new URLSearchParams(location.hash.split('?')[1] || '').get('mac') || '';
    const body = h('div');
    root.append(h('a', { href: '#/devices', class: 'back-link' }, icon('arrowRight', 'rot-left'), 'All devices'), body);
    let pool = null;

    async function load() {
      let all;
      try { all = await get('/api/devices'); } catch (e) { clear(body).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      pool = all.pool;
      const d = all.devices.find((x) => x.mac === mac);
      if (!d) { clear(body).append(h('div', { class: 'card empty' }, `No device with the MAC address ${mac}.`)); return; }
      document.querySelector('.topbar h1').textContent = displayName(d) || d.mac;
      const rows = [
        ['Status', d.online ? badge('online', 'ok') : badge('offline')],
        ['Custom name', d.alias || h('span', { class: 'muted' }, '–')],
        ['Reported by the device', d.hostname || h('span', { class: 'muted' }, '–')],
        ['IP address', h('span', { class: 'mono' }, d.ip || '–')],
        ['DNS name', d.dns_name ? h('span', { class: 'mono' }, d.dns_name) : h('span', { class: 'muted' }, '–')],
        ['MAC address', [h('span', { class: 'mono' }, d.mac), d.random_mac && h('div', { class: 'muted small' }, 'Locally administered address: often randomly generated (e.g. "private Wi-Fi address" on phones). It can change, then reservations and blocks no longer apply.')]],
        ['Address', d.reservation ? [badge('fest', 'info'), ` ${d.reservation.ip}`] : d.lease_state === 'active' ? badge('dynamic') : '–'],
        ['Lease until', d.lease_ends && !d.reservation ? fmt.date(d.lease_ends) : '–'],
        ['First seen', d.first_seen ? [fmt.date(d.first_seen), d.new && h('span', { class: 'badge info small-badge' }, 'new')] : '–'],
        ['Last seen', d.online ? 'now' : d.last_seen ? fmt.date(d.last_seen) : '–'],
        ['Internet', d.block ? badge(blockText(d.block), d.block.enabled ? 'crit' : '') : badge('allowed', 'ok')],
        ['Note', d.note || h('span', { class: 'muted' }, '–')],
      ];
      clear(body).append(h('div', { class: 'grid cols-2' },
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('devices')), h('h2', null, 'Details')),
          h('dl', { class: 'kv' }, rows.flatMap(([k, v]) => [h('dt', null, k), h('dd', null, v)]))),
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('settings')), h('h2', null, 'Actions')),
          h('div', { class: 'action-list' },
            action('edit', 'Name and note', 'Only for this overview', async () => { if (await metaDialog(d)) { toast('Saved.'); load(); } }),
            action('pin', d.reservation ? 'Edit fixed address' : 'Assign a fixed address', 'Always the same IP and a name under home.arpa', async () => { if (await reservationDialog(d, pool)) { toast('Saved.'); load(); } }),
            d.reservation && action('trash', 'Remove fixed address', d.reservation.ip, async () => { if (await deleteReservation(d)) load(); }, 'danger'),
            action('power', 'Wake up (Wake-on-LAN)', 'Sends a wake packet into the home network', wake),
            action('lock', d.block ? 'Change block' : 'Block internet', d.block ? blockText(d.block) : 'Always or at certain times', async () => { if (await blockDialog(d)) { toast('Block saved.'); load(); } }),
            d.block && action('check', 'Remove block', 'Allow internet again', unblock))),
        d.ip && activity(d.ip)));

      async function wake() {
        try { toast((await post('/api/devices/wake', { mac: d.mac })).message); } catch (e) { toast(e.message, 'crit'); }
      }
      async function unblock() {
        if (!(await confirmDialog({ title: 'Remove block?', text: `${displayName(d) || d.mac} may then use the internet again.`, confirm: 'Remove' }))) return;
        try { toast((await post('/api/blocks/delete', { mac: d.mac })).message); } catch (e) { toast(e.message, 'crit'); }
        load();
      }
    }

    load();
  },
};

// Activity: connections and most frequent DNS names of the device (24 h)
function activity(ip) {
  const conns = h('span', { class: 'muted' }, '…');
  const domains = h('tbody', null, h('tr', null, h('td', { class: 'muted' }, 'Loading …')));
  get(`/api/connections?ip=${encodeURIComponent(ip)}&limit=1`).then((c) => {
    const dev = c.devices.find((x) => x.ip === ip);
    conns.textContent = t(dev ? `${dev.total} active (${dev.tcp} TCP, ${dev.udp} UDP)` : 'none');
  }).catch(() => { conns.textContent = t('–'); });
  get(`/api/dns/stats?hours=24&client=${encodeURIComponent(ip)}`).then((s) => {
    clear(domains).append(...(s.top_domains.length ? s.top_domains.slice(0, 12).map((r) => h('tr', null, h('td', { class: 'mono small' }, r.name), h('td', { class: 'num-col' }, String(r.count))))
      : [h('tr', null, h('td', { class: 'muted' }, 'No DNS queries in the last 24 h.'))]));
  }).catch((e) => clear(domains).append(h('tr', null, h('td', { class: 'muted' }, e.message))));
  return h('section', { class: 'card span-2' },
    h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('activity')), h('h2', null, 'Activity'),
      h('a', { class: 'btn sm', href: `#/connections?ip=${encodeURIComponent(ip)}` }, 'View connections')),
    h('dl', { class: 'kv' }, h('dt', null, 'Connections'), h('dd', null, conns)),
    h('h3', { class: 'sub mt' }, 'Most frequent DNS names (24 h)'),
    h('div', { class: 'table-wrap flat' }, h('table', null, domains)));
}

function action(ic, title, sub, onclick, kind) {
  return h('button', { class: `action ${kind || ''}`, onclick }, h('span', { class: 'icon-badge' }, icon(ic)), h('div', null, h('div', { class: 'cell-main' }, title), h('div', { class: 'muted small' }, sub)), icon('arrowRight'));
}
