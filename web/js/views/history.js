// History: GET /history with the contract's filters, newest first, paged
// with next_before. The recorded=true filter of v1.1 is sent to the backend
// and applied here as well, so it works on a backend that ignores it.

import { api } from '../api.js';
import { el, badge, dash, errorLine, infoLine, table, field, replace } from '../dom.js';
import { fmtTime, fmtGeo, fmtDecider, fmtBytes } from '../format.js';

export const title = 'History';

const STATUSES = ['pending', 'approved', 'denied', 'timeout', 'whitelisted', 'blocked_ip', 'blocked_geo', 'notified'];
const PAGE = 50;

// Items seen in this session, by id, so the recording view can show the
// request behind a recording without a route for a single request.
const seen = new Map();
export function knownRequest(id) { return seen.get(id) || null; }

export function render(container) {
  const server = el('input', { type: 'text', name: 'server', placeholder: 'any server' });
  const username = el('input', { type: 'text', name: 'username', placeholder: 'any user' });
  const context = el('select', { name: 'context' },
    el('option', { value: '' }, 'ssh and sudo'),
    el('option', { value: 'ssh' }, 'ssh'),
    el('option', { value: 'sudo' }, 'sudo'));
  const status = el('select', { name: 'status' },
    el('option', { value: '' }, 'any status'),
    STATUSES.map((s) => el('option', { value: s }, s)));
  const recorded = el('input', { type: 'checkbox', name: 'recorded' });
  const results = el('div', {});
  const more = el('button', { type: 'button', hidden: true }, 'Load more');
  const form = el('form', { class: 'filters' },
    field('Server', server), field('User', username), field('Context', context), field('Status', status),
    el('label', { class: 'field check' }, recorded, el('span', {}, 'with a recording')),
    el('button', { type: 'submit', class: 'primary' }, 'Search'),
  );
  replace(container, el('h1', {}, 'History'), form, results, el('p', {}, more));

  let items = [];
  let nextBefore = null;

  function params(before) {
    return {
      limit: PAGE,
      before,
      server: server.value.trim(),
      username: username.value.trim(),
      context: context.value,
      status: status.value,
      recorded: recorded.checked ? 'true' : '',
    };
  }

  async function load(before) {
    more.disabled = true;
    if (!before) { items = []; results.replaceChildren(infoLine('Loading…')); }
    try {
      const res = await api.history(params(before));
      let page = res.items || [];
      if (recorded.checked) page = page.filter((it) => it.recording);
      for (const it of page) seen.set(it.id, it);
      items = items.concat(page);
      nextBefore = res.next_before || null;
      results.replaceChildren(renderTable(items));
      more.hidden = !nextBefore;
    } catch (err) {
      results.replaceChildren(errorLine(err));
      more.hidden = true;
    } finally {
      more.disabled = false;
    }
  }

  form.addEventListener('submit', (ev) => { ev.preventDefault(); load(null); });
  more.addEventListener('click', () => load(nextBefore));
  load(null);
}

function renderTable(items) {
  const rows = items.map((it) => [
    fmtTime(it.created_at),
    dash(it.server),
    badge(it.context, it.context),
    it.username,
    it.source_ip ? [it.source_ip, geoLine(it.geo)] : 'local',
    it.command ? el('code', {}, it.command) : dash(it.tty),
    badge(it.status, it.status),
    dash(fmtDecider(it)),
    it.decided_at ? fmtTime(it.decided_at) : '—',
    recordingCell(it),
  ]);
  return table(['Asked', 'Server', 'Context', 'User', 'Source', 'Command / tty', 'Status', 'Decided by', 'Decided at', 'Recording'], rows, 'No request matches.');
}

function geoLine(geo) {
  const s = fmtGeo(geo);
  return s ? el('div', { class: 'muted small' }, s) : null;
}

function recordingCell(it) {
  if (!it.recording) return '—';
  const r = it.recording;
  return el('a', { href: `#recording/${encodeURIComponent(it.id)}`, class: 'rec' },
    `view (${fmtBytes(r.size_bytes)}${r.truncated ? ', truncated' : ''})`);
}
