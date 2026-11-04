// SPDX-License-Identifier: AGPL-3.0-or-later
// Zugriff auf die API. Ändernde Anfragen tragen den Header X-Routerui, den der Server
// als Teil des CSRF-Schutzes verlangt.

export class ApiError extends Error {
  constructor(msg, status) { super(msg); this.status = status; }
}

let onUnauthorized = () => {};
export function setUnauthorizedHandler(fn) { onUnauthorized = fn; }

export async function api(path, { method = 'GET', body } = {}) {
  const opts = { method, credentials: 'same-origin', headers: {} };
  if (method !== 'GET') {
    opts.headers['X-Routerui'] = '1';
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body ?? {});
  }
  let res;
  try {
    res = await fetch(path, opts);
  } catch {
    throw new ApiError('Der Router ist nicht erreichbar.', 0);
  }
  let data = null;
  try { data = await res.json(); } catch { /* leere Antwort */ }
  if (res.status === 401 && !path.startsWith('/api/login') && !path.startsWith('/api/setup')) {
    onUnauthorized();
  }
  if (!res.ok) throw new ApiError(data?.error || `Fehler ${res.status}`, res.status);
  return data;
}

export const get = (path) => api(path);
export const post = (path, body) => api(path, { method: 'POST', body });
export const put = (path, body) => api(path, { method: 'PUT', body });
export const del = (path, body) => api(path, { method: 'DELETE', body });
