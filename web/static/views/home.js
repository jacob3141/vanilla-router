// SPDX-License-Identifier: AGPL-3.0-or-later
// Smart Home: devices sorted by rooms (docs/smarthome-concept.md). Names come from devices and the owner: only through h().
import { t } from '../i18n.js';
import { h, clear, icon, toast, badge, confirmDialog, formDialog, fmt } from '../ui.js';
import { get, post } from '../api.js';

const STATE = { open: 'Open', closed: 'Closed', opening: 'Opening', closing: 'Closing', stopped: 'Stopped' };

export default {
  title: 'Smart Home',
  subtitle: 'Your devices, sorted by room',
  render(root) {
    let data = null;
    let dialogs = 0;
    let es = null;
    const banner = h('div');
    const toolbar = h('div', { class: 'row end' },
      h('button', { class: 'btn', onclick: () => editRoom(null) }, icon('plus'), 'Room'));
    const body = h('div', { class: 'home-rooms' });
    root.append(banner, toolbar, body);

    function value(e) {
      const parts = [];
      if (e.kind === 'cover') {
        parts.push(badge(t(STATE[e.state] || e.state || 'Unknown'), e.state === 'opening' || e.state === 'closing' ? 'info' : ''));
        if (e.position != null) parts.push(h('span', { class: 'num' }, `${e.position} %`));
      } else if (e.on != null) {
        parts.push(badge(e.on ? 'On' : 'Off', e.on ? 'ok' : ''));
        if (e.level != null && e.on) parts.push(h('span', { class: 'num' }, `${e.level} %`));
      }
      if (e.power != null) parts.push(h('span', { class: 'num' }, `${fmt.num(e.power, e.power < 100 ? 1 : 0)} W`));
      return parts;
    }

    function deviceTile(d) {
      return h('div', { class: `home-device${d.online ? '' : ' offline'}` },
        h('div', { class: 'row' },
          h('div', { class: 'spacer' }, h('div', { class: 'cell-main' }, d.name), h('div', { class: 'muted small' }, `${d.model} · ${d.host}`)),
          !d.online && badge('Offline', 'crit'),
          h('button', { class: 'btn sm icon', title: 'Edit', onclick: () => editDevice(d) }, icon('edit'))),
        h('div', { class: 'home-entities' }, d.entities.map((e) => h('div', { class: 'row home-entity' }, value(e)))));
    }

    function section(title, devices, room) {
      return h('section', { class: 'card' },
        h('div', { class: 'card-head' },
          h('h2', { class: 'spacer' }, title),
          room && h('button', { class: 'btn sm icon', title: 'Rename', onclick: () => editRoom(room) }, icon('edit')),
          room && h('button', { class: 'btn sm icon danger', title: 'Remove', onclick: () => removeRoom(room) }, icon('trash'))),
        devices.length ? h('div', { class: 'home-devices' }, devices.map(deviceTile)) : h('p', { class: 'muted small' }, 'No devices in this room yet.'));
    }

    function draw() {
      clear(body);
      if (!data.devices.length) {
        body.append(h('section', { class: 'card hero' }, h('h2', null, 'Looking for devices'), h('p', null, 'The router asks the devices it knows in your home network. Supported so far: Shelly. New devices appear here by themselves.')));
      }
      for (const r of data.rooms) body.append(section(r.name, data.devices.filter((d) => d.room === r.id), r));
      const rest = data.devices.filter((d) => !d.room || !data.rooms.some((r) => r.id === d.room));
      if (rest.length) body.append(section('Not sorted yet', rest, null));
    }

    function apply(d) {
      data = d;
      if (dialogs === 0) draw();
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
        fields: [
          { name: 'name', label: 'Name', value: d.name, hint: 'Leave empty for the automatic name.' },
          { name: 'room', label: 'Room', type: 'select', value: d.room || '', options: [{ value: '', label: 'Not sorted yet' }, ...data.rooms.map((r) => ({ value: r.id, label: r.name }))] },
        ],
        onSubmit: (v) => post('/api/home/device', { id: d.id, name: v.name, room: v.room }),
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
