// Small DOM helpers. Everything is built with createElement and textContent
// (no HTML strings are ever parsed), so nothing coming from the API can be
// interpreted as markup. No inline styles either: the CSP of /dashboard
// forbids them, visibility goes through the hidden attribute and classes.

import { errorMessage } from './api.js';

/**
 * el('button', { class: 'primary', onclick: fn, disabled: true }, 'text', child, [more])
 * Attributes: "class" and "text" are special; "on<event>" keys become listeners;
 * boolean false removes the attribute; "dataset" is an object.
 */
export function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === null || v === undefined || v === false) continue;
    if (k === 'class') node.className = v;
    else if (k === 'text') node.textContent = v;
    else if (k === 'dataset') Object.assign(node.dataset, v);
    else if (k.startsWith('on') && typeof v === 'function') node.addEventListener(k.slice(2), v);
    else if (v === true) node.setAttribute(k, '');
    else node.setAttribute(k, String(v));
  }
  append(node, children);
  return node;
}

/** Appends strings, nodes, arrays of them; null and undefined are skipped. */
export function append(node, children) {
  for (const c of children) {
    if (c === null || c === undefined || c === false) continue;
    if (Array.isArray(c)) append(node, c);
    else node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
  }
  return node;
}

export function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
  return node;
}

/** Replaces the content of node with children. */
export function replace(node, ...children) {
  clear(node);
  return append(node, children);
}

/** One error line, for anything a call can throw. */
export function errorLine(err) {
  return el('p', { class: 'error', role: 'alert' }, errorMessage(err));
}

/** One informational line. */
export function infoLine(text) {
  return el('p', { class: 'info' }, text);
}

/** A table: columns are header labels, rows are arrays of cells (strings or nodes). */
export function table(columns, rows, emptyText = 'nothing here') {
  if (!rows.length) return infoLine(emptyText);
  return el('div', { class: 'table-wrap' },
    el('table', {},
      el('thead', {}, el('tr', {}, columns.map((c) => el('th', {}, c)))),
      el('tbody', {}, rows.map((r) => el('tr', {}, r.map((c) => el('td', {}, c))))),
    ),
  );
}

/** Status pill: class by status so the stylesheet colours it. */
export function badge(text, kind = text) {
  return el('span', { class: `badge badge-${String(kind).replace(/[^a-z0-9_-]/gi, '')}` }, text);
}

/** A button that disables itself while its async handler runs and shows its error next to it. */
export function asyncButton(label, handler, attrs = {}) {
  const btn = el('button', { type: 'button', ...attrs }, label);
  btn.addEventListener('click', async () => {
    btn.disabled = true;
    const old = btn.nextElementSibling;
    if (old && old.classList.contains('error')) old.remove();
    try {
      await handler(btn);
    } catch (err) {
      btn.after(errorLine(err));
    } finally {
      if (btn.isConnected) btn.disabled = false;
    }
  });
  return btn;
}

/** window.confirm behind a function, so a view can be read without the prompt. */
export function confirmAction(message) {
  return window.confirm(message);
}

/**
 * Panel showing a token exactly once (contract: tokens are never returned
 * again). Copy button uses the clipboard API, available on https and localhost.
 */
export function tokenPanel(title, token, hint) {
  const code = el('code', { class: 'token' }, token);
  const copy = el('button', { type: 'button' }, 'Copy');
  copy.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(token);
      copy.textContent = 'Copied';
    } catch {
      copy.textContent = 'Select and copy by hand';
    }
  });
  const panel = el('div', { class: 'token-panel', role: 'status' },
    el('p', { class: 'token-title' }, title),
    el('p', {}, code, ' ', copy),
    el('p', { class: 'muted' }, hint || 'Shown once. It is not stored anywhere else.'),
    el('button', { type: 'button', onclick: () => panel.remove() }, 'Dismiss'),
  );
  return panel;
}

/** TTL picker used by "always allow" and the whitelist form: returns the fieldset and a read() function. */
export function ttlPicker(name) {
  const choice = el('select', { name: `${name}-choice` },
    el('option', { value: 'permanent' }, 'permanent'),
    el('option', { value: '1h' }, '1 hour'),
    el('option', { value: '24h' }, '24 hours'),
    el('option', { value: 'custom' }, 'custom'),
  );
  const amount = el('input', { type: 'number', min: '1', step: '1', value: '7', name: `${name}-amount`, hidden: true, 'aria-label': 'duration' });
  const unit = el('select', { name: `${name}-unit`, hidden: true, 'aria-label': 'unit' },
    el('option', { value: 'min' }, 'minutes'),
    el('option', { value: 'h' }, 'hours'),
    el('option', { value: 'd', selected: true }, 'days'),
  );
  choice.addEventListener('change', () => {
    const custom = choice.value === 'custom';
    amount.hidden = !custom;
    unit.hidden = !custom;
  });
  const box = el('span', { class: 'ttl' }, choice, ' ', amount, ' ', unit);
  return { node: box, read: () => ({ choice: choice.value, amount: amount.value, unit: unit.value }) };
}

/** Labelled field for forms. */
export function field(label, input) {
  return el('label', { class: 'field' }, el('span', {}, label), input);
}

/** Text shown when a value is missing. */
export function dash(v) {
  return v === null || v === undefined || v === '' ? '—' : String(v);
}
