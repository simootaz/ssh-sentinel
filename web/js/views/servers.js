// Servers (v1.1): the enrolled servers, rotate a token, revoke one.
// Enrollment stays a script on the backend host (docs/architecture.md, 6).

import { api } from '../api.js';
import { el, badge, errorLine, infoLine, table, asyncButton, confirmAction, tokenPanel, replace } from '../dom.js';
import { fmtTime, fmtRelative } from '../format.js';

export const title = 'Servers';

export function render(container) {
  const panels = el('div', {});
  const list = el('div', {});
  replace(container, el('h1', {}, 'Servers'),
    el('p', { class: 'muted' }, 'Rotating a token makes the agent deny every login until its config holds the new one: rotate and update the server in the same sitting, with the break-glass file as the safety net. Revoking is final; the row stays for the history.'),
    panels, list);

  async function load() {
    list.replaceChildren(infoLine('Loading…'));
    try {
      const res = await api.servers();
      const rows = (res.items || []).map((s) => [
        el('strong', {}, s.name),
        s.os,
        s.revoked ? badge('revoked', 'revoked') : badge('enrolled', 'active'),
        fmtTime(s.created_at),
        s.last_seen_at ? `${fmtTime(s.last_seen_at)} (${fmtRelative(s.last_seen_at)})` : 'never',
        s.revoked ? '' : el('span', { class: 'row-actions' },
          asyncButton('Rotate token', async () => {
            if (!confirmAction(`Rotate the token of ${s.name}? The agent denies every login until /etc/ssh-sentinel/config.json holds the new token.`)) return;
            const r = await api.rotateServer(s.id);
            panels.prepend(tokenPanel(`New token of ${r.name}`, r.token, `Rotated ${fmtTime(r.rotated_at)}. Put it in the agent's config file now.`));
            await load();
          }, { class: 'small' }),
          ' ',
          asyncButton('Revoke', async () => {
            if (!confirmAction(`Revoke ${s.name}? No token will match it any more. Enrol it again under a new name to bring it back.`)) return;
            await api.revokeServer(s.id);
            await load();
          }, { class: 'danger small' }),
        ),
      ]);
      list.replaceChildren(table(['Name', 'OS', 'Status', 'Enrolled', 'Last seen', ''], rows, 'No server enrolled.'));
    } catch (err) { list.replaceChildren(errorLine(err)); }
  }
  load();
}
