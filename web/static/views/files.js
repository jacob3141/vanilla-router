// SPDX-License-Identifier: AGPL-3.0-or-later
import { t } from '../i18n.js';
import { h, clear, icon, fmt, badge, toast, confirmDialog, formDialog } from '../ui.js';
import { get, post } from '../api.js';

export default {
  title: 'File share',
  subtitle: 'The router\'s share in your browser',
  render(root) {
    let cwd = new URLSearchParams(location.hash.split('?')[1] || '').get('p') || '';
    let data = null;
    const selected = new Set();

    const crumbs = h('div', { class: 'crumbs' });
    const tbody = h('tbody');
    const space = h('div', { class: 'muted small' });
    const banner = h('div');
    const progress = h('div', { class: 'upload-progress hidden' });
    const fileInput = h('input', { type: 'file', multiple: true, class: 'hidden', onchange: (e) => { upload([...e.target.files]); e.target.value = ''; } });
    const delBtn = h('button', { class: 'btn danger', disabled: true, onclick: () => remove([...selected]) }, icon('trash'), 'Delete');
    const selectAll = h('input', { type: 'checkbox', title: 'Select all', onchange: (e) => { for (const en of data?.entries || []) e.target.checked ? selected.add(join(cwd, en.name)) : selected.delete(join(cwd, en.name)); draw(); } });

    const card = h('section', { class: 'card drop-zone' },
      h('div', { class: 'card-head' }, crumbs,
        h('button', { class: 'btn', onclick: mkdir }, icon('plus'), 'Folder'),
        delBtn,
        h('button', { class: 'btn primary', onclick: () => fileInput.click() }, icon('arrowRight', 'rot-up'), 'Upload'),
        fileInput),
      progress,
      h('div', { class: 'table-wrap' }, h('table', null,
        h('thead', null, h('tr', null, h('th', { class: 'check' }, selectAll), h('th', null, 'Name'), h('th', { class: 'num-col' }, 'Size'), h('th', null, 'Modified'), h('th'))),
        tbody)),
      h('div', { class: 'files-foot' }, space, h('span', { class: 'muted small' }, 'Drag files here to upload · Deleted items go to .trash')));
    const smb = h('div');
    root.append(banner, card, h('section', { class: 'card mt' },
      h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('devices')), h('h2', null, 'Connected through Samba (network drive)')), smb));

    async function loadSamba() {
      let s;
      try { s = await get('/api/samba'); } catch (e) { clear(smb).append(h('p', { class: 'muted' }, e.message)); return; }
      const sessions = Object.values(s.sessions || {});
      const files = Object.values(s.open_files || {});
      clear(smb);
      if (!sessions.length) { smb.append(h('p', { class: 'muted' }, 'No device is connected to the network drive right now.')); return; }
      smb.append(h('div', { class: 'table-wrap flat' }, h('table', null,
        h('thead', null, h('tr', null, ['Device', 'User', 'Protocol', 'Encryption'].map((t) => h('th', null, t)))),
        h('tbody', null, sessions.map((x) => h('tr', null,
          h('td', { class: 'mono small' }, x.remote_machine || x.hostname || '–'), h('td', null, x.username || '–'),
          h('td', null, x.session_dialect || '–'), h('td', null, x.encryption?.cipher ? badge(x.encryption.cipher, 'ok') : badge('none', 'warn'))))))),
        h('p', { class: 'muted small mt' }, files.length ? `${files.length} file(s) open right now: ${files.slice(0, 8).map((f) => f.filename).join(', ')}${files.length > 8 ? ' …' : ''}` : 'No files open.'));
    }
    loadSamba();

    // Drag & Drop
    card.addEventListener('dragover', (e) => { e.preventDefault(); card.classList.add('dragging'); });
    card.addEventListener('dragleave', (e) => { if (e.target === card) card.classList.remove('dragging'); });
    card.addEventListener('drop', (e) => {
      e.preventDefault();
      card.classList.remove('dragging');
      const files = [...(e.dataTransfer?.files || [])];
      if (files.length) upload(files);
    });

    async function load(path = cwd) {
      try {
        data = await get(`/api/files/list?path=${encodeURIComponent(path)}`);
      } catch (e) {
        clear(banner).append(h('div', { class: 'banner crit' }, icon('alert'), e.message));
        return;
      }
      clear(banner);
      cwd = data.path;
      selected.clear();
      const want = `#/files${cwd ? `?p=${encodeURIComponent(cwd)}` : ''}`;
      if (location.hash !== want) history.replaceState(null, '', want);
      draw();
    }

    function draw() {
      // breadcrumbs
      const parts = cwd ? cwd.split('/') : [];
      clear(crumbs).append(
        h('a', { href: '#', onclick: (e) => { e.preventDefault(); load(''); } }, icon('home'), 'Share'),
        ...parts.map((p, i) => [h('span', { class: 'sep' }, '/'),
          h('a', { href: '#', onclick: (e) => { e.preventDefault(); load(parts.slice(0, i + 1).join('/')); } }, p)]));

      clear(tbody);
      if (cwd) {
        tbody.append(h('tr', { class: 'clickable', onclick: () => load(parts.slice(0, -1).join('/')) },
          h('td'), h('td', { colspan: 4 }, h('span', { class: 'file-name' }, icon('arrowRight', 'rot-left'), '..'))));
      }
      if (!data.entries.length) tbody.append(h('tr', null, h('td', { colspan: 5, class: 'empty' }, 'This folder is empty.')));
      for (const en of data.entries) {
        const p = join(cwd, en.name);
        const isTrash = !cwd && en.name === data.trash;
        tbody.append(h('tr', { class: en.dir ? 'clickable' : '' },
          h('td', { class: 'check' }, !isTrash && h('input', { type: 'checkbox', checked: selected.has(p), onclick: (e) => e.stopPropagation(), onchange: (e) => { e.target.checked ? selected.add(p) : selected.delete(p); updateSel(); } })),
          h('td', { onclick: () => en.dir && load(p) },
            en.dir
              ? h('span', { class: 'file-name dir' }, icon(isTrash ? 'trash' : 'folder'), en.name)
              : h('a', { class: 'file-name', href: `/api/files/download?path=${encodeURIComponent(p)}`, download: en.name }, icon('file'), en.name)),
          h('td', { class: 'num-col muted' }, en.dir ? '' : fmt.bytes(en.size)),
          h('td', { class: 'muted small' }, fmt.date(en.modified)),
          h('td', { class: 'actions' },
            !en.dir && h('a', { class: 'btn sm ghost icon', title: 'Download', href: `/api/files/download?path=${encodeURIComponent(p)}`, download: en.name }, icon('download')),
            !isTrash && h('button', { class: 'btn sm ghost icon', title: 'Rename', onclick: () => rename(en) }, icon('edit')),
            !isTrash && h('button', { class: 'btn sm ghost icon danger', title: 'Delete', onclick: () => remove([p]) }, icon('trash')))));
      }
      if (data.total) {
        const used = data.total - data.free;
        clear(space).append(`${fmt.bytes(data.free)} free of ${fmt.bytes(data.total)}`,
          h('div', { class: 'meter space-meter' }, h('span', { style: { width: `${100 * used / data.total}%` } })));
      }
      updateSel();
    }

    function updateSel() {
      delBtn.disabled = selected.size === 0;
      clear(delBtn).append(icon('trash'), selected.size ? `Delete (${selected.size})` : 'Delete');
      selectAll.checked = data && data.entries.length > 0 && selected.size === data.entries.length;
    }

    async function mkdir() {
      const ok = await formDialog({
        title: 'New folder', fields: [{ name: 'name', label: 'Name', placeholder: 'e.g. Invoices 2026' }], submit: 'Create',
        onSubmit: (v) => post('/api/files/mkdir', { path: cwd, name: v.name }),
      });
      if (ok) load();
    }

    async function rename(en) {
      const ok = await formDialog({
        title: `Rename "${en.name}"`, fields: [{ name: 'name', label: 'New name', value: en.name }], submit: 'Rename',
        onSubmit: (v) => post('/api/files/rename', { path: join(cwd, en.name), new_name: v.name }),
      });
      if (ok) load();
    }

    async function remove(paths) {
      const inTrash = cwd === data.trash || cwd.startsWith(`${data.trash}/`);
      const what = paths.length === 1 ? `"${paths[0].split('/').pop()}"` : `${paths.length} entries`;
      if (!(await confirmDialog({
        title: inTrash ? `Permanently delete ${what}?` : `Delete ${what}?`,
        text: inTrash ? 'This cannot be undone.' : 'Deleted items go to the .trash folder and can be restored from there.',
        confirm: inTrash ? 'Delete permanently' : 'Delete', danger: true,
      }))) return;
      try { await post('/api/files/delete', { paths }); toast(inTrash ? 'Permanently deleted.' : 'Moved to the trash.'); } catch (e) { toast(e.message, 'crit'); }
      load();
    }

    // Upload through XMLHttpRequest, because fetch reports no progress when uploading
    function upload(files) {
      const total = files.reduce((n, f) => n + f.size, 0);
      const bar = h('span');
      const label = h('div', { class: 'small' }, `${files.length} file(s), ${fmt.bytes(total)} …`);
      clear(progress).append(label, h('div', { class: 'meter' }, bar));
      progress.classList.remove('hidden');
      const form = new FormData();
      for (const f of files) form.append('file', f, f.name);
      const xhr = new XMLHttpRequest();
      xhr.open('POST', `/api/files/upload?path=${encodeURIComponent(cwd)}`);
      xhr.setRequestHeader('X-Routerui', '1');
      xhr.upload.onprogress = (e) => {
        if (!e.lengthComputable) return;
        bar.style.width = `${100 * e.loaded / e.total}%`;
        label.textContent = t(`${fmt.bytes(e.loaded)} of ${fmt.bytes(e.total)} uploaded`);
      };
      xhr.onload = () => {
        progress.classList.add('hidden');
        let res = {};
        try { res = JSON.parse(xhr.responseText); } catch { /* leer */ }
        if (xhr.status === 200) toast(`${res.saved.length} file(s) uploaded.`);
        else toast(res.error || `Upload failed (${xhr.status}).`, 'crit');
        load();
      };
      xhr.onerror = () => { progress.classList.add('hidden'); toast('Upload cancelled.', 'crit'); };
      xhr.send(form);
    }

    load();
  },
};

function join(dir, name) { return dir ? `${dir}/${name}` : name; }
