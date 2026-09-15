// Blocked IPs: the auto-block list, with unblock.

import { api } from '../api.js';
import { el, errorLine, infoLine, table, asyncButton, confirmAction, replace } from '../dom.js';
import { fmtTime, fmtExpiry } from '../format.js';

export const title = 'Blocked IPs';

export function render(container) {
  const list = el('div', {});
  replace(container, el('h1', {}, 'Blocked IPs'),
    el('p', { class: 'muted' }, 'IPs blocked after repeated admin denials. Their requests are denied without a push until unblocked.'),
    list);

  async function load() {
    list.replaceChildren(infoLine('Loading…'));
    try {
      const res = await api.blockedIPs();
      const rows = (res.items || []).map((b) => [
        el('code', {}, b.ip),
        b.reason,
        String(b.denial_count),
        `${fmtTime(b.first_denied_at)} to ${fmtTime(b.last_denied_at)}`,
        String(b.hit_count),
        b.last_hit_at ? fmtTime(b.last_hit_at) : '—',
        b.expires_at ? fmtExpiry(b.expires_at) : 'until unblocked',
        asyncButton('Unblock', async () => {
          if (!confirmAction(`Unblock ${b.ip}? Its next request is pushed normally.`)) return;
          await api.unblockIP(b.id);
          await load();
        }, { class: 'small' }),
      ]);
      list.replaceChildren(table(['IP', 'Reason', 'Denials', 'Denied between', 'Hits since block', 'Last hit', 'Expires', ''], rows, 'No IP is blocked.'));
    } catch (err) { list.replaceChildren(errorLine(err)); }
  }
  load();
}
