// SPDX-License-Identifier: AGPL-3.0-or-later
import { t } from '../i18n.js';
import { h, every, clear, icon, fmt, badge, toast, confirmDialog, formDialog } from '../ui.js';
import { get, post } from '../api.js';

export const DAYS = [['1', 'Mon'], ['2', 'Tue'], ['3', 'Wed'], ['4', 'Thu'], ['5', 'Fri'], ['6', 'Sat'], ['0', 'Sun']];

export function displayName(d) {
  return d.alias || d.reservation?.name || d.hostname || '';
}

export function blockText(b) {
  if (!b) return '';
  if (!b.enabled) return 'Block paused';
  if (b.mode === 'always') return 'Internet blocked';
  const days = b.days?.length && b.days.length < 7 ? ` (${DAYS.filter(([v]) => b.days.includes(+v)).map(([, l]) => l).join(', ')})` : '';
  return `blocked ${b.from}–${b.to}${days}`;
}

// --- Dialogs, also used by the detail page ---------------------------------------

export async function reservationDialog(d, pool) {
  const r = d?.reservation;
  const suggestion = (r?.name || d?.alias || d?.hostname || '').toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').replace(/^-+|-+$/g, '').slice(0, 63);
  return formDialog({
    title: r ? `Fixed address for ${r.name}` : 'Assign a fixed address',
    text: 'The device always gets the same IP address and is reachable at <name>.home.arpa. The address is left out of the dynamic range.',
    fields: [
      { name: 'name', label: 'Name', value: suggestion, placeholder: 'e.g. printer-basement', hint: 'Lowercase letters, digits and hyphens. Becomes name.home.arpa.' },
      { name: 'mac', label: 'MAC address', value: r?.mac || d?.mac || '', placeholder: 'aa:bb:cc:dd:ee:ff' },
      { name: 'ip', label: 'IP address', value: r?.ip || d?.ip || '', placeholder: 'e.g. 192.168.42.20', hint: pool ? `Dynamic range: ${pool.start} – ${pool.end}. Any free address in the home network is possible.` : '' },
      { name: 'comment', label: 'Note (optional)', value: r?.comment || '', placeholder: 'e.g. 3D printer in the basement' },
    ],
    onSubmit: (v) => post('/api/reservations', { ...v, original_mac: r?.mac || '' }),
  });
}

export async function deleteReservation(d) {
  const r = d.reservation;
  if (!(await confirmDialog({ title: `Remove fixed address for ${r.name}?`, text: `${r.ip} is assigned dynamically again, the name ${r.name}.home.arpa disappears. The device keeps its address until the end of the lease.`, confirm: 'Remove', danger: true }))) return false;
  try { toast((await post('/api/reservations/delete', { mac: r.mac })).message); return true; } catch (e) { toast(e.message, 'crit'); return false; }
}

export function metaDialog(d) {
  return formDialog({
    title: 'Name and note',
    text: 'Only for the overview in the web interface. For a name on the network (name.home.arpa) assign a fixed address.',
    fields: [
      { name: 'alias', label: 'Name', value: d.alias || '', placeholder: d.hostname || 'e.g. Laptop office' },
      { name: 'note', label: 'Note', value: d.note || '', placeholder: 'e.g. living room, belongs to a guest' },
    ],
    onSubmit: (v) => post('/api/devices/meta', { mac: d.mac, ...v }),
  });
}

export function blockDialog(d) {
  const b = d.block;
  const days = b?.days?.length ? b.days : [0, 1, 2, 3, 4, 5, 6];
  return formDialog({
    title: `Internet access for ${displayName(d) || d.mac}`,
    text: 'The device then no longer gets to the internet. The home network (printer, file share, this router) stays reachable. Times are in the router\'s local time.',
    fields: [
      { name: 'mode', label: 'Block', type: 'select', value: b?.mode || 'always', options: [{ value: 'always', label: 'Always blocked' }, { value: 'schedule', label: 'Blocked at certain times' }] },
      { name: 'from', label: 'Blocked from (only with times)', value: b?.from || '21:00', placeholder: 'HH:MM' },
      { name: 'to', label: 'Blocked until', value: b?.to || '07:00', placeholder: 'HH:MM', hint: 'Across midnight also works, e.g. 21:00 to 07:00. The weekdays mean the start: "Sun–Thu" blocks the nights before school days until Friday morning.' },
      ...DAYS.map(([v, l]) => ({ name: `day${v}`, label: l, type: 'checkbox', value: days.includes(+v) })),
      { name: 'enabled', label: 'Block active', type: 'checkbox', value: b ? b.enabled : true },
    ],
    submit: 'Save block',
    onSubmit: (v) => {
      const sel = DAYS.filter(([n]) => v[`day${n}`]).map(([n]) => +n);
      if (v.mode === 'schedule' && !sel.length) throw new Error('Select at least one weekday.');
      return post('/api/blocks', {
        mac: d.mac, name: (displayName(d) || '').replace(/[^\p{L}\p{N} ._,:()/+-]/gu, '').slice(0, 64), mode: v.mode,
        from: v.mode === 'schedule' ? v.from : '', to: v.mode === 'schedule' ? v.to : '',
        days: v.mode === 'schedule' && sel.length < 7 ? sel : [], enabled: v.enabled,
      });
    },
  });
}

// --- List -----------------------------------------------------------------------------

export default {
  title: 'Devices',
  subtitle: 'Devices in the home network, DHCP leases and fixed addresses',
  render(root) {
    let devices = [];
    let pool = null;
    let filter = '';
    let view = 'all';

    const search = h('input', { class: 'input', type: 'search', placeholder: 'Search by name, IP or MAC …', oninput: (e) => { filter = e.target.value.toLowerCase(); draw(); } });
    const viewSeg = h('div', { class: 'seg' }, [['all', 'All'], ['online', 'Online'], ['new', 'New'], ['blocked', 'Blocked']].map(([v, l]) =>
      h('button', { class: v === view ? 'active' : '', 'data-v': v, onclick: () => { view = v; for (const b of viewSeg.children) b.classList.toggle('active', b.dataset.v === v); draw(); } }, l)));
    const counts = h('span', { class: 'muted small' });
    const tbody = h('tbody');
    const banner = h('div');

    root.append(banner, h('section', { class: 'card' },
      h('div', { class: 'card-head' },
        h('div', { class: 'row', style: { flex: '1', minWidth: '0' } }, h('div', { style: { flex: '1', minWidth: '220px', maxWidth: '420px' } }, search), viewSeg, counts),
        h('button', { class: 'btn', title: 'Download as a CSV file', onclick: exportCSV }, icon('download'), 'CSV'),
        h('button', { class: 'btn primary', onclick: async () => { if (await reservationDialog(null, pool)) { toast('Saved.'); load(true); } } }, icon('plus'), 'Fixed address')),
      h('div', { class: 'table-wrap' }, h('table', null,
        h('thead', null, h('tr', null, ['', 'Name', 'IP address', 'MAC address', 'Address', 'Last seen', ''].map((t) => h('th', null, t)))),
        tbody))));

    let last = '';
    async function load(force) {
      try {
        const d = await get('/api/devices');
        // Only redraw if something has changed, otherwise clicks during the rebuild get lost
        const key = JSON.stringify(d);
        if (key === last && !force) return;
        last = key;
        devices = d.devices;
        pool = d.pool;
        clear(banner);
        if (d.dry_run) banner.append(h('div', { class: 'banner warn' }, icon('info'), 'Test instance: changes are checked, but not applied.'));
        draw();
      } catch (e) {
        clear(banner).append(h('div', { class: 'banner crit' }, icon('alert'), e.message));
      }
    }

    function visible() {
      return devices.filter((d) => {
        if (view === 'online' && !d.online) return false;
        if (view === 'new' && !d.new) return false;
        if (view === 'blocked' && !d.block?.enabled) return false;
        if (!filter) return true;
        return [d.alias, d.hostname, d.reservation?.name, d.dns_name, d.ip, d.mac, d.note].some((v) => v && v.toLowerCase().includes(filter));
      });
    }

    function draw() {
      const rows = visible();
      counts.textContent = t(`${devices.filter((d) => d.online).length} online · ${devices.filter((d) => d.reservation).length} fixed · ${devices.filter((d) => d.new).length} new · ${devices.length} total`);
      clear(tbody);
      if (!rows.length) {
        tbody.append(h('tr', null, h('td', { colspan: 7, class: 'empty' }, devices.length ? 'No matches.' : 'No devices seen yet.')));
        return;
      }
      for (const d of rows) {
        const name = displayName(d);
        const open = () => { location.hash = `#/device?mac=${encodeURIComponent(d.mac)}`; };
        tbody.append(h('tr', { class: 'clickable', onclick: open },
          h('td', null, h('span', { class: `dot ${d.online ? 'ok' : ''}`, title: d.online ? 'online' : 'offline' })),
          h('td', null,
            h('div', { class: 'cell-main' }, name || h('span', { class: 'muted' }, 'unknown'),
              d.new && h('span', { class: 'badge info small-badge' }, 'new'),
              d.block?.enabled && h('span', { class: 'badge crit small-badge', title: blockText(d.block) }, 'blocked')),
            d.dns_name && h('div', { class: 'muted small mono' }, d.dns_name),
            (d.note || d.reservation?.comment) && h('div', { class: 'muted small' }, d.note || d.reservation.comment)),
          h('td', { class: 'mono' }, d.ip || '–'),
          h('td', null, h('span', { class: 'mono' }, d.mac), d.random_mac && h('span', { class: 'badge warn small-badge', title: 'Locally administered MAC address: often randomly generated and can change. A reservation or block then no longer applies.' }, 'random?')),
          h('td', null, d.reservation ? badge('fest', 'info') : d.lease_state === 'active' ? badge('dynamic') : h('span', { class: 'muted' }, '–')),
          h('td', { class: 'muted small' }, d.online ? 'now' : d.last_seen ? fmt.date(d.last_seen) : '–'),
          h('td', { class: 'actions' }, icon('arrowRight'))));
      }
    }

    function exportCSV() {
      const cols = ['Name', 'Hostname', 'IP', 'MAC', 'DNS name', 'Fest', 'Online', 'First seen', 'Last seen', 'Note'];
      const esc = (v) => {
        let s = String(v ?? '');
        if (/^[=+\-@\t\r]/.test(s)) s = `'${s}`; // no formulas in spreadsheet programs
        return /[";\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
      };
      const lines = [cols.join(';'), ...visible().map((d) => [displayName(d), d.hostname, d.ip, d.mac, d.dns_name, d.reservation ? 'yes' : 'no', d.online ? 'yes' : 'no', d.first_seen || '', d.last_seen || '', d.note || ''].map(esc).join(';'))];
      // BOM, so that Excel reads non-ASCII characters correctly
      const blob = new Blob(['﻿' + lines.join('\r\n')], { type: 'text/csv;charset=utf-8' });
      const a = h('a', { href: URL.createObjectURL(blob), download: `devices-${new Date().toISOString().slice(0, 10)}.csv` });
      document.body.appendChild(a);
      a.click();
      setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
    }

    load();
    return every(20000, load);
  },
};
