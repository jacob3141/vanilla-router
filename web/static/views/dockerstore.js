// SPDX-License-Identifier: AGPL-3.0-or-later
// Images and volumes of Docker.
import { h, every, clear, icon, badge, toast, formDialog, confirmDialog, fmt } from '../ui.js';
import { get, post } from '../api.js';
import { table, card, empty, trash, act, removeWith, jobWatcher, textDialog, lines } from './docker-common.js';

const FOLDERS = 'A file under /docker, /srv, /media or /mnt.';

export const images = {
  title: 'Images',
  subtitle: 'The images Docker keeps on the router',
  render(root) {
    const body = h('tbody');
    const job = jobWatcher(root, () => load());
    const btn = (label, onclick) => h('button', { class: 'btn sm', onclick }, label);
    root.append(job.box, card('file', 'Images', h('div', { class: 'row' },
      btn('Pull', () => one('Pull an image', [{ name: 'image', label: 'Image', placeholder: 'e.g. nginx:latest', required: true }], '/api/containers/image/pull')),
      btn('Build', () => build()),
      btn('Import', () => one('Import an image', [{ name: 'file', label: 'File', placeholder: '/srv/image.tar', required: true, hint: FOLDERS }], '/api/containers/image/import')),
      btn('Prune', prune)), table(['Image', 'Size', 'Created', ''], body)));

    async function load() {
      const list = await get('/api/containers/images').catch(() => []);
      clear(body);
      empty(body, list.length, 4, 'No images.');
      for (const i of list) {
        body.append(h('tr', null, h('td', { class: 'mono small' }, i.name, !i.in_use && ' ', !i.in_use && badge('unused')), h('td', { class: 'muted' }, i.size), h('td', { class: 'muted small' }, i.created || ''),
          h('td', { class: 'actions' }, btn('Inspect', () => inspect(i)),
            btn('Tag', () => formDialog({ title: `Tag ${i.name}`, fields: [{ name: 'tag', label: 'New name', required: true }], onSubmit: (v) => post('/api/containers/image/tag', { image: i.name, tag: v.tag }) }).then(load)),
            btn('Push', () => confirmDialog({ title: `Upload ${i.name}?`, text: 'To the registry in its name. You must be logged in there.', confirm: 'Upload' }).then((ok) => ok && act('/api/containers/image/push', { image: i.name }, null, job.watch))),
            btn('Export', () => one(`Export ${i.name}`, [{ name: 'file', label: 'File', placeholder: '/srv/image.tar', required: true, hint: FOLDERS }], '/api/containers/image/export', { image: i.name })),
            !i.in_use && trash('Remove', () => removeWith('/api/containers/image/remove', { id: i.id }, `Remove ${i.name}?`, 'It would be downloaded again when a container needs it.', load)))));
      }
    }
    async function inspect(i) {
      try { const t = (await post('/api/containers/image/inspect', { name: i.id.replace('sha256:', '') })).data.text; textDialog(i.name, t); } catch (e) { toast(e.message, 'crit'); }
    }
    function one(title, fields, url, extra = {}) {
      return formDialog({ title, submit: 'Start', fields, onSubmit: (v) => post(url, { ...extra, ...v }) }).then((ok) => { if (ok) job.watch(); });
    }
    const build = () => formDialog({ title: 'Build an image', submit: 'Build', wide: true, fields: [
      { name: 'tag', label: 'Name', placeholder: 'e.g. my/app:1', required: true },
      { name: 'dockerfile', label: 'Dockerfile', type: 'textarea', rows: 14, placeholder: 'FROM alpine:3\nRUN apk add curl' },
    ], onSubmit: (v) => post('/api/containers/image/build', v) }).then((ok) => { if (ok) job.watch(); });
    async function prune() {
      if (await confirmDialog({ title: 'Remove all unused images?', text: 'Images no container uses are deleted.', confirm: 'Remove', danger: true })) act('/api/containers/image/prune', {}, 'Done.', load);
    }
    load();
    return every(30000, load);
  },
};

export const volumes = {
  title: 'Volumes',
  subtitle: 'Where containers keep their data',
  render(root) {
    const body = h('tbody');
    const btn = (label, onclick) => h('button', { class: 'btn sm', onclick }, label);
    root.append(card('drive', 'Volumes', h('div', { class: 'row' }, h('button', { class: 'btn sm primary', onclick: create }, icon('plus'), 'Volume'), btn('Prune', prune)), table(['Name', 'Used by', ''], body)));

    async function load() {
      const list = await get('/api/containers/volumes').catch(() => []);
      clear(body);
      empty(body, list.length, 3, 'No volumes.');
      for (const v of list) {
        body.append(h('tr', null, h('td', { class: 'mono small' }, v.name), h('td', null, v.used_by.join(', ') || badge('unused')),
          h('td', { class: 'actions' }, btn('Browse', () => browse(v.name, '')), trash('Remove', () => removeWith('/api/containers/volume/remove', { name: v.name }, `Remove ${v.name}?`, 'The data in this volume is deleted for good.', load), v.used_by.length > 0))));
      }
    }

    function create() {
      return formDialog({ title: 'New volume', submit: 'Create', fields: [
        { name: 'name', label: 'Name', placeholder: 'e.g. media', required: true },
        { name: 'kind', label: 'Kind', type: 'select', value: 'local', options: [{ value: 'local', label: 'On the router' }, { value: 'nfs', label: 'NFS share' }, { value: 'cifs', label: 'Windows share (CIFS)' }] },
        { name: 'server', label: 'Server', placeholder: 'e.g. 192.168.42.5', hint: 'Only for NFS and CIFS.' },
        { name: 'share', label: 'Share', placeholder: 'e.g. /export/media or media' },
        { name: 'options', label: 'Mount options', placeholder: 'NFS: nfsvers=4,rw. CIFS: username=x,password=y' },
      ], onSubmit: (v) => {
        const opts = [];
        if (v.kind === 'nfs') opts.push('type=nfs', `o=addr=${v.server}${v.options ? `,${v.options}` : ''}`, `device=:${v.share}`);
        if (v.kind === 'cifs') opts.push('type=cifs', `o=${v.options}`, `device=//${v.server}/${v.share}`);
        return post('/api/containers/volume', { name: v.name, driver: 'local', opts: lines(opts.join('\n')) });
      } }).then((ok) => { if (ok) load(); });
    }

    async function browse(name, path) {
      let d;
      try { d = (await post('/api/containers/volume/browse', { name, path })).data; } catch (e) { toast(e.message, 'crit'); return; }
      const dlg = h('dialog', { class: 'wide' },
        h('div', { class: 'dialog-body' }, h('h2', null, `${name}: /${d.path === '.' ? '' : d.path}`),
          table(['Name', 'Size', 'Changed'], h('tbody', null,
            d.path !== '.' && h('tr', { class: 'clickable', onclick: () => { dlg.close(); browse(name, d.path.split('/').slice(0, -1).join('/')); } }, h('td', { colspan: 3 }, '..')),
            d.entries.map((e) => h('tr', { class: e.dir ? 'clickable' : '', onclick: () => { if (e.dir) { dlg.close(); browse(name, d.path === '.' ? e.name : `${d.path}/${e.name}`); } } },
              h('td', { class: 'mono small' }, e.name, e.dir ? '/' : ''), h('td', { class: 'muted' }, e.dir ? '' : fmt.bytes(e.size)), h('td', { class: 'muted small' }, fmt.date(e.modified))))))),
        h('div', { class: 'dialog-foot' }, h('button', { class: 'btn', onclick: () => dlg.close() }, 'Close')));
      dlg.addEventListener('close', () => dlg.remove());
      document.body.appendChild(dlg);
      dlg.showModal();
    }

    async function prune() {
      if (await confirmDialog({ title: 'Remove all unused volumes?', text: 'Their data is deleted for good.', confirm: 'Remove', danger: true })) act('/api/containers/volume/prune', {}, 'Done.', load);
    }
    load();
    return every(30000, load);
  },
};
