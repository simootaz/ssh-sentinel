// Admins (v1.1): list, add with the token shown once, rotate, disable.
// On a backend without the /admins routes the list shows the 404 it got.

import { api } from '../api.js';
import { el, badge, errorLine, infoLine, table, field, asyncButton, confirmAction, tokenPanel, replace } from '../dom.js';
import { fmtTime, fmtRelative } from '../format.js';

export const title = 'Admins';

export function render(container) {
  const name = el('input', { type: 'text', name: 'name', required: true, maxlength: '64', pattern: '[A-Za-z0-9._-]{1,64}', placeholder: 'alice', title: '1 to 64 characters: letters, digits, . _ -' });
  const formStatus = el('div', {});
  const panels = el('div', {});
  const list = el('div', {});
  const form = el('form', { class: 'inline-form' },
    field('Name', name),
    el('button', { type: 'submit', class: 'primary' }, 'Add admin'),
    formStatus,
  );
  replace(container, el('h1', {}, 'Admins'),
    el('p', { class: 'muted' }, 'One token per admin, shown once. Disabling keeps the row so the history stays readable; rotating a disabled admin re-enables it. The bootstrap token of the environment is not listed.'),
    form, panels, list);

  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    formStatus.replaceChildren();
    try {
      const a = await api.addAdmin({ name: name.value.trim() });
      panels.prepend(tokenPanel(`Token of ${a.name}`, a.token, 'Shown once. Give it to that admin for the app or the dashboard; it is not stored anywhere else.'));
      name.value = '';
      await load();
    } catch (err) { formStatus.replaceChildren(errorLine(err)); }
  });

  async function load() {
    list.replaceChildren(infoLine('Loading…'));
    try {
      const res = await api.admins();
      const rows = (res.items || []).map((a) => [
        el('strong', {}, a.name),
        a.disabled_at ? badge(`disabled ${fmtRelative(a.disabled_at)}`, 'disabled') : badge('active', 'active'),
        fmtTime(a.created_at),
        a.last_seen_at ? `${fmtTime(a.last_seen_at)} (${fmtRelative(a.last_seen_at)})` : 'never',
        el('span', { class: 'row-actions' },
          asyncButton(a.disabled_at ? 'Rotate and re-enable' : 'Rotate token', async () => {
            if (!confirmAction(`Rotate the token of ${a.name}? The current one stops working at once.`)) return;
            const r = await api.rotateAdmin(a.id);
            panels.prepend(tokenPanel(`New token of ${r.name}`, r.token, `Rotated ${fmtTime(r.rotated_at)}. The previous token is dead; update every phone that used it.`));
            await load();
          }, { class: 'small' }),
          ' ',
          a.disabled_at ? null : asyncButton('Disable', async () => {
            if (!confirmAction(`Disable ${a.name}? Its token stops working at once.`)) return;
            await api.disableAdmin(a.id);
            await load();
          }, { class: 'danger small' }),
        ),
      ]);
      list.replaceChildren(table(['Name', 'Status', 'Created', 'Last seen', ''], rows, 'No admin account yet. Only the bootstrap token is in use.'));
    } catch (err) { list.replaceChildren(errorLine(err)); }
  }
  load();
}
