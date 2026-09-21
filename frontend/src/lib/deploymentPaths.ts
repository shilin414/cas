/** Vite BASE_URL is generated from the shared deployment.json configuration.
 * Router links remain app-relative; only browser/network URLs use these helpers.
 */
export function appPath(path = ''): string {
  const base = import.meta.env.BASE_URL || '/';
  const prefix = base.replace(/\/$/, '');
  if (prefix && (path === prefix || path.startsWith(`${prefix}/`))) return path;
  return `${base}${path.replace(/^\/+/, '')}`;
}

export function apiUrl(path = ''): string {
  const base = (import.meta.env.VITE_API_BASE_URL || appPath('api')).replace(/\/+$/, '');
  return path ? `${base}/${path.replace(/^\/+/, '')}` : base;
}

/** Backend-owned URLs are internal /api paths; CDN/data/blob URLs are untouched. */
export function deploymentAssetUrl(url: string): string {
  if (url === '/api' || url.startsWith('/api/')) return apiUrl(url.slice(4));
  return url;
}

/** Convert a browser return_to URL to a safe, basename-relative router target. */
export function routerPath(target: string): string {
  if (!target.startsWith('/') || target.startsWith('//') || /[\\\r\n]/.test(target)) return '/';
  const parsed = new URL(target, 'https://deployment.invalid');
  const prefix = (import.meta.env.BASE_URL || '/').replace(/\/$/, '');
  if (parsed.pathname !== prefix && !parsed.pathname.startsWith(`${prefix}/`)) return '/';
  const path = parsed.pathname.slice(prefix.length) || '/';
  // Strip basename only after URL normalization, and never hand the router an
  // origin-relative //host target (including encoded separators).
  if (path.startsWith('//') || /%2f|%5c/i.test(path)) return '/';
  return `${path}${parsed.search}${parsed.hash}`;
}
