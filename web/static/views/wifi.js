// SPDX-License-Identifier: AGPL-3.0-or-later
import { t, locale } from '../i18n.js';
// Wi-Fi access point (F-WIFI, D-18). Network data (names, addresses) is not trustworthy: only through h().
import { h, clear, icon, toast, badge, every, confirmDialog } from '../ui.js';
import { get, post } from '../api.js';

export default {
  title: 'Wi-Fi',
  subtitle: 'Access point of the router in the home network',
  render(root) {
    let data = null;
    let countdown = null;
    let poll = null;
    let installTimer = null;
    const top = h('div');
    const pendingBox = h('div');
    const body = h('div', { class: 'grid cols-2' });
    root.append(top, pendingBox, body);

    async function load() {
      if (poll) { poll(); poll = null; }
      try { data = await get('/api/wifi'); } catch (e) { clear(top).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      clear(top);
      if (data.dry_run) top.append(h('div', { class: 'banner warn' }, icon('info'), 'Test instance: changes are checked, but not applied. The Wi-Fi adapter may be simulated.'));
      drawPending();
      clear(body);
      if (!data.adapters.length) { body.append(noAdapter()); return; }
      if (!data.installed) { body.append(installCard()); return; }
      const status = statusCard();
      body.append(settingsCard(), status.el);
      poll = every(5000, status.refresh);
    }

    // --- No hardware, or not ready yet (F-WIFI-04) -----------------------------------
    function noAdapter() {
      const lost = data.configured && data.config.enabled;
      const title = data.card_without_driver ? 'The Wi-Fi card is not ready yet' : (lost ? 'The Wi-Fi adapter is gone' : 'This router has no Wi-Fi adapter');
      const text = data.card_without_driver
        ? (data.firmware_fetched
          ? 'The router has a Wi-Fi card and has already fetched all firmware. If a restart does not help, Linux has no working driver for this card. A USB Wi-Fi adapter or an access point on a home network port works instead.'
          : 'The router has a Wi-Fi card, but it still lacks its firmware. The router fetches that by itself as soon as it has been online for a while. Please restart the router afterwards, then this page offers the Wi-Fi settings.')
        : (lost
          ? `The access point is set up for the adapter ${data.config.interface}, but it is no longer there. As soon as it is back, the access point starts by itself.`
          : 'To offer Wi-Fi, the router needs a Wi-Fi card or a USB Wi-Fi adapter. Alternatively, connect an access point to a home network port: that works without any setting here.');
      return h('section', { class: 'card span-2 hero' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('wifi')), h('h2', null, title)),
        h('p', null, text));
    }

    // --- Download on request (F-WIFI-07) ---------------------------------------------
    function installCard() {
      const err = h('div', { class: 'form-error hidden' });
      const log = h('pre', { class: 'small hidden' });
      const btn = h('button', { class: 'btn primary', onclick: start }, icon('check'), 'Download Wi-Fi software');
      async function start() {
        err.classList.add('hidden');
        btn.disabled = true;
        try {
          await post('/api/wifi/install');
          watch();
        } catch (ex) { err.textContent = t(ex.message); err.classList.remove('hidden'); btn.disabled = false; }
      }
      async function watch() {
        clearTimeout(installTimer);
        let st;
        try { st = await get('/api/wifi/install'); } catch { installTimer = setTimeout(watch, 3000); return; }
        if (st.log) { log.textContent = st.log; log.classList.remove('hidden'); }
        if (st.running) { btn.disabled = true; installTimer = setTimeout(watch, 3000); return; }
        btn.disabled = false;
        if (st.installed) { toast('The Wi-Fi software is ready.'); load(); return; }
        if (st.failed) { err.textContent = t('The download failed. Is the router online?'); err.classList.remove('hidden'); }
      }
      if (data.install_running) watch();
      const a = data.adapters[0];
      return h('section', { class: 'card span-2 hero' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('wifi')), h('h2', null, 'Wi-Fi adapter found')),
        h('p', null, `The router has found the adapter ${a.name}${a.driver ? ` (${a.driver})` : ''}. For Wi-Fi it needs some additional software once, from Ubuntu's package sources. This needs an internet connection and takes about a minute.`),
        err, log, h('div', { class: 'row end' }, btn));
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

    async function confirmChange() {
      try { toast((await post('/api/settings/confirm')).message); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    async function revert() {
      try { toast((await post('/api/settings/revert')).message, 'warn'); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    // --- Settings (F-WIFI-02) --------------------------------------------------------
    function settingsCard() {
      const c = data.config;
      const err = h('div', { class: 'form-error hidden' });
      const enabled = h('input', { type: 'checkbox', checked: data.configured ? c.enabled : true });
      const known = data.adapters.some((a) => a.name === c.interface);
      const adapter = h('select', { class: 'input' }, data.adapters.map((a) =>
        h('option', { value: a.name, selected: a.name === c.interface }, `${a.name}${a.driver ? ` (${a.driver})` : ''}${a.usb ? ' · USB' : ''}`)));
      if (c.interface && !known) adapter.append(h('option', { value: c.interface, selected: true }, `${c.interface} (not found)`));
      const ssid = h('input', { class: 'input', value: c.ssid, maxlength: '32', autocomplete: 'off', spellcheck: 'false' });
      const pass = h('input', { class: 'input', type: 'password', autocomplete: 'new-password', maxlength: '63',
        placeholder: data.has_password ? 'leave unchanged' : 'at least 8 characters' });
      const country = h('select', { class: 'input' },
        !c.country && h('option', { value: '', selected: true }, 'please choose'),
        countryList(data.countries).map((x) => h('option', { value: x.code, selected: x.code === c.country }, `${x.name} (${x.code})`)),
        c.country && !(data.countries || []).some((x) => x.code === c.country) && h('option', { value: c.country, selected: true }, c.country));
      const band = h('select', { class: 'input' });
      const channel = h('select', { class: 'input' });
      const width = h('select', { class: 'input' });
      const hint = h('div', { class: 'hint' });
      const warn = h('div');

      const cur = () => data.adapters.find((a) => a.name === adapter.value) || data.adapters[0];
      const curBand = () => (cur().bands || []).find((b) => b.band === band.value);

      function drawBands() {
        const bands = (cur().bands || []).filter((b) => b.band === '2.4' || b.band === '5');
        const want = band.value || c.band;
        clear(band).append(...(bands.length ? bands.map((b) => b.band) : [c.band]).map((b) =>
          h('option', { value: b, selected: b === want }, `${b} GHz`)));
        clear(warn);
        if (cur().known && !cur().ap) warn.append(h('div', { class: 'banner crit' }, icon('alert'), 'This adapter cannot run as an access point.'));
        drawChannels();
      }

      // Without DFS channels, same list as config.WiFiChannels (F-WIFI-09)
      function drawChannels() {
        const allowed = band.value === '5' ? [36, 40, 44, 48, 149, 153, 157, 161, 165] : [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13];
        const info = new Map((curBand()?.channels || []).map((x) => [x.channel, x]));
        const prev = +channel.value || (band.value === c.band ? c.channel : 0);
        const list = allowed.filter((n) => !info.size || (info.has(n) && !info.get(n).disabled));
        const pick = list.includes(prev) ? prev : (band.value === '5' ? 36 : 6);
        clear(channel).append(...list.map((n) => h('option', { value: String(n), selected: n === pick }, info.get(n)?.no_ir ? `${n} (may be refused)` : String(n))));
        drawWidths();
        const ch = info.get(+channel.value);
        clear(hint);
        if (band.value === '5') hint.append(t(ch?.no_ir
          ? 'The adapter does not allow starting a network here yet. That can change once the country is set, but many built-in cards never allow it on 5 GHz. If the start fails, the old state comes back.'
          : '5 GHz is faster, 2.4 GHz reaches further and is understood by older devices.'));
      }

      function drawWidths() {
        const b = curBand();
        const n = +channel.value;
        let opts = [20];
        if (band.value === '5' && n !== 165) {
          if (!cur().known || b?.ht40) opts.push(40);
          if (!cur().known || b?.vht) opts.push(80);
        }
        const prev = +width.value || (band.value === c.band ? c.width : 0);
        const pick = opts.includes(prev) ? prev : opts[opts.length - 1];
        clear(width).append(...opts.map((w) => h('option', { value: String(w), selected: w === pick }, `${w} MHz`)));
      }

      adapter.addEventListener('change', drawBands);
      band.addEventListener('change', () => { channel.value = ''; width.value = ''; drawChannels(); });
      channel.addEventListener('change', drawChannels);
      drawBands();

      const save = h('button', { class: 'btn primary', type: 'submit' }, icon('check'), 'Apply');
      const form = h('form', null, err, warn,
        h('div', { class: 'field' }, h('label', { class: 'row' }, enabled, 'Offer Wi-Fi')),
        field('Network name (SSID)', ssid, 'This is the name your devices show.'),
        field('Password', pass, data.has_password ? 'Only enter one to change it. It cannot be shown again.' : '8 to 63 characters, without umlauts or other special letters.'),
        field('Country', country, 'Decides which channels are legal. Please choose where the router really is.'),
        field('Band', band),
        field('Channel', channel),
        hint,
        h('details', { class: 'mt' }, h('summary', null, 'Advanced'),
          data.adapters.length > 1 || !known && c.interface ? field('Adapter', adapter) : null,
          field('Channel width', width, 'Wider is faster, but only as long as no neighbor uses the same channels.')),
        h('div', { class: 'row end mt' }, save));
      form.addEventListener('submit', async (e) => {
        e.preventDefault();
        err.classList.add('hidden');
        const req = { enabled: enabled.checked, interface: adapter.value, ssid: ssid.value, password: pass.value,
          country: country.value, band: band.value, channel: +channel.value, width: +width.value };
        if (data.status.running && !(await confirmDialog({ title: 'Apply the Wi-Fi change?', confirm: 'Apply',
          text: 'Devices on Wi-Fi are disconnected for a moment. If you are on Wi-Fi yourself, reconnect, with the new password if you changed it, and confirm the change within 5 minutes. Otherwise the router restores the old setting.' }))) return;
        save.disabled = true;
        try {
          const res = await post('/api/wifi', req);
          pass.value = '';
          toast(res.data?.pending ? res.message : 'Saved, nothing has changed.', res.data?.pending ? 'warn' : 'ok');
          load();
        } catch (ex) { err.textContent = t(ex.message); err.classList.remove('hidden'); } finally { save.disabled = false; }
      });
      return h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('wifi')), h('h2', null, 'Wi-Fi settings')),
        form);
    }

    // --- State and devices (F-WIFI-03) -----------------------------------------------
    function statusCard() {
      const state = h('div');
      const list = h('div');
      let names = new Map();
      async function loadNames() {
        try {
          const devices = await get('/api/devices');
          names = new Map((Array.isArray(devices) ? devices : devices.devices || []).map((d) => [d.mac, d]));
        } catch { /* the list works without names too */ }
      }
      function draw(st) {
        clear(state).append(st.running
          ? h('dl', { class: 'kv' },
            h('dt', null, 'State'), h('dd', null, badge('On', 'ok')),
            h('dt', null, 'Network name'), h('dd', null, st.ssid),
            h('dt', null, 'Channel'), h('dd', null, `${st.channel}${st.width ? ` · ${st.width} MHz` : ''}`))
          : h('dl', { class: 'kv' }, h('dt', null, 'State'), h('dd', null, badge(data.config.enabled && data.configured ? 'Not running' : 'Off', data.config.enabled && data.configured ? 'crit' : ''))));
        clear(list);
        const clients = st.clients || [];
        if (!st.running) return;
        if (!clients.length) { list.append(h('p', { class: 'muted small' }, 'No device is connected.')); return; }
        list.append(h('div', { class: 'table-wrap' }, h('table', null,
          h('thead', null, h('tr', null, ['Device', 'Signal', 'Speed', 'Connected'].map((x) => h('th', null, x)))),
          h('tbody', null, clients.map((s) => {
            const d = names.get(s.mac);
            return h('tr', null,
              h('td', null, h('div', { class: 'cell-main' }, d?.alias || d?.hostname || d?.dns_name || s.mac), h('div', { class: 'muted small mono' }, d?.ip ? `${d.ip} · ${s.mac}` : s.mac)),
              h('td', null, s.has_signal ? badge(`${s.signal_dbm} dBm`, s.signal_dbm >= -67 ? 'ok' : (s.signal_dbm >= -77 ? 'warn' : 'crit')) : '–'),
              h('td', { class: 'num' }, `${Math.round(s.tx_mbit)} / ${Math.round(s.rx_mbit)} Mbit/s`),
              h('td', { class: 'num' }, duration(s.connected_seconds)));
          })))));
      }
      draw(data.status);
      loadNames().then(() => draw(data.status));
      return {
        el: h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('activity')), h('h2', null, 'Status')),
          state, h('h3', { class: 'sub' }, 'Connected devices'), list),
        async refresh() {
          try {
            data.status = await get('/api/wifi/status');
            if (data.status.clients?.some((c) => !names.has(c.mac))) await loadNames();
            draw(data.status);
          } catch { /* next round */ }
        },
      };
    }

    load();
    return () => { clearInterval(countdown); clearTimeout(installTimer); if (poll) poll(); };
  },
};

// Country names in the language of the page, from the browser. The names from the router are the fallback.
export function countryList(list) {
  let names = null;
  try { names = new Intl.DisplayNames([locale() || 'en'], { type: 'region' }); } catch { /* old browser */ }
  return (list || []).map((x) => ({ code: x.code, name: names?.of(x.code) || x.name }))
    .sort((a, b) => a.name.localeCompare(b.name, locale()));
}

function duration(s) {
  if (s < 60) return `${s} s`;
  if (s < 3600) return `${Math.floor(s / 60)} min`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ${Math.floor((s % 3600) / 60)} min`;
  return `${Math.floor(s / 86400)} d`;
}

function field(label, input, hint) {
  return h('div', { class: 'field' }, h('label', null, label), input, hint && h('div', { class: 'hint' }, hint));
}
