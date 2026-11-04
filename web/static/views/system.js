// SPDX-License-Identifier: AGPL-3.0-or-later
import { h, clear, icon, badge, toast, confirmDialog, fmt } from '../ui.js';
import { get, post } from '../api.js';

const LEVEL = { CRIT: ['kritisch', 'crit', 'alert'], WARN: ['Warnung', 'warn', 'alert'], INFO: ['Info', 'info', 'info'], OK: ['OK', 'ok', 'check'] };
const ORDER = { CRIT: 0, WARN: 1, INFO: 2, OK: 3 };

export default {
  title: 'System',
  subtitle: 'Updates, Neustart und Sicherheits-Audit',
  render(root) {
    const updBody = h('div');
    const updLog = h('pre', { class: 'code log hidden' });
    const auditBody = h('div');
    let poll = null;

    const hwBody = h('dl', { class: 'kv' });
    const diskBody = h('div');
    const histBody = h('div');

    root.append(h('div', { class: 'grid cols-2' },
      h('section', { class: 'card span-2' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('refresh')), h('h2', null, 'Updates')),
        updBody, updLog),
      h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('cpu')), h('h2', null, 'Hardware und Zeit')), hwBody),
      h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('drive')), h('h2', null, 'Datenträger')), diskBody),
      h('section', { class: 'card span-2' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('clock')), h('h2', null, 'Update-Verlauf')), histBody),
      h('section', { class: 'card span-2' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('shield')), h('h2', null, 'Sicherheits-Audit'),
          h('button', { class: 'btn', onclick: runAudit }, icon('play'), 'Jetzt prüfen')),
        auditBody),
      h('section', { class: 'card danger-zone' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('power')), h('h2', null, 'Neustart und Herunterfahren')),
        h('p', { class: 'muted small' }, 'Während des Neustarts (etwa eine Minute) ist das ganze Heimnetz ohne Internet. Nach dem Herunterfahren muss jemand den Router von Hand wieder einschalten.'),
        h('div', { class: 'row' },
          h('button', { class: 'btn', onclick: () => power('reboot') }, icon('refresh'), 'Neu starten'),
          h('button', { class: 'btn danger', onclick: () => power('poweroff') }, icon('power'), 'Herunterfahren')))));

    async function loadUpdates() {
      let d;
      try { d = await get('/api/system/updates'); } catch (e) { clear(updBody).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      const sec = d.updates?.filter((u) => u.security).length || 0;
      const n = d.updates?.length || 0;
      // Element.append() macht aus false den Text "false", deshalb über h() einhängen
      updBody.replaceChildren(h('div', null,
        h('div', { class: 'row' },
          n ? badge(`${n} Updates verfügbar`, sec ? 'warn' : 'info') : badge('System ist aktuell', 'ok'),
          sec > 0 && badge(`${sec} Sicherheitsupdates`, 'crit'),
          d.reboot_required && badge('Neustart nötig', 'warn'),
          h('span', { class: 'muted small' }, d.lists_updated ? `Paketlisten vom ${fmt.date(d.lists_updated)}` : ''),
          h('div', { class: 'spacer' }),
          h('button', { class: 'btn primary', disabled: d.job === 'active' || d.job === 'activating', onclick: startUpdate }, icon('refresh'), d.job === 'active' ? 'Läuft …' : 'Jetzt aktualisieren')),
        d.reboot_required && d.reboot_packages?.length > 0 && h('p', { class: 'muted small' }, `Neustart nötig wegen: ${d.reboot_packages.join(', ')}`),
        n > 0 && h('div', { class: 'table-wrap', style: { marginTop: '12px' } }, h('table', null,
          h('thead', null, h('tr', null, ['Paket', 'Installiert', 'Neu', ''].map((t) => h('th', null, t)))),
          h('tbody', null, d.updates.map((u) => h('tr', null,
            h('td', { class: 'mono' }, u.package), h('td', { class: 'mono muted' }, u.from || 'neu'), h('td', { class: 'mono' }, u.to),
            h('td', null, u.security && badge('Sicherheit', 'crit')))))))));
      if (d.job === 'active' || d.job === 'activating') watchUpdate();
    }

    async function startUpdate() {
      if (!(await confirmDialog({ title: 'Updates installieren?', text: 'Paketlisten werden aktualisiert und alle verfügbaren Updates installiert. Bei Konflikten bleiben deine Konfigurationsdateien erhalten. Einzelne Dienste starten dabei neu.', confirm: 'Installieren' }))) return;
      try { toast((await post('/api/system/updates')).message); } catch (e) { toast(e.message, 'crit'); return; }
      watchUpdate();
    }

    function watchUpdate() {
      updLog.classList.remove('hidden');
      clearInterval(poll);
      const tick = async () => {
        try {
          const s = await get('/api/system/updates/status');
          updLog.textContent = s.log || 'Startet …';
          updLog.scrollTop = updLog.scrollHeight;
          if (!/ActiveState=(active|activating)/.test(s.state)) {
            clearInterval(poll);
            toast(/Result=success/.test(s.state) ? 'Updates installiert.' : 'Aktualisierung beendet, siehe Protokoll.', /Result=success/.test(s.state) ? 'ok' : 'warn');
            loadUpdates();
          }
        } catch { /* nächster Versuch */ }
      };
      tick();
      poll = setInterval(tick, 2000);
    }

    function drawAudit(r) {
      clear(auditBody);
      if (!r) { auditBody.append(h('p', { class: 'muted' }, 'Noch kein Audit auf dem Router gelaufen.')); return; }
      const count = (lv) => r.checks.filter((c) => c.level === lv).length;
      auditBody.append(
        h('div', { class: 'row', style: { marginBottom: '12px' } },
          ['CRIT', 'WARN', 'INFO', 'OK'].map((lv) => badge(`${count(lv)} ${LEVEL[lv][0]}`, LEVEL[lv][1])),
          h('span', { class: 'muted small' }, `geprüft am ${fmt.date(r.time)}`)),
        h('div', { class: 'audit-list' }, [...r.checks].sort((a, b) => ORDER[a.level] - ORDER[b.level]).filter((c) => c.level !== 'OK').map((c) =>
          h('div', { class: 'audit-item' }, badge(LEVEL[c.level][0], LEVEL[c.level][1]), h('div', null, h('div', null, c.text), h('div', { class: 'id' }, c.id))))),
        h('p', { class: 'muted small' }, `${count('OK')} Prüfungen ohne Befund.`));
    }

    async function loadAudit() {
      try { drawAudit(await get('/api/system/audit')); } catch (e) { clear(auditBody).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); }
    }

    async function runAudit(e) {
      const btn = e.currentTarget;
      btn.disabled = true;
      clear(auditBody).append(h('p', { class: 'muted' }, h('span', { class: 'spinner' }), ' Das Audit läuft, das dauert etwa eine halbe Minute …'));
      try { drawAudit((await post('/api/system/audit')).data); } catch (ex) { toast(ex.message, 'crit'); loadAudit(); }
      btn.disabled = false;
    }

    async function power(action) {
      const reboot = action === 'reboot';
      if (!(await confirmDialog({ title: reboot ? 'Router neu starten?' : 'Router herunterfahren?', text: reboot ? 'Das Heimnetz ist etwa eine Minute ohne Internet. Die Seite verbindet sich danach neu.' : 'Der Router geht aus und muss von Hand wieder eingeschaltet werden. Bis dahin gibt es kein Internet.', confirm: reboot ? 'Neu starten' : 'Herunterfahren', danger: true }))) return;
      try { toast((await post('/api/system/power', { action })).message, 'warn'); } catch (e) { toast(e.message, 'crit'); }
    }

    async function loadHardware() {
      try {
        const d = await get('/api/system/hardware');
        const synced = d.time.NTPSynchronized === 'yes';
        const kv = [
          ['Gerät', `${d.manufacturer} ${d.product}`.trim() || '–'],
          ['Prozessor', `${d.cpu} (${d.cores} Kerne)`],
          ['BIOS', d.bios || '–'],
          ['Uhrzeit', `${fmt.date(d.now)} (Zeitzone ${d.time.Timezone || '?'})`],
          ['Zeitabgleich', [synced ? badge('synchron', 'ok') : badge('nicht synchron', 'warn'), d.timesync.ServerName && h('span', { class: 'muted small' }, ` über ${d.timesync.ServerName}`)]],
        ];
        clear(hwBody).append(...kv.flatMap(([k, v]) => [h('dt', null, k), h('dd', null, v)]));
        if (d.time.Timezone === 'Etc/UTC' || d.time.Timezone === 'UTC') {
          hwBody.append(h('dt', null, 'Hinweis'), h('dd', { class: 'muted small' }, 'Der Router läuft auf UTC. Zeitsperren rechnet die Oberfläche trotzdem in deutscher Zeit.'));
        }
      } catch (e) { clear(hwBody).append(h('dd', null, e.message)); }
    }

    async function loadDisks() {
      try {
        const d = await get('/api/system/disks');
        clear(diskBody);
        for (const m of d.mounts) {
          const used = m.total - m.free;
          const pct = m.total ? (100 * used) / m.total : 0;
          diskBody.append(h('div', { class: 'mount' },
            h('div', { class: 'row' }, h('span', { class: 'mono' }, m.path), h('span', { class: 'muted small' }, `${m.device} · ${m.fs}`), h('div', { class: 'spacer' }), h('span', { class: 'small' }, `${fmt.bytes(m.free)} frei von ${fmt.bytes(m.total)}`)),
            h('div', { class: `meter ${pct > 90 ? 'crit' : pct > 80 ? 'warn' : ''}` }, h('span', { style: { width: `${pct}%` } }))));
        }
        for (const n of d.nvme || []) {
          const ok = n.critical_warning === 0 && n.media_errors === 0 && n.available_spare > n.spare_threshold;
          const stat = (l, v) => h('div', { class: 'stat' }, h('div', { class: 'stat-label' }, l), h('div', { class: 'stat-value small-value' }, v));
          diskBody.append(h('div', { class: 'mt' },
            h('div', { class: 'row' }, h('strong', null, n.model || n.device), h('span', { class: 'muted small' }, `Firmware ${n.firmware}`), h('div', { class: 'spacer' }), ok ? badge('gesund', 'ok') : badge('Warnung', 'crit')),
            h('div', { class: 'health-grid' },
              stat('Abnutzung', `${n.percent_used} %`),
              stat('Reserve', `${n.available_spare} %`),
              stat('Temperatur', `${n.temperature_c} °C`),
              stat('Betriebsstunden', fmt.num(n.power_on_hours, 0)),
              stat('Geschrieben', `${fmt.num(n.data_written_tb, 1)} TB`),
              stat('Medienfehler', String(n.media_errors)),
              stat('Unsauber aus', String(n.unsafe_shutdowns)))));
        }
        for (const e of d.errors || []) diskBody.append(h('p', { class: 'muted small' }, `SMART nicht lesbar: ${e}`));
      } catch (e) { clear(diskBody).append(h('p', { class: 'muted' }, e.message)); }
    }

    async function loadHistory() {
      try {
        const d = await get('/api/system/update-history');
        const auto = d.settings['APT::Periodic::Unattended-Upgrade'] === '1';
        const reboot = d.settings['Unattended-Upgrade::Automatic-Reboot'] === 'true';
        const last = /LastTriggerUSec=@(\d+)/.exec(d.timer)?.[1];
        const next = /NextElapseUSecRealtime=@(\d+)/.exec(d.timer)?.[1];
        clear(histBody).append(
          h('div', { class: 'row' },
            auto ? badge('Sicherheitsupdates automatisch', 'ok') : badge('keine automatischen Updates', 'warn'),
            badge(reboot ? `Neustart automatisch ${d.settings['Unattended-Upgrade::Automatic-Reboot-Time'] || ''}` : 'kein automatischer Neustart', reboot ? 'info' : ''),
            last && h('span', { class: 'muted small' }, `zuletzt ${fmt.date(new Date(+last * 1000).toISOString())}`),
            next && h('span', { class: 'muted small' }, `nächster Lauf ${fmt.date(new Date(+next * 1000).toISOString())}`)),
          h('div', { class: 'table-wrap mt' }, h('table', null,
            h('thead', null, h('tr', null, ['Zeit', 'Auslöser', 'Änderungen'].map((t) => h('th', null, t)))),
            h('tbody', null, (d.runs || []).slice(0, 15).map((r) => h('tr', null,
              h('td', { class: 'muted small nowrap' }, r.start),
              h('td', { class: 'small' }, /unattended/.test(r.command) ? badge('automatisch', 'info') : h('span', { class: 'mono small' }, (r.command || '').replace(/^apt(-get)? /, '').slice(0, 60))),
              h('td', { class: 'small' },
                r.upgraded?.length > 0 && h('div', null, `${r.upgraded.length} aktualisiert: `, h('span', { class: 'muted' }, r.upgraded.slice(0, 8).join(', '), r.upgraded.length > 8 ? ' …' : '')),
                r.installed?.length > 0 && h('div', null, `${r.installed.length} neu: `, h('span', { class: 'muted' }, r.installed.slice(0, 6).join(', '))),
                r.removed?.length > 0 && h('div', null, `${r.removed.length} entfernt`),
                r.error && h('div', { class: 'text-crit' }, r.error))))))));
      } catch (e) { clear(histBody).append(h('p', { class: 'muted' }, e.message)); }
    }

    loadUpdates();
    loadAudit();
    loadHardware();
    loadDisks();
    loadHistory();
    return () => clearInterval(poll);
  },
};
