// SPDX-License-Identifier: AGPL-3.0-or-later
// Access to the API. Changing requests carry the X-Routerui header, which the server
// as part of the CSRF protection.

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
    throw new ApiError('The router is not reachable.', 0);
  }
  let data = null;
  try { data = await res.json(); } catch { /* empty response */ }
  if (res.status === 401 && !path.startsWith('/api/login') && !path.startsWith('/api/setup')) {
    onUnauthorized();
  }
  if (!res.ok) throw new ApiError(data?.error || `Error ${res.status}`, res.status);
  return data;
}

export const get = (path) => api(path);
export const post = (path, body) => api(path, { method: 'POST', body });
export const put = (path, body) => api(path, { method: 'PUT', body });
export const del = (path, body) => api(path, { method: 'DELETE', body });
