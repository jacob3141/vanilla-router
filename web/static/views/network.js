// SPDX-License-Identifier: AGPL-3.0-or-later
import { t } from '../i18n.js';
import { h, every, clear, icon, fmt, badge, toast, confirmDialog } from '../ui.js';
import { get, post } from '../api.js';

// The roles a port can have. Another use, for example a second internet connection, is one more line here.
const ROLES = [['wan', 'Internet (WAN)'], ['lan', 'Home network (LAN)'], ['unused', 'Not used']];
const SWITCH_WAIT_MS = 8000;

export default {
  title: 'Network ports',
  subtitle: 'The physical ports of the router and what they are used for',
  render(root) {
    const pendingBox = h('div');
    const tbody = h('tbody');
    const apply = h('button', { class: 'btn primary', disabled: true, onclick: save }, icon('check'), 'Apply');
    const hint = h('p', { class: 'muted small' }, 'Use "Identify" to find a port: its LED blinks for 10 seconds, if the hardware supports it.');
    root.append(pendingBox, h('section', { class: 'card' },
      h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('globe')), h('h2', null, 'Ports'), h('div', { class: 'spacer' }), apply),
      hint,
      h('div', { class: 'table-wrap' }, h('table', null,
        h('thead', null, h('tr', null, ['', 'Port', 'Use', 'Speed', 'MAC address', 'Received', 'Sent', ''].map((x) => h('th', null, x)))),
        tbody))));

    let ports = [];
    let draft = {};
    let countdown = null;
    let shownPending = '';

    const changed = () => ports.some((p) => (draft[p.mac] || p.role) !== p.role);

    function draw() {
      clear(tbody);
      for (const p of ports) {
        const role = draft[p.mac] || p.role;
        tbody.append(h('tr', null,
          h('td', null, h('span', { class: `dot ${p.carrier ? 'ok' : ''}`, title: p.carrier ? 'connected' : 'no cable' })),
          h('td', null, h('div', { class: 'cell-main' }, p.label || 'Free port'), !p.present && badge('missing', 'warn')),
          h('td', null, h('select', { class: 'input auto', 'aria-label': 'Use', onchange: (e) => { draft[p.mac] = e.target.value; apply.disabled = !changed(); } },
            ROLES.map(([v, label]) => h('option', { value: v, selected: v === role }, label)))),
          h('td', null, p.carrier && p.speed_mbit ? `${fmt.num(p.speed_mbit, 0)} Mbit/s` : '–'),
          h('td', { class: 'mono small' }, p.mac),
          h('td', { class: 'muted' }, fmt.bytes(p.rx_bytes)),
          h('td', { class: 'muted' }, fmt.bytes(p.tx_bytes)),
          h('td', { class: 'actions' }, p.present && h('button', { class: 'btn sm', onclick: (e) => identify(p, e.currentTarget) }, 'Identify'))));
      }
      apply.disabled = !changed();
    }

    async function load() {
      let d;
      try { d = await get('/api/interfaces'); } catch { return; }
      const pendKey = d.pending ? d.pending.deadline : '';
      if (pendKey !== shownPending) { shownPending = pendKey; drawPending(d.pending); }
      ports = d.ports;
      // Do not pull a list away from under the pointer or overwrite a choice that is not applied yet
      if (!changed() && document.activeElement?.tagName !== 'SELECT') { draft = {}; draw(); }
    }

    function drawPending(p) {
      clearInterval(countdown);
      clear(pendingBox);
      if (!p) return;
      const left = h('strong', { class: 'num' });
      pendingBox.append(h('div', { class: 'banner warn pending' }, icon('alert'),
        h('div', { class: 'spacer' }, 'The new assignment is active. Move the cables to the new ports. Without confirmation it is rolled back automatically in ', left, '.'),
        h('button', { class: 'btn', onclick: revert }, 'Roll back'),
        h('button', { class: 'btn primary', onclick: confirmChange }, icon('check'), 'Confirm')));
      const tick = () => {
        const s = Math.max(0, Math.round((new Date(p.deadline) - Date.now()) / 1000));
        left.textContent = t(s >= 60 ? `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')} min` : `${s} s`);
        if (s <= 0) { clearInterval(countdown); setTimeout(load, 2000); }
      };
      tick();
      countdown = setInterval(tick, 1000);
    }

    async function save() {
      if (!(await confirmDialog({ title: 'Change the use of the ports?', danger: true, confirm: 'Switch',
        text: 'The ports change their use right away and the connection can drop. Move the cables to the new ports, open this page again and confirm within 5 minutes. Otherwise the router restores the old assignment by itself.' }))) return;
      const body = { ports: ports.map((p) => ({ mac: p.mac, role: draft[p.mac] || p.role })) };
      // The answer may never arrive, the cable it would travel on can be the one that just changed its use
      const result = await Promise.race([
        post('/api/interfaces', body).then((r) => ({ r }), (e) => ({ e })),
        new Promise((r) => setTimeout(() => r({ lost: true }), SWITCH_WAIT_MS)),
      ]);
      if (result.lost || result.e?.status === 0) {
        toast('The router is switching the ports. Move the cables, then open this page and confirm within 5 minutes.', 'warn');
        return;
      }
      if (result.e) { toast(result.e.message, 'crit'); return; }
      toast(result.r.data?.pending ? result.r.message : 'Saved, nothing has changed.', result.r.data?.pending ? 'warn' : 'ok');
      draft = {};
      load();
    }

    async function identify(p, btn) {
      btn.disabled = true;
      try { toast((await post('/api/interfaces/identify', { mac: p.mac })).message); } catch (e) { toast(e.message, 'crit'); }
      setTimeout(() => { btn.disabled = false; }, 3000);
    }

    async function confirmChange() {
      try { toast((await post('/api/settings/confirm')).message); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    async function revert() {
      try { toast((await post('/api/settings/revert')).message, 'warn'); } catch (e) { toast(e.message, 'crit'); }
      draft = {};
      load();
    }

    load();
    const stop = every(10000, load);
    return () => { stop(); clearInterval(countdown); };
  },
};
