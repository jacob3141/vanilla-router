// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, every, clear, icon, badge, toast, confirmDialog, logDialog, stateKind, STATE_LABEL, fmt } from '../ui.js';
import { get, post } from '../api.js';

export default {
  title: 'Containers',
  subtitle: 'Docker containers on the router',
  render(root) {
    const grid = h('div', { class: 'grid cols-2' });
    const banner = h('div');
    root.append(banner, grid);

    let last = '';
    async function load() {
      let list;
      try { list = await get('/api/containers'); } catch (e) { clear(banner).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      clear(banner);
      const key = JSON.stringify(list);
      if (key === last) return;
      last = key;
      clear(grid);
      if (!list.length) grid.append(h('section', { class: 'card empty' }, 'No containers.'));
      for (const c of list) {
        const age = c.image_created ? Math.floor((Date.now() - new Date(c.image_created)) / 86400000) : null;
        const running = c.state === 'running';
        grid.append(h('section', { class: 'card' },
          h('div', { class: 'card-head' },
            h('span', { class: `dot ${stateKind(c.state)}` }),
            h('h2', null, c.name),
            badge(STATE_LABEL[c.state] || c.state, stateKind(c.state)),
            c.health && badge(c.health === 'healthy' ? 'healthy' : c.health, c.health === 'healthy' ? 'ok' : 'warn')),
          h('dl', { class: 'kv' },
            h('dt', null, 'Image'), h('dd', { class: 'mono' }, c.image),
            h('dt', null, 'Image from'), h('dd', null, c.image_created ? fmt.date(c.image_created) : '–',
              age != null && age > 90 && badge(`${age} days old`, 'warn')),
            h('dt', null, 'IP'), h('dd', { class: 'mono' }, c.ip || '–'),
            h('dt', null, 'Restart'), h('dd', null, { 'unless-stopped': 'automatic (unless stopped)', always: 'always automatic', 'on-failure': 'on errors', no: 'no' }[c.restart_policy] || c.restart_policy || 'no'),
            c.docker_sock && [h('dt', null, 'Note'), h('dd', null, badge('has docker.sock = root rights', 'crit'))]),
          h('div', { class: 'row end', style: { marginTop: '14px' } },
            h('button', { class: 'btn sm', onclick: () => logDialog(`Logs: ${c.name}`, async () => (await get(`/api/containers/logs?name=${encodeURIComponent(c.name)}&lines=300`)).text) }, icon('file'), 'Logs'),
            running
              ? [h('button', { class: 'btn sm', onclick: () => act(c, 'restart') }, icon('refresh'), 'Restart'),
                 h('button', { class: 'btn sm danger', onclick: () => act(c, 'stop') }, icon('stop'), 'Stop')]
              : h('button', { class: 'btn sm primary', onclick: () => act(c, 'start') }, icon('play'), 'Start'))));
      }
    }

    async function act(c, action) {
      const label = { start: 'start', stop: 'stop', restart: 'restart' }[action];
      if (action !== 'start' && !(await confirmDialog({ title: `${c.name} ${label}?`, text: action === 'stop' ? 'The service in the container is no longer reachable afterwards.' : 'The container is briefly unreachable.', confirm: label[0].toUpperCase() + label.slice(1), danger: action === 'stop' }))) return;
      try { await post('/api/containers/action', { name: c.name, action }); toast(`${c.name}: ${label} done.`); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    load();
    return every(20000, load);
  },
};
