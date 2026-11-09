// SPDX-License-Identifier: AGPL-3.0-or-later
import { t } from '../i18n.js';
import { h, clear, icon, fmt, badge, stateKind, STATE_LABEL } from '../ui.js';
import { get } from '../api.js';
import { chartElement, drawLines, clockFmt } from '../charts.js';

const SERVICE_LABEL = {
  'vr-dnsmasq': 'DNS and DHCP', 'systemd-networkd': 'Network', 'vr-firewall': 'Firewall',
  'vr-pppoe': 'Dial-up (PPPoE)', ssh: 'SSH', 'unattended-upgrades': 'Auto-Updates',
  smbd: 'Samba', docker: 'Docker',
};

const LIVE_POINTS = 90; // 2 s apart = 3 minutes
const RANGES = [['live', 'Live'], ['60', '1 h'], ['1440', '24 h']];

export default {
  title: 'Overview',
  subtitle: 'Router status at a glance',
  render(root) {
    const live = { wan0: [], lan0: [] };
    let iface = 'wan0';
    let range = 'live';
    let lastLive = null;
    let hist = null;

    const cpuVal = h('div', { class: 'stat-value' }, '–');
    const cpuBar = h('span');
    const memVal = h('div', { class: 'stat-value' }, '–');
    const memSub = h('div', { class: 'stat-sub' });
    const memBar = h('span');
    const tempVal = h('div', { class: 'stat-value' }, '–');
    const upVal = h('div', { class: 'stat-value' }, '–');
    const upSub = h('div', { class: 'stat-sub' });

    const rxVal = h('span', { class: 'legend-value' }, '–');
    const txVal = h('span', { class: 'legend-value' }, '–');
    const rxLabel = h('span', { class: 'muted' }, 'Received');
    const txLabel = h('span', { class: 'muted' }, 'Sent');
    const totals = h('div', { class: 'stat-sub' });
    const chart = chartElement(190);
    const resChart = chartElement(150);
    const ifSeg = seg([['wan0', 'WAN'], ['lan0', 'LAN']], iface, (v) => { iface = v; draw(); });
    const rangeSeg = seg(RANGES, range, async (v) => { range = v; if (v !== 'live') await loadHistory(); draw(); });

    const wanBody = h('dl', { class: 'kv' });
    const lanBody = h('dl', { class: 'kv' });
    const services = h('div', { class: 'service-grid' });
    const sysBody = h('dl', { class: 'kv' });
    const banner = h('div');
    const hintsBox = h('div');

    root.append(
      banner, hintsBox,
      h('div', { class: 'grid cols-4' },
        statCard('CPU', 'cpu', cpuVal, h('div', { class: 'meter' }, cpuBar)),
        statCard('Memory', 'memory', memVal, memSub, h('div', { class: 'meter' }, memBar)),
        statCard('Temperature', 'thermo', tempVal),
        statCard('Uptime', 'clock', upVal, upSub)),
      h('div', { class: 'grid cols-2 mt' },
        h('section', { class: 'card span-2' },
          h('div', { class: 'card-head' }, badgeIcon('activity'), h('h2', null, 'Traffic'), rangeSeg, ifSeg),
          h('div', { class: 'legend' },
            legend('var(--rx)', rxLabel, rxVal),
            legend('var(--tx)', txLabel, txVal),
            h('div', { class: 'spacer' }), totals),
          chart),
        h('section', { class: 'card' }, h('div', { class: 'card-head' }, badgeIcon('globe'), h('h2', null, 'Internet (WAN)')), wanBody),
        h('section', { class: 'card' }, h('div', { class: 'card-head' }, badgeIcon('home'), h('h2', null, 'Home network (LAN)')), lanBody),
        h('section', { class: 'card span-2' },
          h('div', { class: 'card-head' }, badgeIcon('cpu'), h('h2', null, 'Load (24 h)'),
            h('div', { class: 'legend compact' }, legend('var(--accent)', h('span', { class: 'muted' }, 'CPU')), legend('var(--tx)', h('span', { class: 'muted' }, 'RAM')))),
          resChart),
        h('section', { class: 'card' }, h('div', { class: 'card-head' }, badgeIcon('services'), h('h2', null, 'Services')), services),
        h('section', { class: 'card' }, h('div', { class: 'card-head' }, badgeIcon('system'), h('h2', null, 'System')), sysBody)));

    function applyLive(l) {
      lastLive = l;
      const cpu = l.cpu || 0;
      cpuVal.textContent = fmt.pct(cpu);
      setMeter(cpuBar, cpu);
      if (l.mem?.total) {
        const used = l.mem.total - l.mem.available;
        const pct = (100 * used) / l.mem.total;
        memVal.textContent = fmt.pct(pct);
        memSub.textContent = t(`${fmt.bytes(used)} of ${fmt.bytes(l.mem.total)}`);
        setMeter(memBar, pct);
      }
      const at = Date.parse(l.time) / 1000;
      for (const n of ['wan0', 'lan0']) {
        const r = l.net?.[n];
        if (!r) continue;
        live[n].push([at, r.rx, r.tx]);
        if (live[n].length > LIVE_POINTS) live[n].shift();
      }
      if (range === 'live') draw();
    }

    function draw() {
      const now = Date.now() / 1000;
      let rx, tx, t0;
      if (range === 'live') {
        const pts = live[iface];
        rx = pts.map((p) => [p[0], p[1]]);
        tx = pts.map((p) => [p[0], p[2]]);
        t0 = now - LIVE_POINTS * 2;
        const cur = pts[pts.length - 1];
        rxLabel.textContent = t('Received');
        txLabel.textContent = t('Sent');
        rxVal.textContent = cur ? fmt.rate(cur[1]) : '–';
        txVal.textContent = cur ? fmt.rate(cur[2]) : '–';
      } else {
        const pts = hist || [];
        const k = iface === 'wan0' ? ['wr', 'wt'] : ['lr', 'lt'];
        rx = pts.map((p) => [p.t + 30, p[k[0]]]);
        tx = pts.map((p) => [p.t + 30, p[k[1]]]);
        t0 = now - +range * 60;
        // data volume in the period (minute average × 60 s)
        const sum = (key) => pts.reduce((n, p) => n + p[key] * 60, 0);
        rxLabel.textContent = t('Received in total');
        txLabel.textContent = t('Sent in total');
        rxVal.textContent = fmt.bytes(sum(k[0]));
        txVal.textContent = fmt.bytes(sum(k[1]));
      }
      const r = lastLive?.net?.[iface];
      totals.textContent = t(r ? `since start: ↓ ${fmt.bytes(r.rx_total)} · ↑ ${fmt.bytes(r.tx_total)}` : '');
      drawLines(chart, [{ points: rx, cls: 'rx-line', area: 'rx-area' }, { points: tx, cls: 'tx-line' }],
        { t0, t1: now, yMin: 1000, yFmt: fmt.rate, tFmt: range === 'live' ? (t) => (t === now ? 'now' : '−3 min') : clockFmt, gapSec: range === 'live' ? 10 : 180 });
    }

    async function loadHistory() {
      try { hist = (await get(`/api/history?minutes=${range === 'live' ? 1440 : range}`)).points; } catch { hist = []; }
    }

    async function loadResources() {
      let pts = [];
      try { pts = (await get('/api/history?minutes=1440')).points; } catch { /* leer */ }
      const now = Date.now() / 1000;
      drawLines(resChart, [
        { points: pts.map((p) => [p.t + 30, p.cpu]), cls: 'cpu-line' },
        { points: pts.map((p) => [p.t + 30, p.mem]), cls: 'tx-line' },
      ], { t0: now - 86400, t1: now, yMax: 100, yFmt: (v) => `${v} %`, tFmt: clockFmt, gapSec: 180 });
    }

    async function loadHints() {
      let d;
      try { d = await get('/api/hints'); } catch { return; }
      clear(hintsBox);
      if (!d.hints.length) return;
      const order = { crit: 0, warn: 1, info: 2 };
      hintsBox.append(h('section', { class: 'card hints' },
        h('div', { class: 'card-head' }, badgeIcon('alert'), h('h2', null, 'Notes'), badge(String(d.hints.length))),
        h('div', { class: 'hint-list' }, d.hints.sort((a, b) => order[a.level] - order[b.level]).map((x) =>
          h(x.link ? 'a' : 'div', { class: `hint-item ${x.level}`, href: x.link || null },
            h('span', { class: `dot ${x.level === 'info' ? '' : x.level}` }),
            h('div', null, h('div', { class: 'hint-title' }, x.title), x.text && h('div', { class: 'muted small' }, x.text)),
            x.link && icon('arrowRight'))))));
    }

    async function loadStatic() {
      let d;
      try { d = await get('/api/dashboard'); } catch (e) { clear(banner).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      clear(banner);
      tempVal.textContent = t(d.temperature != null ? `${fmt.num(d.temperature)} °C` : '–');
      upVal.textContent = fmt.duration(d.uptime_s);
      upSub.textContent = t(`Last ${d.load.map((x) => fmt.num(x, 2)).join(' · ')}`);

      fillKV(wanBody, [
        ['Status', d.wan.up ? badge('connected', 'ok') : badge('disconnected', 'crit')],
        ['IPv4', d.wan.ipv4.join(', ') || '–'],
        ['IPv6', d.wan.ipv6.length ? d.wan.ipv6.map((a) => h('div', { class: 'mono' }, a)) : '–'],
        ['Gateway', d.gateway.ip || '–'],
        ['Connection', d.wan.speed_mbit ? `${d.wan.speed_mbit} Mbit/s` : '–'],
      ]);
      fillKV(lanBody, [
        ['Status', d.lan.up ? badge('active', 'ok') : badge('aus', 'crit')],
        ['Address', d.lan.ipv4.join(', ') || '–'],
        ['Devices online', h('a', { href: '#/devices', class: 'num' }, String(d.clients))],
        ['Connection', d.lan.speed_mbit ? `${d.lan.speed_mbit} Mbit/s` : '–'],
        ['MAC', h('span', { class: 'mono' }, d.lan.mac)],
      ]);
      fillKV(sysBody, [
        ['Name', d.hostname],
        ['System', d.release],
        ['Kernel', h('span', { class: 'mono' }, d.kernel)],
        ['Helper', d.helper_ok ? badge('ready', 'ok') : badge('not reachable', 'crit')],
        ['Sessions', String(d.sessions)],
      ]);
      clear(services).append(...Object.entries(d.services).sort(([a], [b]) => a.localeCompare(b)).map(([name, state]) =>
        h('div', { class: 'service' },
          h('span', { class: `dot ${stateKind(state)}` }),
          h('span', { class: 'name', title: name }, SERVICE_LABEL[name] || name),
          h('span', { class: 'state' }, STATE_LABEL[state] || state))));
      if (d.live && !lastLive) applyLive(d.live);
    }

    loadStatic();
    loadHints();
    loadResources();
    const timer = setInterval(() => {
      loadStatic();
      loadResources();
      if (range !== 'live') loadHistory().then(draw);
    }, 30000);
    const hintTimer = setInterval(loadHints, 120000);
    const es = new EventSource('/api/events');
    es.onmessage = (e) => applyLive(JSON.parse(e.data));

    return () => { es.close(); clearInterval(timer); clearInterval(hintTimer); };
  },
};

export function seg(options, value, onChange) {
  const box = h('div', { class: 'seg' });
  for (const [v, label] of options) {
    box.append(h('button', { class: v === value ? 'active' : '', onclick: () => {
      for (const b of box.children) b.classList.toggle('active', b.dataset.v === v);
      onChange(v);
    }, 'data-v': v }, label));
  }
  return box;
}

function statCard(label, ic, ...body) {
  return h('section', { class: 'card stat' }, h('div', { class: 'stat-label' }, icon(ic), label), ...body);
}
function badgeIcon(name) { return h('span', { class: 'icon-badge' }, icon(name)); }
function legend(color, label, value) {
  return h('div', { class: 'legend-item' }, h('span', { class: 'legend-swatch', style: { background: color } }), label, value);
}
function setMeter(bar, pct) {
  bar.style.width = `${Math.min(100, Math.max(0, pct))}%`;
  bar.parentElement.className = `meter ${pct > 90 ? 'crit' : pct > 75 ? 'warn' : ''}`;
}
function fillKV(dl, rows) {
  clear(dl).append(...rows.flatMap(([k, v]) => [h('dt', null, k), h('dd', null, v)]));
}
