// Recording playback (v1.1): GET /recordings/{request_id} as plain text.
// The bytes are the raw typescript of script(1); ansi.js turns them into
// readable text, the raw view shows every control character.

import { api } from '../api.js';
import { el, badge, dash, errorLine, infoLine, replace } from '../dom.js';
import { fmtTime, fmtBytes, fmtDecider } from '../format.js';
import { toPlainText, toVisible } from '../ansi.js';
import { knownRequest } from './history.js';

export const title = 'Recording';

export function render(container, { id }) {
  const item = knownRequest(id);
  const head = el('div', { class: 'recording-head' },
    el('p', {}, el('a', { href: '#history' }, '← History')),
    item ? summary(item) : el('p', { class: 'muted' }, `Request ${id}`),
  );
  const body = el('div', {});
  replace(container, el('h1', {}, 'Recording'), head, body);
  load();

  async function load() {
    body.replaceChildren(infoLine('Loading…'));
    let resp;
    try {
      resp = await api.recording(id);
    } catch (err) {
      body.replaceChildren(explain(err));
      return;
    }
    const text = await resp.text();
    const truncated = resp.headers.get('X-Recording-Truncated') === 'true';
    const plain = toPlainText(text);
    const pre = el('pre', { class: 'typescript' }, plain);
    let raw = false;
    const toggle = el('button', { type: 'button' }, 'Show raw');
    toggle.addEventListener('click', () => {
      raw = !raw;
      pre.textContent = raw ? toVisible(text) : plain;
      toggle.textContent = raw ? 'Show plain text' : 'Show raw';
    });
    const download = el('button', { type: 'button' }, 'Download');
    download.addEventListener('click', () => {
      // A plain link cannot carry the bearer token, so the bytes already
      // fetched are handed to the browser as a file.
      const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
      const a = el('a', { href: url, download: `${id}.log` });
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    });
    body.replaceChildren(
      el('p', { class: 'toolbar' },
        `${fmtBytes(text.length)} of text`,
        truncated ? [' ', badge('truncated by the agent', 'truncated')] : null,
        el('span', { class: 'spacer' }),
        toggle, ' ', download,
      ),
      pre,
    );
  }
}

function summary(it) {
  const r = it.recording || {};
  return el('dl', { class: 'summary' },
    el('dt', {}, 'Login'), el('dd', {}, [el('strong', {}, it.username), ' on ', el('strong', {}, it.server || it.hostname), it.source_ip ? ` from ${it.source_ip}` : '']),
    el('dt', {}, 'Asked'), el('dd', {}, fmtTime(it.created_at)),
    el('dt', {}, 'Decided'), el('dd', {}, [badge(it.status, it.status), ' ', dash(fmtDecider(it))]),
    r.started_at ? [el('dt', {}, 'Session'), el('dd', {}, `${fmtTime(r.started_at)} to ${r.ended_at ? fmtTime(r.ended_at) : '?'}`)] : null,
    r.uploaded_at ? [el('dt', {}, 'Uploaded'), el('dd', {}, fmtTime(r.uploaded_at))] : null,
  );
}

function explain(err) {
  switch (err.status) {
    case 404: return infoLine('This request has no recording, or the request is unknown.');
    case 501: return infoLine('Recordings are not supported on this deployment (no recording storage).');
    default: return errorLine(err);
  }
}
