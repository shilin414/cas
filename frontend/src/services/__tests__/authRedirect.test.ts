import { afterEach, describe, expect, it, vi } from 'vitest';
import { captureSessionLoginReturnTo, navigateToSessionLogin, registerSessionLoginReturnTo, oauthLoginURL, safeLoginReturnTo, sessionLoginRoute } from '../authRedirect';
import { routerPath } from '@/lib/deploymentPaths';
afterEach(() => {vi.unstubAllEnvs();vi.unstubAllGlobals();});
describe('safe login return paths', () => {
 it.each(['/', '/xiaoan-platform/', '/nested/platform/'])('preserves query snapshot through guard, OAuth and callback at %s', base => {
  vi.stubEnv('BASE_URL', base); vi.stubEnv('VITE_API_BASE_URL', '');
  const target = `${base}app/material-query?result=${'a'.repeat(64)}`;
  const login = new URL(sessionLoginRoute(target), 'https://example.test');
  const oauth = new URL(oauthLoginURL(login.searchParams.get('return_to')), 'https://example.test');
  expect(oauth.pathname).toBe(`${base}api/identity/oauth/start`);
  expect(oauth.searchParams.get('return_to')).toBe(target);
  expect(routerPath(oauth.searchParams.get('return_to')!)).toBe(`/app/material-query?result=${'a'.repeat(64)}`);
 });
 it('keeps explicit logout manual while preserving the destination', () => {
  vi.stubEnv('BASE_URL', '/studio/'); const url = new URL(sessionLoginRoute('/studio/app/a?result=abc', true), 'https://example.test');
  expect(url.pathname).toBe('/auth/login'); expect(url.searchParams.get('logged_out')).toBe('1'); expect(url.searchParams.get('return_to')).toBe('/studio/app/a?result=abc');
 });
 it.each(['https://evil.test', '//evil.test', '/elsewhere', '/studio/../elsewhere', '/studio/%2f%2fevil', '/studio/\\evil', '/studio/login?return_to=x', '/studio/auth/feishu/callback?code=secret'])('rejects external, sibling or authentication loops: %s', target => {
  vi.stubEnv('BASE_URL', '/studio/'); expect(safeLoginReturnTo(target)).toBe('/studio/');
 });
});


it('captures an in-memory query token before 401 clears auth and unmounts the result', () => {
 vi.stubEnv('BASE_URL', '/studio/'); const location = { pathname: '/studio/app/barcode-query', search: '', hash: '', href: '' }; vi.stubGlobal('window', {location});
 const unregister = registerSessionLoginReturnTo('/studio/app/barcode-query?result=original-token');
 const captured = captureSessionLoginReturnTo(); unregister(); // clearAuth may synchronously unmount subscribers
 navigateToSessionLogin(captured);
 const login = new URL(location.href, 'https://example.test'); expect(login.searchParams.get('return_to')).toBe('/studio/app/barcode-query?result=original-token');
 expect(captureSessionLoginReturnTo()).toBe('/studio/app/barcode-query');
});
it('does not carry an old query recovery token into another route', () => {
 vi.stubEnv('BASE_URL', '/studio/'); const location = {pathname:'/studio/app/barcode-query',search:'',hash:''};vi.stubGlobal('window',{location});
 const unregister = registerSessionLoginReturnTo('/studio/app/barcode-query?result=old');location.pathname='/studio/apps';expect(captureSessionLoginReturnTo()).toBe('/studio/apps');unregister();
});
