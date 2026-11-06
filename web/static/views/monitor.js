// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, every, clear, icon, fmt, badge } from '../ui.js';
import { get } from '../api.js';
import { chartElement, drawLines, clockFmt } from '../charts.js';
import { seg } from './dashboard.js';

export default {
  title: 'Online-Monitor',
  subtitle: 'Response times and outages of the internet connection',
  render(root) {
    let minutes = 60;
    const status = h('div');
    const grid = h('div', { class: 'grid cols-3' });
    const outages = h('tbody');
    const rangeSeg = seg([['60', '1 h'], ['360', '6 h'], ['1440', '24 h']], '60', (v) => { minutes = +v; load(); });

    root.append(
      h('div', { class: 'row', style: { marginBottom: '16px' } }, status, h('div', { class: 'spacer' }), rangeSeg),
      grid,
      h('section', { class: 'card mt' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('alert')), h('h2', null, 'Outages')),
        h('p', { class: 'muted small' }, 'An outage is when neither 8.8.8.8 nor 1.1.1.1 answers. Measured every 30 seconds from the router.'),
        h('div', { class: 'table-wrap' }, h('table', null,
          h('thead', null, h('tr', null, ['Start', 'End', 'Duration'].map((t) => h('th', null, t)))), outages))));

    async function load() {
      let d;
      try { d = await get(`/api/monitor?minutes=${minutes}`); } catch (e) { clear(status).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      clear(status).append(d.internet_down ? badge('Internet not reachable', 'crit') : badge('Internet reachable', 'ok'));
      const now = Date.now() / 1000;
      const t0 = now - minutes * 60;
      const gaps = d.outages.map((o) => [Date.parse(o.start) / 1000, o.end ? Date.parse(o.end) / 1000 : now]);
      clear(grid);
      for (const t of d.targets) {
        const chart = chartElement(130);
        const lossKind = t.loss > 5 ? 'crit' : t.loss > 0 ? 'warn' : 'ok';
        grid.append(h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('h2', null, t.name), h('span', { class: 'muted mono small' }, t.addr || '–')),
          h('div', { class: 'row monitor-stats' },
            stat('Current', t.last >= 0 ? `${fmt.num(t.last)} ms` : 'no answer', t.last < 0 ? 'crit' : ''),
            stat('Average', t.avg ? `${fmt.num(t.avg)} ms` : '–'),
            stat('Loss', `${fmt.num(t.loss)} %`, lossKind)),
          chart));
        // only draw after mounting, so that the width is right
        requestAnimationFrame(() => drawLines(chart, [{ points: t.probes.map((p) => [p.t, p.rtt >= 0 ? p.rtt : null]), cls: 'rx-line' }],
          { t0, t1: now, yMin: 10, yFmt: (v) => `${v} ms`, tFmt: clockFmt, gaps, gapSec: d.interval_s * 2.5 }));
      }
      clear(outages);
      if (!d.outages.length) outages.append(h('tr', null, h('td', { colspan: 3, class: 'empty' }, 'No outages in the selected period.')));
      for (const o of [...d.outages].reverse()) {
        const end = o.end ? Date.parse(o.end) : Date.now();
        outages.append(h('tr', null,
          h('td', null, fmt.date(o.start)),
          h('td', null, o.end ? fmt.date(o.end) : badge('ongoing', 'crit')),
          h('td', null, fmt.duration(Math.max(60, (end - Date.parse(o.start)) / 1000)))));
      }
    }

    load();
    return every(30000, load);
  },
};

function stat(label, value, kind) {
  return h('div', { class: 'stat' }, h('div', { class: 'stat-label' }, label), h('div', { class: `stat-value small-value ${kind ? `text-${kind}` : ''}` }, value));
}
