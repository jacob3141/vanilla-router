// SPDX-License-Identifier: AGPL-3.0-or-later
// Smart Home: devices sorted by rooms (docs/smarthome-concept.md). Names come from devices and the owner: only through h().
// Normally a compact page for using the devices. Rooms and assignments are changed in a separate "Arrange" mode.
import { t } from '../i18n.js';
import { h, clear, icon, toast, badge, confirmDialog, formDialog, fmt } from '../ui.js';
import { get, post } from '../api.js';

const MOVING = { opening: 'Opening', closing: 'Closing' };
const CONTROLLABLE = ['switch', 'light', 'cover'];

export default {
  title: 'Smart Home',
  subtitle: 'Your devices, sorted by room',
  render(root) {
    let data = null;
    let dialogs = 0;
    let es = null;
    let arranging = false;
    let dragging = false;
    const busy = new Set();
    const expanded = new Set();
    const banner = h('div');
    const addRoom = h('button', { class: 'btn sm hidden', onclick: () => editRoom(null) }, icon('plus'), 'Room');
    const arrange = h('button', { class: 'btn sm', onclick: () => {
      arranging = !arranging;
      arrange.textContent = t(arranging ? 'Done' : 'Arrange');
      addRoom.classList.toggle('hidden', !arranging);
      if (data) draw();
    } }, 'Arrange');
    const toolbar = h('div', { class: 'row end home-toolbar' }, addRoom, arrange);
    const body = h('div', { class: 'home-rooms' });
    root.append(banner, toolbar, body);

    const key = (d, e) => `${d.id}/${e.id}`;

    async function act(d, e, action, before, value) {
      busy.add(key(d, e));
      draw();
      try {
        await post('/api/home/command', { device: d.id, entity: e.id, action, ...(value === undefined ? {} : { value }) });
      } catch (err) {
        if (before !== undefined) e.on = before;
        toast(err.message, 'crit');
      }
      busy.delete(key(d, e));
      draw();
    }

    function toggle(d, e) {
      const k = key(d, e);
      return h('button', { class: `home-toggle${e.on ? ' on' : ''}`, role: 'switch', 'aria-checked': e.on ? 'true' : 'false', 'aria-label': d.name, disabled: busy.has(k) || !d.online,
        onclick: () => { const before = e.on; e.on = !before; act(d, e, e.on ? 'on' : 'off', before); } });
    }

    function cover(d, e) {
      const off = busy.has(key(d, e)) || !d.online;
      const b = (action, label, ic) => h('button', { class: 'btn sm icon', title: label, 'aria-label': label, disabled: off, onclick: () => act(d, e, action) }, ic);
      return h('div', { class: 'home-cover' }, b('open', 'Open', icon('arrowRight', 'rot-up')), b('stop', 'Stop', icon('stop')), b('close', 'Close', icon('arrowRight', 'rot-down')));
    }

    function value(e) {
      const parts = [];
      if (e.kind === 'cover' && MOVING[e.state]) parts.push(t(MOVING[e.state]));
      if (e.power != null && (e.kind !== 'cover' || e.power > 0)) parts.push(`${fmt.num(e.power, e.power < 100 ? 1 : 0)} W`);
      return h('span', { class: 'home-val' }, parts.join(' · '));
    }

    // What the slider of an entity sets: [current value, lowest value, action], or null if it has none
    function range(e) {
      if (e.kind === 'light' && e.level != null) return [e.level, 1, 'level'];
      if (e.kind === 'cover' && e.position != null) return [e.position, 0, 'position'];
      return null;
    }

    // The percentage opens a slider below the row, so that the list stays short
    function percent(d, e) {
      const r = range(e);
      if (!r || MOVING[e.state]) return null;
      const k = key(d, e);
      return h('button', { class: `home-percent${expanded.has(k) ? ' open' : ''}`, 'aria-expanded': expanded.has(k) ? 'true' : 'false', title: e.kind === 'cover' ? 'Set position' : 'Set brightness', disabled: !d.online,
        onclick: () => { if (expanded.has(k)) expanded.delete(k); else expanded.add(k); draw(); } }, `${r[0]} %`);
    }

    function slider(d, e) {
      const [current, min, action] = range(e);
      const label = h('span', { class: 'home-val num' }, `${current} %`);
      const input = h('input', { type: 'range', class: 'home-slider', min: String(min), max: '100', value: String(current), disabled: busy.has(key(d, e)) || !d.online,
        'aria-label': e.kind === 'cover' ? `Position of ${d.name}` : `Brightness of ${d.name}`,
        onpointerdown: () => { dragging = true; },
        // A click that does not change the value sends no change event
        onpointerup: () => { setTimeout(() => { dragging = false; }, 50); },
        onpointercancel: () => { dragging = false; },
        oninput: (ev) => { dragging = true; label.textContent = `${ev.target.value} %`; },
        onchange: (ev) => {
          dragging = false;
          const v = +ev.target.value;
          if (action === 'level') { e.level = v; e.on = true; } else e.position = v;
          act(d, e, action, undefined, v);
        } });
      return h('div', { class: 'home-slide' }, input, label);
    }

    const row = (d, label, ...rest) => h('div', { class: `home-row${d.online ? '' : ' offline'}`, title: `${d.model} · ${d.host}` },
      h('span', { class: 'home-name' }, label), !d.online && badge('Offline', 'crit'), ...rest);

    function deviceRows(d) {
      const controls = d.entities.filter((e) => CONTROLLABLE.includes(e.kind));
      if (controls.length) {
        return controls.flatMap((e, i) => {
          const line = row(d, controls.length > 1 ? `${d.name} · ${i + 1}` : d.name, value(e), percent(d, e), e.kind === 'cover' ? cover(d, e) : toggle(d, e));
          return expanded.has(key(d, e)) && range(e) ? [line, slider(d, e)] : [line];
        });
      }
      const power = d.entities.filter((e) => e.kind === 'power');
      return [row(d, d.name, ...power.map(value))];
    }

    function arrangeRow(d) {
      const rooms = h('select', { class: 'input auto', 'aria-label': 'Room', onchange: async (ev) => {
        const room = ev.target.value;
        ev.target.blur();
        try { await post('/api/home/device', { id: d.id, name: d.renamed ? d.name : '', room }); } catch (e) { toast(e.message, 'crit'); }
      } }, [{ id: '', name: 'Not sorted yet' }, ...data.rooms].map((r) => h('option', { value: r.id, selected: r.id === (d.room || '') }, r.name)));
      return row(d, d.name, rooms, h('button', { class: 'btn sm icon', title: 'Rename', onclick: () => editDevice(d) }, icon('edit')));
    }

    function section(title, devices, room) {
      return h('section', { class: 'card home-room' },
        h('div', { class: 'card-head' },
          h('h2', { class: 'spacer' }, title),
          h('span', { class: 'muted small' }, String(devices.length)),
          arranging && room && h('button', { class: 'btn sm icon', title: 'Rename', onclick: () => editRoom(room) }, icon('edit')),
          arranging && room && h('button', { class: 'btn sm icon danger', title: 'Remove', onclick: () => removeRoom(room) }, icon('trash'))),
        devices.length ? devices.flatMap((d) => (arranging ? [arrangeRow(d)] : deviceRows(d))) : h('p', { class: 'muted small' }, 'No devices in this room yet.'));
    }

    function draw() {
      clear(body);
      if (!data.devices.length) {
        body.append(h('section', { class: 'card hero' }, h('h2', null, 'Looking for devices'), h('p', null, 'The router asks the devices it knows in your home network. Supported so far: Shelly. New devices appear here by themselves.')));
      }
      const rest = data.devices.filter((d) => !d.room || !data.rooms.some((r) => r.id === d.room));
      const unsorted = rest.length ? section('Not sorted yet', rest, null) : null;
      if (arranging && unsorted) body.append(unsorted);
      for (const r of data.rooms) body.append(section(r.name, data.devices.filter((d) => d.room === r.id), r));
      if (!arranging && unsorted) body.append(unsorted);
    }

    function apply(d) {
      data = d;
      // An open list would close if the page were redrawn under it
      const open = document.activeElement?.tagName === 'SELECT' && body.contains(document.activeElement);
      if (dialogs === 0 && !open && !dragging) draw();
    }

    function connect() {
      es = new EventSource('/api/home/events');
      es.onmessage = (ev) => { clear(banner); apply(JSON.parse(ev.data)); };
      es.onerror = () => clear(banner).append(h('div', { class: 'banner warn' }, icon('alert'), 'Connection lost, the values may be out of date.'));
    }

    async function dialog(opts) {
      dialogs++;
      try { await formDialog(opts); } finally { dialogs--; if (data) draw(); }
    }

    function editDevice(d) {
      return dialog({
        title: d.name, submit: 'Save',
        fields: [{ name: 'name', label: 'Name', value: d.renamed ? d.name : '', placeholder: d.name, hint: 'Leave empty for the automatic name.' }],
        onSubmit: (v) => post('/api/home/device', { id: d.id, name: v.name, room: d.room || '' }),
      });
    }

    function editRoom(r) {
      return dialog({
        title: r ? r.name : 'New room', submit: 'Save',
        fields: [{ name: 'name', label: 'Name', value: r?.name || '', placeholder: 'e.g. Kitchen' }],
        onSubmit: (v) => post('/api/home/room', { id: r?.id || '', name: v.name }),
      });
    }

    async function removeRoom(r) {
      dialogs++;
      let ok;
      try { ok = await confirmDialog({ title: `Remove ${r.name}?`, confirm: 'Remove', danger: true, text: 'The devices stay and appear under "Not sorted yet".' }); } finally { dialogs--; }
      if (!ok) { draw(); return; }
      try { await post('/api/home/room/delete', { id: r.id }); } catch (e) { toast(e.message, 'crit'); }
    }

    get('/api/home/overview').then(apply).catch((e) => clear(banner).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)));
    connect();
    return () => es && es.close();
  },
};
