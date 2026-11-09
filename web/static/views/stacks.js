// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, every, clear, icon, badge, toast, formDialog } from '../ui.js';
import { get, post } from '../api.js';
import { table, card, empty, trash, act, removeWith, jobWatcher, textDialog, webhookDialog } from './docker-common.js';

export default {
  title: 'Stacks',
  subtitle: 'Groups of containers from a Compose file',
  render(root) {
    const stacks = h('tbody');
    const templates = h('tbody');
    const catalogBody = h('tbody');
    let catalog = [];
    let search = '';
    const job = jobWatcher(root, () => load());
    root.append(job.box,
      card('container', 'Stacks', h('div', { class: 'row' }, h('button', { class: 'btn sm', onclick: () => gitForm() }, icon('plus'), 'From Git'), h('button', { class: 'btn primary sm', onclick: () => edit() }, icon('plus'), 'Stack')), table(['Name', 'State', ''], stacks)),
      card('file', 'Templates', h('button', { class: 'btn sm', onclick: () => editTemplate() }, icon('plus'), 'Template'), table(['Name', ''], templates)),
      card('download', 'Application templates', h('input', { class: 'input auto', placeholder: 'Search', oninput: (e) => { search = e.target.value.toLowerCase(); drawCatalog(); } }),
        h('p', { class: 'muted small' }, 'The community catalog of Portainer. These are other people\'s images: check what you run.'), table(['Name', 'Kind', ''], catalogBody)));

    const btn = (label, onclick) => h('button', { class: 'btn sm', onclick }, label);

    async function load() {
      const [list, tpl] = await Promise.all([get('/api/stacks').catch(() => []), get('/api/stacks/templates').catch(() => [])]);
      clear(stacks);
      empty(stacks, list.length, 3, 'No stacks.');
      for (const s of list) {
        stacks.append(h('tr', null, h('td', { class: 'mono' }, s.name, !s.editable && ' ', !s.editable && badge('made elsewhere'), s.git && ' ', s.git && badge('Git', 'info')), h('td', null, badge(s.status, s.status.startsWith('running') ? 'ok' : '')),
          h('td', { class: 'actions' }, s.editable && btn('Edit', () => (s.git ? gitEdit(s.name) : edit(s.name))), s.editable && btn('Webhook', () => webhookDialog('stack', s.name)), btn('Start', () => act('/api/stacks/start', { name: s.name }, 'Done.', load)), btn('Stop', () => act('/api/stacks/stop', { name: s.name }, 'Done.', load)),
            btn('Logs', () => logs(s.name)), trash('Remove', () => removeWith('/api/stacks/remove', { name: s.name }, `Remove ${s.name}?`, 'Its containers are stopped and deleted. Named volumes stay.', load)))));
      }
      clear(templates);
      empty(templates, tpl.length, 2, 'No templates.');
      for (const t of tpl) {
        templates.append(h('tr', null, h('td', { class: 'mono' }, t.name), h('td', { class: 'actions' }, btn('Deploy', () => edit(null, t)), btn('Edit', () => editTemplate(t)), trash('Remove', () => removeWith('/api/stacks/template/remove', { name: t.name }, `Remove ${t.name}?`, '', load)))));
      }
    }

    async function logs(name) {
      try { textDialog(`Logs: ${name}`, (await post('/api/stacks/logs', { name })).data.text); } catch (e) { toast(e.message, 'crit'); }
    }

    async function edit(name, template) {
      let s = { name: '', compose: template?.compose || 'services:\n  app:\n    image: \n    restart: unless-stopped\n', env: '' };
      if (name) {
        try { s = (await post('/api/stacks/get', { name })).data; } catch (e) { toast(e.message, 'crit'); return; }
      }
      const file = h('input', { type: 'file', accept: '.yml,.yaml', onchange: async (e) => { const f = e.target.files[0]; if (f) document.querySelector('dialog[open] [name=compose]').value = await f.text(); } });
      const ok = await formDialog({
        title: name ? `Edit ${name}` : 'New stack', submit: 'Deploy', wide: true,
        fields: [
          { name: 'name', label: 'Name', value: s.name, required: true, placeholder: 'e.g. media', hint: 'Lowercase letters, digits, underscores and hyphens.' },
          { type: 'custom', label: 'Load a file', element: file },
          { name: 'compose', label: 'Compose file', type: 'textarea', rows: 16, value: s.compose },
          { name: 'env', label: 'Environment', type: 'textarea', value: s.env, placeholder: 'TZ=Europe/Berlin', hint: 'One per line, NAME=value. Stored readable only by root.' },
          { name: 'pull', label: 'Download the images again', type: 'checkbox', value: false },
          { name: 'as_template', label: 'Also save as a template', type: 'checkbox', value: false },
        ],
        onSubmit: async (v) => {
          if (v.as_template) await post('/api/stacks/template', { name: v.name, compose: v.compose });
          return post('/api/stacks/deploy', { name: v.name, compose: v.compose, env: v.env, pull: v.pull });
        },
      });
      if (ok) job.watch();
    }

    async function gitEdit(name) {
      try { const s = (await post('/api/stacks/get', { name })).data; gitForm({ name, env: s.env, ...s.git }); } catch (e) { toast(e.message, 'crit'); }
    }

    function gitForm(g = {}) {
      return formDialog({ title: g.name ? `Git stack ${g.name}` : 'Stack from Git', submit: 'Deploy', wide: true, fields: [
        { name: 'name', label: 'Name', value: g.name || '', required: true, hint: 'Lowercase letters, digits, underscores and hyphens.' },
        { name: 'url', label: 'Repository', value: g.url || '', placeholder: 'https://github.com/owner/repo.git', required: true },
        { name: 'ref', label: 'Branch or tag', value: g.ref || '', placeholder: 'Empty: the default branch' },
        { name: 'path', label: 'Compose file in the repository', value: g.path || 'docker-compose.yml', required: true },
        { name: 'user', label: 'User name', value: g.user || '', hint: 'Only for private repositories.' },
        { name: 'token', label: 'Access token', type: 'password', placeholder: g.has_token ? 'Saved, leave empty to keep it' : '' },
        { name: 'interval', label: 'Check for new commits every (minutes)', type: 'number', value: g.interval ?? 0, hint: '0: only on request. On a new commit the stack is deployed again.' },
        { name: 'env', label: 'Environment', type: 'textarea', value: g.env || '', placeholder: 'TZ=Europe/Berlin' },
        { name: 'pull', label: 'Download the images again', type: 'checkbox', value: !!g.name },
      ], onSubmit: (v) => post('/api/stacks/git', { ...v, interval: +v.interval || 0 }) }).then((ok) => { if (ok) job.watch(); });
    }

    function drawCatalog() {
      clear(catalogBody);
      const list = catalog.filter((t) => !search || `${t.title} ${t.description} ${(t.categories || []).join(' ')}`.toLowerCase().includes(search));
      empty(catalogBody, list.length, 3, catalog.length ? 'No matches.' : 'The list could not be loaded.');
      for (const t of list.slice(0, 100)) {
        catalogBody.append(h('tr', null, h('td', null, h('div', { class: 'cell-main' }, t.title), h('div', { class: 'muted small' }, t.description)), h('td', { class: 'muted' }, t.type === 3 ? 'Stack' : 'Container'), h('td', { class: 'actions' }, btn('Deploy', () => deployTemplate(t)))));
      }
    }

    function deployTemplate(t) {
      const env = (t.env || []).map((e) => `${e.name}=${e.default ?? ''}`).join('\n');
      if (t.type === 3 && t.repository) {
        gitForm({ url: t.repository.url, path: t.repository.stackfile, env });
        return;
      }
      const slug = t.title.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
      const mounts = (t.volumes || []).map((v, i) => `${v.bind || `${slug}-${i}`}:${v.container}${v.readonly ? ':ro' : ''}`);
      sessionStorage.setItem('docker-prefill', JSON.stringify({ name: slug, image: t.image, env: env ? env.split('\n') : [], mounts, restart: t.restart || 'unless-stopped' }));
      location.hash = '#/containers';
    }

    function editTemplate(t) {
      return formDialog({ title: t ? `Edit ${t.name}` : 'New template', wide: true, fields: [
        { name: 'name', label: 'Name', value: t?.name || '', required: true },
        { name: 'compose', label: 'Compose file', type: 'textarea', rows: 16, value: t?.compose || '' },
      ], onSubmit: (v) => post('/api/stacks/template', v) }).then((ok) => { if (ok) load(); });
    }

    load();
    get('/api/stacks/catalog').then((c) => { catalog = c; drawCatalog(); }).catch(() => drawCatalog());
    return every(20000, load);
  },
};
