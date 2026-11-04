// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, clear, icon, badge, toast, confirmDialog, formDialog } from '../ui.js';
import { get, post } from '../api.js';

const SOURCE = {
  record: ['eigener Eintrag', 'info'],
  reservation: ['aus fester Adresse', ''],
  manual: ['handgeschrieben', 'warn'],
};

export default {
  title: 'DNS',
  subtitle: 'Lokale Namen unter home.arpa und Upstream-Server',
  render(root) {
    const tbody = h('tbody');
    const upstreams = h('div', { class: 'row' });
    const banner = h('div');

    root.append(banner,
      h('div', { class: 'grid cols-2' },
        h('section', { class: 'card span-2' },
          h('div', { class: 'card-head' },
            h('span', { class: 'icon-badge' }, icon('dns')), h('h2', null, 'Lokale Namen'),
            h('button', { class: 'btn', onclick: flush }, icon('refresh'), 'Cache leeren'),
            h('button', { class: 'btn primary', onclick: () => edit(null) }, icon('plus'), 'Eintrag')),
          h('div', { class: 'table-wrap' }, h('table', null,
            h('thead', null, h('tr', null, ['Name', 'Adresse', 'Herkunft', ''].map((t) => h('th', null, t)))),
            tbody))),
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('globe')), h('h2', null, 'Upstream-Server')),
          h('p', { class: 'muted small' }, 'An diese Server fragt dnsmasq alle Namen weiter, die nicht lokal sind. Die Anfragen sind unverschlüsselt.'),
          upstreams),
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('info')), h('h2', null, 'Hinweise')),
          h('ul', { class: 'muted small list' },
            h('li', null, 'Namen aus festen Adressen pflegst du unter „Geräte“.'),
            h('li', null, 'Handgeschriebene Einträge stehen in /etc/dnsmasq.conf und lassen sich hier nicht ändern.'),
            h('li', null, 'Alle eigenen Einträge liefern auch die Rückwärtsauflösung (IP → Name).')))));

    // --- Statistik (wie Pi-hole), aus dem Protokoll von dnsmasq ---------------------------
    let hours = 24;
    let client = '';
    const statBody = h('div');
    const statSeg = h('div', { class: 'seg' }, [[1, '1 Std'], [24, '24 Std'], [168, '7 Tage']].map(([v, l]) =>
      h('button', { class: v === hours ? 'active' : '', 'data-v': v, onclick: () => { hours = v; for (const b of statSeg.children) b.classList.toggle('active', +b.dataset.v === v); loadStats(); } }, l)));
    root.append(h('section', { class: 'card mt' },
      h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('activity')), h('h2', null, 'Statistik'), statSeg),
      statBody));

    async function loadStats() {
      clear(statBody).append(h('p', { class: 'muted' }, h('span', { class: 'spinner' }), ' Werte das Protokoll aus …'));
      let s;
      try { s = await get(`/api/dns/stats?hours=${hours}${client ? `&client=${encodeURIComponent(client)}` : ''}`); } catch (e) { clear(statBody).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      if (!s.logging) { clear(statBody).append(h('p', { class: 'muted' }, 'Keine Anfragen im Protokoll. Die Statistik braucht log-queries in der dnsmasq-Konfiguration.')); return; }
      const answered = s.cached + s.forwarded + s.local;
      const kpi = (label, value, sub) => h('div', { class: 'stat' }, h('div', { class: 'stat-label' }, label), h('div', { class: 'stat-value small-value' }, value), sub && h('div', { class: 'stat-sub' }, sub));
      const list = (title, rows, label) => h('div', null, h('h3', { class: 'sub' }, title),
        h('div', { class: 'table-wrap flat' }, h('table', null, h('tbody', null, rows.map((r) => h('tr', null,
          h('td', null, label(r)), h('td', { class: 'num-col' }, fmt.num(r.count, 0))))))));
      clear(statBody).append(
        client && h('div', { class: 'banner' }, icon('info'), h('div', { class: 'spacer' }, `Nur Anfragen von ${s.names[client] || client} (${client}).`), h('button', { class: 'btn sm', onclick: () => { client = ''; loadStats(); } }, 'Alle Geräte')),
        h('div', { class: 'row kpis' },
          kpi('Anfragen', fmt.num(s.queries, 0), `≈ ${fmt.num(s.queries / hours, 0)} pro Stunde`),
          !client && kpi('Aus dem Cache', answered ? fmt.pct((100 * s.cached) / answered) : '–', `${fmt.num(s.cached, 0)} Antworten`),
          !client && kpi('Weitergeleitet', fmt.num(s.forwarded, 0), 'an die Upstream-Server'),
          !client && kpi('Lokal beantwortet', fmt.num(s.local, 0), 'Namen unter home.arpa')),
        h('div', { class: 'grid cols-2 mt' },
          list('Häufigste Namen', s.top_domains, (r) => h('span', { class: 'mono small' }, r.name)),
          !client && list('Aktivste Geräte', s.top_clients, (r) => h('a', { href: '#', onclick: (e) => { e.preventDefault(); client = r.name; loadStats(); } }, s.names[r.name] || r.name, s.names[r.name] && h('span', { class: 'muted small mono' }, ` ${r.name}`)))));
    }
    loadStats();

    async function load() {
      let d;
      try { d = await get('/api/dns'); } catch (e) { clear(banner).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      clear(banner);
      if (d.dry_run) banner.append(h('div', { class: 'banner warn' }, icon('info'), 'Testinstanz: Änderungen werden geprüft, aber nicht eingespielt.'));
      clear(upstreams).append(...(d.upstreams.length ? d.upstreams.map((u) => h('span', { class: 'badge mono' }, u)) : [h('span', { class: 'muted' }, 'keine (Systemvorgabe)')]));
      clear(tbody);
      if (!d.entries.length) tbody.append(h('tr', null, h('td', { colspan: 4, class: 'empty' }, 'Keine lokalen Namen.')));
      for (const e of d.entries) {
        const [label, kind] = SOURCE[e.source];
        tbody.append(h('tr', null,
          h('td', { class: 'mono' }, e.name),
          h('td', { class: 'mono' }, e.ip),
          h('td', null, badge(label, kind), e.source === 'manual' && h('span', { class: 'muted small mono' }, ` ${e.detail}`)),
          h('td', { class: 'actions' }, e.source === 'record' && [
            h('button', { class: 'btn sm ghost icon', title: 'Bearbeiten', onclick: () => edit(e) }, icon('edit')),
            h('button', { class: 'btn sm ghost icon danger', title: 'Löschen', onclick: () => remove(e) }, icon('trash'))])));
      }
    }

    async function edit(e) {
      const short = e?.name.replace(/\.home\.arpa$/, '');
      const ok = await formDialog({
        title: e ? `${e.name} bearbeiten` : 'Lokalen Namen anlegen',
        text: 'Für Geräte mit fester IP oder Dienste, die nicht per DHCP kommen. Für DHCP-Geräte besser unter „Geräte“ eine feste Adresse vergeben.',
        fields: [
          { name: 'name', label: 'Name', value: short || '', placeholder: 'z. B. nas', hint: 'Wird zu name.home.arpa.' },
          { name: 'ip', label: 'IP-Adresse', value: e?.ip || '', placeholder: 'IPv4 oder IPv6' },
        ],
        onSubmit: (v) => post('/api/dns', { ...v, original_name: short || '' }),
      });
      if (ok) { toast(ok.message); load(); }
    }

    async function remove(e) {
      if (!(await confirmDialog({ title: `${e.name} löschen?`, text: 'Der Name ist danach im Heimnetz nicht mehr auflösbar.', confirm: 'Löschen', danger: true }))) return;
      try { toast((await post('/api/dns/delete', { name: e.name })).message); load(); } catch (ex) { toast(ex.message, 'crit'); }
    }

    async function flush() {
      try { toast((await post('/api/dns/flush')).message); } catch (ex) { toast(ex.message, 'crit'); }
    }

    load();
  },
};
