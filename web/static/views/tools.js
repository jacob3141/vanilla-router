// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, icon } from '../ui.js';
import { post } from '../api.js';

export default {
  title: 'Werkzeuge',
  subtitle: 'Ping, Routenverfolgung und DNS-Abfrage vom Router aus',
  render(root) {
    root.append(h('div', { class: 'grid cols-2' },
      tool('Ping', 'activity', 'Erreichbarkeit und Antwortzeit prüfen.', [
        { name: 'target', placeholder: 'z. B. 9.9.9.9 oder drucker.home.arpa' },
        { name: 'ipv6', type: 'checkbox', label: 'IPv6' },
      ], (v) => post('/api/tools/ping', { target: v.target, count: 4, ipv6: v.ipv6 })),
      tool('Routenverfolgung', 'arrowRight', 'Über welche Stationen Pakete laufen (tracepath).', [
        { name: 'target', placeholder: 'z. B. ubuntu.com' },
        { name: 'ipv6', type: 'checkbox', label: 'IPv6' },
      ], (v) => post('/api/tools/trace', { target: v.target, ipv6: v.ipv6 })),
      tool('DNS-Abfrage', 'dns', 'Fragt dnsmasq auf dem Router (oder einen anderen Server).', [
        { name: 'name', placeholder: 'Name oder IP (für PTR)' },
        { name: 'type', type: 'select', options: ['A', 'AAAA', 'PTR', 'MX', 'TXT', 'NS', 'CNAME', 'SOA', 'SRV'] },
        { name: 'server', placeholder: 'Server (leer = dieser Router)' },
      ], (v) => post('/api/tools/dns', v)),
      tool('Port prüfen', 'shield', 'Kann der Router eine TCP-Verbindung zu einem Dienst aufbauen? Z. B. ob ein Gerät im Heimnetz auf Port 80 antwortet.', [
        { name: 'target', placeholder: 'Ziel, z. B. drucker.home.arpa' },
        { name: 'port', placeholder: 'Port', width: '90px' },
      ], async (v) => {
        const r = (await post('/api/tools/port', { target: v.target, port: +v.port || 0 })).data;
        const what = { open: 'OFFEN: Der Dienst nimmt Verbindungen an', closed: 'GESCHLOSSEN: Das Ziel lehnt ab (nichts lauscht dort)', timeout: 'KEINE ANTWORT: Firewall oder Gerät aus', error: `FEHLER: ${r.error}` }[r.state];
        return { data: { text: `${r.target}:${r.port}\n${what}\nnach ${r.ms.toFixed(1)} ms${r.remote ? `\nverbunden mit ${r.remote}` : ''}` } };
      }),
    ));
  },
};

function tool(title, ic, text, fields, run) {
  const out = h('pre', { class: 'code log tool-out hidden' });
  const inputs = {};
  const btn = h('button', { class: 'btn primary', type: 'submit' }, icon('play'), 'Ausführen');
  const form = h('form', { class: 'row' },
    fields.map((f) => {
      if (f.type === 'checkbox') {
        inputs[f.name] = h('input', { type: 'checkbox' });
        return h('label', { class: 'row small' }, inputs[f.name], f.label);
      }
      if (f.type === 'select') {
        inputs[f.name] = h('select', { class: 'input', style: { width: '110px' } }, f.options.map((o) => h('option', { value: o }, o)));
        return inputs[f.name];
      }
      inputs[f.name] = h('input', { class: 'input', placeholder: f.placeholder, spellcheck: 'false', autocomplete: 'off', style: f.width ? { width: f.width } : { flex: '1', minWidth: '180px' } });
      return inputs[f.name];
    }),
    btn);
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const v = {};
    for (const [k, el] of Object.entries(inputs)) v[k] = el.type === 'checkbox' ? el.checked : el.value.trim();
    btn.disabled = true;
    out.classList.remove('hidden');
    out.textContent = 'Läuft …';
    try { out.textContent = (await run(v)).data?.text || '(keine Ausgabe)'; } catch (ex) { out.textContent = ex.message; }
    btn.disabled = false;
  });
  return h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon(ic)), h('h2', null, title)),
    h('p', { class: 'muted small' }, text), form, out);
}
