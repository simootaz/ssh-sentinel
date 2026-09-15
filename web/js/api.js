// One place for the base URL, the admin token and every call of the
// contract (docs/architecture.md, section 5). The views never call fetch.
//
// The token lives in sessionStorage: it is gone when the tab closes, and it
// is sent as the bearer on every call. No cookie, so no CSRF surface.

const TOKEN_KEY = 'ssh-sentinel.admin-token';

/**
 * Base URL of the API, derived from where the dashboard is served: the page
 * is <base>/dashboard/, the API is <base>. Works with or without a path
 * prefix in front of the backend.
 */
export function apiBase(pathname = location.pathname) {
  const i = pathname.indexOf('/dashboard');
  return i < 0 ? '' : pathname.slice(0, i);
}

export function getToken() {
  try { return sessionStorage.getItem(TOKEN_KEY) || ''; } catch { return ''; }
}

export function setToken(token) {
  sessionStorage.setItem(TOKEN_KEY, token);
}

export function clearToken() {
  try { sessionStorage.removeItem(TOKEN_KEY); } catch { /* storage unavailable: nothing to clear */ }
}

/** Error of a call: HTTP status (0 for a network failure) and the decoded body when any. */
export class ApiError extends Error {
  constructor(status, body, message) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.body = body;
  }
}

/** Message shown to the user for any error, API or not. */
export function errorMessage(err) {
  if (err instanceof ApiError) {
    if (err.status === 0) return err.message;
    return `HTTP ${err.status}: ${err.message}`;
  }
  return err && err.message ? err.message : String(err);
}

/** Builds a query string from an object; empty, null and undefined values are skipped. */
export function query(params) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params || {})) {
    if (v === undefined || v === null || v === '') continue;
    q.set(k, String(v));
  }
  const s = q.toString();
  return s ? `?${s}` : '';
}

// Called when a call answers 401, so the app can drop the token and show
// the login screen. Set once by app.js.
let onUnauthorized = null;
export function setUnauthorizedHandler(fn) { onUnauthorized = fn; }

/**
 * Sends one request with the bearer token. JSON in, JSON out; a non-2xx
 * answer becomes an ApiError carrying the contract's {"error": ...} message,
 * or the status text when the body is not JSON. With raw: true the Response
 * is returned untouched (recordings are text, not JSON).
 */
export async function request(method, path, { body, raw } = {}) {
  const headers = { Authorization: `Bearer ${getToken()}` };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  let resp;
  try {
    resp = await fetch(apiBase() + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch (e) {
    throw new ApiError(0, null, `backend unreachable: ${e.message}`);
  }
  if (resp.status === 401 && onUnauthorized) onUnauthorized();
  if (!resp.ok) throw await toError(resp);
  if (raw) return resp;
  if (resp.status === 204) return null;
  const text = await resp.text();
  if (!text) return null;
  try { return JSON.parse(text); } catch { throw new ApiError(resp.status, null, 'answer is not JSON'); }
}

async function toError(resp) {
  let text = '';
  try { text = await resp.text(); } catch { /* body unreadable: the status is enough */ }
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = null; }
  const msg = (data && typeof data.error === 'string' && data.error)
    || (text && text.length < 200 ? text.trim() : '')
    || resp.statusText
    || 'request failed';
  return new ApiError(resp.status, data, msg);
}

const enc = encodeURIComponent;

// The routes, one function each, named after the contract.
export const api = {
  history: (params) => request('GET', `/history${query(params)}`),
  verdict: (body) => request('POST', '/verdict', { body }),

  whitelist: () => request('GET', '/whitelist'),
  addWhitelist: (body) => request('POST', '/whitelist', { body }),
  deleteWhitelist: (id) => request('DELETE', `/whitelist/${enc(id)}`),

  blockedIPs: () => request('GET', '/blocked-ips'),
  unblockIP: (id) => request('DELETE', `/blocked-ips/${enc(id)}`),

  geoRules: () => request('GET', '/geo-rules'),
  addGeoRule: (body) => request('POST', '/geo-rules', { body }),
  deleteGeoRule: (id) => request('DELETE', `/geo-rules/${enc(id)}`),

  // v1.1 routes. On a backend without them every call answers 404, which
  // the views show as an error line instead of a list.
  admins: () => request('GET', '/admins'),
  addAdmin: (body) => request('POST', '/admins', { body }),
  rotateAdmin: (id) => request('POST', `/admins/${enc(id)}/rotate`),
  disableAdmin: (id) => request('DELETE', `/admins/${enc(id)}`),

  servers: () => request('GET', '/servers'),
  rotateServer: (id) => request('POST', `/servers/${enc(id)}/rotate`),
  revokeServer: (id) => request('DELETE', `/servers/${enc(id)}`),

  recording: (requestId) => request('GET', `/recordings/${enc(requestId)}`, { raw: true }),
};
