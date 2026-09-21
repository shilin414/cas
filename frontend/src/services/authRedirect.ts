import { apiUrl, appPath, routerPath } from '@/lib/deploymentPaths';

/** Accept only normalized browser paths within this deployment, never an origin. */
export function safeLoginReturnTo(target?: string | null): string {
  const route = routerPath(target || appPath('/'));
  if (/^\/(?:login|auth)(?:\/|\?|#|$)/i.test(route)) return appPath('/');
  return appPath(route);
}
export function sessionLoginRoute(target: string, explicitlyLoggedOut = false): string {
  const params = new URLSearchParams();
  if (explicitlyLoggedOut) params.set('logged_out', '1');
  params.set('return_to', safeLoginReturnTo(target));
  return `${explicitlyLoggedOut ? '/auth/login' : '/login'}?${params}`;
}
export function oauthLoginURL(target?: string | null): string {
  return apiUrl(`/identity/oauth/start?${new URLSearchParams({ return_to: safeLoginReturnTo(target) })}`);
}
/** Preserve the snapshot deep link across a normal 401 session boundary. */
export function navigateToSessionLogin(returnTo?: string): void {
  window.location.href = appPath(sessionLoginRoute(returnTo || captureSessionLoginReturnTo()));
}

// A query can exist only in component state. Keep its safe recovery identifier
// in memory while that result is mounted; never retain business result bodies.
let activeReturn: { source: string; target: string } | null = null;
function currentBrowserPath(): string { return window.location.pathname + window.location.search + window.location.hash; }
export function registerSessionLoginReturnTo(target: string): () => void {
  const registration = { source: currentBrowserPath(), target: safeLoginReturnTo(target) };
  activeReturn = registration;
  return () => { if (activeReturn === registration) activeReturn = null; };
}
export function captureSessionLoginReturnTo(): string {
  const current = currentBrowserPath();
  return activeReturn?.source === current ? activeReturn.target : safeLoginReturnTo(current);
}
