import { afterEach, describe, expect, it, vi } from 'vitest';
import { appPath, apiUrl, deploymentAssetUrl, routerPath } from '../deploymentPaths';

afterEach(() => vi.unstubAllEnvs());
describe('deployment URL boundaries', () => {
  it.each(['/xiaoan-platform/', '/other/nested/', '/'])('uses base %s consistently', (base) => {
    vi.stubEnv('BASE_URL', base);
    vi.stubEnv('VITE_API_BASE_URL', '');
    expect(appPath('/login')).toBe(`${base}login`);
    expect(apiUrl('/v2/runs/1/stream')).toBe(`${base}api/v2/runs/1/stream`);
    expect(deploymentAssetUrl('/api/v2/applications/1/avatar?v=2')).toBe(`${base}api/v2/applications/1/avatar?v=2`);
    expect(appPath(`${base}share/abc`)).toBe(`${base}share/abc`);
    expect(routerPath(`${base}chat/one?mode=x#last`)).toBe('/chat/one?mode=x#last');
  });
  it('keeps external images and explicit API bases intact', () => {
    vi.stubEnv('BASE_URL', '/studio/');
    vi.stubEnv('VITE_API_BASE_URL', 'https://api.example/gateway/api/');
    expect(apiUrl('/v2/runs')).toBe('https://api.example/gateway/api/v2/runs');
    expect(deploymentAssetUrl('/api/v2/avatar')).toBe('https://api.example/gateway/api/v2/avatar');
    for (const url of ['https://cdn.example/a.png', '//cdn.example/a.png', 'data:image/png;base64,abc', 'blob:abc', '']) {
      expect(deploymentAssetUrl(url)).toBe(url);
    }
  });
  it('rejects external and sibling OAuth return locations', () => {
    vi.stubEnv('BASE_URL', '/studio/');
    for (const target of ['//evil.example', '/studio-evil/chat', '/elsewhere', '/studio/\\evil.example', 'https://evil.example', '/studio//evil.example', '/studio/../outside', '/studio/%2e%2e/outside', '/studio/%2f%2fevil.example']) {
      expect(routerPath(target)).toBe('/');
    }
    expect(routerPath('/studio/')).toBe('/');
  });
});
