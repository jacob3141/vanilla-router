// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, clear, icon, badge, toast, confirmDialog, formDialog } from '../ui.js';
import { get, post } from '../api.js';

const SOURCE = {
  record: ['own entry', 'info'],
  reservation: ['from a fixed address', ''],
  manual: ['this router', 'ok'],
};

export default {
  title: 'DNS',
  subtitle: 'Local names under home.arpa and upstream servers',
  render(root) {
    const tbody = h('tbody');
    const upstreams = h('div', { class: 'row' });
    const banner = h('div');

    root.append(banner,
      h('div', { class: 'grid cols-2' },
        h('section', { class: 'card span-2' },
          h('div', { class: 'card-head' },
            h('span', { class: 'icon-badge' }, icon('dns')), h('h2', null, 'Local names'),
            h('button', { class: 'btn', onclick: flush }, icon('refresh'), 'Clear cache'),
            h('button', { class: 'btn primary', onclick: () => edit(null) }, icon('plus'), 'Entry')),
          h('div', { class: 'table-wrap' }, h('table', null,
            h('thead', null, h('tr', null, ['Name', 'Address', 'Source', ''].map((t) => h('th', null, t)))),
            tbody))),
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('globe')), h('h2', null, 'Upstream-Server')),
          h('p', { class: 'muted small' }, 'dnsmasq forwards all names that are not local to these servers. The queries are not encrypted.'),
          upstreams),
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('info')), h('h2', null, 'Notes')),
          h('ul', { class: 'muted small list' },
            h('li', null, 'You manage names from fixed addresses under "Devices".'),
            h('li', null, 'The name of the router itself is set during installation (setting "Router name").'),
            h('li', null, 'All your own entries also provide reverse lookup (IP → name).')))));

    // --- Statistics (like Pi-hole), from the dnsmasq log ---------------------------
    let hours = 24;
    let client = '';
    const statBody = h('div');
    const statSeg = h('div', { class: 'seg' }, [[1, '1 h'], [24, '24 h'], [168, '7 days']].map(([v, l]) =>
      h('button', { class: v === hours ? 'active' : '', 'data-v': v, onclick: () => { hours = v; for (const b of statSeg.children) b.classList.toggle('active', +b.dataset.v === v); loadStats(); } }, l)));
    root.append(h('section', { class: 'card mt' },
      h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('activity')), h('h2', null, 'Statistics'), statSeg),
      statBody));

    async function loadStats() {
      clear(statBody).append(h('p', { class: 'muted' }, h('span', { class: 'spinner' }), ' Evaluating the log …'));
      let s;
      try { s = await get(`/api/dns/stats?hours=${hours}${client ? `&client=${encodeURIComponent(client)}` : ''}`); } catch (e) { clear(statBody).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      if (!s.logging) { clear(statBody).append(h('p', { class: 'muted' }, 'To protect privacy, the router does not log DNS queries. That is why there are no statistics here.')); return; }
      const answered = s.cached + s.forwarded + s.local;
      const kpi = (label, value, sub) => h('div', { class: 'stat' }, h('div', { class: 'stat-label' }, label), h('div', { class: 'stat-value small-value' }, value), sub && h('div', { class: 'stat-sub' }, sub));
      const list = (title, rows, label) => h('div', null, h('h3', { class: 'sub' }, title),
        h('div', { class: 'table-wrap flat' }, h('table', null, h('tbody', null, rows.map((r) => h('tr', null,
          h('td', null, label(r)), h('td', { class: 'num-col' }, fmt.num(r.count, 0))))))));
      clear(statBody).append(
        client && h('div', { class: 'banner' }, icon('info'), h('div', { class: 'spacer' }, `Only queries from ${s.names[client] || client} (${client}).`), h('button', { class: 'btn sm', onclick: () => { client = ''; loadStats(); } }, 'All devices')),
        h('div', { class: 'row kpis' },
          kpi('Queries', fmt.num(s.queries, 0), `≈ ${fmt.num(s.queries / hours, 0)} per hour`),
          !client && kpi('From the cache', answered ? fmt.pct((100 * s.cached) / answered) : '–', `${fmt.num(s.cached, 0)} answers`),
          !client && kpi('Forwarded', fmt.num(s.forwarded, 0), 'to the upstream servers'),
          !client && kpi('Answered locally', fmt.num(s.local, 0), 'Names under home.arpa')),
        h('div', { class: 'grid cols-2 mt' },
          list('Most frequent names', s.top_domains, (r) => h('span', { class: 'mono small' }, r.name)),
          !client && list('Most active devices', s.top_clients, (r) => h('a', { href: '#', onclick: (e) => { e.preventDefault(); client = r.name; loadStats(); } }, s.names[r.name] || r.name, s.names[r.name] && h('span', { class: 'muted small mono' }, ` ${r.name}`)))));
    }
    loadStats();

    async function load() {
      let d;
      try { d = await get('/api/dns'); } catch (e) { clear(banner).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      clear(banner);
      if (d.dry_run) banner.append(h('div', { class: 'banner warn' }, icon('info'), 'Test instance: changes are checked, but not applied.'));
      clear(upstreams).append(...(d.upstreams.length ? d.upstreams.map((u) => h('span', { class: 'badge mono' }, u)) : [h('span', { class: 'muted' }, 'none (system default)')]));
      clear(tbody);
      if (!d.entries.length) tbody.append(h('tr', null, h('td', { colspan: 4, class: 'empty' }, 'No local names.')));
      for (const e of d.entries) {
        const [label, kind] = SOURCE[e.source];
        tbody.append(h('tr', null,
          h('td', { class: 'mono' }, e.name),
          h('td', { class: 'mono' }, e.ip),
          h('td', null, badge(label, kind)),
          h('td', { class: 'actions' }, e.source === 'record' && [
            h('button', { class: 'btn sm ghost icon', title: 'Edit', onclick: () => edit(e) }, icon('edit')),
            h('button', { class: 'btn sm ghost icon danger', title: 'Delete', onclick: () => remove(e) }, icon('trash'))])));
      }
    }

    async function edit(e) {
      const short = e?.name.replace(/\.home\.arpa$/, '');
      const ok = await formDialog({
        title: e ? `Edit ${e.name}` : 'Create a local name',
        text: 'For devices with a fixed IP or services that do not come through DHCP. For DHCP devices, better assign a fixed address under "Devices".',
        fields: [
          { name: 'name', label: 'Name', value: short || '', placeholder: 'e.g. nas', hint: 'Becomes name.home.arpa.' },
          { name: 'ip', label: 'IP address', value: e?.ip || '', placeholder: 'IPv4 or IPv6' },
        ],
        onSubmit: (v) => post('/api/dns', { ...v, original_name: short || '' }),
      });
      if (ok) { toast(ok.message); load(); }
    }

    async function remove(e) {
      if (!(await confirmDialog({ title: `Delete ${e.name}?`, text: 'The name can then no longer be resolved in the home network.', confirm: 'Delete', danger: true }))) return;
      try { toast((await post('/api/dns/delete', { name: e.name })).message); load(); } catch (ex) { toast(ex.message, 'crit'); }
    }

    async function flush() {
      try { toast((await post('/api/dns/flush')).message); } catch (ex) { toast(ex.message, 'crit'); }
    }

    load();
  },
};
