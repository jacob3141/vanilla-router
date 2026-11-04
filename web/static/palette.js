// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, clear, icon } from './ui.js';
import { get } from './api.js';

// Schnellsuche (Strg+K oder /): Seiten, Geräte und Aktionen, per Tastatur bedienbar.
export function setupPalette(pages) {
  let dlg = null;

  async function open() {
    if (dlg) return;
    const input = h('input', { class: 'input palette-input', placeholder: 'Seite, Gerät, IP oder MAC suchen …', spellcheck: 'false', autocomplete: 'off' });
    const list = h('div', { class: 'palette-list' });
    dlg = h('dialog', { class: 'palette' }, h('div', { class: 'palette-body' }, h('div', { class: 'palette-search' }, icon('search'), input), list,
      h('div', { class: 'muted small palette-foot' }, '↑ ↓ wählen · Enter öffnen · Esc schließen')));
    document.body.appendChild(dlg);
    dlg.addEventListener('close', () => { dlg.remove(); dlg = null; });
    dlg.addEventListener('click', (e) => { if (e.target === dlg) dlg.close(); });
    dlg.showModal();
    input.focus();

    const items = pages.map((p) => ({ label: p.label, sub: p.section || 'Seite', icon: p.icon, href: `#/${p.path}`, text: `${p.label} ${p.section || ''}` }));
    items.push(
      { label: 'Feste Adresse vergeben', sub: 'Aktion', icon: 'pin', href: '#/devices', text: 'reservierung dhcp fest ip' },
      { label: 'Portweiterleitung anlegen', sub: 'Aktion', icon: 'arrowRight', href: '#/firewall', text: 'port forward weiterleitung nat' },
      { label: 'Passwort ändern', sub: 'Aktion', icon: 'lock', href: '#/settings', text: 'passwort kennwort' },
    );
    let sel = 0;
    let shown = [];

    function draw() {
      const q = input.value.trim().toLowerCase();
      const words = q.split(/\s+/).filter(Boolean);
      shown = items.filter((it) => words.every((w) => it.text.toLowerCase().includes(w))).slice(0, 12);
      sel = Math.min(sel, Math.max(0, shown.length - 1));
      clear(list);
      if (!shown.length) list.append(h('div', { class: 'empty' }, 'Nichts gefunden.'));
      shown.forEach((it, i) => list.append(h('a', { class: `palette-item ${i === sel ? 'active' : ''}`, href: it.href, onclick: () => dlg.close(), onmousemove: () => { if (sel !== i) { sel = i; draw(); } } },
        icon(it.icon), h('div', null, h('div', { class: 'cell-main' }, it.label), h('div', { class: 'muted small' }, it.sub)))));
    }
    input.addEventListener('input', () => { sel = 0; draw(); });
    input.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowDown') { e.preventDefault(); sel = Math.min(sel + 1, shown.length - 1); draw(); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); sel = Math.max(sel - 1, 0); draw(); }
      else if (e.key === 'Enter' && shown[sel]) { e.preventDefault(); location.hash = shown[sel].href; dlg.close(); }
    });
    draw();

    // Geräte nachladen, die Seiten sind sofort da
    try {
      const d = await get('/api/devices');
      for (const dev of d.devices) {
        const name = dev.alias || dev.reservation?.name || dev.hostname || dev.mac;
        items.push({ label: name, sub: `Gerät · ${dev.ip || 'keine IP'} · ${dev.mac}${dev.online ? ' · online' : ''}`, icon: 'devices',
          href: `#/device?mac=${encodeURIComponent(dev.mac)}`, text: `${name} ${dev.hostname || ''} ${dev.dns_name || ''} ${dev.ip || ''} ${dev.mac} ${dev.note || ''}` });
      }
      if (dlg) draw();
    } catch { /* ohne Geräte */ }
  }

  document.addEventListener('keydown', (e) => {
    const inField = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName);
    if ((e.key === 'k' && (e.ctrlKey || e.metaKey)) || (e.key === '/' && !inField && !dlg)) {
      if (!document.querySelector('.shell')) return; // erst nach dem Anmelden
      e.preventDefault();
      open();
    }
  });
  return open;
}
