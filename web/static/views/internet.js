// SPDX-License-Identifier: AGPL-3.0-or-later
import { t } from '../i18n.js';
// Internet port, home network and router name (F-UI-03, F-UI-08, F-UI-09).
// With ?setup=1 this is the setup step "internet access" after the first login.
import { h, clear, icon, toast, badge, confirmDialog } from '../ui.js';
import { get, post } from '../api.js';

const TYPES = [
  { value: 'dhcp', label: 'Automatic', hint: 'A router or modem in front hands out the address (cable, many fiber connections).' },
  { value: 'pppoe', label: 'With credentials', hint: 'Dial-up with the user name and password from your provider\'s letter (DSL, Telekom fiber).' },
  { value: 'static', label: 'Fixed address', hint: 'The provider has specified a fixed IP address (rare, usually business customers).' },
];

const LEASES = [['1h', '1 hour'], ['12h', '12 hours'], ['24h', '1 day'], ['168h', '1 week']];

export const SETUP_LATER_KEY = 'vr-internet-setup-later';

export default {
  title: 'Internet & home network',
  subtitle: 'Connection, addresses and router name',
  render(root) {
    const setupMode = /[?&]setup=1/.test(location.hash);
    let data = null;
    let countdown = null;
    const pendingBox = h('div');
    const top = h('div');
    const body = h('div', { class: 'grid cols-2' });
    root.append(top, pendingBox, body);

    async function load() {
      try { data = await get('/api/settings'); } catch (e) { clear(top).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      clear(top);
      if (data.dry_run) top.append(h('div', { class: 'banner warn' }, icon('info'), 'Test instance: changes are checked, but not applied.'));
      if (data.needs_setup) top.append(setupIntro());
      drawPending();
      clear(body).append(wanCard(), systemCard(), lanCard(), dhcpCard());
    }

    // --- Setup step -----------------------------------------------------------
    function setupIntro() {
      const d = data.detected || {};
      const text = d.method === 'pppoe'
        ? `The router has detected that your connection needs a dial-up with credentials${d.vlan ? ` (over VLAN ${d.vlan}, typical for Telekom)` : ''}. The credentials are in your provider's letter.`
        : 'The modem did not answer during installation. Please choose how your connection works. If you are unsure: with cable connections and if another router is in front, it is "Automatic".';
      return h('section', { class: 'card hero' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('globe')), h('h2', null, setupMode ? 'Almost there: set up internet access' : 'Internet access is not set up yet')),
        h('p', null, text),
        setupMode && h('div', { class: 'row end' }, h('button', { class: 'btn', onclick: later }, 'Later')));
    }

    function later() {
      try { localStorage.setItem(SETUP_LATER_KEY, String(Date.now())); } catch { /* privater Modus */ }
      location.hash = '#/dashboard';
    }

    // --- Confirmation -------------------------------------------------------------------
    function drawPending() {
      clearInterval(countdown);
      clear(pendingBox);
      const p = data.pending;
      if (!p) return;
      const left = h('strong', { class: 'num' });
      const net = h('span', { class: 'muted' }, ' Checking the internet …');
      pendingBox.append(h('div', { class: 'banner warn pending' }, icon('alert'),
        h('div', { class: 'spacer' }, 'The new setting is active. Without confirmation it is rolled back automatically in ', left, '.', net),
        h('button', { class: 'btn', onclick: revert }, 'Roll back'),
        h('button', { class: 'btn primary', onclick: confirmChange }, icon('check'), 'Confirm')));
      const tick = () => {
        const s = Math.max(0, Math.round((new Date(p.deadline) - Date.now()) / 1000));
        left.textContent = t(s >= 60 ? `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')} min` : `${s} s`);
        if (s <= 0) { clearInterval(countdown); setTimeout(load, 2000); }
      };
      tick();
      countdown = setInterval(tick, 1000);
      checkInternet(net);
    }

    async function checkInternet(el) {
      for (let i = 0; i < 6 && el.isConnected; i++) {
        try {
          if ((await get('/api/settings/check')).internet) { clear(el).append(' ', badge('Internet reachable', 'ok')); return; }
        } catch { /* keep trying */ }
        await new Promise((r) => setTimeout(r, 5000));
      }
      if (el.isConnected) clear(el).append(' ', badge('Internet not reachable', 'crit'), ' Check the details or roll back the change.');
    }

    async function confirmChange() {
      try { toast((await post('/api/settings/confirm')).message); } catch (e) { toast(e.message, 'crit'); }
      if (setupMode) { location.hash = '#/dashboard'; return; }
      load();
    }

    async function revert() {
      try { toast((await post('/api/settings/revert')).message, 'warn'); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    // --- Internet port ------------------------------------------------------------
    function wanCard() {
      const w = data.wan;
      const d = data.detected || {};
      const err = h('div', { class: 'form-error hidden' });
      // In the setup step, prefill with what was detected
      let type = data.needs_setup && d.method === 'pppoe' ? 'pppoe' : w.type;
      const hasTelekom = w.type === 'pppoe' && (w.pppoe_user || '').endsWith('@t-online.de');
      const telekomGuess = hasTelekom || (data.needs_setup && d.vlan === 7);
      const provider = h('select', { class: 'input' },
        h('option', { value: 'telekom', selected: telekomGuess }, 'Telekom'),
        h('option', { value: 'other', selected: !telekomGuess }, 'another provider'));
      const inp = (attrs) => h('input', { class: 'input', autocomplete: 'off', spellcheck: 'false', ...attrs });
      const ak = inp({ inputmode: 'numeric', placeholder: '12 digits' });
      const zn = inp({ inputmode: 'numeric', placeholder: 'usually 12 digits' });
      const mb = inp({ inputmode: 'numeric', value: '0001' });
      const user = inp({ value: w.pppoe_user, placeholder: 'e.g. customer@provider.example' });
      const pass = h('input', { class: 'input', type: 'password', autocomplete: 'new-password',
        placeholder: w.has_password ? 'leave unchanged' : 'password from the letter' });
      const vlan = inp({ inputmode: 'numeric', value: w.pppoe_vlan ? String(w.pppoe_vlan) : (data.needs_setup && d.vlan ? String(d.vlan) : ''), placeholder: 'empty = none' });
      const addr = inp({ value: w.static_address, placeholder: 'e.g. 203.0.113.10/29' });
      const gw = inp({ value: w.static_gateway, placeholder: 'e.g. 203.0.113.9' });
      const dns = inp({ value: (w.static_dns || []).join(', '), placeholder: 'e.g. 9.9.9.9, 149.112.112.112' });
      const ipv6 = h('input', { type: 'checkbox', checked: w.ipv6 });
      const mtu = inp({ inputmode: 'numeric', value: w.mtu ? String(w.mtu) : '', placeholder: 'empty = automatic' });

      const typeBox = h('div', { class: 'choice-list' });
      const fields = h('div');
      function draw() {
        clear(typeBox).append(...TYPES.map((t) => h('label', { class: `choice ${type === t.value ? 'active' : ''}` },
          h('input', { type: 'radio', name: 'wan-type', value: t.value, checked: type === t.value, onchange: () => { type = t.value; draw(); } }),
          h('div', null, h('div', { class: 'cell-main' }, t.label), h('div', { class: 'muted small' }, t.hint)))));
        clear(fields);
        if (type === 'pppoe') {
          fields.append(field('Provider', provider));
          if (provider.value === 'telekom') {
            fields.append(
              h('p', { class: 'muted small' }, 'The details are in the letter "Your personal access data". The user name is built from them.'),
              hasTelekom && h('p', { class: 'small' }, 'Set up: ', h('span', { class: 'mono' }, w.pppoe_user), '. Leave the fields empty to keep it.'),
              field('Line ID (Anschlusskennung)', ak), field('Access number (T-Online number)', zn), field('Co-user number (Mitbenutzernummer)', mb, 'Usually 0001.'),
              field('Personal password', pass));
          } else {
            fields.append(field('User name', user), field('Password', pass),
              field('VLAN (optional)', vlan, 'Only if the provider names a VLAN ID. Telekom uses 7.'));
          }
        } else if (type === 'static') {
          fields.append(field('IP address with prefix', addr), field('Gateway', gw), field('DNS-Server (optional)', dns, 'Comma-separated. Empty = public DNS servers from the DNS settings.'));
        }
      }
      provider.addEventListener('change', draw);
      draw();

      const save = h('button', { class: 'btn primary', type: 'submit' }, icon('check'), 'Apply');
      const form = h('form', null, err, typeBox, fields,
        h('details', { class: 'mt' }, h('summary', null, 'Advanced'),
          h('label', { class: 'row' }, ipv6, 'Request IPv6 from the provider (recommended)'),
          field('MTU', mtu, 'Only change if the provider requires it.')),
        h('div', { class: 'row end mt' }, save));
      form.addEventListener('submit', async (e) => {
        e.preventDefault();
        err.classList.add('hidden');
        const req = { type, ipv6: ipv6.checked, mtu: mtu.value.trim() ? +mtu.value : 0 };
        if (type === 'pppoe') {
          req.pppoe_password = pass.value;
          if (provider.value === 'telekom' && hasTelekom && !ak.value.trim() && !zn.value.trim()) {
            req.pppoe_user = w.pppoe_user; // keep the previous user name
            req.pppoe_vlan = 7;
          } else if (provider.value === 'telekom') {
            req.telekom = { line_id: ak.value.replace(/\s/g, ''), access_number: zn.value.replace(/\s/g, ''), co_user: mb.value.trim() };
          } else {
            req.pppoe_user = user.value.trim();
            req.pppoe_vlan = vlan.value.trim() ? +vlan.value : 0;
          }
        } else if (type === 'static') {
          Object.assign(req, { static_address: addr.value.trim(), static_gateway: gw.value.trim(), static_dns: dns.value.split(/[\s,;]+/).filter(Boolean) });
        }
        save.disabled = true;
        try {
          const res = await post('/api/settings/wan', req);
          pass.value = '';
          toast(res.data?.pending ? res.message : 'Saved, nothing has changed.', res.data?.pending ? 'warn' : 'ok');
          load();
        } catch (ex) { err.textContent = t(ex.message); err.classList.remove('hidden'); } finally { save.disabled = false; }
      });
      return h('section', { class: 'card span-2' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('globe')), h('h2', null, 'Internet port')),
        form);
    }

    // --- Name and time zone -------------------------------------------------------------
    function systemCard() {
      const err = h('div', { class: 'form-error hidden' });
      const host = h('input', { class: 'input', value: data.hostname, autocomplete: 'off', spellcheck: 'false' });
      const tz = h('select', { class: 'input' }, h('option', { value: data.timezone, selected: true }, data.timezone));
      get('/api/settings/timezones').then((z) => {
        clear(tz).append(...z.zones.map((n) => h('option', { value: n, selected: n === data.timezone }, n.replaceAll('_', ' '))));
      }).catch(() => {});
      const form = h('form', null, err,
        field('Router name', host, `Reachable as ${data.hostname}.${data.domain}. Lowercase letters, digits and hyphens.`),
        field('Time zone', tz, 'Applies to time-based blocks, logs and the time for restarts after updates.'),
        h('div', { class: 'row end' }, h('button', { class: 'btn primary', type: 'submit' }, 'Save')));
      form.addEventListener('submit', async (e) => {
        e.preventDefault();
        err.classList.add('hidden');
        const name = host.value.trim().toLowerCase();
        if (name !== data.hostname && !(await confirmDialog({ title: 'Change the name?',
          text: `The web interface is then reachable at https://${name}.${data.domain}. It restarts briefly for this and gets a new certificate, so the browser warns once more.`, confirm: 'Change' }))) return;
        try {
          const res = await post('/api/settings/system', { hostname: name, timezone: tz.value });
          toast(res.message);
          if (res.data?.restart) moveTo(res.data.new_url);
          else load();
        } catch (ex) { err.textContent = t(ex.message); err.classList.remove('hidden'); }
      });
      return h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('router')), h('h2', null, 'Router')),
        form);
    }

    // --- Home network ----------------------------------------------------------------------
    function lanCard() {
      const l = data.lan;
      const err = h('div', { class: 'form-error hidden' });
      const addr = h('input', { class: 'input mono', value: l.address, autocomplete: 'off', spellcheck: 'false' });
      const prefix = h('select', { class: 'input' }, [16, 20, 22, 23, 24, 25, 26, 27, 28].map((b) =>
        h('option', { value: String(b), selected: b === l.prefix }, `/${b} (${(2 ** (32 - b) - 2).toLocaleString()} devices)`)));
      const form = h('form', null, err,
        field('Router address', addr, 'Private ranges: 192.168.x.x, 10.x.x.x or 172.16–31.x.x.'),
        field('Network size', prefix),
        h('div', { class: 'row end' }, h('button', { class: 'btn primary', type: 'submit' }, 'Change …')));
      form.addEventListener('submit', async (e) => {
        e.preventDefault();
        err.classList.add('hidden');
        const a = addr.value.trim();
        if (a === l.address && +prefix.value === l.prefix) return;
        if (!(await confirmDialog({ title: 'Change the home network?', danger: true, confirm: 'Switch',
          text: `The router is then reachable at https://${a}. Fixed addresses, DNS names and port forwards move along.`,
          details: h('ol', { class: 'small list' },
            h('li', null, 'The connection to this page will drop.'),
            h('li', null, 'Reconnect this device to the home network (turn Wi-Fi off and on or briefly unplug the cable), so that it gets a new address.'),
            h('li', null, `Open https://${a}, sign in and confirm the change within 5 minutes. Otherwise the router restores the old network.`)) }))) return;
        try {
          const res = await post('/api/settings/lan', { address: a, prefix: +prefix.value });
          if (res.data?.restart) moveTo(res.data.new_url, true);
          else load();
        } catch (ex) { err.textContent = t(ex.message); err.classList.remove('hidden'); }
      });
      return h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('devices')), h('h2', null, 'Home network')),
        form);
    }

    function dhcpCard() {
      const l = data.lan;
      const err = h('div', { class: 'form-error hidden' });
      const start = h('input', { class: 'input mono', value: l.pool_start, autocomplete: 'off', spellcheck: 'false' });
      const end = h('input', { class: 'input mono', value: l.pool_end, autocomplete: 'off', spellcheck: 'false' });
      const lease = h('select', { class: 'input' }, LEASES.map(([v, t]) => h('option', { value: v, selected: v === l.lease }, t)),
        !LEASES.some(([v]) => v === l.lease) && h('option', { value: l.lease, selected: true }, l.lease));
      const form = h('form', null, err,
        h('div', { class: 'grid cols-2 tight' }, field('First address', start), field('Last address', end)),
        field('Validity of an address', lease, 'Fixed addresses (Devices page) apply regardless of the range.'),
        h('div', { class: 'row end' }, h('button', { class: 'btn primary', type: 'submit' }, 'Save')));
      form.addEventListener('submit', async (e) => {
        e.preventDefault();
        err.classList.add('hidden');
        try {
          toast((await post('/api/settings/dhcp', { pool_start: start.value.trim(), pool_end: end.value.trim(), lease: lease.value })).message);
          load();
        } catch (ex) { err.textContent = t(ex.message); err.classList.remove('hidden'); }
      });
      return h('section', { class: 'card span-2' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('pin')), h('h2', null, 'Automatic addresses (DHCP)')),
        form);
    }

    // After a change of name or address: a note and a link to the new address.
    function moveTo(url, lan) {
      clearInterval(countdown);
      clear(top); clear(pendingBox);
      clear(body).append(h('section', { class: 'card span-2 hero' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('arrowRight')), h('h2', null, 'The web interface is moving')),
        h('p', null, lan
          ? 'Reconnect this device to the home network now and then open the new address. Confirm the change there within 5 minutes.'
          : 'The web interface is restarting. In a few seconds it will be reachable at the new address.'),
        h('p', null, h('a', { class: 'btn primary', href: url }, url)),
        data.dry_run && h('p', { class: 'muted small' }, 'Test instance: nothing moves here, the old address stays.')));
    }

    load();
    return () => clearInterval(countdown);
  },
};

function field(label, input, hint) {
  return h('div', { class: 'field' }, h('label', null, label), input, hint && h('div', { class: 'hint' }, hint));
}
