// Whitelist: every unexpired entry, add one with an optional TTL, remove one.

import { api } from '../api.js';
import { el, badge, dash, errorLine, infoLine, table, field, ttlPicker, asyncButton, confirmAction, replace } from '../dom.js';
import { fmtTime, fmtExpiry, ttlSeconds } from '../format.js';

export const title = 'Whitelist';

export function render(container) {
  const username = el('input', { type: 'text', name: 'username', required: true, placeholder: 'deploy' });
  const context = el('select', { name: 'context' }, el('option', { value: 'ssh' }, 'ssh'), el('option', { value: 'sudo' }, 'sudo'));
  const server = el('input', { type: 'text', name: 'server', placeholder: 'every server' });
  const ttl = ttlPicker('wl');
  const formStatus = el('div', {});
  const list = el('div', {});
  const form = el('form', { class: 'inline-form' },
    field('User', username), field('Context', context), field('Server name', server), field('Expires', ttl.node),
    el('button', { type: 'submit', class: 'primary' }, 'Add'),
    formStatus,
  );
  replace(container, el('h1', {}, 'Whitelist'),
    el('p', { class: 'muted' }, 'Users allowed without a push, per context. An entry for one server or for every server, permanent or with an expiry.'),
    form, list);

  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    formStatus.replaceChildren();
    let body;
    try {
      const t = ttl.read();
      body = {
        username: username.value.trim(),
        context: context.value,
        server: server.value.trim() || null,
        ttl_seconds: ttlSeconds(t.choice, t.amount, t.unit),
      };
    } catch (err) { formStatus.replaceChildren(errorLine(err)); return; }
    try {
      const entry = await api.addWhitelist(body);
      formStatus.replaceChildren(infoLine(`Entry for ${entry.username} (${entry.context}) on ${entry.server || 'every server'}, expires: ${fmtExpiry(entry.expires_at)}`));
      username.value = '';
      await load();
    } catch (err) { formStatus.replaceChildren(errorLine(err)); }
  });

  async function load() {
    list.replaceChildren(infoLine('Loading…'));
    try {
      const res = await api.whitelist();
      const rows = (res.items || []).map((e) => [
        e.username,
        badge(e.context, e.context),
        e.server || el('em', {}, 'every server'),
        fmtExpiry(e.expires_at),
        fmtTime(e.created_at),
        dash([e.created_by_admin, e.created_by_device].filter(Boolean).join(' / ')),
        e.created_from_request ? el('code', { class: 'small' }, e.created_from_request.slice(0, 8)) : '—',
        asyncButton('Remove', async () => {
          if (!confirmAction(`Remove the whitelist entry of ${e.username} (${e.context}) on ${e.server || 'every server'}?`)) return;
          await api.deleteWhitelist(e.id);
          await load();
        }, { class: 'danger small' }),
      ]);
      list.replaceChildren(table(['User', 'Context', 'Server', 'Expires', 'Added', 'By', 'From request', ''], rows, 'The whitelist is empty.'));
    } catch (err) { list.replaceChildren(errorLine(err)); }
  }
  load();
}
