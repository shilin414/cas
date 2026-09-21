// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  patch: vi.fn(),
  delete: vi.fn(),
}));

vi.mock('@/services/api', () => ({ api: mocks }));

import { enterpriseApi } from '../enterpriseApi';

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const OLD_JOBS = [{ id: 1, target_code: 'directory' }];
const FRESH_JOBS = [{ id: 2, target_code: 'directory' }];

beforeEach(() => {
  Object.values(mocks).forEach((mock) => mock.mockReset());
});

describe('enterprise sync capability request options', () => {
  it('forwards silentError to the shared Axios layer', async () => {
    mocks.get.mockResolvedValue([]);

    await enterpriseApi.syncTargets({ silentError: true, fresh: true });

    expect(mocks.get).toHaveBeenCalledWith(
      '/v2/admin/sync-targets',
      undefined,
      { silentError: true },
    );
  });
});

describe('enterprise sync read cache', () => {
  it('post-write fresh reads bypass an older in-flight deduped GET', async () => {
    const oldRequest = deferred<typeof OLD_JOBS>();
    mocks.get
      .mockImplementationOnce(() => oldRequest.promise)
      .mockResolvedValueOnce(FRESH_JOBS);
    mocks.post.mockResolvedValue({ id: 3, target_code: 'directory' });

    const stalePromise = enterpriseApi.syncJobs(50);
    await enterpriseApi.triggerSyncTarget('directory');
    const fresh = await enterpriseApi.syncJobs(50, { fresh: true });

    expect(mocks.get).toHaveBeenCalledTimes(2);
    expect(fresh).toBe(FRESH_JOBS);

    oldRequest.resolve(OLD_JOBS);
    await expect(stalePromise).resolves.toBe(OLD_JOBS);
    expect(fresh).not.toBe(OLD_JOBS);
  });
});
