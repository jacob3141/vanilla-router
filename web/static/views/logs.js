// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, clear, icon, badge } from '../ui.js';
import { get } from '../api.js';

const UNITS = ['', 'vr-dnsmasq', 'systemd-networkd', 'vr-firewall', 'vr-pppoe', 'systemd-resolved', 'chrony', 'ssh',
  'unattended-upgrades', 'apt-daily-upgrade', 'cron', 'vr-webui', 'vr-webui-helper', 'vr-health', 'docker', 'smbd'];
const PRIO = [['3', 'Fehler und schlimmer'], ['4', 'ab Warnung'], ['5', 'ab Hinweis'], ['6', 'ab Info'], ['7', 'alles (Debug)']];
const KIND = { change: ['Änderung', 'info'], rejected: ['abgelehnt', 'warn'], auth: ['Anmeldung', ''], files: ['Datei', 'ok'] };

export default {
  title: 'Protokolle',
  subtitle: 'Systemprotokoll und Änderungen über die Oberfläche',
  render(root) {
    const out = h('pre', { class: 'code log tall' });
    const unit = h('select', { class: 'input auto' }, UNITS.map((u) => h('option', { value: u }, u || 'Alle Dienste')));
    const prio = h('select', { class: 'input auto' }, PRIO.map(([v, l]) => h('option', { value: v, selected: v === '6' }, l)));
    const hours = h('select', { class: 'input auto' }, [['1', '1 Std'], ['24', '24 Std'], ['168', '7 Tage']].map(([v, l]) => h('option', { value: v, selected: v === '24' }, l)));
    const query = h('input', { class: 'input', type: 'search', placeholder: 'Suchen …', style: { flex: '1', minWidth: '160px' } });
    const count = h('span', { class: 'muted small' });
    const changes = h('tbody');

    const form = h('form', { class: 'row' }, unit, prio, hours, query, h('button', { class: 'btn primary', type: 'submit' }, icon('search'), 'Anzeigen'), count);
    form.addEventListener('submit', (e) => { e.preventDefault(); loadJournal(); });
    for (const el of [unit, prio, hours]) el.addEventListener('change', loadJournal);

    root.append(
      h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('file')), h('h2', null, 'Systemprotokoll')), form, h('div', { class: 'mt' }, out)),
      h('section', { class: 'card mt' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('edit')), h('h2', null, 'Änderungsprotokoll (30 Tage)')),
        h('p', { class: 'muted small' }, 'Was über diese Oberfläche geändert wurde, dazu Anmeldungen und Dateiaktionen in der Dateiablage.'),
        h('div', { class: 'table-wrap' }, h('table', null, h('thead', null, h('tr', null, ['Zeit', 'Art', 'Eintrag'].map((t) => h('th', null, t)))), changes))));

    async function loadJournal() {
      out.textContent = 'Lädt …';
      const p = new URLSearchParams({ unit: unit.value, priority: prio.value, hours: hours.value, q: query.value.trim(), lines: '500' });
      try {
        const d = await get(`/api/logs/system?${p}`);
        const lines = d.lines.filter((l) => l);
        out.textContent = lines.length ? lines.join('\n') : '(keine Einträge)';
        count.textContent = `${lines.length} Zeilen`;
        out.scrollTop = out.scrollHeight;
      } catch (e) { out.textContent = e.message; }
    }

    async function loadChanges() {
      let d;
      try { d = await get('/api/logs/changes'); } catch (e) { clear(changes).append(h('tr', null, h('td', { colspan: 3, class: 'empty' }, e.message))); return; }
      clear(changes);
      if (!d.events?.length) { changes.append(h('tr', null, h('td', { colspan: 3, class: 'empty' }, 'Noch keine Änderungen.'))); return; }
      for (const ev of d.events) {
        const [label, kind] = KIND[ev.kind] || [ev.kind, ''];
        changes.append(h('tr', null, h('td', { class: 'muted small nowrap' }, ev.time.replace('T', ' ').slice(0, 19)), h('td', null, badge(label, kind)), h('td', { class: 'mono small' }, ev.text)));
      }
    }

    loadJournal();
    loadChanges();
  },
};
