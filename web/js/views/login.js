// Login: the admin token, checked with one harmless call, then kept in
// session storage (docs/architecture.md, section 8, decision 31).

import { api, setToken, clearToken } from '../api.js';
import { el, errorLine, field } from '../dom.js';

export const title = 'Log in';

export function render(container, { onLogin, message }) {
  const input = el('input', { type: 'password', name: 'token', autocomplete: 'off', required: true, spellcheck: 'false' });
  const submit = el('button', { type: 'submit', class: 'primary' }, 'Log in');
  const status = el('div', {});
  const form = el('form', { class: 'login' },
    el('h1', {}, 'ssh-sentinel'),
    el('p', {}, 'Paste an admin token. It is kept in this tab only and forgotten when the tab closes.'),
    message ? el('p', { class: 'error', role: 'alert' }, message) : null,
    field('Admin token', input),
    el('p', {}, submit),
    status,
  );
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const token = input.value.trim();
    if (!token) return;
    submit.disabled = true;
    status.replaceChildren();
    setToken(token);
    try {
      // The cheapest authenticated call of the contract: one history row.
      await api.history({ limit: 1 });
      onLogin();
    } catch (err) {
      clearToken();
      status.replaceChildren(errorLine(err));
      submit.disabled = false;
      input.focus();
    }
  });
  container.replaceChildren(form);
  input.focus();
}
