// SPDX-License-Identifier: AGPL-3.0-or-later
// Docker networks, and the state of Docker itself.
import { h, every, clear, icon, badge, confirmDialog, formDialog, fmt } from '../ui.js';
import { get, post } from '../api.js';
import { table, card, empty, trash, removeWith, act, lines } from './docker-common.js';

const hooksNote = 'A webhook redeploys its container or stack when something POSTs to its secret address. You create them on the Containers and Stacks pages.';

const ROLE = { lan: 'LAN', wan: 'WAN', segment: 'Segment', free: 'Free port' };

export const networks = {
  title: 'Docker networks',
  subtitle: 'How containers are connected, and to which interface of the router',
  render(root) {
    const body = h('tbody');
    let data = { networks: [], parents: [] };
    root.append(card('globe', 'Networks', h('button', { class: 'btn sm primary', onclick: create }, icon('plus'), 'Network'),
      h('p', { class: 'muted small' }, 'A network hangs off one interface of the router, and its containers get their own address there.'), table(['Name', 'Driver', 'Interface', 'Network', 'Containers', ''], body)));

    const label = (name) => { const p = data.parents.find((x) => x.name === name); return p ? `${ROLE[p.role] || ''}${p.role === 'segment' || p.role === 'free' ? ` ${name}` : ''}` : name || '–'; };

    async function load() {
      try { data = await get('/api/containers/networks'); } catch { return; }
      clear(body);
      for (const n of data.networks.filter((x) => x.name !== 'none')) {
        body.append(h('tr', null, h('td', { class: 'mono small' }, n.name, n.internal && ' ', n.internal && badge('internal')), h('td', { class: 'muted' }, n.driver),
          h('td', null, n.name === 'host' ? 'Router itself' : label(n.parent)), h('td', { class: 'mono small' }, n.subnet || '–'), h('td', null, String(n.in_use || 0)),
          h('td', { class: 'actions' }, !n.builtin && trash('Remove', () => removeWith('/api/containers/network/remove', { name: n.name }, `Remove ${n.name}?`, 'Only possible if no container uses it.', load), n.in_use > 0))));
      }
    }

    function create() {
      const parents = data.parents;
      const options = parents.map((p) => ({ value: p.name, label: label(p.name) }));
      return formDialog({ title: 'New network', submit: 'Create', fields: [
        { name: 'name', label: 'Name', placeholder: 'e.g. lan-services', required: true },
        { name: 'driver', label: 'Driver', type: 'select', value: 'ipvlan', options: [{ value: 'ipvlan', label: 'ipvlan: own address, shares the MAC' }, { value: 'macvlan', label: 'macvlan: own address and MAC' }, { value: 'bridge', label: 'bridge: only between containers' }] },
        { name: 'mode', label: 'Mode', type: 'select', value: 'l2', options: [{ value: 'l2', label: 'L2' }, { value: 'l3', label: 'L3 (ipvlan only)' }], hint: 'L2 unless you know you need L3.' },
        { name: 'parent', label: 'Interface', type: 'select', value: options[0]?.value, options, hint: 'Not used by a bridge.' },
        { name: 'subnet', label: 'Network', placeholder: parents[0]?.subnet || 'e.g. 192.168.42.0/24', hint: 'Empty: taken from the interface.' },
        { name: 'gateway', label: 'Gateway', placeholder: parents[0]?.gateway || '', hint: 'Empty: taken from the interface.' },
        { name: 'subnet6', label: 'IPv6 network' },
        { name: 'gateway6', label: 'IPv6 gateway' },
        { name: 'labels', label: 'Labels', type: 'textarea', hint: 'One per line, name=value.' },
        { name: 'internal', label: 'Internal: no way out through a gateway', type: 'checkbox', value: false },
      ], onSubmit: async (v) => {
        const p = parents.find((x) => x.name === v.parent);
        const bridge = v.driver === 'bridge';
        if (!bridge && p.role === 'wan' && !(await confirmDialog({ title: 'Connect containers directly to the internet port?', danger: true, confirm: 'Connect',
          text: 'Containers on this network sit directly on the internet side. Their traffic does not pass through the router, so the firewall does not protect them. Each container must protect itself.' }))) throw new Error('Not created.');
        return post('/api/containers/network', { name: v.name, driver: v.driver, mode: v.mode, parent: bridge ? '' : v.parent, subnet: v.subnet || (bridge ? '' : p.subnet), gateway: v.internal || bridge ? v.gateway : (v.gateway || p.gateway),
          subnet6: v.subnet6, gateway6: v.gateway6, labels: lines(v.labels), internal: v.internal });
      } }).then((ok) => { if (ok) load(); });
    }
    load();
    return every(30000, load);
  },
};

export const host = {
  title: 'Docker host',
  subtitle: 'State of Docker, events and registries',
  render(root) {
    const counts = h('div', { class: 'grid cols-4' });
    const info = h('dl', { class: 'kv' });
    const disk = h('tbody');
    const regs = h('tbody');
    const events = h('tbody');
    const hooks = h('tbody');
    root.append(counts,
      card('system', 'Docker', null, info),
      card('drive', 'Disk use', null, table(['Kind', 'Total', 'Active', 'Size', 'Reclaimable'], disk)),
      card('lock', 'Registries', h('button', { class: 'btn sm primary', onclick: login }, icon('plus'), 'Log in'), h('p', { class: 'muted small' }, 'Accounts for private images. Docker Hub works without one.'), table(['Server', ''], regs)),
      card('settings', 'Webhooks', null, h('p', { class: 'muted small' }, hooksNote), table(['For', 'Created', ''], hooks)),
      card('activity', 'Events of the last 24 hours', null, table(['Time', 'Type', 'Action', 'Object'], events)));

    async function load() {
      const [d, ev, r, cs, im, vo, ne, st] = await Promise.all([get('/api/docker/host').catch(() => null), get('/api/docker/events').catch(() => []), get('/api/docker/registries').catch(() => []),
        get('/api/containers').catch(() => []), get('/api/containers/images').catch(() => []), get('/api/containers/volumes').catch(() => []), get('/api/containers/networks').catch(() => ({ networks: [] })), get('/api/stacks').catch(() => [])]);
      const tile = (label, value, sub, href) => h('a', { class: 'card stat', href }, h('div', { class: 'stat-label' }, label), h('div', { class: 'stat-value' }, String(value)), h('div', { class: 'stat-sub' }, sub));
      clear(counts).append(tile('Containers', cs.length, `${cs.filter((c) => c.state === 'running').length} running`, '#/containers'), tile('Stacks', st.length, '', '#/stacks'), tile('Images', im.length, `${im.filter((i) => !i.in_use).length} unused`, '#/images'),
        tile('Volumes', vo.length, `${vo.filter((v) => !v.used_by.length).length} unused · ${ne.networks.filter((n) => !n.builtin).length} networks`, '#/volumes'));
      const i = d?.info || {};
      clear(info).append(...[['Version', i.ServerVersion], ['Operating system', i.OperatingSystem], ['Kernel', i.KernelVersion], ['Storage driver', i.Driver], ['Logging driver', i.LoggingDriver], ['CPUs', i.NCPU], ['Memory', i.MemTotal ? fmt.bytes(i.MemTotal) : ''], ['Folder', i.DockerRootDir]]
        .flatMap(([k, v]) => [h('dt', null, k), h('dd', null, v == null || v === '' ? '–' : String(v))]));
      clear(disk);
      for (const x of d?.disk || []) disk.append(h('tr', null, h('td', null, x.Type), h('td', null, x.TotalCount), h('td', null, x.Active), h('td', null, x.Size), h('td', { class: 'muted' }, x.Reclaimable)));
      clear(regs);
      empty(regs, r.length, 2, 'Not logged in anywhere.');
      for (const s of r) regs.append(h('tr', null, h('td', { class: 'mono small' }, s), h('td', { class: 'actions' }, trash('Log out', () => act('/api/docker/registry/logout', { server: s }, 'Logged out.', load)))));
      const hl = await get('/api/docker/webhooks').catch(() => []);
      clear(hooks);
      empty(hooks, hl.length, 3, 'No webhooks.');
      for (const w of hl) hooks.append(h('tr', null, h('td', null, `${w.kind}: ${w.name}`), h('td', { class: 'muted small' }, fmt.date(w.created)), h('td', { class: 'actions' }, trash('Remove', () => removeWith('/api/docker/webhook/remove', { id: w.id }, 'Remove this webhook?', 'Its address stops working.', load)))));
      clear(events);
      empty(events, ev.length, 4, 'Nothing happened.');
      for (const e of ev.slice(0, 200)) events.append(h('tr', null, h('td', { class: 'muted small' }, fmt.date(new Date(e.time * 1000).toISOString())), h('td', null, e.Type), h('td', null, e.Action), h('td', { class: 'mono small' }, e.Actor?.Attributes?.name || e.Actor?.ID?.slice(0, 12) || '')));
    }

    function login() {
      return formDialog({ title: 'Log in to a registry', submit: 'Log in', fields: [
        { name: 'server', label: 'Server', placeholder: 'Empty for Docker Hub, otherwise e.g. ghcr.io' },
        { name: 'user', label: 'User name', required: true },
        { name: 'password', label: 'Password or token', type: 'password', required: true },
      ], onSubmit: (v) => post('/api/docker/registry/login', v) }).then((ok) => { if (ok) load(); });
    }
    load();
    return every(30000, load);
  },
};
