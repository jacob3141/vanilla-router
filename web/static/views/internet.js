// SPDX-License-Identifier: AGPL-3.0-or-later
// Internet-Anschluss, Heimnetz und Name des Routers (F-UI-03, F-UI-08, F-UI-09).
// Mit ?setup=1 ist das der Einrichtungsschritt „Internetzugang“ nach der ersten Anmeldung.
import { h, clear, icon, toast, badge, confirmDialog } from '../ui.js';
import { get, post } from '../api.js';

const TYPES = [
  { value: 'dhcp', label: 'Automatisch', hint: 'Ein Router oder Modem davor vergibt die Adresse (Kabel, viele Glasfaser-Anschlüsse).' },
  { value: 'pppoe', label: 'Mit Zugangsdaten', hint: 'Einwahl mit Benutzername und Passwort aus dem Brief des Anbieters (DSL, Telekom-Glasfaser).' },
  { value: 'static', label: 'Feste Adresse', hint: 'Der Anbieter hat eine feste IP-Adresse vorgegeben (selten, meist Geschäftskunden).' },
];

const LEASES = [['1h', '1 Stunde'], ['12h', '12 Stunden'], ['24h', '1 Tag'], ['168h', '1 Woche']];

export const SETUP_LATER_KEY = 'vr-internet-setup-later';

export default {
  title: 'Internet & Heimnetz',
  subtitle: 'Anschluss, Adressen und Name des Routers',
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
      if (data.dry_run) top.append(h('div', { class: 'banner warn' }, icon('info'), 'Testinstanz: Änderungen werden geprüft, aber nicht eingespielt.'));
      if (data.needs_setup) top.append(setupIntro());
      drawPending();
      clear(body).append(wanCard(), systemCard(), lanCard(), dhcpCard());
    }

    // --- Einrichtungsschritt -----------------------------------------------------------
    function setupIntro() {
      const d = data.detected || {};
      const text = d.method === 'pppoe'
        ? `Der Router hat erkannt, dass Ihr Anschluss eine Einwahl mit Zugangsdaten braucht${d.vlan ? ` (über VLAN ${d.vlan}, typisch für die Telekom)` : ''}. Die Zugangsdaten stehen im Brief Ihres Anbieters.`
        : 'Bei der Installation hat das Modem nicht geantwortet. Wählen Sie bitte, wie Ihr Anschluss funktioniert. Wenn Sie unsicher sind: Bei Kabel-Anschlüssen und wenn ein anderer Router davor steht, ist es „Automatisch“.';
      return h('section', { class: 'card hero' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('globe')), h('h2', null, setupMode ? 'Fast geschafft: Internetzugang einrichten' : 'Internetzugang ist noch nicht eingerichtet')),
        h('p', null, text),
        setupMode && h('div', { class: 'row end' }, h('button', { class: 'btn', onclick: later }, 'Später')));
    }

    function later() {
      try { localStorage.setItem(SETUP_LATER_KEY, String(Date.now())); } catch { /* privater Modus */ }
      location.hash = '#/dashboard';
    }

    // --- Bestätigung -------------------------------------------------------------------
    function drawPending() {
      clearInterval(countdown);
      clear(pendingBox);
      const p = data.pending;
      if (!p) return;
      const left = h('strong', { class: 'num' });
      const net = h('span', { class: 'muted' }, ' Internet wird geprüft …');
      pendingBox.append(h('div', { class: 'banner warn pending' }, icon('alert'),
        h('div', { class: 'spacer' }, 'Die neue Einstellung ist aktiv. Ohne Bestätigung wird sie in ', left, ' automatisch zurückgenommen.', net),
        h('button', { class: 'btn', onclick: revert }, 'Zurücknehmen'),
        h('button', { class: 'btn primary', onclick: confirmChange }, icon('check'), 'Bestätigen')));
      const tick = () => {
        const s = Math.max(0, Math.round((new Date(p.deadline) - Date.now()) / 1000));
        left.textContent = s >= 60 ? `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')} min` : `${s} s`;
        if (s <= 0) { clearInterval(countdown); setTimeout(load, 2000); }
      };
      tick();
      countdown = setInterval(tick, 1000);
      checkInternet(net);
    }

    async function checkInternet(el) {
      for (let i = 0; i < 6 && el.isConnected; i++) {
        try {
          if ((await get('/api/settings/check')).internet) { clear(el).append(' ', badge('Internet erreichbar', 'ok')); return; }
        } catch { /* weiter versuchen */ }
        await new Promise((r) => setTimeout(r, 5000));
      }
      if (el.isConnected) clear(el).append(' ', badge('Internet nicht erreichbar', 'crit'), ' Prüfen Sie die Angaben oder nehmen Sie die Änderung zurück.');
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

    // --- Internet-Anschluss ------------------------------------------------------------
    function wanCard() {
      const w = data.wan;
      const d = data.detected || {};
      const err = h('div', { class: 'form-error hidden' });
      // Im Einrichtungsschritt mit dem Erkannten vorbelegen
      let type = data.needs_setup && d.method === 'pppoe' ? 'pppoe' : w.type;
      const hasTelekom = w.type === 'pppoe' && (w.pppoe_user || '').endsWith('@t-online.de');
      const telekomGuess = hasTelekom || (data.needs_setup && d.vlan === 7);
      const provider = h('select', { class: 'input' },
        h('option', { value: 'telekom', selected: telekomGuess }, 'Telekom'),
        h('option', { value: 'other', selected: !telekomGuess }, 'anderer Anbieter'));
      const inp = (attrs) => h('input', { class: 'input', autocomplete: 'off', spellcheck: 'false', ...attrs });
      const ak = inp({ inputmode: 'numeric', placeholder: '12 Ziffern' });
      const zn = inp({ inputmode: 'numeric', placeholder: 'meist 12 Ziffern' });
      const mb = inp({ inputmode: 'numeric', value: '0001' });
      const user = inp({ value: w.pppoe_user, placeholder: 'z. B. kunde@anbieter.de' });
      const pass = h('input', { class: 'input', type: 'password', autocomplete: 'new-password',
        placeholder: w.has_password ? 'unverändert lassen' : 'Passwort aus dem Brief' });
      const vlan = inp({ inputmode: 'numeric', value: w.pppoe_vlan ? String(w.pppoe_vlan) : (data.needs_setup && d.vlan ? String(d.vlan) : ''), placeholder: 'leer = ohne' });
      const addr = inp({ value: w.static_address, placeholder: 'z. B. 203.0.113.10/29' });
      const gw = inp({ value: w.static_gateway, placeholder: 'z. B. 203.0.113.9' });
      const dns = inp({ value: (w.static_dns || []).join(', '), placeholder: 'z. B. 9.9.9.9, 149.112.112.112' });
      const ipv6 = h('input', { type: 'checkbox', checked: w.ipv6 });
      const mtu = inp({ inputmode: 'numeric', value: w.mtu ? String(w.mtu) : '', placeholder: 'leer = automatisch' });

      const typeBox = h('div', { class: 'choice-list' });
      const fields = h('div');
      function draw() {
        clear(typeBox).append(...TYPES.map((t) => h('label', { class: `choice ${type === t.value ? 'active' : ''}` },
          h('input', { type: 'radio', name: 'wan-type', value: t.value, checked: type === t.value, onchange: () => { type = t.value; draw(); } }),
          h('div', null, h('div', { class: 'cell-main' }, t.label), h('div', { class: 'muted small' }, t.hint)))));
        clear(fields);
        if (type === 'pppoe') {
          fields.append(field('Anbieter', provider));
          if (provider.value === 'telekom') {
            fields.append(
              h('p', { class: 'muted small' }, 'Die Angaben stehen im Brief „Ihre persönlichen Zugangsdaten“. Der Benutzername wird daraus zusammengesetzt.'),
              hasTelekom && h('p', { class: 'small' }, 'Eingerichtet: ', h('span', { class: 'mono' }, w.pppoe_user), '. Felder leer lassen, um ihn zu behalten.'),
              field('Anschlusskennung', ak), field('Zugangsnummer (T-Online-Nummer)', zn), field('Mitbenutzernummer', mb, 'Meist 0001.'),
              field('Persönliches Kennwort', pass));
          } else {
            fields.append(field('Benutzername', user), field('Passwort', pass),
              field('VLAN (optional)', vlan, 'Nur wenn der Anbieter eine VLAN-ID nennt. Die Telekom nutzt 7.'));
          }
        } else if (type === 'static') {
          fields.append(field('IP-Adresse mit Präfix', addr), field('Gateway', gw), field('DNS-Server (optional)', dns, 'Kommagetrennt. Leer = öffentliche DNS-Server aus den DNS-Einstellungen.'));
        }
      }
      provider.addEventListener('change', draw);
      draw();

      const save = h('button', { class: 'btn primary', type: 'submit' }, icon('check'), 'Übernehmen');
      const form = h('form', null, err, typeBox, fields,
        h('details', { class: 'mt' }, h('summary', null, 'Erweitert'),
          h('label', { class: 'row' }, ipv6, 'IPv6 vom Anbieter anfordern (empfohlen)'),
          field('MTU', mtu, 'Nur ändern, wenn der Anbieter es verlangt.')),
        h('div', { class: 'row end mt' }, save));
      form.addEventListener('submit', async (e) => {
        e.preventDefault();
        err.classList.add('hidden');
        const req = { type, ipv6: ipv6.checked, mtu: mtu.value.trim() ? +mtu.value : 0 };
        if (type === 'pppoe') {
          req.pppoe_password = pass.value;
          if (provider.value === 'telekom' && hasTelekom && !ak.value.trim() && !zn.value.trim()) {
            req.pppoe_user = w.pppoe_user; // bisherigen Benutzernamen behalten
            req.pppoe_vlan = 7;
          } else if (provider.value === 'telekom') {
            req.telekom = { anschlusskennung: ak.value.replace(/\s/g, ''), zugangsnummer: zn.value.replace(/\s/g, ''), mitbenutzer: mb.value.trim() };
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
          toast(res.data?.pending ? res.message : 'Gespeichert, es hat sich nichts geändert.', res.data?.pending ? 'warn' : 'ok');
          load();
        } catch (ex) { err.textContent = ex.message; err.classList.remove('hidden'); } finally { save.disabled = false; }
      });
      return h('section', { class: 'card span-2' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('globe')), h('h2', null, 'Internet-Anschluss')),
        form);
    }

    // --- Name und Zeitzone -------------------------------------------------------------
    function systemCard() {
      const err = h('div', { class: 'form-error hidden' });
      const host = h('input', { class: 'input', value: data.hostname, autocomplete: 'off', spellcheck: 'false' });
      const tz = h('select', { class: 'input' }, h('option', { value: data.timezone, selected: true }, data.timezone));
      get('/api/settings/timezones').then((z) => {
        clear(tz).append(...z.zones.map((n) => h('option', { value: n, selected: n === data.timezone }, n.replaceAll('_', ' '))));
      }).catch(() => {});
      const form = h('form', null, err,
        field('Name des Routers', host, `Erreichbar als ${data.hostname}.${data.domain}. Kleinbuchstaben, Ziffern und Bindestrich.`),
        field('Zeitzone', tz, 'Gilt für Zeitsperren, Protokolle und den Zeitpunkt für Neustarts nach Updates.'),
        h('div', { class: 'row end' }, h('button', { class: 'btn primary', type: 'submit' }, 'Speichern')));
      form.addEventListener('submit', async (e) => {
        e.preventDefault();
        err.classList.add('hidden');
        const name = host.value.trim().toLowerCase();
        if (name !== data.hostname && !(await confirmDialog({ title: 'Namen ändern?',
          text: `Die Oberfläche ist danach unter https://${name}.${data.domain} erreichbar. Sie startet dafür kurz neu und bekommt ein neues Zertifikat, der Browser warnt deshalb einmal neu.`, confirm: 'Ändern' }))) return;
        try {
          const res = await post('/api/settings/system', { hostname: name, timezone: tz.value });
          toast(res.message);
          if (res.data?.restart) moveTo(res.data.new_url);
          else load();
        } catch (ex) { err.textContent = ex.message; err.classList.remove('hidden'); }
      });
      return h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('router')), h('h2', null, 'Router')),
        form);
    }

    // --- Heimnetz ----------------------------------------------------------------------
    function lanCard() {
      const l = data.lan;
      const err = h('div', { class: 'form-error hidden' });
      const addr = h('input', { class: 'input mono', value: l.address, autocomplete: 'off', spellcheck: 'false' });
      const prefix = h('select', { class: 'input' }, [16, 20, 22, 23, 24, 25, 26, 27, 28].map((b) =>
        h('option', { value: String(b), selected: b === l.prefix }, `/${b} (${(2 ** (32 - b) - 2).toLocaleString('de-DE')} Geräte)`)));
      const form = h('form', null, err,
        field('Adresse des Routers', addr, 'Private Bereiche: 192.168.x.x, 10.x.x.x oder 172.16–31.x.x.'),
        field('Netzgröße', prefix),
        h('div', { class: 'row end' }, h('button', { class: 'btn primary', type: 'submit' }, 'Ändern …')));
      form.addEventListener('submit', async (e) => {
        e.preventDefault();
        err.classList.add('hidden');
        const a = addr.value.trim();
        if (a === l.address && +prefix.value === l.prefix) return;
        if (!(await confirmDialog({ title: 'Heimnetz umstellen?', danger: true, confirm: 'Umstellen',
          text: `Der Router ist danach unter https://${a} erreichbar. Feste Adressen, DNS-Namen und Weiterleitungen werden mit umgezogen.`,
          details: h('ol', { class: 'small list' },
            h('li', null, 'Die Verbindung zu dieser Seite bricht ab.'),
            h('li', null, 'Verbinden Sie dieses Gerät neu mit dem Heimnetz (WLAN aus und wieder an oder Kabel kurz abziehen), damit es eine neue Adresse bekommt.'),
            h('li', null, `Öffnen Sie https://${a}, melden Sie sich an und bestätigen Sie die Änderung innerhalb von 5 Minuten. Sonst stellt der Router das alte Netz wieder her.`)) }))) return;
        try {
          const res = await post('/api/settings/lan', { address: a, prefix: +prefix.value });
          if (res.data?.restart) moveTo(res.data.new_url, true);
          else load();
        } catch (ex) { err.textContent = ex.message; err.classList.remove('hidden'); }
      });
      return h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('devices')), h('h2', null, 'Heimnetz')),
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
        h('div', { class: 'grid cols-2 tight' }, field('Erste Adresse', start), field('Letzte Adresse', end)),
        field('Gültigkeit einer Adresse', lease, 'Feste Adressen (Seite Geräte) gelten unabhängig vom Bereich.'),
        h('div', { class: 'row end' }, h('button', { class: 'btn primary', type: 'submit' }, 'Speichern')));
      form.addEventListener('submit', async (e) => {
        e.preventDefault();
        err.classList.add('hidden');
        try {
          toast((await post('/api/settings/dhcp', { pool_start: start.value.trim(), pool_end: end.value.trim(), lease: lease.value })).message);
          load();
        } catch (ex) { err.textContent = ex.message; err.classList.remove('hidden'); }
      });
      return h('section', { class: 'card span-2' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('pin')), h('h2', null, 'Automatische Adressen (DHCP)')),
        form);
    }

    // Nach einer Änderung von Name oder Adresse: Hinweis und Link auf die neue Adresse.
    function moveTo(url, lan) {
      clearInterval(countdown);
      clear(top); clear(pendingBox);
      clear(body).append(h('section', { class: 'card span-2 hero' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('arrowRight')), h('h2', null, 'Die Oberfläche zieht um')),
        h('p', null, lan
          ? 'Verbinden Sie dieses Gerät jetzt neu mit dem Heimnetz und öffnen Sie dann die neue Adresse. Bestätigen Sie dort die Änderung innerhalb von 5 Minuten.'
          : 'Die Oberfläche startet gerade neu. In ein paar Sekunden ist sie unter der neuen Adresse erreichbar.'),
        h('p', null, h('a', { class: 'btn primary', href: url }, url)),
        data.dry_run && h('p', { class: 'muted small' }, 'Testinstanz: Hier zieht nichts um, die alte Adresse bleibt.')));
    }

    load();
    return () => clearInterval(countdown);
  },
};

function field(label, input, hint) {
  return h('div', { class: 'field' }, h('label', null, label), input, hint && h('div', { class: 'hint' }, hint));
}
