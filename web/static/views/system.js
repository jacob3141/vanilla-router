// SPDX-License-Identifier: AGPL-3.0-or-later
import { t } from '../i18n.js';
import { h, clear, icon, badge, toast, confirmDialog, fmt } from '../ui.js';
import { get, post } from '../api.js';

const LEVEL = { CRIT: ['critical', 'crit', 'alert'], WARN: ['Warning', 'warn', 'alert'], INFO: ['Info', 'info', 'info'], OK: ['OK', 'ok', 'check'] };
const ORDER = { CRIT: 0, WARN: 1, INFO: 2, OK: 3 };

export default {
  title: 'System',
  subtitle: 'Updates, hardware and restart',
  render(root) {
    const updBody = h('div');
    const updLog = h('pre', { class: 'code log hidden' });
    const auditBody = h('div');
    let auditCard = null; // Extension, only visible when the check script is installed
    let poll = null;

    const hwBody = h('dl', { class: 'kv' });
    const diskBody = h('div');
    const histBody = h('div');

    root.append(h('div', { class: 'grid cols-2' },
      h('section', { class: 'card span-2' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('refresh')), h('h2', null, 'Updates')),
        updBody, updLog),
      h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('cpu')), h('h2', null, 'Hardware and time')), hwBody),
      h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('drive')), h('h2', null, 'Disks')), diskBody),
      h('section', { class: 'card span-2' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('clock')), h('h2', null, 'Update history')), histBody),
      auditCard = h('section', { class: 'card span-2 hidden' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('shield')), h('h2', null, 'Security audit'),
          h('button', { class: 'btn', onclick: runAudit }, icon('play'), 'Check now')),
        auditBody),
      h('section', { class: 'card danger-zone' },
        h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon('power')), h('h2', null, 'Restart and shutdown')),
        h('p', { class: 'muted small' }, 'During the restart (about a minute) the whole home network has no internet. After a shutdown someone must turn the router back on by hand.'),
        h('div', { class: 'row' },
          h('button', { class: 'btn', onclick: () => power('reboot') }, icon('refresh'), 'Restart'),
          h('button', { class: 'btn danger', onclick: () => power('poweroff') }, icon('power'), 'Shut down')))));

    async function loadUpdates() {
      let d;
      try { d = await get('/api/system/updates'); } catch (e) { clear(updBody).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); return; }
      const sec = d.updates?.filter((u) => u.security).length || 0;
      const n = d.updates?.length || 0;
      // Element.append() turns false into the text "false", so mount through h()
      updBody.replaceChildren(h('div', null,
        h('div', { class: 'row' },
          n ? badge(`${n} updates available`, sec ? 'warn' : 'info') : badge('System is up to date', 'ok'),
          sec > 0 && badge(`${sec} security updates`, 'crit'),
          d.reboot_required && badge('Restart required', 'warn'),
          h('span', { class: 'muted small' }, d.lists_updated ? `Package lists from ${fmt.date(d.lists_updated)}` : ''),
          h('div', { class: 'spacer' }),
          h('button', { class: 'btn primary', disabled: d.job === 'active' || d.job === 'activating', onclick: startUpdate }, icon('refresh'), d.job === 'active' ? 'Running …' : 'Update now')),
        d.reboot_required && d.reboot_packages?.length > 0 && h('p', { class: 'muted small' }, `Restart required because of: ${d.reboot_packages.join(', ')}`),
        n > 0 && h('div', { class: 'table-wrap', style: { marginTop: '12px' } }, h('table', null,
          h('thead', null, h('tr', null, ['Package', 'Installed', 'New', ''].map((t) => h('th', null, t)))),
          h('tbody', null, d.updates.map((u) => h('tr', null,
            h('td', { class: 'mono' }, u.package), h('td', { class: 'mono muted' }, u.from || 'new'), h('td', { class: 'mono' }, u.to),
            h('td', null, u.security && badge('Security', 'crit')))))))));
      if (d.job === 'active' || d.job === 'activating') watchUpdate();
    }

    async function startUpdate() {
      if (!(await confirmDialog({ title: 'Install updates?', text: 'Package lists are updated and all available updates installed. On conflicts your configuration files stay. Some services restart during this.', confirm: 'Install' }))) return;
      try { toast((await post('/api/system/updates')).message); } catch (e) { toast(e.message, 'crit'); return; }
      watchUpdate();
    }

    function watchUpdate() {
      updLog.classList.remove('hidden');
      clearInterval(poll);
      const tick = async () => {
        try {
          const s = await get('/api/system/updates/status');
          updLog.textContent = s.log || t('Starting …');
          updLog.scrollTop = updLog.scrollHeight;
          if (!/ActiveState=(active|activating)/.test(s.state)) {
            clearInterval(poll);
            toast(/Result=success/.test(s.state) ? 'Updates installed.' : 'Update finished, see log.', /Result=success/.test(s.state) ? 'ok' : 'warn');
            loadUpdates();
          }
        } catch { /* next attempt */ }
      };
      tick();
      poll = setInterval(tick, 2000);
    }

    function drawAudit(r) {
      clear(auditBody);
      if (!r) { auditBody.append(h('p', { class: 'muted' }, 'No audit has run on the router yet.')); return; }
      const count = (lv) => r.checks.filter((c) => c.level === lv).length;
      auditBody.append(
        h('div', { class: 'row', style: { marginBottom: '12px' } },
          ['CRIT', 'WARN', 'INFO', 'OK'].map((lv) => badge(`${count(lv)} ${LEVEL[lv][0]}`, LEVEL[lv][1])),
          h('span', { class: 'muted small' }, `checked on ${fmt.date(r.time)}`)),
        h('div', { class: 'audit-list' }, [...r.checks].sort((a, b) => ORDER[a.level] - ORDER[b.level]).filter((c) => c.level !== 'OK').map((c) =>
          h('div', { class: 'audit-item' }, badge(LEVEL[c.level][0], LEVEL[c.level][1]), h('div', null, h('div', null, c.text), h('div', { class: 'id' }, c.id))))),
        h('p', { class: 'muted small' }, `${count('OK')} checks without findings.`));
    }

    async function loadAudit() {
      try { drawAudit(await get('/api/system/audit')); } catch (e) { clear(auditBody).append(h('div', { class: 'banner crit' }, icon('alert'), e.message)); }
    }

    async function runAudit(e) {
      const btn = e.currentTarget;
      btn.disabled = true;
      clear(auditBody).append(h('p', { class: 'muted' }, h('span', { class: 'spinner' }), ' The audit is running, that takes about half a minute …'));
      try { drawAudit((await post('/api/system/audit')).data); } catch (ex) { toast(ex.message, 'crit'); loadAudit(); }
      btn.disabled = false;
    }

    async function power(action) {
      const reboot = action === 'reboot';
      if (!(await confirmDialog({ title: reboot ? 'Restart the router?' : 'Shut down the router?', text: reboot ? 'The home network has no internet for about a minute. The page reconnects afterwards.' : 'The router turns off and must be turned back on by hand. Until then there is no internet.', confirm: reboot ? 'Restart' : 'Shut down', danger: true }))) return;
      try { toast((await post('/api/system/power', { action })).message, 'warn'); } catch (e) { toast(e.message, 'crit'); }
    }

    async function loadHardware() {
      try {
        const d = await get('/api/system/hardware');
        const synced = d.time.NTPSynchronized === 'yes';
        const kv = [
          ['Device', `${d.manufacturer} ${d.product}`.trim() || '–'],
          ['Processor', `${d.cpu} (${d.cores} cores)`],
          ['BIOS', d.bios || '–'],
          ['Time', `${fmt.date(d.now)} (time zone ${d.time.Timezone || '?'})`],
          ['Time synchronization', [synced ? badge('synchronized', 'ok') : badge('not synchronized', 'warn'), d.timesync.ServerName && h('span', { class: 'muted small' }, ` via ${d.timesync.ServerName}`)]],
        ];
        clear(hwBody).append(...kv.flatMap(([k, v]) => [h('dt', null, k), h('dd', null, v)]));
        if (d.time.Timezone === 'Etc/UTC' || d.time.Timezone === 'UTC') {
          hwBody.append(h('dt', null, 'Note'), h('dd', { class: 'muted small' }, 'The router runs on UTC. The web interface still calculates time-based blocks in the configured time zone.'));
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
            h('div', { class: 'row' }, h('span', { class: 'mono' }, m.path), h('span', { class: 'muted small' }, `${m.device} · ${m.fs}`), h('div', { class: 'spacer' }), h('span', { class: 'small' }, `${fmt.bytes(m.free)} free of ${fmt.bytes(m.total)}`)),
            h('div', { class: `meter ${pct > 90 ? 'crit' : pct > 80 ? 'warn' : ''}` }, h('span', { style: { width: `${pct}%` } }))));
        }
        for (const n of d.nvme || []) {
          const ok = n.critical_warning === 0 && n.media_errors === 0 && n.available_spare > n.spare_threshold;
          const stat = (l, v) => h('div', { class: 'stat' }, h('div', { class: 'stat-label' }, l), h('div', { class: 'stat-value small-value' }, v));
          diskBody.append(h('div', { class: 'mt' },
            h('div', { class: 'row' }, h('strong', null, n.model || n.device), h('span', { class: 'muted small' }, `Firmware ${n.firmware}`), h('div', { class: 'spacer' }), ok ? badge('healthy', 'ok') : badge('Warning', 'crit')),
            h('div', { class: 'health-grid' },
              stat('Wear', `${n.percent_used} %`),
              stat('Reserve', `${n.available_spare} %`),
              stat('Temperature', `${n.temperature_c} °C`),
              stat('Power-on hours', fmt.num(n.power_on_hours, 0)),
              stat('Written', `${fmt.num(n.data_written_tb, 1)} TB`),
              stat('Media errors', String(n.media_errors)),
              stat('Unclean shutdowns', String(n.unsafe_shutdowns)))));
        }
        for (const e of d.errors || []) diskBody.append(h('p', { class: 'muted small' }, `SMART not readable: ${e}`));
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
            auto ? badge('Automatic security updates', 'ok') : badge('no automatic updates', 'warn'),
            badge(reboot ? `Automatic restart ${d.settings['Unattended-Upgrade::Automatic-Reboot-Time'] || ''}` : 'no automatic restart', reboot ? 'info' : ''),
            last && h('span', { class: 'muted small' }, `last ${fmt.date(new Date(+last * 1000).toISOString())}`),
            next && h('span', { class: 'muted small' }, `next run ${fmt.date(new Date(+next * 1000).toISOString())}`)),
          h('div', { class: 'table-wrap mt' }, h('table', null,
            h('thead', null, h('tr', null, ['Time', 'Trigger', 'Changes'].map((t) => h('th', null, t)))),
            h('tbody', null, (d.runs || []).slice(0, 15).map((r) => h('tr', null,
              h('td', { class: 'muted small nowrap' }, r.start),
              h('td', { class: 'small' }, /unattended/.test(r.command) ? badge('automatic', 'info') : h('span', { class: 'mono small' }, (r.command || '').replace(/^apt(-get)? /, '').slice(0, 60))),
              h('td', { class: 'small' },
                r.upgraded?.length > 0 && h('div', null, `${r.upgraded.length} updated: `, h('span', { class: 'muted' }, r.upgraded.slice(0, 8).join(', '), r.upgraded.length > 8 ? ' …' : '')),
                r.installed?.length > 0 && h('div', null, `${r.installed.length} new: `, h('span', { class: 'muted' }, r.installed.slice(0, 6).join(', '))),
                r.removed?.length > 0 && h('div', null, `${r.removed.length} removed`),
                r.error && h('div', { class: 'text-crit' }, r.error))))))));
      } catch (e) { clear(histBody).append(h('p', { class: 'muted' }, e.message)); }
    }

    loadUpdates();
    get('/api/state').then((s) => {
      if (s.features && s.features.audit) { auditCard.classList.remove('hidden'); loadAudit(); }
    }).catch(() => {});
    loadHardware();
    loadDisks();
    loadHistory();
    return () => clearInterval(poll);
  },
};
