// Geo rules: the country blocklist, add and remove.

import { api } from '../api.js';
import { el, dash, errorLine, infoLine, table, field, asyncButton, confirmAction, replace } from '../dom.js';
import { fmtTime } from '../format.js';

export const title = 'Geo rules';

export function render(container) {
  const country = el('input', { type: 'text', name: 'country', required: true, maxlength: '2', minlength: '2', placeholder: 'KP', pattern: '[A-Za-z]{2}', 'aria-label': 'country code' });
  const note = el('input', { type: 'text', name: 'note', placeholder: 'why' });
  const formStatus = el('div', {});
  const list = el('div', {});
  const form = el('form', { class: 'inline-form' },
    field('Country (ISO 3166-1 alpha-2)', country), field('Note', note),
    el('button', { type: 'submit', class: 'primary' }, 'Block country'),
    formStatus,
  );
  replace(container, el('h1', {}, 'Geo rules'),
    el('p', { class: 'muted' }, 'Requests whose source country matches are denied without a push. A failed geo lookup leaves the country unknown and the request is pushed.'),
    form, list);

  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    formStatus.replaceChildren();
    try {
      const rule = await api.addGeoRule({ country: country.value.trim().toUpperCase(), note: note.value.trim() || null });
      formStatus.replaceChildren(infoLine(`${rule.country} is now blocked.`));
      country.value = ''; note.value = '';
      await load();
    } catch (err) { formStatus.replaceChildren(errorLine(err)); }
  });

  async function load() {
    list.replaceChildren(infoLine('Loading…'));
    try {
      const res = await api.geoRules();
      const rows = (res.items || []).map((r) => [
        el('strong', {}, r.country),
        dash(r.note),
        fmtTime(r.created_at),
        asyncButton('Remove', async () => {
          if (!confirmAction(`Remove the rule for ${r.country}?`)) return;
          await api.deleteGeoRule(r.id);
          await load();
        }, { class: 'danger small' }),
      ]);
      list.replaceChildren(table(['Country', 'Note', 'Added', ''], rows, 'No country is blocked.'));
    } catch (err) { list.replaceChildren(errorLine(err)); }
  }
  load();
}
