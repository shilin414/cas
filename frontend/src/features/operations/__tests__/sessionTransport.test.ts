// @vitest-environment jsdom
import { afterAll, beforeEach, describe, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ messageError: vi.fn(), clearAuth: vi.fn(), navigate: vi.fn() }));
vi.mock('antd', () => ({ message: { error: mocks.messageError } }));
vi.mock('@/stores/useAuthStore', () => ({ useAuthStore: { getState: () => ({ isLoggingOut: false, explicitlyLoggedOut: false, clearAuth: mocks.clearAuth }) } }));
vi.mock('@/services/authRedirect', () => ({ navigateToSessionLogin: mocks.navigate, captureSessionLoginReturnTo: () => '/enterprise/operations' }));
import http from '@/services/axios';
import { operationsApi } from '../operationsApi';
const originalAdapter = http.defaults.adapter;
beforeEach(() => { vi.clearAllMocks(); localStorage.clear(); document.cookie = 'studio_csrf=offline%2Bcsrf; path=/'; });
afterAll(() => { http.defaults.adapter = originalAdapter; document.cookie = 'studio_csrf=; max-age=0; path=/'; });

describe('actual shared session/CSRF interceptors with offline adapter', () => {
  it('keeps session cookies, CSRF, organization and the exact capacity payload', async () => {
    localStorage.setItem('organization-storage', JSON.stringify({ state: { currentOrganizationId: '90071992547409939999' } }));
    const body = { max_inflight: 8, expected_max_inflight: 10, reason: 'offline test' };
    const response = { provider: 'aily', previous_max_inflight: 10, max_inflight: 8, effective_for: 'new_admissions', updated_at: '2026-09-22T00:00:00Z' };
    const adapter = vi.fn(async config => ({ status: 200, statusText: 'OK', headers: {}, config, data: response }));
    http.defaults.adapter = adapter;
    await expect(operationsApi.updateCapacity('aily', body)).resolves.toEqual(response);
    const config = adapter.mock.calls[0][0];
    expect(config.withCredentials).toBe(true);
    expect(config.headers['X-CSRF-Token']).toBe('offline+csrf');
    expect(config.headers['X-Organization-ID']).toBe('90071992547409939999');
    expect(JSON.parse(config.data)).toEqual(body);
    expect(config.url).toBe('/v2/admin/operations/providers/aily/capacity');
  });
  it('lets the page own 409 without displaying server error text globally', async () => {
    http.defaults.adapter = async config => Promise.reject({ config, response: { status: 409, data: { detail: 'PRIVATE ERROR' }, config }, isAxiosError: true });
    await expect(operationsApi.updateCapacity('aily', { max_inflight: 5, expected_max_inflight: 10, reason: 'offline' })).rejects.toMatchObject({ response: { status: 409 } });
    expect(mocks.messageError).not.toHaveBeenCalled();
  });
  it('continues to honor the existing 401 authentication boundary', async () => {
    http.defaults.adapter = async config => Promise.reject({ config, response: { status: 401, data: {}, config }, isAxiosError: true });
    await expect(operationsApi.overview()).rejects.toMatchObject({ response: { status: 401 } });
    expect(mocks.clearAuth).toHaveBeenCalledOnce(); expect(mocks.navigate).toHaveBeenCalledWith('/enterprise/operations');
  });
});
