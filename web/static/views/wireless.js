// SPDX-License-Identifier: AGPL-3.0-or-later
// Access points and networks with the router as their center (docs/sdn-concept.md).
// Names come from the network and from the owner: only through h().
import { t } from '../i18n.js';
import { h, clear, icon, toast, badge, every, confirmDialog, formDialog, logDialog } from '../ui.js';
import { get, post } from '../api.js';
import { countryList } from './wifi.js';

const POWER = [['low', 'Low'], ['medium', 'Medium'], ['high', 'High']];
const BANDS = ['2.4', '5', '6'];
// Channels offered for pinning, the same blocks the plan uses
const PINS = {
  2.4: [[1, 20], [6, 20], [11, 20]],
  5: [[36, 80], [52, 80], [100, 80], [132, 80], [149, 80], [36, 160], [100, 160]],
  6: [[1, 80], [17, 80], [33, 80], [49, 80], [65, 80], [81, 80], [1, 160], [33, 160], [65, 160]],
};

export default {
  title: 'Access points',
  subtitle: 'Wi-Fi networks and access points of the whole home',
  render(root) {
    let data = null;
    let last = '';
    let countdown = null;
    let dialogs = 0;
    const top = h('div');
    const pendingBox = h('div');
    const body = h('div', { class: 'grid' });
    root.append(top, pendingBox, body);

    async function load(force = true) {
      let d;
      try { d = await get('/api/wireless'); } catch (e) {
        if (force) clear(top).append(h('div', { class: 'banner crit' }, icon('alert'), e.message));
        return;
      }
      // The list refreshes by itself, but never under an open dialog
      const s = JSON.stringify(d, (k, v) => (k === 'seen' ? undefined : v));
      if (!force && (s === last || dialogs > 0)) return;
      last = s;
      data = d;
      draw();
    }

    function draw() {
      clear(top);
      if (data.dry_run) top.append(h('div', { class: 'banner warn' }, icon('info'), 'Test instance: changes are checked, but not applied. The access points are simulated.'));
      drawPending();
      clear(body);
      if (!data.configured) { body.append(setupCard()); return; }
      body.append(pairingCard(), apCard(), networksCard(), segmentsCard(), planCard());
    }

    function dialog(opts) {
      dialogs++;
      return formDialog(opts).finally(() => { dialogs--; });
    }

    async function ask(opts) {
      dialogs++;
      try { return await confirmDialog(opts); } finally { dialogs--; }
    }

    // --- Confirmation ----------------------------------------------------------------
    function drawPending() {
      clearInterval(countdown);
      clear(pendingBox);
      const p = data.pending;
      if (!p) return;
      const left = h('strong', { class: 'num' });
      pendingBox.append(h('div', { class: 'banner warn pending' }, icon('alert'),
        h('div', { class: 'spacer' }, 'The new setting is active. Without confirmation it is rolled back automatically in ', left, '.'),
        h('button', { class: 'btn', onclick: () => finish('/api/settings/revert', 'warn') }, 'Roll back'),
        h('button', { class: 'btn primary', onclick: () => finish('/api/settings/confirm', 'ok') }, icon('check'), 'Confirm')));
      const tick = () => {
        const s = Math.max(0, Math.round((new Date(p.deadline) - Date.now()) / 1000));
        left.textContent = t(s >= 60 ? `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')} min` : `${s} s`);
        if (s <= 0) { clearInterval(countdown); setTimeout(load, 2000); }
      };
      tick();
      countdown = setInterval(tick, 1000);
    }

    async function finish(path, kind) {
      try { toast((await post(path)).message, kind); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    // --- Saving: the page always sends the whole document ---------------------------
    function doc() {
      return {
        country: data.country,
        segments: data.segments.map((s) => ({ ...s, ipv4: s.ipv4 || '', reachable_from: s.reachable_from || [] })),
        networks: data.networks.map((n) => ({ id: n.id, ssid: n.ssid, segment: n.segment, security: n.security, bands: [...n.bands], enabled: n.enabled, password: '' })),
        access_points: data.access_points.map((a) => ({ id: a.id, name: a.name, power: a.power, radios: a.radios.map((r) => ({ ...r })) })),
      };
    }

    async function save(mod, okText = 'Saved.') {
      const d = doc();
      mod(d);
      const res = await post('/api/wireless', d);
      toast(res.data?.pending ? 'Saved. Please confirm the change.' : okText, res.data?.pending ? 'warn' : 'ok');
      await load();
    }

    const countrySelect = (value) => h('select', { class: 'input' },
      !value && h('option', { value: '', selected: true }, 'please choose'),
      countryList(data.countries).map((x) => h('option', { value: x.code, selected: x.code === value }, `${x.name} (${x.code})`)));

    // --- First setup -----------------------------------------------------------------
    function setupCard() {
      const err = h('div', { class: 'form-error hidden' });
      const country = countrySelect(data.country);
      const ssid = h('input', { class: 'input', maxlength: '32', autocomplete: 'off', spellcheck: 'false', value: data.local_wifi_ssid === 'Home' ? '' : 'Home' });
      const pass = h('input', { class: 'input', type: 'password', autocomplete: 'new-password', maxlength: '63', placeholder: 'at least 8 characters' });
      const go = h('button', { class: 'btn primary', type: 'submit' }, icon('check'), 'Set up');
      const form = h('form', null, err,
        field('Network name (SSID)', ssid, 'Every access point offers this name, your devices move between them by themselves.'),
        field('Password', pass, '8 to 63 characters, without umlauts or other special letters.'),
        field('Country', country, 'Decides which channels are legal. Please choose where the access points really are.'),
        h('div', { class: 'row end mt' }, go));
      form.addEventListener('submit', async (e) => {
        e.preventDefault();
        err.classList.add('hidden');
        go.disabled = true;
        try {
          await post('/api/wireless', {
            country: country.value,
            segments: [{ id: 'home', name: t('Home network'), vlan: 1, access: 'lan', ipv4: '', reachable_from: [] }],
            networks: [{ id: '', ssid: ssid.value.trim(), segment: 'home', security: 'wpa3', bands: ['5', '6'], enabled: true, password: pass.value }],
            access_points: [],
          });
          load();
        } catch (ex) { err.textContent = t(ex.message); err.classList.remove('hidden'); } finally { go.disabled = false; }
      });
      return h('section', { class: 'card hero' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('wifi')), h('h2', null, 'One Wi-Fi for the whole home')),
        h('p', null, 'Set up your Wi-Fi once here. Access points you connect later take over these settings by themselves, you never configure one of them separately.'),
        h('p', { class: 'muted small' }, 'The home network uses WPA3 only. Older devices can get a network of their own later.'),
        form);
    }

    // --- Pairing (concept 4.3) -------------------------------------------------------
    function pairingCard() {
      const list = data.pairing.map((p) => h('div', { class: 'pair-request' },
        h('div', { class: 'row' },
          h('div', { class: 'spacer' },
            h('div', { class: 'cell-main' }, p.model || p.hostname || 'Access point'),
            h('div', { class: 'muted small mono' }, p.mac),
            h('div', { class: 'muted small' }, p.radios.map((r) => `${r.chip || r.id} (${r.bands.join(', ')} GHz)`).join(' · '))),
          h('div', { class: 'pair-code' }, p.code)),
        h('p', { class: 'small' }, 'Is this the same code as on the screen of the access point?'),
        h('div', { class: 'row end' },
          h('button', { class: 'btn', onclick: () => reject(p) }, 'No, reject'),
          h('button', { class: 'btn primary', onclick: () => pair(p) }, icon('check'), 'Yes, add it'))));
      return h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('plus')), h('h2', null, 'Add an access point')),
        list.length ? h('div', { class: 'grid tight' }, list)
          : h('p', { class: 'muted' }, 'Install vanilla-router from the boot stick on a PC with a supported Wi-Fi card and connect it by cable to the home network. It shows a code on its screen, and it appears here.'),
        data.simulated && h('div', { class: 'row end mt' }, h('button', { class: 'btn wrap', onclick: simulate }, icon('refresh'), 'Let the simulated access points ask again')));
    }

    async function pair(p) {
      try {
        const res = await post('/api/wireless/pair', { id: p.id, code: p.code });
        toast(`${res.data.name} is paired.`);
      } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    async function reject(p) {
      try { toast((await post('/api/wireless/reject', { id: p.id })).message, 'warn'); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    async function simulate() {
      try { toast((await post('/api/wireless/simulate')).message); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    // --- Access points ---------------------------------------------------------------
    function radioText(a, r) {
      const p = data.plan.radios.find((x) => x.ap === a.id && x.radio === r.id);
      if (!p) return `${r.band} GHz`;
      if (p.idle) return `${r.band} GHz · ${t(p.reason === 'band not planned for this country' ? 'not available in this country' : 'unused, no network on this band')}`;
      return `${r.band} GHz · ${t('channel')} ${p.channel} · ${p.width} MHz${p.pinned ? ` · ${t('pinned')}` : ''}${p.dfs ? ' · DFS' : ''}`;
    }

    function apState(a) {
      if (!a.online) return badge('Offline', 'crit');
      if (a.report?.applied !== a.generation) return badge('Updating', 'warn');
      return badge('Online', 'ok');
    }

    function apCard() {
      const rows = data.access_points.map((a) => h('tr', null,
        h('td', null, h('div', { class: 'cell-main' }, a.name), h('div', { class: 'muted small mono' }, a.mac), h('div', { class: 'muted small mono', title: 'Fingerprint of the key' }, a.fingerprint)),
        h('td', null, apState(a), a.online && a.report ? h('div', { class: 'muted small' }, `${a.report.clients} devices`) : null),
        h('td', null, a.radios.map((r) => {
          const c = a.caps.find((x) => x.id === r.id);
          return h('div', { class: 'small' }, radioText(a, r), c?.chip && h('span', { class: 'muted' }, ` (${c.chip})`));
        })),
        h('td', null, POWER.find(([v]) => v === a.power)?.[1] || a.power),
        h('td', null, h('div', { class: 'row end' },
          h('button', { class: 'btn sm icon', title: 'Edit', onclick: () => editAP(a) }, icon('edit')),
          h('button', { class: 'btn sm icon', title: 'Show configuration', onclick: () => previewAP(a) }, icon('file')),
          h('button', { class: 'btn sm icon danger', title: 'Remove', onclick: () => removeAP(a) }, icon('trash'))))));
      return h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('router')), h('h2', null, 'Access points')),
        rows.length ? h('div', { class: 'table-wrap' }, h('table', null,
          h('thead', null, h('tr', null, ['Name', 'State', 'Wi-Fi cards', 'Power', ''].map((x) => h('th', null, x)))),
          h('tbody', null, rows)))
          : h('p', { class: 'muted' }, 'No access point is paired yet.'));
    }

    // One select per card: band and channel together, so that the two cannot contradict each other
    function radioOptions(a, r) {
      const c = a.caps.find((x) => x.id === r.id);
      const out = [];
      for (const b of BANDS) {
        if (c && !c.bands.includes(b)) continue;
        out.push({ value: `${b}|0|0`, label: `${b} GHz · ${t('planned by the router')}` });
        for (const [ch, w] of PINS[b]) out.push({ value: `${b}|${ch}|${w}`, label: `${b} GHz · ${t('channel')} ${ch} · ${w} MHz` });
      }
      const cur = `${r.band}|${r.channel || 0}|${r.width || 0}`;
      if (!out.some((o) => o.value === cur)) out.push({ value: cur, label: `${r.band} GHz · ${t('channel')} ${r.channel} · ${r.width} MHz` });
      return { options: out, value: cur };
    }

    function editAP(a) {
      return dialog({
        title: a.name, submit: 'Save',
        fields: [
          { name: 'name', label: 'Name', value: a.name, hint: 'For example the room it is in.' },
          { name: 'power', label: 'Transmit power', type: 'select', value: a.power, options: POWER.map(([value, label]) => ({ value, label })), hint: 'Too strong makes devices cling to a distant access point.' },
          ...a.radios.map((r, i) => ({ name: `r${i}`, label: `Wi-Fi card ${i + 1}: ${a.caps.find((x) => x.id === r.id)?.chip || r.id}`, type: 'select', ...radioOptions(a, r) })),
        ],
        onSubmit: (v) => save((d) => {
          const x = d.access_points.find((y) => y.id === a.id);
          x.name = v.name;
          x.power = v.power;
          x.radios.forEach((r, i) => {
            const [band, ch, w] = v[`r${i}`].split('|');
            r.band = band; r.channel = +ch; r.width = +w;
          });
        }),
      });
    }

    function previewAP(a) {
      logDialog(`Configuration of ${a.name}`, async () => {
        const res = await post('/api/wireless/preview', { id: a.id });
        return res.data.map((f) => `### ${f.path}\n${f.content}`).join('\n');
      });
    }

    async function removeAP(a) {
      const ok = await ask({ title: `Remove ${a.name}?`, confirm: 'Remove', danger: true,
        text: 'The access point stops offering Wi-Fi and the roaming keys are renewed, which your devices do not notice. If it was stolen or sold, please also change the Wi-Fi passwords: it still knows them.' });
      if (!ok) return;
      try { toast((await post('/api/wireless/remove', { id: a.id })).message); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    // --- Networks --------------------------------------------------------------------
    function segName(id) { return data.segments.find((s) => s.id === id)?.name || id; }

    function networksCard() {
      const rows = data.networks.map((n) => h('tr', null,
        h('td', null, h('div', { class: 'cell-main' }, n.ssid), h('div', { class: 'muted small' }, segName(n.segment))),
        h('td', null, n.security === 'wpa3' ? badge('WPA3', 'ok') : badge('WPA2 + WPA3', 'warn')),
        h('td', { class: 'num' }, n.bands.map((b) => `${b} GHz`).join(', ')),
        h('td', null, n.enabled ? badge('On', 'ok') : badge('Off')),
        h('td', null, h('div', { class: 'row end' },
          h('button', { class: 'btn sm icon', title: 'Edit', onclick: () => editNetwork(n) }, icon('edit')),
          n.segment !== 'home' && h('button', { class: 'btn sm icon danger', title: 'Remove', onclick: () => removeNetwork(n) }, icon('trash'))))));
      const has = (id) => data.segments.some((s) => s.id === id);
      return h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('wifi')), h('h2', null, 'Wi-Fi networks')),
        h('div', { class: 'table-wrap flat' }, h('table', null,
          h('thead', null, h('tr', null, ['Name', 'Security', 'Bands', 'State', ''].map((x) => h('th', null, x)))),
          h('tbody', null, rows))),
        h('div', { class: 'row end mt' },
          !has('guest') && h('button', { class: 'btn', onclick: addGuest }, icon('plus'), 'Guest network'),
          !has('older') && h('button', { class: 'btn', onclick: addOlder }, icon('plus'), 'Network for older devices'),
          h('button', { class: 'btn', onclick: () => editNetwork(null) }, icon('plus'), 'Other network')));
    }

    function networkFields(n) {
      const bands = n?.bands || ['5', '6'];
      return [
        { name: 'ssid', label: 'Network name (SSID)', value: n?.ssid || '' },
        { name: 'segment', label: 'Segment', type: 'select', value: n?.segment || 'home', options: data.segments.map((s) => ({ value: s.id, label: s.name })) },
        { name: 'security', label: 'Security', type: 'select', value: n?.security || 'wpa3', options: [{ value: 'wpa3', label: 'WPA3 only' }, { value: 'wpa2-wpa3', label: 'WPA2 and WPA3 (for older devices)' }], hint: 'The home network uses WPA3 only.' },
        { name: 'b24', label: '2.4 GHz', type: 'checkbox', value: bands.includes('2.4') },
        { name: 'b5', label: '5 GHz', type: 'checkbox', value: bands.includes('5') },
        { name: 'b6', label: '6 GHz', type: 'checkbox', value: bands.includes('6'), hint: '6 GHz works with WPA3 only.' },
        { name: 'enabled', label: 'Offer this network', type: 'checkbox', value: n ? n.enabled : true },
        { name: 'password', label: 'Password', type: 'password', value: '', placeholder: n?.has_password ? 'leave unchanged' : 'at least 8 characters', hint: n?.has_password ? 'Only enter one to change it. It cannot be shown again.' : '8 to 63 characters, without umlauts or other special letters.' },
      ];
    }

    const netFrom = (v, id) => ({ id, ssid: v.ssid, segment: v.segment, security: v.security,
      bands: [v.b24 && '2.4', v.b5 && '5', v.b6 && '6'].filter(Boolean), enabled: v.enabled, password: v.password });

    function editNetwork(n) {
      return dialog({
        title: n ? n.ssid : 'New network', submit: 'Save', fields: networkFields(n),
        onSubmit: (v) => save((d) => {
          if (n) d.networks[d.networks.findIndex((x) => x.id === n.id)] = netFrom(v, n.id);
          else d.networks.push(netFrom(v, ''));
        }),
      });
    }

    // A free /24 for a new segment: 192.168.<vlan>.1 if nothing else uses it
    function freeRange(vlan) {
      const prefix = (ip) => ip.split('.').slice(0, 3).join('.');
      const used = new Set(data.segments.filter((s) => s.ipv4).map((s) => prefix(s.ipv4)));
      used.add(prefix(data.lan_address));
      for (let x = vlan; x < vlan + 200; x++) {
        const p = `192.168.${x % 255}`;
        if (!used.has(p)) return `${p}.1/24`;
      }
      return '';
    }

    function addPreset({ id, vlan, name, title, text, security, bands, reachable }) {
      return dialog({
        title, text, submit: 'Create',
        fields: [
          { name: 'ssid', label: 'Network name (SSID)', value: t(name) },
          { name: 'password', label: 'Password', type: 'password', value: '', placeholder: 'at least 8 characters' },
        ],
        onSubmit: (v) => save((d) => {
          d.segments.push({ id, name: t(name), vlan, access: 'internet', ipv4: freeRange(vlan), reachable_from: reachable });
          d.networks.push({ id, ssid: v.ssid, segment: id, security, bands, enabled: true, password: v.password });
        }),
      });
    }

    const addGuest = () => addPreset({ id: 'guest', vlan: 20, name: 'Guests', title: 'Guest network', security: 'wpa3', bands: ['5'], reachable: [],
      text: 'Guests get internet access, but cannot reach your devices or each other. Your switches must pass VLAN tags.' });
    const addOlder = () => addPreset({ id: 'older', vlan: 30, name: 'Older devices', title: 'Network for older devices', security: 'wpa2-wpa3', bands: ['2.4'], reachable: ['home'],
      text: 'For devices that cannot use WPA3. It has its own name, only 2.4 GHz and internet access. Your home devices can reach it, it cannot reach them. If you can, replacing the old device is the better way.' });

    async function removeNetwork(n) {
      if (!(await ask({ title: `Remove ${n.ssid}?`, confirm: 'Remove', danger: true, text: 'Devices in this network lose their Wi-Fi.' }))) return;
      try { await save((d) => { d.networks = d.networks.filter((x) => x.id !== n.id); }, 'Removed.'); } catch (e) { toast(e.message, 'crit'); }
    }

    // --- Segments --------------------------------------------------------------------
    function segmentsCard() {
      const rows = data.segments.map((s) => h('tr', null,
        h('td', { class: 'cell-main' }, s.name),
        h('td', { class: 'num' }, s.id === 'home' ? 'untagged' : String(s.vlan)),
        h('td', { class: 'mono small' }, s.ipv4 || data.lan_address),
        h('td', null, s.access === 'lan' ? 'Home network' : (s.reachable_from?.length ? 'Internet, reachable from home' : 'Internet only')),
        h('td', null, h('div', { class: 'row end' }, s.id !== 'home' && [
          h('button', { class: 'btn sm icon', title: 'Edit', onclick: () => editSegment(s) }, icon('edit')),
          h('button', { class: 'btn sm icon danger', title: 'Remove', onclick: () => removeSegment(s) }, icon('trash'))]))));
      return h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('shield')), h('h2', null, 'Segments')),
        h('p', { class: 'muted small' }, 'A segment separates devices from your home network. Guest and older-devices networks need switches that pass VLAN tags.'),
        h('div', { class: 'table-wrap flat' }, h('table', null,
          h('thead', null, h('tr', null, ['Name', 'VLAN', 'Addresses', 'Access', ''].map((x) => h('th', null, x)))),
          h('tbody', null, rows))),
        h('div', { class: 'row end mt' }, h('button', { class: 'btn', onclick: () => editSegment(null) }, icon('plus'), 'Segment')));
    }

    function editSegment(s) {
      const nextVLAN = () => { for (let v = 40; ; v += 10) if (!data.segments.some((x) => x.vlan === v)) return v; };
      const vlan = s?.vlan || nextVLAN();
      return dialog({
        title: s ? s.name : 'New segment', submit: 'Save',
        fields: [
          { name: 'name', label: 'Name', value: s?.name || '' },
          { name: 'vlan', label: 'VLAN', value: String(vlan), hint: '2 to 4094, different for every segment.' },
          { name: 'ipv4', label: 'Router address with prefix', value: s?.ipv4 || freeRange(vlan), hint: 'For example 192.168.20.1/24.' },
          { name: 'home', label: 'Devices in the home network may reach this segment', type: 'checkbox', value: !!s?.reachable_from?.includes('home') },
        ],
        onSubmit: (v) => save((d) => {
          const x = { id: s?.id || '', name: v.name, vlan: +v.vlan, access: 'internet', ipv4: v.ipv4, reachable_from: v.home ? ['home'] : [] };
          if (s) d.segments[d.segments.findIndex((y) => y.id === s.id)] = x;
          else d.segments.push(x);
        }),
      });
    }

    async function removeSegment(s) {
      if (data.networks.some((n) => n.segment === s.id)) { toast(`Please remove the networks in ${s.name} first.`, 'warn'); return; }
      if (!(await ask({ title: `Remove ${s.name}?`, confirm: 'Remove', danger: true }))) return;
      try { await save((d) => { d.segments = d.segments.filter((x) => x.id !== s.id); }, 'Removed.'); } catch (e) { toast(e.message, 'crit'); }
    }

    // --- Channel plan and country ----------------------------------------------------
    function planCard() {
      const country = countrySelect(data.country);
      country.addEventListener('change', async () => {
        try { await save((d) => { d.country = country.value; }); } catch (e) { toast(e.message, 'crit'); load(); }
      });
      const name = (id) => data.access_points.find((a) => a.id === id)?.name || id;
      const active = data.plan.radios.filter((r) => !r.idle);
      return h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('activity')), h('h2', null, 'Channels')),
        field('Country', country, 'Decides which channels are legal. Please choose where the access points really are.'),
        active.length ? h('div', { class: 'table-wrap flat' }, h('table', null,
          h('thead', null, h('tr', null, ['Access point', 'Band', 'Channel', 'Width', ''].map((x) => h('th', null, x)))),
          h('tbody', null, active.map((r) => h('tr', null,
            h('td', { class: 'cell-main' }, name(r.ap)),
            h('td', { class: 'num' }, `${r.band} GHz`),
            h('td', { class: 'num' }, String(r.channel)),
            h('td', { class: 'num' }, `${r.width} MHz`),
            h('td', null, r.pinned ? badge('Pinned', 'info') : badge('Planned'), r.dfs ? ' ' : '', r.dfs ? badge('DFS') : null))))))
          : h('p', { class: 'muted' }, 'The router plans the channels as soon as an access point is paired.'));
    }

    load();
    const stop = every(10000, () => load(false));
    return () => { clearInterval(countdown); stop(); };
  },
};

function field(label, input, hint) {
  return h('div', { class: 'field' }, h('label', null, label), input, hint && h('div', { class: 'hint' }, hint));
}
