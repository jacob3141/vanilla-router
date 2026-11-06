// SPDX-License-Identifier: AGPL-3.0-or-later
import { t } from '../i18n.js';
import { h, clear, icon, badge } from '../ui.js';
import { get } from '../api.js';

const UNITS = ['', 'vr-dnsmasq', 'systemd-networkd', 'vr-firewall', 'vr-pppoe', 'systemd-resolved', 'chrony', 'ssh',
  'unattended-upgrades', 'apt-daily-upgrade', 'cron', 'vr-webui', 'vr-webui-helper', 'vr-health', 'docker', 'smbd'];
const PRIO = [['3', 'Errors and worse'], ['4', 'warnings and worse'], ['5', 'notices and worse'], ['6', 'ab Info'], ['7', 'everything (debug)']];
const KIND = { change: ['Change', 'info'], rejected: ['rejected', 'warn'], auth: ['Login', ''], files: ['File', 'ok'] };

export default {
  title: 'Logs',
  subtitle: 'System log and changes through the web interface',
  render(root) {
    const out = h('pre', { class: 'code log tall' });
    const unit = h('select', { class: 'input auto' }, UNITS.map((u) => h('option', { value: u }, u || 'All services')));
    const prio = h('select', { class: 'input auto' }, PRIO.map(([v, l]) => h('option', { value: v, selected: v === '6' }, l)));
    const hours = h('select', { class: 'input auto' }, [['1', '1 h'], ['24', '24 h'], ['168', '7 days']].map(([v, l]) => h('option', { value: v, selected: v === '24' }, l)));
    const query = h('input', { class: 'input', type: 'search', placeholder: 'Search …', style: { flex: '1', minWidth: '160px' } });
    const count = h('span', { class: 'muted small' });
    const changes = h('tbody');

    const form = h('form', { class: 'row' }, unit, prio, hours, query, h('button', { class: 'btn primary', type: 'submit' }, icon('search'), 'Show'), count);
    form.addEventListener('submit', (e) => { e.preventDefault(); loadJournal(); });
    for (const el of [unit, prio, hours]) el.addEventListener('change', loadJournal);

    root.append(
      h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('file')), h('h2', null, 'System log')), form, h('div', { class: 'mt' }, out)),
      h('section', { class: 'card mt' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('edit')), h('h2', null, 'Change log (30 days)')),
        h('p', { class: 'muted small' }, 'What was changed through this web interface, plus logins and file actions in the file share.'),
        h('div', { class: 'table-wrap' }, h('table', null, h('thead', null, h('tr', null, ['Time', 'Art', 'Entry'].map((t) => h('th', null, t)))), changes))));

    async function loadJournal() {
      out.textContent = t('Loading …');
      const p = new URLSearchParams({ unit: unit.value, priority: prio.value, hours: hours.value, q: query.value.trim(), lines: '500' });
      try {
        const d = await get(`/api/logs/system?${p}`);
        const lines = d.lines.filter((l) => l);
        out.textContent = lines.length ? lines.join('\n') : t('(no entries)');
        count.textContent = t(`${lines.length} lines`);
        out.scrollTop = out.scrollHeight;
      } catch (e) { out.textContent = t(e.message); }
    }

    async function loadChanges() {
      let d;
      try { d = await get('/api/logs/changes'); } catch (e) { clear(changes).append(h('tr', null, h('td', { colspan: 3, class: 'empty' }, e.message))); return; }
      clear(changes);
      if (!d.events?.length) { changes.append(h('tr', null, h('td', { colspan: 3, class: 'empty' }, 'No changes yet.'))); return; }
      for (const ev of d.events) {
        const [label, kind] = KIND[ev.kind] || [ev.kind, ''];
        changes.append(h('tr', null, h('td', { class: 'muted small nowrap' }, ev.time.replace('T', ' ').slice(0, 19)), h('td', null, badge(label, kind)), h('td', { class: 'mono small' }, ev.text)));
      }
    }

    loadJournal();
    loadChanges();
  },
};
