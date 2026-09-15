// Live pending requests: the list comes from the poll in app.js (GET
// /history?status=pending every 2 s), each card has the three verdict
// buttons of the phone app. First verdict wins: a 409 shows who did.

import { api, errorMessage } from '../api.js';
import { el, badge, dash, errorLine, infoLine, ttlPicker, replace } from '../dom.js';
import { fmtTime, fmtGeo, secondsLeft, ttlSeconds, fmtExpiry } from '../format.js';

export const title = 'Pending';

// How long a decided card stays on screen before it goes.
const SETTLED_MS = 8000;

export function render(container, { poller }) {
  const cards = new Map(); // request id -> card
  const list = el('div', { class: 'cards' });
  const status = el('p', { class: 'muted' }, 'Waiting for the first poll…');
  replace(container, el('h1', {}, 'Pending requests'), status, list);

  function sync(items, err) {
    if (err) {
      status.replaceChildren(el('span', { class: 'error' }, `Poll failed: ${errorMessage(err)}`));
      return;
    }
    status.textContent = items.length
      ? `${items.length} waiting for a verdict`
      : 'Nothing pending. New requests appear here within 2 s.';
    const seen = new Set();
    for (const item of items) {
      seen.add(item.id);
      const known = cards.get(item.id);
      if (known) { known.item = item; continue; }
      const card = buildCard(item, cards);
      cards.set(item.id, card);
      list.prepend(card.node);
    }
    for (const [id, card] of cards) {
      if (!seen.has(id) && !card.settled) {
        // Decided elsewhere (a phone, another tab) or expired: gone from the poll.
        card.node.remove();
        cards.delete(id);
      }
    }
  }

  const unsubscribe = poller.subscribe(sync);
  const timer = setInterval(() => { for (const c of cards.values()) c.tick(); }, 1000);
  return () => { unsubscribe(); clearInterval(timer); };
}

function buildCard(item, cards) {
  const countdown = el('span', { class: 'countdown' });
  const outcome = el('div', { class: 'outcome' });
  const buttons = el('div', { class: 'actions' });
  const card = { item, settled: false, node: null, tick };

  const ttl = ttlPicker('always');
  const alwaysBox = el('div', { class: 'always', hidden: true },
    el('span', {}, 'Always allow for '), ttl.node, ' ',
    el('button', { type: 'button', class: 'primary', onclick: () => decide('approve_always') }, 'Confirm'),
    ' ',
    el('button', { type: 'button', onclick: () => { alwaysBox.hidden = true; } }, 'Cancel'),
  );

  buttons.append(
    el('button', { type: 'button', class: 'danger', onclick: () => decide('deny') }, 'Deny'),
    ' ',
    el('button', { type: 'button', class: 'primary', onclick: () => decide('approve') }, 'Approve'),
    ' ',
    el('button', { type: 'button', onclick: () => { alwaysBox.hidden = !alwaysBox.hidden; } }, 'Always allow…'),
  );

  const geo = fmtGeo(item.geo);
  card.node = el('article', { class: `card card-${item.context}`, dataset: { id: item.id } },
    el('header', {},
      badge(item.context, item.context), ' ',
      el('strong', {}, item.username), ' on ', el('strong', {}, item.server || item.hostname),
      el('span', { class: 'spacer' }),
      countdown,
    ),
    el('dl', {},
      el('dt', {}, 'Source'), el('dd', {}, item.source_ip ? [item.source_ip, geo ? el('span', { class: 'muted' }, ` ${geo}`) : null] : 'local'),
      el('dt', {}, 'Host'), el('dd', {}, [dash(item.hostname), item.tty ? ` (${item.tty})` : '']),
      item.command ? [el('dt', {}, 'Command'), el('dd', {}, el('code', {}, item.command))] : null,
      el('dt', {}, 'Asked'), el('dd', {}, fmtTime(item.created_at)),
    ),
    buttons,
    alwaysBox,
    outcome,
  );
  tick();
  return card;

  function tick() {
    const left = secondsLeft(card.item.expires_at);
    countdown.textContent = left > 0 ? `${left} s left` : 'expired';
    countdown.classList.toggle('urgent', left > 0 && left <= 10);
    if (left <= 0 && !card.settled) setButtons(false);
  }

  function setButtons(enabled) {
    for (const b of buttons.querySelectorAll('button')) b.disabled = !enabled;
    for (const b of alwaysBox.querySelectorAll('button')) b.disabled = !enabled;
  }

  function settle(node) {
    card.settled = true;
    setButtons(false);
    alwaysBox.hidden = true;
    outcome.replaceChildren(node);
    setTimeout(() => { card.node.remove(); cards.delete(item.id); }, SETTLED_MS);
  }

  async function decide(verdict) {
    const body = { request_id: item.id, verdict };
    if (verdict === 'approve_always') {
      try {
        const { choice, amount, unit } = ttl.read();
        const secs = ttlSeconds(choice, amount, unit);
        if (secs !== null) body.ttl_seconds = secs;
      } catch (err) {
        outcome.replaceChildren(errorLine(err));
        return;
      }
    }
    setButtons(false);
    outcome.replaceChildren(infoLine('Sending…'));
    try {
      const res = await api.verdict(body);
      const entry = res.whitelist_entry;
      settle(el('div', { class: 'ok' },
        el('p', {}, badge(res.status, res.status), ` by you${res.decided_by_admin ? ` (${res.decided_by_admin})` : ''}`),
        entry ? el('p', {}, `Whitelisted ${entry.username} for ${entry.context} on ${entry.server || 'every server'}, expires: ${fmtExpiry(entry.expires_at)}`) : null,
        res.auto_blocked ? el('p', { class: 'warn' }, `IP ${res.auto_blocked.ip} is now blocked (${res.auto_blocked.denial_count} denials)`) : null,
      ));
    } catch (err) {
      if (err.status === 409 && err.body && err.body.status) {
        // Somebody else was first, or the request timed out: show the outcome.
        const b = err.body;
        const who = [b.decided_by_admin, b.decided_by_device].filter(Boolean).join(' / ');
        settle(el('div', { class: 'warn' },
          el('p', {}, 'Already decided: ', badge(b.status, b.status), who ? ` by ${who}` : '', b.decided_at ? ` at ${fmtTime(b.decided_at)}` : ''),
        ));
      } else {
        outcome.replaceChildren(errorLine(err));
        if (secondsLeft(card.item.expires_at) > 0) setButtons(true);
      }
    }
  }
}
