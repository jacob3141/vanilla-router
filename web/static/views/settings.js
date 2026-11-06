// SPDX-License-Identifier: AGPL-3.0-or-later
import { t } from '../i18n.js';
import { h, clear, icon, toast, badge, fmt, confirmDialog } from '../ui.js';
import { get, post } from '../api.js';

export default {
  title: 'Settings',
  subtitle: 'Access to the web interface',
  render(root) {
    const err = h('div', { class: 'form-error hidden' });
    const cur = h('input', { class: 'input', type: 'password', autocomplete: 'current-password', required: true });
    const pw1 = h('input', { class: 'input', type: 'password', autocomplete: 'new-password', required: true, minlength: 12 });
    const pw2 = h('input', { class: 'input', type: 'password', autocomplete: 'new-password', required: true });
    const form = h('form', null, err,
      field('Current password', cur),
      field('New password', pw1, 'At least 12 characters.'),
      field('Repeat new password', pw2),
      h('div', { class: 'row end' }, h('button', { class: 'btn primary', type: 'submit' }, icon('lock'), 'Change password')));
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      err.classList.add('hidden');
      if (pw1.value !== pw2.value) { err.textContent = t('The two new passwords are not the same.'); err.classList.remove('hidden'); return; }
      try {
        toast((await post('/api/settings/password', { current: cur.value, new: pw1.value })).message);
        form.reset();
      } catch (ex) { err.textContent = t(ex.message); err.classList.remove('hidden'); }
    });

    const importInput = h('input', { type: 'file', accept: '.json,application/json', class: 'hidden', onchange: async (e) => {
      const file = e.target.files[0];
      e.target.value = '';
      if (!file) return;
      let data;
      try { data = JSON.parse(await file.text()); } catch { toast('The file is not valid JSON.', 'crit'); return; }
      const st = data.state || {};
      if (!(await confirmDialog({ title: 'Apply configuration?', text: `Export from ${data.exported ? fmt.date(data.exported) : '?'}: ${st.reservations?.length || 0} fixed addresses, ${st.records?.length || 0} DNS names, ${st.port_forwards?.length || 0} port forwards, ${st.blocks?.length || 0} blocks. The current entries will be replaced.`, confirm: 'Apply', danger: true }))) return;
      try { toast((await post('/api/config/import', data)).message, 'warn'); } catch (ex) { toast(ex.message, 'crit'); }
    } });
    const sessions = h('div', { class: 'table-wrap flat' });
    async function loadSessions() {
      let d;
      try { d = await get('/api/sessions'); } catch { return; }
      clear(sessions).append(h('table', null, h('tbody', null, d.sessions.sort((a, b) => b.last_seen.localeCompare(a.last_seen)).map((x) => h('tr', null,
        h('td', { class: 'mono small' }, x.ip), h('td', { class: 'muted small' }, `since ${fmt.date(x.created)}, active ${fmt.date(x.last_seen)}`),
        h('td', { class: 'actions' }, x.current ? badge('this one', 'ok') : h('button', { class: 'btn sm', onclick: async () => { try { toast((await post('/api/sessions/delete', { id: x.id })).message); } catch (e) { toast(e.message, 'crit'); } loadSessions(); } }, 'Sign out')))))));
    }
    loadSessions();

    root.append(h('div', { class: 'grid cols-2' },
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('lock')), h('h2', null, 'Password')),
        form),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('logout')), h('h2', null, 'Sessions')),
        h('p', { class: 'muted small' }, 'Sessions end after 30 minutes without activity, at the latest after 12 hours. A restart of the service signs everyone out.'),
        sessions,
        h('button', { class: 'btn mt', onclick: async () => { try { toast((await post('/api/settings/logout-all')).message); loadSessions(); } catch (e) { toast(e.message, 'crit'); } } }, 'Sign out all others')),
      h('section', { class: 'card span-2' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('download')), h('h2', null, 'Back up configuration')),
        h('p', { class: 'muted small' }, 'Fixed addresses, DNS names, port forwards, blocks and device names as a JSON file. Without passwords and without system files. When applied, everything is checked like any other change, you must confirm firewall changes.'),
        h('div', { class: 'row' },
          h('a', { class: 'btn', href: '/api/config/export', download: '' }, icon('download'), 'Export'),
          h('button', { class: 'btn', onclick: () => importInput.click() }, icon('arrowRight', 'rot-up'), 'Import …'),
          importInput)),
      h('section', { class: 'card span-2' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('info')), h('h2', null, 'Forgot your password?')),
        h('p', { class: 'muted small' }, 'Set a new password on the router through SSH. The command reads it from the keyboard without displaying it:'),
        h('pre', { class: 'code' }, "read -rs PW && echo \"$PW\" | sudo -u vr-webui vr-webui set-password && unset PW && sudo systemctl restart vr-webui"))));
  },
};

function field(label, input, hint) {
  return h('div', { class: 'field' }, h('label', null, label), input, hint && h('div', { class: 'hint' }, hint));
}
