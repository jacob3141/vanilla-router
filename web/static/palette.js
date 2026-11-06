// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, clear, icon } from './ui.js';
import { get } from './api.js';

// Quick search (Ctrl+K or /): pages, devices and actions, usable by keyboard.
export function setupPalette(pages) {
  let dlg = null;

  async function open() {
    if (dlg) return;
    const input = h('input', { class: 'input palette-input', placeholder: 'Search page, device, IP or MAC …', spellcheck: 'false', autocomplete: 'off' });
    const list = h('div', { class: 'palette-list' });
    dlg = h('dialog', { class: 'palette' }, h('div', { class: 'palette-body' }, h('div', { class: 'palette-search' }, icon('search'), input), list,
      h('div', { class: 'muted small palette-foot' }, '↑ ↓ choose · Enter open · Esc close')));
    document.body.appendChild(dlg);
    dlg.addEventListener('close', () => { dlg.remove(); dlg = null; });
    dlg.addEventListener('click', (e) => { if (e.target === dlg) dlg.close(); });
    dlg.showModal();
    input.focus();

    const items = pages.map((p) => ({ label: p.label, sub: p.section || 'Page', icon: p.icon, href: `#/${p.path}`, text: `${p.label} ${p.section || ''}` }));
    items.push(
      { label: 'Assign a fixed address', sub: 'Action', icon: 'pin', href: '#/devices', text: 'reservation reservierung dhcp fixed fest ip' },
      { label: 'Create a port forward', sub: 'Action', icon: 'arrowRight', href: '#/firewall', text: 'port forward nat weiterleitung' },
      { label: 'Change password', sub: 'Action', icon: 'lock', href: '#/settings', text: 'password passwort kennwort' },
      { label: 'Provider credentials', sub: 'Action', icon: 'globe', href: '#/internet', text: 'pppoe dsl telekom credentials wan internet zugangsdaten anschluss' },
      { label: 'Change home network address', sub: 'Action', icon: 'devices', href: '#/internet', text: 'lan ip address adresse subnet subnetz network netz dhcp range bereich' },
      { label: 'Name and time zone', sub: 'Action', icon: 'router', href: '#/internet', text: 'hostname name time zone zeitzone timezone' },
    );
    let sel = 0;
    let shown = [];

    function draw() {
      const q = input.value.trim().toLowerCase();
      const words = q.split(/\s+/).filter(Boolean);
      shown = items.filter((it) => words.every((w) => it.text.toLowerCase().includes(w))).slice(0, 12);
      sel = Math.min(sel, Math.max(0, shown.length - 1));
      clear(list);
      if (!shown.length) list.append(h('div', { class: 'empty' }, 'Nothing found.'));
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

    // Load devices afterwards, the pages are there right away
    try {
      const d = await get('/api/devices');
      for (const dev of d.devices) {
        const name = dev.alias || dev.reservation?.name || dev.hostname || dev.mac;
        items.push({ label: name, sub: `Device · ${dev.ip || 'no IP'} · ${dev.mac}${dev.online ? ' · online' : ''}`, icon: 'devices',
          href: `#/device?mac=${encodeURIComponent(dev.mac)}`, text: `${name} ${dev.hostname || ''} ${dev.dns_name || ''} ${dev.ip || ''} ${dev.mac} ${dev.note || ''}` });
      }
      if (dlg) draw();
    } catch { /* without devices */ }
  }

  document.addEventListener('keydown', (e) => {
    const inField = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName);
    if ((e.key === 'k' && (e.ctrlKey || e.metaKey)) || (e.key === '/' && !inField && !dlg)) {
      if (!document.querySelector('.shell')) return; // only after signing in
      e.preventDefault();
      open();
    }
  });
  return open;
}
