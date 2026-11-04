// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, every, clear, icon, fmt, badge, toast, confirmDialog, formDialog } from '../ui.js';
import { get, post } from '../api.js';

export const DAYS = [['1', 'Mo'], ['2', 'Di'], ['3', 'Mi'], ['4', 'Do'], ['5', 'Fr'], ['6', 'Sa'], ['0', 'So']];

export function displayName(d) {
  return d.alias || d.reservation?.name || d.hostname || '';
}

export function blockText(b) {
  if (!b) return '';
  if (!b.enabled) return 'Sperre pausiert';
  if (b.mode === 'always') return 'Internet gesperrt';
  const days = b.days?.length && b.days.length < 7 ? ` (${DAYS.filter(([v]) => b.days.includes(+v)).map(([, l]) => l).join(', ')})` : '';
  return `gesperrt ${b.from}–${b.to} Uhr${days}`;
}

// --- Dialoge, auch von der Detailseite genutzt ---------------------------------------

export async function reservationDialog(d, pool) {
  const r = d?.reservation;
  const suggestion = (r?.name || d?.alias || d?.hostname || '').toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').replace(/^-+|-+$/g, '').slice(0, 63);
  return formDialog({
    title: r ? `Feste Adresse für ${r.name}` : 'Feste Adresse vergeben',
    text: 'Das Gerät bekommt immer dieselbe IP-Adresse und ist unter <name>.home.arpa erreichbar. Die Adresse wird aus dem dynamischen Bereich ausgespart.',
    fields: [
      { name: 'name', label: 'Name', value: suggestion, placeholder: 'z. B. drucker-keller', hint: 'Kleinbuchstaben, Ziffern und Bindestriche. Wird zu name.home.arpa.' },
      { name: 'mac', label: 'MAC-Adresse', value: r?.mac || d?.mac || '', placeholder: 'aa:bb:cc:dd:ee:ff' },
      { name: 'ip', label: 'IP-Adresse', value: r?.ip || d?.ip || '', placeholder: 'z. B. 192.168.42.20', hint: pool ? `Dynamischer Bereich: ${pool.start} – ${pool.end}. Jede freie Adresse im Heimnetz ist möglich.` : '' },
      { name: 'comment', label: 'Notiz (optional)', value: r?.comment || '', placeholder: 'z. B. 3D-Drucker im Keller' },
    ],
    onSubmit: (v) => post('/api/reservations', { ...v, original_mac: r?.mac || '' }),
  });
}

export async function deleteReservation(d) {
  const r = d.reservation;
  if (!(await confirmDialog({ title: `Feste Adresse für ${r.name} entfernen?`, text: `${r.ip} wird wieder dynamisch vergeben, der Name ${r.name}.home.arpa verschwindet. Das Gerät behält seine Adresse bis zum Ende der Lease.`, confirm: 'Entfernen', danger: true }))) return false;
  try { toast((await post('/api/reservations/delete', { mac: r.mac })).message); return true; } catch (e) { toast(e.message, 'crit'); return false; }
}

export function metaDialog(d) {
  return formDialog({
    title: 'Name und Notiz',
    text: 'Nur für die Übersicht in der Oberfläche. Für einen Namen im Netz (name.home.arpa) eine feste Adresse vergeben.',
    fields: [
      { name: 'alias', label: 'Name', value: d.alias || '', placeholder: d.hostname || 'z. B. Laptop Arbeitszimmer' },
      { name: 'note', label: 'Notiz', value: d.note || '', placeholder: 'z. B. Wohnzimmer, gehört Gast' },
    ],
    onSubmit: (v) => post('/api/devices/meta', { mac: d.mac, ...v }),
  });
}

export function blockDialog(d) {
  const b = d.block;
  const days = b?.days?.length ? b.days : [0, 1, 2, 3, 4, 5, 6];
  return formDialog({
    title: `Internetzugang für ${displayName(d) || d.mac}`,
    text: 'Das Gerät kommt dann nicht mehr ins Internet. Das Heimnetz (Drucker, Dateiablage, dieser Router) bleibt erreichbar. Zeiten in deutscher Ortszeit.',
    fields: [
      { name: 'mode', label: 'Sperre', type: 'select', value: b?.mode || 'always', options: [{ value: 'always', label: 'Immer gesperrt' }, { value: 'schedule', label: 'Zu bestimmten Zeiten gesperrt' }] },
      { name: 'from', label: 'Gesperrt ab (nur bei Zeiten)', value: b?.from || '21:00', placeholder: 'HH:MM' },
      { name: 'to', label: 'Gesperrt bis', value: b?.to || '07:00', placeholder: 'HH:MM', hint: 'Über Mitternacht geht auch, z. B. 21:00 bis 07:00. Die Wochentage meinen den Beginn: „So–Do“ sperrt die Nächte vor Schultagen bis Freitagmorgen.' },
      ...DAYS.map(([v, l]) => ({ name: `day${v}`, label: l, type: 'checkbox', value: days.includes(+v) })),
      { name: 'enabled', label: 'Sperre aktiv', type: 'checkbox', value: b ? b.enabled : true },
    ],
    submit: 'Sperre speichern',
    onSubmit: (v) => {
      const sel = DAYS.filter(([n]) => v[`day${n}`]).map(([n]) => +n);
      if (v.mode === 'schedule' && !sel.length) throw new Error('Mindestens einen Wochentag auswählen.');
      return post('/api/blocks', {
        mac: d.mac, name: (displayName(d) || '').replace(/[^\p{L}\p{N} ._,:()/+-]/gu, '').slice(0, 64), mode: v.mode,
        from: v.mode === 'schedule' ? v.from : '', to: v.mode === 'schedule' ? v.to : '',
        days: v.mode === 'schedule' && sel.length < 7 ? sel : [], enabled: v.enabled,
      });
    },
  });
}

// --- Liste -----------------------------------------------------------------------------

export default {
  title: 'Geräte',
  subtitle: 'Geräte im Heimnetz, DHCP-Leases und feste Adressen',
  render(root) {
    let devices = [];
    let pool = null;
    let filter = '';
    let view = 'all';

    const search = h('input', { class: 'input', type: 'search', placeholder: 'Suchen nach Name, IP oder MAC …', oninput: (e) => { filter = e.target.value.toLowerCase(); draw(); } });
    const viewSeg = h('div', { class: 'seg' }, [['all', 'Alle'], ['online', 'Online'], ['new', 'Neu'], ['blocked', 'Gesperrt']].map(([v, l]) =>
      h('button', { class: v === view ? 'active' : '', 'data-v': v, onclick: () => { view = v; for (const b of viewSeg.children) b.classList.toggle('active', b.dataset.v === v); draw(); } }, l)));
    const counts = h('span', { class: 'muted small' });
    const tbody = h('tbody');
    const banner = h('div');

    root.append(banner, h('section', { class: 'card' },
      h('div', { class: 'card-head' },
        h('div', { class: 'row', style: { flex: '1', minWidth: '0' } }, h('div', { style: { flex: '1', minWidth: '220px', maxWidth: '420px' } }, search), viewSeg, counts),
        h('button', { class: 'btn', title: 'Als CSV-Datei herunterladen', onclick: exportCSV }, icon('download'), 'CSV'),
        h('button', { class: 'btn primary', onclick: async () => { if (await reservationDialog(null, pool)) { toast('Gespeichert.'); load(true); } } }, icon('plus'), 'Feste Adresse')),
      h('div', { class: 'table-wrap' }, h('table', null,
        h('thead', null, h('tr', null, ['', 'Name', 'IP-Adresse', 'MAC-Adresse', 'Adresse', 'Zuletzt gesehen', ''].map((t) => h('th', null, t)))),
        tbody))));

    let last = '';
    async function load(force) {
      try {
        const d = await get('/api/devices');
        // Nur neu zeichnen, wenn sich etwas geändert hat, sonst gehen Klicks während des Aufbaus verloren
        const key = JSON.stringify(d);
        if (key === last && !force) return;
        last = key;
        devices = d.devices;
        pool = d.pool;
        clear(banner);
        if (d.dry_run) banner.append(h('div', { class: 'banner warn' }, icon('info'), 'Testinstanz: Änderungen werden geprüft, aber nicht eingespielt.'));
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
      counts.textContent = `${devices.filter((d) => d.online).length} online · ${devices.filter((d) => d.reservation).length} fest · ${devices.filter((d) => d.new).length} neu · ${devices.length} gesamt`;
      clear(tbody);
      if (!rows.length) {
        tbody.append(h('tr', null, h('td', { colspan: 7, class: 'empty' }, devices.length ? 'Keine Treffer.' : 'Noch keine Geräte gesehen.')));
        return;
      }
      for (const d of rows) {
        const name = displayName(d);
        const open = () => { location.hash = `#/device?mac=${encodeURIComponent(d.mac)}`; };
        tbody.append(h('tr', { class: 'clickable', onclick: open },
          h('td', null, h('span', { class: `dot ${d.online ? 'ok' : ''}`, title: d.online ? 'online' : 'offline' })),
          h('td', null,
            h('div', { class: 'cell-main' }, name || h('span', { class: 'muted' }, 'unbekannt'),
              d.new && h('span', { class: 'badge info small-badge' }, 'neu'),
              d.block?.enabled && h('span', { class: 'badge crit small-badge', title: blockText(d.block) }, 'gesperrt')),
            d.dns_name && h('div', { class: 'muted small mono' }, d.dns_name),
            (d.note || d.reservation?.comment) && h('div', { class: 'muted small' }, d.note || d.reservation.comment)),
          h('td', { class: 'mono' }, d.ip || '–'),
          h('td', null, h('span', { class: 'mono' }, d.mac), d.random_mac && h('span', { class: 'badge warn small-badge', title: 'Lokal verwaltete MAC-Adresse: wird oft zufällig erzeugt und kann sich ändern. Eine Reservierung oder Sperre greift dann nicht mehr.' }, 'zufällig?')),
          h('td', null, d.reservation ? badge('fest', 'info') : d.lease_state === 'active' ? badge('dynamisch') : h('span', { class: 'muted' }, '–')),
          h('td', { class: 'muted small' }, d.online ? 'jetzt' : d.last_seen ? fmt.date(d.last_seen) : '–'),
          h('td', { class: 'actions' }, icon('arrowRight'))));
      }
    }

    function exportCSV() {
      const cols = ['Name', 'Hostname', 'IP', 'MAC', 'DNS-Name', 'Fest', 'Online', 'Zuerst gesehen', 'Zuletzt gesehen', 'Notiz'];
      const esc = (v) => {
        let s = String(v ?? '');
        if (/^[=+\-@\t\r]/.test(s)) s = `'${s}`; // keine Formeln in Tabellenprogrammen
        return /[";\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
      };
      const lines = [cols.join(';'), ...visible().map((d) => [displayName(d), d.hostname, d.ip, d.mac, d.dns_name, d.reservation ? 'ja' : 'nein', d.online ? 'ja' : 'nein', d.first_seen || '', d.last_seen || '', d.note || ''].map(esc).join(';'))];
      // BOM, damit Excel die Umlaute richtig liest
      const blob = new Blob(['﻿' + lines.join('\r\n')], { type: 'text/csv;charset=utf-8' });
      const a = h('a', { href: URL.createObjectURL(blob), download: `geraete-${new Date().toISOString().slice(0, 10)}.csv` });
      document.body.appendChild(a);
      a.click();
      setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
    }

    load();
    return every(20000, load);
  },
};
