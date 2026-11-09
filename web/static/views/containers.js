// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, every, clear, icon, badge, toast, confirmDialog, formDialog, logDialog, stateKind, STATE_LABEL, fmt } from '../ui.js';
import { get, post } from '../api.js';
import { lines, text, jobWatcher, textDialog, removeWith, webhookDialog } from './docker-common.js';
import { openTerminal } from './terminal.js';
import { chartElement, drawLines } from '../charts.js';

const RESTART = { 'unless-stopped': 'automatic (unless stopped)', always: 'always automatic', 'on-failure': 'on errors', no: 'no' };
const PARENT_ROLE = { lan: 'LAN', wan: 'WAN', segment: 'Segment', free: 'Free port' };

export default {
  title: 'Containers',
  subtitle: 'Docker containers on the router',
  render(root) {
    const banner = h('div');
    const summary = h('div', { class: 'row', style: { marginBottom: '12px' } });
    const grid = h('div', { class: 'grid cols-2' });
    const job = jobWatcher(root, () => { last = ''; load(); });
    let nets = { networks: [], parents: [] };
    let last = '';
    let updates = {};
    root.append(banner, job.box, h('div', { class: 'row' }, summary, h('div', { class: 'spacer' }), h('button', { class: 'btn', onclick: checkUpdates }, icon('refresh'), 'Check for updates'), h('button', { class: 'btn primary', onclick: () => editor(null) }, icon('plus'), 'Container')), grid);

    const portLabel = (name) => { const p = nets.parents.find((x) => x.name === name); return p ? `${PARENT_ROLE[p.role] || ''}${p.role === 'segment' || p.role === 'free' ? ` ${name}` : ''}` : name || ''; };

    function draw(list) {
      const key = JSON.stringify(list);
      if (key === last) return;
      last = key;
      const running = list.filter((c) => c.state === 'running').length;
      clear(summary).append(badge(`${running} running`, 'ok'), badge(`${list.length - running} stopped`));
      clear(grid);
      if (!list.length) grid.append(h('section', { class: 'card empty' }, 'No containers.'));
      for (const c of list) {
        const up = c.state === 'running';
        const paused = c.state === 'paused';
        const age = c.image_created ? Math.floor((Date.now() - new Date(c.image_created)) / 86400000) : null;
        grid.append(h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: `dot ${stateKind(c.state)}` }), h('h2', null, c.name), badge(STATE_LABEL[c.state] || c.state, stateKind(c.state)),
            c.health && badge(c.health === 'healthy' ? 'healthy' : c.health, c.health === 'healthy' ? 'ok' : 'warn'),
            updates[c.image] === 'update' && badge('update available', 'info')),
          h('dl', { class: 'kv' },
            h('dt', null, 'Image'), h('dd', { class: 'mono' }, c.image),
            h('dt', null, 'Image from'), h('dd', null, c.image_created ? fmt.date(c.image_created) : '–', age != null && age > 90 && badge(`${age} days old`, 'warn')),
            h('dt', null, 'Network'), h('dd', null, c.network || '–'),
            h('dt', null, 'IP'), h('dd', { class: 'mono' }, c.ip || '–'),
            h('dt', null, 'Restart'), h('dd', null, RESTART[c.restart_policy] || c.restart_policy || 'no'),
            c.docker_sock && [h('dt', null, 'Note'), h('dd', null, badge('has docker.sock = root rights', 'crit'))]),
          h('div', { class: 'row end wrap', style: { marginTop: '14px' } },
            up && !paused && [btn('Stop', () => control(c, 'stop', true), 'stop', 'danger'), btn('Restart', () => control(c, 'restart', true), 'refresh'), btn('Pause', () => control(c, 'pause'))],
            paused && btn('Resume', () => control(c, 'unpause'), 'play', 'primary'),
            !up && !paused && btn('Start', () => control(c, 'start'), 'play', 'primary'),
            up && btn('Kill', () => control(c, 'kill', true)),
            btn('Logs', () => logs(c), 'file'), btn('Details', () => details(c), 'info'), up && btn('Stats', () => stats(c), 'activity'), up && btn('Console', () => consoleFor(c), 'tools'),
            btn('Edit', () => editor(c), 'edit'), btn('Update', () => update(c), 'download'), btn('More', () => more(c), 'settings'),
            h('button', { class: 'btn sm icon danger', title: 'Remove', onclick: () => removeWith('/api/containers/remove', { name: c.name }, `Remove ${c.name}?`, 'The container is stopped and deleted. Named volumes and folders of the router stay.', reload) }, icon('trash')))));
      }
    }
    const btn = (label, onclick, ic, cls = '') => h('button', { class: `btn sm ${cls}`, onclick }, ic && icon(ic), label);
    const reload = () => { last = ''; load(); };

    async function load() {
      const [list, net, up] = await Promise.all([get('/api/containers').catch((e) => e), get('/api/containers/networks').catch(() => null), get('/api/containers/updates').catch(() => null)]);
      if (up && JSON.stringify(up) !== JSON.stringify(updates)) { updates = up; last = ''; }
      if (list instanceof Error) { clear(banner).append(h('div', { class: 'banner crit' }, icon('alert'), list.message)); return; }
      clear(banner);
      if (net) nets = net;
      draw(list);
    }

    async function control(c, action, ask) {
      if (ask && !(await confirmDialog({ title: `${c.name}: ${action}?`, text: 'The service in the container is interrupted.', confirm: action[0].toUpperCase() + action.slice(1), danger: action !== 'restart' }))) return;
      try { await post('/api/containers/action', { name: c.name, action }); toast('Done.'); } catch (e) { toast(e.message, 'crit'); }
      reload();
    }

    function logs(c) {
      return logDialog(`Logs: ${c.name}`, async () => (await get(`/api/containers/logs?name=${encodeURIComponent(c.name)}&lines=1000`)).text);
    }

    async function details(c) {
      try {
        const [i, top, st] = await Promise.all([post('/api/containers/inspect', { name: c.name }), post('/api/containers/top', { name: c.name }).catch(() => null), post('/api/containers/stats', { name: c.name }).catch(() => null)]);
        let stats = '';
        try { const s = JSON.parse(st.data.text); stats = `CPU ${s.CPUPerc}   Memory ${s.MemUsage} (${s.MemPerc})   Network ${s.NetIO}   Disk ${s.BlockIO}   Processes ${s.PIDs}\n\n`; } catch { /* stopped */ }
        const pretty = (() => { try { return JSON.stringify(JSON.parse(i.data.text)[0], null, 2); } catch { return i.data.text; } })();
        textDialog(c.name, `${stats}${top?.data?.text ? `${top.data.text}\n` : ''}${pretty}`);
      } catch (e) { toast(e.message, 'crit'); }
    }

    async function checkUpdates() {
      try { await post('/api/containers/updates/check', {}); job.watch(); } catch (e) { toast(e.message, 'crit'); }
    }

    function consoleFor(c) {
      return formDialog({ title: `Console: ${c.name}`, submit: 'Open', fields: [{ name: 'shell', label: 'Shell', value: '/bin/sh', required: true, hint: 'Many images also have /bin/bash.' }],
        onSubmit: async (v) => { setTimeout(() => openTerminal(c.name, 'exec', v.shell), 0); return true; } });
    }

    function stats(c) {
      const cpu = [];
      const mem = [];
      const chart = chartElement(190);
      const now = h('div', { class: 'muted small' }, 'Measuring …');
      let open = true;
      const dlg = h('dialog', { class: 'wide' }, h('div', { class: 'dialog-body' }, h('h2', null, `Statistics: ${c.name}`), h('div', { class: 'legend' }, h('span', { class: 'legend-item cpu' }, 'CPU'), h('span', { class: 'legend-item mem' }, 'Memory')), chart, now),
        h('div', { class: 'dialog-foot' }, h('button', { class: 'btn', onclick: () => dlg.close() }, 'Close')));
      dlg.addEventListener('close', () => { open = false; dlg.remove(); });
      document.body.appendChild(dlg);
      dlg.showModal();
      (async () => {
        while (open) {
          try {
            const s = JSON.parse((await post('/api/containers/stats', { name: c.name })).data.text);
            const t = Date.now() / 1000;
            cpu.push([t, parseFloat(s.CPUPerc)]); mem.push([t, parseFloat(s.MemPerc)]);
            if (cpu.length > 150) { cpu.shift(); mem.shift(); }
            now.textContent = `CPU ${s.CPUPerc}   Memory ${s.MemUsage} (${s.MemPerc})   Network ${s.NetIO}   Disk ${s.BlockIO}   Processes ${s.PIDs}`;
            drawLines(chart, [{ points: cpu, cls: 'cpu-line' }, { points: mem, cls: 'tx-line' }], { t0: t - 300, t1: t, yMax: Math.max(100, ...cpu.map((p) => p[1])), yFmt: (v) => `${v} %`, tFmt: (x) => (x === t ? 'now' : '−5 min'), gapSec: 30 });
          } catch { break; }
          await new Promise((r) => setTimeout(r, 2000));
        }
      })();
    }

    async function update(c) {
      if (!(await confirmDialog({ title: `Update ${c.name}?`, text: 'The image is downloaded again and the container is replaced with the same settings. Data in volumes stays, data inside the container is lost.', confirm: 'Update' }))) return;
      try {
        const spec = (await post('/api/containers/spec', { name: c.name })).data;
        await post('/api/containers/replace', { old: c.name, pull: true, spec });
        job.watch();
      } catch (e) { toast(e.message, 'crit'); }
    }

    function more(c) {
      const dlg = h('dialog', null,
        h('div', { class: 'dialog-body' }, h('h2', null, c.name),
          h('div', { class: 'row wrap' },
            btn('Rename', () => { dlg.close(); rename(c); }),
            btn('Duplicate', () => { dlg.close(); editor(c, true); }),
            btn('Create image', () => { dlg.close(); commit(c); }),
            btn('Join network', () => { dlg.close(); joinNet(c, false); }),
            btn('Leave network', () => { dlg.close(); joinNet(c, true); }),
            btn('Attach', () => { dlg.close(); openTerminal(c.name, 'attach'); }),
            btn('Webhook', () => { dlg.close(); webhookDialog('container', c.name); }))),
        h('div', { class: 'dialog-foot' }, h('button', { class: 'btn', onclick: () => dlg.close() }, 'Close')));
      dlg.addEventListener('close', () => dlg.remove());
      document.body.appendChild(dlg);
      dlg.showModal();
    }
    const rename = (c) => formDialog({ title: `Rename ${c.name}`, fields: [{ name: 'new', label: 'New name', value: c.name, required: true }], onSubmit: (v) => post('/api/containers/rename', { name: c.name, new: v.new }) }).then(reload);
    const commit = (c) => formDialog({ title: `Create an image from ${c.name}`, submit: 'Create', fields: [{ name: 'image', label: 'Image name', placeholder: 'e.g. my/backup:1', required: true }], onSubmit: (v) => post('/api/containers/commit', { name: c.name, image: v.image }) }).then((ok) => { if (ok) toast('Image created.'); });
    const netOptions = () => nets.networks.filter((n) => n.name !== 'none').map((n) => ({ value: n.name, label: n.name === 'host' ? 'host (shares the router\'s network)' : `${n.name}${n.parent ? ` (${portLabel(n.parent)})` : ''}` }));
    const joinNet = (c, leave) => formDialog({ title: `${c.name}: ${leave ? 'leave' : 'join'} a network`, submit: leave ? 'Leave' : 'Join',
      fields: [{ name: 'network', label: 'Network', type: 'select', value: netOptions()[0]?.value, options: netOptions() }, ...(leave ? [] : [{ name: 'ip', label: 'IP address', hint: 'Needed on networks with their own addresses.' }])],
      onSubmit: (v) => post('/api/containers/network/connect', { container: c.name, network: v.network, ip: v.ip || '', leave }) }).then(reload);

    async function editor(c, duplicate, prefill) {
      const options = netOptions();
      if (!options.length) { toast('Please create a network first.', 'warn'); return; }
      let s = { restart: 'unless-stopped', network: options[0].value, ...prefill };
      if (c) {
        try { s = (await post('/api/containers/spec', { name: c.name })).data; } catch (e) { toast(e.message, 'crit'); return; }
      }
      if (duplicate) { s.name = `${s.name}-copy`; s.ip = ''; }
      const replacing = c && !duplicate;
      const ok = await formDialog({
        title: replacing ? `Edit ${c.name}` : 'New container', submit: replacing ? 'Replace' : 'Create',
        text: replacing ? 'Docker cannot change a container in place: it is replaced with these settings. Data in volumes stays, the old container comes back if the new one does not start.' : '',
        fields: [
          { name: 'name', label: 'Name', value: s.name, placeholder: 'e.g. media-server', required: true },
          { name: 'image', label: 'Image', value: s.image, placeholder: 'e.g. jellyfin/jellyfin:latest', required: true },
          { name: 'always_pull', label: 'Always download the image first', type: 'checkbox', value: false },
          { name: 'network', label: 'Network', type: 'select', value: s.network, options },
          { name: 'ip', label: 'IP address', value: s.ip, placeholder: 'e.g. 192.168.42.60', hint: 'Needed on networks with their own addresses. It must be free and outside the range for devices.' },
          { name: 'restart', label: 'Restart', type: 'select', value: s.restart, options: Object.entries(RESTART).map(([value, label]) => ({ value, label })) },
          { name: 'mounts', label: 'Storage', type: 'textarea', value: text(s.mounts), placeholder: 'media:/data\n/srv/movies:/movies:ro', hint: 'One per line: a volume name or a folder under /docker, /srv, /media or /mnt, then the path in the container. Add :ro for read only.' },
          { name: 'env', label: 'Environment', type: 'textarea', value: text(s.env), placeholder: 'TZ=Europe/Berlin', hint: 'One per line, NAME=value.' },
          { type: 'section', label: 'Command' },
          { name: 'command', label: 'Command', value: s.command },
          { name: 'entrypoint', label: 'Entrypoint', value: s.entrypoint },
          { name: 'workdir', label: 'Working folder', value: s.workdir },
          { name: 'user', label: 'User', value: s.user, placeholder: 'e.g. 1000:1000' },
          { name: 'init', label: 'Run an init process', type: 'checkbox', value: !!s.init },
          { name: 'auto_remove', label: 'Remove when it stops', type: 'checkbox', value: !!s.auto_remove },
          { type: 'section', label: 'Network details' },
          { name: 'hostname', label: 'Host name', value: s.hostname },
          { name: 'domain', label: 'Domain', value: s.domain },
          { name: 'mac', label: 'MAC address', value: s.mac },
          { name: 'ipv6', label: 'IPv6 address', value: s.ipv6 },
          { name: 'dns', label: 'DNS servers', type: 'textarea', value: text(s.dns), hint: 'One per line.' },
          { name: 'hosts', label: 'Hosts entries', type: 'textarea', value: text(s.hosts), placeholder: 'nas:192.168.42.5', hint: 'One per line, name:address.' },
          { type: 'section', label: 'Labels and logging' },
          { name: 'labels', label: 'Labels', type: 'textarea', value: text(s.labels), hint: 'One per line, name=value.' },
          { name: 'log_driver', label: 'Logging driver', value: s.log_driver, placeholder: 'json-file' },
          { name: 'log_opts', label: 'Logging options', type: 'textarea', value: text(s.log_opts), placeholder: 'max-size=10m' },
          { type: 'section', label: 'Limits' },
          { name: 'memory', label: 'Memory limit', value: s.memory, placeholder: 'e.g. 512m' },
          { name: 'memory_reservation', label: 'Memory reservation', value: s.memory_reservation },
          { name: 'cpus', label: 'CPU limit', value: s.cpus, placeholder: 'e.g. 1.5' },
          { name: 'shm_size', label: 'Shared memory', value: s.shm_size, placeholder: 'e.g. 64m' },
          { type: 'section', label: 'Advanced: can reach into the router' },
          { name: 'advanced', label: 'Allow these settings, and folders anywhere on the router', type: 'checkbox', value: !!s.advanced },
          { name: 'privileged', label: 'Privileged', type: 'checkbox', value: !!s.privileged },
          { name: 'devices', label: 'Devices', type: 'textarea', value: text(s.devices), placeholder: '/dev/ttyUSB0:/dev/ttyUSB0' },
          { name: 'cap_add', label: 'Add capabilities', type: 'textarea', value: text(s.cap_add), placeholder: 'NET_ADMIN' },
          { name: 'cap_drop', label: 'Drop capabilities', type: 'textarea', value: text(s.cap_drop) },
          { name: 'sysctls', label: 'Sysctls', type: 'textarea', value: text(s.sysctls) },
          { name: 'security_opt', label: 'Security options', type: 'textarea', value: text(s.security_opt) },
          { name: 'gpus', label: 'GPUs', value: s.gpus, placeholder: 'all' },
        ],
        onSubmit: async (v) => {
          const list = ['mounts', 'env', 'dns', 'hosts', 'labels', 'log_opts', 'devices', 'cap_add', 'cap_drop', 'sysctls', 'security_opt'];
          const spec = { ...v };
          for (const k of list) spec[k] = lines(v[k]);
          if (v.advanced && !s.advanced && !(await confirmDialog({ title: 'Allow a container to reach into the router?', danger: true, confirm: 'Allow', text: 'With these settings a program in the container can take over the whole router. Use them only for software you trust.' }))) throw new Error('Not saved.');
          if (replacing) return post('/api/containers/replace', { old: c.name, pull: v.always_pull, spec });
          return post('/api/containers', spec);
        },
      });
      if (ok) job.watch();
    }

    load();
    // Coming from an application template
    const fromTemplate = sessionStorage.getItem('docker-prefill');
    if (fromTemplate) { sessionStorage.removeItem('docker-prefill'); setTimeout(async () => { await load(); editor(null, false, JSON.parse(fromTemplate)); }, 0); }
    return every(20000, load);
  },
};
