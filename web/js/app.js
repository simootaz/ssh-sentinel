// Entry point: login gate, hash router, the 2 s poll of pending requests.
// Views live in ./views, one module each, exporting title and
// render(container, ctx) which may return a cleanup function.

import { api, getToken, clearToken, setUnauthorizedHandler, errorMessage } from './api.js';
import * as login from './views/login.js';
import * as pending from './views/pending.js';
import * as history from './views/history.js';
import * as whitelist from './views/whitelist.js';
import * as blocked from './views/blocked.js';
import * as georules from './views/georules.js';
import * as admins from './views/admins.js';
import * as servers from './views/servers.js';
import * as recording from './views/recording.js';

const POLL_MS = 2000;

const VIEWS = { pending, history, whitelist, blocked, georules, admins, servers };

const main = document.getElementById('main');
const nav = document.getElementById('nav');
const session = document.getElementById('session');
const pendingCount = document.getElementById('pending-count');
const pollState = document.getElementById('poll-state');

// Poller: one GET /history?status=pending every 2 s while logged in and the
// tab is visible. Subscribers (the pending view, the nav badge) get the
// items or the error of every round.
const poller = {
  subs: new Set(),
  timer: null,
  inflight: false,
  subscribe(fn) { this.subs.add(fn); return () => this.subs.delete(fn); },
  start() { if (!this.timer) { this.timer = setInterval(() => this.round(), POLL_MS); this.round(); } },
  stop() { clearInterval(this.timer); this.timer = null; },
  async round() {
    if (this.inflight || document.hidden) return;
    this.inflight = true;
    try {
      const res = await api.history({ status: 'pending', limit: 200 });
      const items = res.items || [];
      for (const fn of this.subs) fn(items, null);
      pendingCount.textContent = String(items.length);
      pendingCount.hidden = items.length === 0;
      pollState.textContent = '';
    } catch (err) {
      for (const fn of this.subs) fn([], err);
      pollState.textContent = `poll: ${errorMessage(err)}`;
    } finally {
      this.inflight = false;
    }
  },
};

let cleanup = null;

function route() {
  const hash = location.hash.replace(/^#/, '');
  const [name, ...rest] = hash.split('/');
  if (name === 'recording' && rest.length) return { view: recording, ctx: { id: decodeURIComponent(rest.join('/')) }, active: 'history' };
  const view = VIEWS[name] || pending;
  return { view, ctx: { poller }, active: VIEWS[name] ? name : 'pending' };
}

function show() {
  if (cleanup) { cleanup(); cleanup = null; }
  if (!getToken()) { showLogin(); return; }
  nav.hidden = false;
  session.hidden = false;
  const { view, ctx, active } = route();
  for (const a of nav.querySelectorAll('a[data-view]')) a.classList.toggle('active', a.dataset.view === active);
  document.title = `${view.title} · ssh-sentinel`;
  const r = view.render(main, ctx);
  if (typeof r === 'function') cleanup = r;
  poller.start();
}

function showLogin(message) {
  poller.stop();
  nav.hidden = true;
  session.hidden = true;
  pendingCount.hidden = true;
  document.title = 'ssh-sentinel';
  login.render(main, { message, onLogin: () => { if (!location.hash) location.hash = '#pending'; show(); } });
}

setUnauthorizedHandler(() => {
  if (!getToken()) return;
  clearToken();
  showLogin('The token was rejected (HTTP 401). Log in again.');
});

document.getElementById('logout').addEventListener('click', () => {
  clearToken();
  location.hash = '';
  showLogin();
});

window.addEventListener('hashchange', show);
document.addEventListener('visibilitychange', () => { if (!document.hidden && poller.timer) poller.round(); });
show();
