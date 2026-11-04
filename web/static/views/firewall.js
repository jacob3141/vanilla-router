// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, clear, icon, badge, toast, confirmDialog, formDialog } from '../ui.js';
import { get, post } from '../api.js';

const PROTO = { tcp: 'TCP', udp: 'UDP', both: 'TCP + UDP' };

export default {
  title: 'Firewall',
  subtitle: 'Portweiterleitungen und Regeln',
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
            h('span', { class: 'icon-badge' }, icon('arrowRight')), h('h2', null, 'Portweiterleitungen'),
            h('button', { class: 'btn primary', onclick: () => edit(null) }, icon('plus'), 'Weiterleitung')),
          natHint,
          h('div', { class: 'table-wrap' }, h('table', null,
            h('thead', null, h('tr', null, ['Aktiv', 'Protokoll', 'Extern', 'Ziel im LAN', 'Notiz', ''].map((t) => h('th', null, t)))),
            tbody))),
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('shield')), h('h2', null, 'Grundregeln')),
          h('ul', { class: 'muted small list' },
            h('li', null, 'Aus dem Internet ist der Router nicht erreichbar, für IPv4 und IPv6 (policy drop).'),
            h('li', null, 'Aus dem Heimnetz ist alles erlaubt, Geräte dürfen ins Internet.'),
            h('li', null, 'Antworten auf eigene Verbindungen kommen durch, alles andere nur über eine Weiterleitung.'))),
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('globe')), h('h2', null, 'IPv6-Freigaben')),
          h('p', { class: 'muted small' }, 'Geräte im Heimnetz haben derzeit keine öffentlichen IPv6-Adressen, weil kein Präfix vom vorgeschalteten Router an das LAN weitergegeben wird (Prefix Delegation). Freigaben für IPv6 sind deshalb im Moment nicht möglich und auch nicht nötig.')),
        h('section', { class: 'card span-2' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('lock')), h('h2', null, 'Gesperrte Angreifer (fail2ban)')),
          h('p', { class: 'muted small' }, 'fail2ban sperrt IP-Adressen nach wiederholten Fehlversuchen, z. B. bei SSH oder der Anmeldung an dieser Oberfläche.'),
          f2b),
        h('section', { class: 'card span-2' },
          h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('file')), h('h2', null, 'Geladene Regeln')),
          rules)));

    async function loadF2B() {
      let jails;
      try { jails = await get('/api/fail2ban'); } catch (e) { clear(f2b).append(h('p', { class: 'muted' }, e.message)); return; }
      clear(f2b).append(h('div', { class: 'grid cols-2' }, jails.map((j) => h('div', { class: 'jail' },
        h('div', { class: 'row' }, h('strong', { class: 'mono' }, j.name), h('div', { class: 'spacer' }),
          badge(`${j.current_banned} gesperrt`, j.current_banned ? 'warn' : 'ok'), badge(`${j.total_failed} Fehlversuche`)),
        j.banned.length ? h('div', { class: 'chip-list mt' }, j.banned.map((ip) => h('span', { class: 'chip' }, h('span', { class: 'mono' }, ip),
          h('button', { class: 'btn sm ghost', title: 'Sperre aufheben', onclick: () => unban(j.name, ip) }, '×'))))
          : h('p', { class: 'muted small' }, 'Niemand gesperrt.')))));
    }

    async function unban(jail, ip) {
      if (!(await confirmDialog({ title: `${ip} entsperren?`, text: `Die Adresse darf dann wieder Verbindungen zu ${jail} aufbauen.`, confirm: 'Entsperren' }))) return;
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
          h('strong', null, 'Geteilte Adresse (CGNAT): '),
          'Ihr Anbieter teilt sich eine IPv4-Adresse mit anderen Kunden. Weiterleitungen über IPv4 kommen deshalb nicht an. Über IPv6 (Freigaben) geht es.')));
      } else if (dn) {
        natHint.append(h('div', { class: 'banner' }, icon('info'), h('div', null,
          h('strong', null, 'Doppeltes NAT: '),
          `Vor diesem Router steht noch ein Router (${dn.gateway || 'unbekannt'}). Damit eine Weiterleitung aus dem Internet ankommt, muss dort derselbe Port an `,
          h('span', { class: 'mono' }, dn.wan_ip), ' weitergeleitet werden.')));
      }
      if (data.dry_run) banner.append(h('div', { class: 'banner warn' }, icon('info'), 'Testinstanz: Änderungen werden geprüft, aber nicht eingespielt.'));
      drawPending(data.pending_until);
      drawTable();
      drawRules();
    }

    function drawTable() {
      clear(tbody);
      const list = data.port_forwards || [];
      if (!list.length) { tbody.append(h('tr', null, h('td', { colspan: 6, class: 'empty' }, 'Keine Weiterleitungen. Aus dem Internet kommt nichts ins Heimnetz.'))); return; }
      for (const p of [...list].sort((a, b) => a.ext_port - b.ext_port)) {
        const ext = p.ext_port_end ? `${p.ext_port}–${p.ext_port_end}` : String(p.ext_port);
        const intPort = p.ext_port_end ? 'gleiche Ports' : String(p.int_port || p.ext_port);
        const name = data.devices[p.int_ip];
        tbody.append(h('tr', null,
          h('td', null, h('input', { type: 'checkbox', checked: p.enabled, title: p.enabled ? 'Deaktivieren' : 'Aktivieren', onchange: () => save({ ...p, enabled: !p.enabled }) })),
          h('td', null, badge(PROTO[p.proto])),
          h('td', { class: 'mono' }, ext),
          h('td', null, h('span', { class: 'mono' }, `${p.int_ip} : ${intPort}`), name && h('div', { class: 'muted small' }, name)),
          h('td', { class: 'muted' }, p.comment || ''),
          h('td', { class: 'actions' },
            h('button', { class: 'btn sm ghost icon', title: 'Bearbeiten', onclick: () => edit(p) }, icon('edit')),
            h('button', { class: 'btn sm ghost icon danger', title: 'Löschen', onclick: () => remove(p) }, icon('trash')))));
      }
    }

    function drawRules() {
      clear(rules);
      for (const [name, text] of Object.entries(data.tables)) {
        rules.append(h('details', { class: 'rules' }, h('summary', null, h('span', { class: 'mono' }, name)), h('pre', { class: 'code' }, text)));
      }
      rules.append(h('p', { class: 'muted small' }, 'Alle geladenen Tabellen: ', data.all_tables.join(' · '), '. Tabellen ohne „vr_“ gehören anderen Diensten (z. B. Docker).'));
    }

    function drawPending(until) {
      clearInterval(countdown);
      clear(pendingBox);
      if (!until) return;
      const left = h('strong', { class: 'num' });
      const box = h('div', { class: 'banner warn pending' }, icon('alert'),
        h('div', { class: 'spacer' }, 'Die Änderung ist aktiv. Ohne Bestätigung wird sie in ', left, ' automatisch zurückgenommen.'),
        h('button', { class: 'btn primary', onclick: confirm }, icon('check'), 'Bestätigen'));
      pendingBox.append(box);
      const tick = () => {
        const s = Math.max(0, Math.round((new Date(until) - Date.now()) / 1000));
        left.textContent = `${s} s`;
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
        title: p ? 'Weiterleitung bearbeiten' : 'Neue Portweiterleitung',
        text: 'Leitet Verbindungen aus dem Internet an ein Gerät im Heimnetz weiter. Gib dem Ziel am besten vorher eine feste Adresse.',
        fields: [
          { name: 'proto', label: 'Protokoll', type: 'select', value: p?.proto || 'tcp', options: Object.entries(PROTO).map(([value, label]) => ({ value, label })) },
          { name: 'ext', label: 'Externer Port oder Bereich', value: p ? (p.ext_port_end ? `${p.ext_port}-${p.ext_port_end}` : String(p.ext_port)) : '', placeholder: 'z. B. 8080 oder 27015-27030' },
          { name: 'int_ip', label: 'Ziel-IP im Heimnetz', value: p?.int_ip || '', placeholder: 'z. B. 192.168.42.20', hint: Object.entries(data.devices).map(([ip, n]) => `${n} = ${ip}`).join(' · ') || '' },
          { name: 'int_port', label: 'Ziel-Port (optional)', value: p?.int_port ? String(p.int_port) : '', placeholder: 'leer = wie extern', hint: 'Bei einem Portbereich bleibt das Feld leer.' },
          { name: 'comment', label: 'Notiz (optional)', value: p?.comment || '', placeholder: 'z. B. Spieleserver' },
          { name: 'enabled', label: 'Aktiv', type: 'checkbox', value: p ? p.enabled : true },
        ],
        submit: 'Übernehmen',
        onSubmit: (v) => {
          const m = /^(\d+)(?:\s*[-–]\s*(\d+))?$/.exec(v.ext);
          if (!m) throw new Error('Externer Port: eine Zahl oder ein Bereich wie 27015-27030.');
          if (v.int_port && !/^\d+$/.test(v.int_port)) throw new Error('Ziel-Port muss eine Zahl sein.');
          return post('/api/portforwards', {
            id: p?.id || '', proto: v.proto, ext_port: +m[1], ext_port_end: m[2] ? +m[2] : 0,
            int_ip: v.int_ip, int_port: v.int_port ? +v.int_port : 0, comment: v.comment, enabled: v.enabled,
          });
        },
      });
      if (ok) { toast(ok.message, 'warn'); load(); }
    }

    async function remove(p) {
      if (!(await confirmDialog({ title: `Weiterleitung von Port ${p.ext_port} entfernen?`, text: `Verbindungen auf Port ${p.ext_port} kommen danach nicht mehr bei ${p.int_ip} an.`, confirm: 'Entfernen', danger: true }))) return;
      try { toast((await post('/api/portforwards/delete', { id: p.id })).message, 'warn'); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    load();
    return () => clearInterval(countdown);
  },
};
