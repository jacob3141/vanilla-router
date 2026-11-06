// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, every, clear, icon, badge, toast, confirmDialog, logDialog, stateKind, STATE_LABEL, fmt } from '../ui.js';
import { get, post } from '../api.js';

const WARN = {
  docker: 'All containers are restarted and are briefly unreachable.',
  'vr-dnsmasq': 'DNS is gone for one or two seconds, devices briefly get no new address. Existing addresses stay valid.',
  ssh: 'Existing SSH connections stay open.',
  'systemd-networkd': 'The network is set up again. Internet and home network may briefly go down.',
  'vr-firewall': 'The firewall is reloaded (IPv4 and IPv6).',
  'vr-pppoe': 'The internet connection is established again. That takes a few seconds, the IPv4 address may change.',
  'vr-hostapd': 'Wi-Fi is gone for a few seconds, devices reconnect by themselves.',
};

export default {
  title: 'Services',
  subtitle: 'System services of the router, restart and logs',
  render(root) {
    const tbody = h('tbody');
    const banner = h('div');
    root.append(banner, h('section', { class: 'card' },
      h('div', { class: 'table-wrap' }, h('table', null,
        h('thead', null, h('tr', null, ['', 'Service', 'Status', 'Since', 'Autostart', ''].map((t) => h('th', null, t)))),
        tbody))));

    let last = '';
    async function load() {
      let list;
      try { list = await get('/api/services'); } catch (e) { clear(banner).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      clear(banner);
      // Only redraw if something has changed, otherwise clicks during the rebuild get lost
      const key = JSON.stringify(list);
      if (key === last) return;
      last = key;
      clear(tbody).append(...list.map((u) => h('tr', null,
        h('td', null, h('span', { class: `dot ${stateKind(u.active)}` })),
        h('td', null, h('div', { class: 'cell-main mono' }, u.name), h('div', { class: 'muted small' }, u.description)),
        h('td', null, badge(STATE_LABEL[u.active] || u.active, stateKind(u.active)), u.sub && !['running', 'exited', 'dead', u.active].includes(u.sub) && h('span', { class: 'muted small' }, ` ${u.sub}`)),
        h('td', { class: 'muted small' }, sinceText(u.since)),
        h('td', null, u.enabled === 'enabled' ? badge('an', 'ok') : badge(u.enabled || '–')),
        h('td', { class: 'actions' },
          h('button', { class: 'btn sm', onclick: () => logDialog(`Logs: ${u.name}`, async () => (await get(`/api/services/logs?unit=${encodeURIComponent(u.name)}&lines=300`)).text) }, icon('file'), 'Logs'),
          u.restartable && h('button', { class: 'btn sm', onclick: () => restart(u) }, icon('refresh'), 'Restart')))));
    }

    // systemctl returns "@<unix-seconds>" (option --timestamp=unix in the helper)
    function sinceText(v) {
      const m = /^@(\d+)/.exec(v || '');
      return m && m[1] !== '0' ? fmt.date(new Date(+m[1] * 1000).toISOString()) : '–';
    }

    async function restart(u) {
      if (!(await confirmDialog({ title: `Restart ${u.name}?`, text: WARN[u.name] || 'The service is briefly interrupted.', confirm: 'Restart' }))) return;
      try { toast((await post('/api/services/restart', { unit: u.name })).message); } catch (e) { toast(e.message, 'crit'); }
      last = '';
      load();
    }

    load();
    return every(15000, load);
  },
};
