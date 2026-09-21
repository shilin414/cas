import { afterAll, beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  messageError: vi.fn(),
  clearAuth: vi.fn(),
  navigateToSessionLogin: vi.fn(),
}));

vi.mock('antd', () => ({
  message: { error: mocks.messageError },
}));

vi.mock('@/stores/useAuthStore', () => ({
  useAuthStore: {
    getState: () => ({
      isLoggingOut: false,
      explicitlyLoggedOut: false,
      clearAuth: mocks.clearAuth,
    }),
  },
}));

vi.mock('@/services/authRedirect', () => ({
  navigateToSessionLogin: mocks.navigateToSessionLogin,
  captureSessionLoginReturnTo: () => '/app/barcode-query?result=original-token',
}));

import axiosInstance from '@/services/axios';

const originalAdapter = axiosInstance.defaults.adapter;

function rejectWith(status: number) {
  axiosInstance.defaults.adapter = async (config) => Promise.reject({
    config,
    response: { status, data: { detail: 'capability unavailable' }, headers: {}, config },
    isAxiosError: true,
  });
}

beforeEach(() => {
  mocks.messageError.mockReset();
  rejectWith(404);
});

afterAll(() => {
  axiosInstance.defaults.adapter = originalAdapter;
});

describe('axios silentError requests', () => {
  it('suppresses shared interceptor notifications for an expected capability probe', async () => {
    await expect(axiosInstance.get('/v2/admin/sync-targets', {
      silentError: true,
    })).rejects.toBeTruthy();

    expect(mocks.messageError).not.toHaveBeenCalled();
  });

  it('keeps normal shared notifications for ordinary requests', async () => {
    await expect(axiosInstance.get('/v2/missing-resource')).rejects.toBeTruthy();

    expect(mocks.messageError).toHaveBeenCalledWith('请求错误，未找到该资源');
  });
});
