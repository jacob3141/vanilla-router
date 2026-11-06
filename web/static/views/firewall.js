// SPDX-License-Identifier: AGPL-3.0-or-later
import { t } from '../i18n.js';
import { h, clear, icon, badge, toast, confirmDialog, formDialog } from '../ui.js';
import { get, post } from '../api.js';

const PROTO = { tcp: 'TCP', udp: 'UDP', both: 'TCP + UDP' };

export default {
  title: 'Firewall',
  subtitle: 'Port forwards and rules',
  render(root) {
    let data = null;
    let countdown = null;

    const pendingBox = h('div');
    const banner = h('div');
    const tbody = h('tbody');
    const rules = h('div');
    const f2b = h('div');
    const natHint = h('div');

    root.append(banner, pendingBox,
      h('div', { class: 'grid cols-2' },
        h('section', { class: 'card span-2' },
          h('div', { class: 'card-head' },
            h('span', { class: 'icon-badge' }, icon('arrowRight')), h('h2', null, 'Port forwards'),
            h('button', { class: 'btn primary', onclick: () => edit(null) }, icon('plus'), 'Port forward')),
          natHint,
          h('div', { class: 'table-wrap' }, h('table', null,
            h('thead', null, h('tr', null, ['Active', 'Protocol', 'External', 'Target in the LAN', 'Note', ''].map((t) => h('th', null, t)))),
            tbody))),
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('shield')), h('h2', null, 'Basic rules')),
          h('ul', { class: 'muted small list' },
            h('li', null, 'The router cannot be reached from the internet, for IPv4 and IPv6 (policy drop).'),
            h('li', null, 'Everything is allowed from the home network, devices may use the internet.'),
            h('li', null, 'Responses to your own connections get through, everything else only through a port forward.'))),
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('globe')), h('h2', null, 'IPv6 pinholes')),
          h('p', { class: 'muted small' }, 'Devices in the home network currently have no public IPv6 addresses, because the upstream router does not pass a prefix on to the LAN (prefix delegation). Pinholes for IPv6 are therefore neither possible nor needed at the moment.')),
        h('section', { class: 'card span-2' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('lock')), h('h2', null, 'Blocked attackers (fail2ban)')),
          h('p', { class: 'muted small' }, 'fail2ban blocks IP addresses after repeated failed attempts, e.g. with SSH or the login to this web interface.'),
          f2b),
        h('section', { class: 'card span-2' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('file')), h('h2', null, 'Loaded rules')),
          rules)));

    async function loadF2B() {
      let jails;
      try { jails = await get('/api/fail2ban'); } catch (e) { clear(f2b).append(h('p', { class: 'muted' }, e.message)); return; }
      clear(f2b).append(h('div', { class: 'grid cols-2' }, jails.map((j) => h('div', { class: 'jail' },
        h('div', { class: 'row' }, h('strong', { class: 'mono' }, j.name), h('div', { class: 'spacer' }),
          badge(`${j.current_banned} blocked`, j.current_banned ? 'warn' : 'ok'), badge(`${j.total_failed} failed attempts`)),
        j.banned.length ? h('div', { class: 'chip-list mt' }, j.banned.map((ip) => h('span', { class: 'chip' }, h('span', { class: 'mono' }, ip),
          h('button', { class: 'btn sm ghost', title: 'Remove block', onclick: () => unban(j.name, ip) }, '×'))))
          : h('p', { class: 'muted small' }, 'Nobody blocked.')))));
    }

    async function unban(jail, ip) {
      if (!(await confirmDialog({ title: `Unblock ${ip}?`, text: `The address may then connect to ${jail} again.`, confirm: 'Unblock' }))) return;
      try { toast((await post('/api/fail2ban/unban', { jail, ip })).message); } catch (e) { toast(e.message, 'crit'); }
      loadF2B();
    }
    loadF2B();

    async function load() {
      try { data = await get('/api/firewall'); } catch (e) { clear(banner).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      clear(banner);
      const dn = data.double_nat;
      clear(natHint);
      if (dn && dn.cgnat) {
        natHint.append(h('div', { class: 'banner warn' }, icon('alert'), h('div', null,
          h('strong', null, 'Shared address (CGNAT): '),
          'Your provider shares one IPv4 address with other customers. Port forwards over IPv4 therefore do not arrive. It works over IPv6 (pinholes).')));
      } else if (dn) {
        natHint.append(h('div', { class: 'banner' }, icon('info'), h('div', null,
          h('strong', null, 'Double NAT: '),
          `There is another router in front of this router (${dn.gateway || 'unknown'}). For a port forward from the internet to arrive, the same port must be forwarded there to `,
          h('span', { class: 'mono' }, dn.wan_ip), '.')));
      }
      if (data.dry_run) banner.append(h('div', { class: 'banner warn' }, icon('info'), 'Test instance: changes are checked, but not applied.'));
      drawPending(data.pending_until);
      drawTable();
      drawRules();
    }

    function drawTable() {
      clear(tbody);
      const list = data.port_forwards || [];
      if (!list.length) { tbody.append(h('tr', null, h('td', { colspan: 6, class: 'empty' }, 'No port forwards. Nothing gets into the home network from the internet.'))); return; }
      for (const p of [...list].sort((a, b) => a.ext_port - b.ext_port)) {
        const ext = p.ext_port_end ? `${p.ext_port}–${p.ext_port_end}` : String(p.ext_port);
        const intPort = p.ext_port_end ? 'same ports' : String(p.int_port || p.ext_port);
        const name = data.devices[p.int_ip];
        tbody.append(h('tr', null,
          h('td', null, h('input', { type: 'checkbox', checked: p.enabled, title: p.enabled ? 'Disable' : 'Enable', onchange: () => save({ ...p, enabled: !p.enabled }) })),
          h('td', null, badge(PROTO[p.proto])),
          h('td', { class: 'mono' }, ext),
          h('td', null, h('span', { class: 'mono' }, `${p.int_ip} : ${intPort}`), name && h('div', { class: 'muted small' }, name)),
          h('td', { class: 'muted' }, p.comment || ''),
          h('td', { class: 'actions' },
            h('button', { class: 'btn sm ghost icon', title: 'Edit', onclick: () => edit(p) }, icon('edit')),
            h('button', { class: 'btn sm ghost icon danger', title: 'Delete', onclick: () => remove(p) }, icon('trash')))));
      }
    }

    function drawRules() {
      clear(rules);
      for (const [name, text] of Object.entries(data.tables)) {
        rules.append(h('details', { class: 'rules' }, h('summary', null, h('span', { class: 'mono' }, name)), h('pre', { class: 'code' }, text)));
      }
      rules.append(h('p', { class: 'muted small' }, 'All loaded tables: ', data.all_tables.join(' · '), '. Tables without "vr_" belong to other services (e.g. Docker).'));
    }

    function drawPending(until) {
      clearInterval(countdown);
      clear(pendingBox);
      if (!until) return;
      const left = h('strong', { class: 'num' });
      const box = h('div', { class: 'banner warn pending' }, icon('alert'),
        h('div', { class: 'spacer' }, 'The change is active. Without confirmation it is rolled back automatically in ', left, '.'),
        h('button', { class: 'btn primary', onclick: confirm }, icon('check'), 'Confirm'));
      pendingBox.append(box);
      const tick = () => {
        const s = Math.max(0, Math.round((new Date(until) - Date.now()) / 1000));
        left.textContent = t(`${s} s`);
        if (s <= 0) { clearInterval(countdown); setTimeout(load, 1500); }
      };
      tick();
      countdown = setInterval(tick, 1000);
    }

    async function confirm() {
      try { toast((await post('/api/portforwards/confirm')).message); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    async function save(p) {
      try {
        const res = await post('/api/portforwards', p);
        toast(res.message, 'warn');
      } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    async function edit(p) {
      const ok = await formDialog({
        title: p ? 'Edit port forward' : 'New port forward',
        text: 'Forwards connections from the internet to a device in the home network. Best give the target a fixed address first.',
        fields: [
          { name: 'proto', label: 'Protocol', type: 'select', value: p?.proto || 'tcp', options: Object.entries(PROTO).map(([value, label]) => ({ value, label })) },
          { name: 'ext', label: 'External port or range', value: p ? (p.ext_port_end ? `${p.ext_port}-${p.ext_port_end}` : String(p.ext_port)) : '', placeholder: 'z. B. 8080 oder 27015-27030' },
          { name: 'int_ip', label: 'Target IP in the home network', value: p?.int_ip || '', placeholder: 'z. B. 192.168.42.20', hint: Object.entries(data.devices).map(([ip, n]) => `${n} = ${ip}`).join(' · ') || '' },
          { name: 'int_port', label: 'Target port (optional)', value: p?.int_port ? String(p.int_port) : '', placeholder: 'empty = same as external', hint: 'With a port range this field stays empty.' },
          { name: 'comment', label: 'Note (optional)', value: p?.comment || '', placeholder: 'e.g. game server' },
          { name: 'enabled', label: 'Active', type: 'checkbox', value: p ? p.enabled : true },
        ],
        submit: 'Apply',
        onSubmit: (v) => {
          const m = /^(\d+)(?:\s*[-–]\s*(\d+))?$/.exec(v.ext);
          if (!m) throw new Error('External port: a number or a range like 27015-27030.');
          if (v.int_port && !/^\d+$/.test(v.int_port)) throw new Error('The target port must be a number.');
          return post('/api/portforwards', {
            id: p?.id || '', proto: v.proto, ext_port: +m[1], ext_port_end: m[2] ? +m[2] : 0,
            int_ip: v.int_ip, int_port: v.int_port ? +v.int_port : 0, comment: v.comment, enabled: v.enabled,
          });
        },
      });
      if (ok) { toast(ok.message, 'warn'); load(); }
    }

    async function remove(p) {
      if (!(await confirmDialog({ title: `Remove the port forward of port ${p.ext_port}?`, text: `Connections to port ${p.ext_port} then no longer reach ${p.int_ip}.`, confirm: 'Remove', danger: true }))) return;
      try { toast((await post('/api/portforwards/delete', { id: p.id })).message, 'warn'); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    load();
    return () => clearInterval(countdown);
  },
};
