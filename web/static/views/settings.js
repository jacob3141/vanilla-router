// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, clear, icon, toast, badge, fmt, confirmDialog } from '../ui.js';
import { get, post } from '../api.js';

export default {
  title: 'Einstellungen',
  subtitle: 'Zugang zur Weboberfläche',
  render(root) {
    const err = h('div', { class: 'form-error hidden' });
    const cur = h('input', { class: 'input', type: 'password', autocomplete: 'current-password', required: true });
    const pw1 = h('input', { class: 'input', type: 'password', autocomplete: 'new-password', required: true, minlength: 12 });
    const pw2 = h('input', { class: 'input', type: 'password', autocomplete: 'new-password', required: true });
    const form = h('form', null, err,
      field('Aktuelles Passwort', cur),
      field('Neues Passwort', pw1, 'Mindestens 12 Zeichen.'),
      field('Neues Passwort wiederholen', pw2),
      h('div', { class: 'row end' }, h('button', { class: 'btn primary', type: 'submit' }, icon('lock'), 'Passwort ändern')));
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      err.classList.add('hidden');
      if (pw1.value !== pw2.value) { err.textContent = 'Die beiden neuen Passwörter sind nicht gleich.'; err.classList.remove('hidden'); return; }
      try {
        toast((await post('/api/settings/password', { current: cur.value, new: pw1.value })).message);
        form.reset();
      } catch (ex) { err.textContent = ex.message; err.classList.remove('hidden'); }
    });

    const importInput = h('input', { type: 'file', accept: '.json,application/json', class: 'hidden', onchange: async (e) => {
      const file = e.target.files[0];
      e.target.value = '';
      if (!file) return;
      let data;
      try { data = JSON.parse(await file.text()); } catch { toast('Die Datei ist kein gültiges JSON.', 'crit'); return; }
      const st = data.state || {};
      if (!(await confirmDialog({ title: 'Konfiguration einspielen?', text: `Export vom ${data.exported ? fmt.date(data.exported) : '?'}: ${st.reservations?.length || 0} feste Adressen, ${st.records?.length || 0} DNS-Namen, ${st.port_forwards?.length || 0} Weiterleitungen, ${st.blocks?.length || 0} Sperren. Die aktuellen Einträge werden dadurch ersetzt.`, confirm: 'Einspielen', danger: true }))) return;
      try { toast((await post('/api/config/import', data)).message, 'warn'); } catch (ex) { toast(ex.message, 'crit'); }
    } });
    const sessions = h('div', { class: 'table-wrap flat' });
    async function loadSessions() {
      let d;
      try { d = await get('/api/sessions'); } catch { return; }
      clear(sessions).append(h('table', null, h('tbody', null, d.sessions.sort((a, b) => b.last_seen.localeCompare(a.last_seen)).map((x) => h('tr', null,
        h('td', { class: 'mono small' }, x.ip), h('td', { class: 'muted small' }, `seit ${fmt.date(x.created)}, aktiv ${fmt.date(x.last_seen)}`),
        h('td', { class: 'actions' }, x.current ? badge('diese', 'ok') : h('button', { class: 'btn sm', onclick: async () => { try { toast((await post('/api/sessions/delete', { id: x.id })).message); } catch (e) { toast(e.message, 'crit'); } loadSessions(); } }, 'Abmelden')))))));
    }
    loadSessions();

    root.append(h('div', { class: 'grid cols-2' },
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('lock')), h('h2', null, 'Passwort')),
        form),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('logout')), h('h2', null, 'Sitzungen')),
        h('p', { class: 'muted small' }, 'Sitzungen enden nach 30 Minuten ohne Aktivität, spätestens nach 12 Stunden. Ein Neustart des Dienstes meldet alle ab.'),
        sessions,
        h('button', { class: 'btn mt', onclick: async () => { try { toast((await post('/api/settings/logout-all')).message); loadSessions(); } catch (e) { toast(e.message, 'crit'); } } }, 'Alle anderen abmelden')),
      h('section', { class: 'card span-2' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('download')), h('h2', null, 'Konfiguration sichern')),
        h('p', { class: 'muted small' }, 'Feste Adressen, DNS-Namen, Portweiterleitungen, Sperren und Gerätenamen als JSON-Datei. Ohne Passwörter und ohne Systemdateien. Beim Einspielen wird alles geprüft wie jede andere Änderung, Firewall-Änderungen musst du bestätigen.'),
        h('div', { class: 'row' },
          h('a', { class: 'btn', href: '/api/config/export', download: '' }, icon('download'), 'Exportieren'),
          h('button', { class: 'btn', onclick: () => importInput.click() }, icon('arrowRight', 'rot-up'), 'Importieren …'),
          importInput)),
      h('section', { class: 'card span-2' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('info')), h('h2', null, 'Passwort vergessen?')),
        h('p', { class: 'muted small' }, 'Per SSH auf dem Router ein neues Passwort setzen. Der Befehl liest es ohne Anzeige von der Tastatur:'),
        h('pre', { class: 'code' }, "read -rs PW && echo \"$PW\" | sudo -u vr-webui vr-webui set-password && unset PW && sudo systemctl restart vr-webui"))));
  },
};

function field(label, input, hint) {
  return h('div', { class: 'field' }, h('label', null, label), input, hint && h('div', { class: 'hint' }, hint));
}
