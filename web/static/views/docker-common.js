// SPDX-License-Identifier: AGPL-3.0-or-later
// What the Docker pages share.
import { h, clear, icon, toast, confirmDialog } from '../ui.js';
import { get, post } from '../api.js';

export const lines = (s) => (s || '').split('\n').map((x) => x.trim()).filter(Boolean);
export const text = (a) => (a || []).join('\n');

export const table = (heads, body) => h('div', { class: 'table-wrap' }, h('table', null, h('thead', null, h('tr', null, heads.map((x) => h('th', null, x)))), body));

export function card(ic, title, action, ...rest) {
  return h('section', { class: 'card mt' }, h('div', { class: 'card-head' }, h('span', { class: 'icon-badge' }, icon(ic)), h('h2', null, title), h('div', { class: 'spacer' }), action), ...rest);
}

export const empty = (body, n, cols, msg) => { if (!n) body.append(h('tr', null, h('td', { colspan: cols, class: 'empty' }, msg))); };

export const trash = (label, onclick, disabled) => h('button', { class: 'btn sm icon danger', title: label, disabled, onclick }, icon('trash'));

export async function act(url, body, ok, after) {
  try { await post(url, body); if (ok) toast(ok); } catch (e) { toast(e.message, 'crit'); }
  if (after) after();
}

export async function removeWith(url, body, title, msg, after) {
  if (await confirmDialog({ title, text: msg, confirm: 'Remove', danger: true })) await act(url, body, 'Removed.', after);
}

// Shows a job (download, build, deployment) until it ends.
export function jobWatcher(root, after) {
  const box = h('div');
  let watching = false;
  async function watch() {
    if (watching) return;
    watching = true;
    for (;;) {
      let j;
      try { j = await get('/api/containers/job'); } catch { j = null; }
      if (!root.isConnected) break;
      if (j?.state === 'running') {
        clear(box).append(h('div', { class: 'banner' }, h('span', { class: 'spinner' }), `${j.name}: working …`));
        await new Promise((r) => setTimeout(r, 2000));
        continue;
      }
      clear(box);
      if (j?.state === 'failed') toast(j.error, 'crit');
      else if (j?.state === 'done') toast(`${j.name}: done.`);
      after();
      break;
    }
    watching = false;
  }
  get('/api/containers/job').then((j) => { if (j.state === 'running') watch(); }).catch(() => {});
  return { box, watch };
}

// Creates a webhook and shows its address, which exists only at this moment.
export async function webhookDialog(kind, name) {
  let token;
  try { token = (await post('/api/docker/webhook', { kind, name })).data.token; } catch (e) { toast(e.message, 'crit'); return; }
  textDialog(`Webhook for ${name}`, `POST this address to download the image again and redeploy. It is shown only once:\n\n${location.origin}/api/webhook/${token}\n\nThe router's web interface is only reachable from the home network, so the webhook is, too.`);
}

// A pop-up with plain text from the router.
export function textDialog(title, content) {
  const dlg = h('dialog', { class: 'wide' },
    h('div', { class: 'dialog-body' }, h('h2', null, title), h('pre', { class: 'code log' }, content)),
    h('div', { class: 'dialog-foot' }, h('button', { class: 'btn', onclick: () => dlg.close() }, 'Close')));
  dlg.addEventListener('close', () => dlg.remove());
  document.body.appendChild(dlg);
  dlg.showModal();
}
