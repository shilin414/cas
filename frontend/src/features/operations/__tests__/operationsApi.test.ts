import { beforeEach, describe, expect, it, vi } from 'vitest';
const http = vi.hoisted(() => ({ get: vi.fn(), patch: vi.fn() }));
vi.mock('@/services/axios', () => ({ default: http }));
import { operationsApi, buildRunsParams } from '../operationsApi';

const emptySnapshot = { sampled_at: '2026-09-22T00:00:00Z', providers: [], alerts: [], limitations: [], totals: { queued: 0, running: 0, waiting_input: 0, waiting_external: 0, cancelling: 0, pending_occurrences: 0, pending_deliveries: 0, sending_deliveries: 0, pending_outbox: 0 } };
beforeEach(() => { vi.resetAllMocks(); });
describe('operations API contract (offline)', () => {
  it('uses active/50 defaults without offset pagination', () => {
    expect(buildRunsParams({})).toEqual({ status: 'active', limit: 50 });
  });
  it('preserves large IDs and opaque cursors byte for byte', async () => {
    const params = { status: 'all' as const, provider: 'aily', owner_user_id: '90071992547409931234', application_id: '90071992547409931235', limit: 100, cursor: 'opaque+/== x' };
    http.get.mockResolvedValue({ results: [], next_cursor: null });
    const signal = new AbortController().signal;
    await operationsApi.runs(params, signal);
    expect(http.get).toHaveBeenCalledWith('/v2/admin/operations/runs', { params, signal, silentError: true });
  });
  it.each(['0', '-1', '1.2', '1e9', 'abc'])('rejects invalid decimal IDs %s', (id) => {
    expect(() => buildRunsParams({ owner_user_id: id })).toThrow();
    expect(() => buildRunsParams({ application_id: id })).toThrow();
  });
  it.each([0, 101, 1.5, NaN])('rejects invalid limits %s', (limit) => {
    expect(() => buildRunsParams({ limit })).toThrow();
  });
  it('uses shared session HTTP with abort and no raw error toast', async () => {
    http.get.mockResolvedValue(emptySnapshot); const signal = new AbortController().signal;
    await operationsApi.overview(signal);
    expect(http.get).toHaveBeenCalledWith('/v2/admin/operations/overview', { signal, silentError: true });
  });
  it('encodes provider path and sends optimistic capacity + reason', async () => {
    const body = { max_inflight: 3, expected_max_inflight: 10, reason: '减小并发' };
    http.patch.mockResolvedValue({ provider: 'a/b', previous_max_inflight: 10, max_inflight: 3, effective_for: 'new_admissions', updated_at: '2026-09-22T00:00:00Z' });
    const signal = new AbortController().signal;
    await operationsApi.updateCapacity('a/b', body, signal);
    expect(http.patch).toHaveBeenCalledWith('/v2/admin/operations/providers/a%2Fb/capacity', body, { silentError: true, signal });
  });
  it.each([0, 10001, 1.5])('does not send invalid capacity %s', async (max_inflight) => {
    await expect(operationsApi.updateCapacity('aily', { max_inflight, expected_max_inflight: 10, reason: 'test' })).rejects.toThrow();
    expect(http.patch).not.toHaveBeenCalled();
  });
  it.each(['', '   ', 'x'.repeat(501)])('rejects missing/oversized reason', async (reason) => {
    await expect(operationsApi.updateCapacity('aily', { max_inflight: 3, expected_max_inflight: 10, reason })).rejects.toThrow();
    expect(http.patch).not.toHaveBeenCalled();
  });
});

describe('additional API boundaries', () => {
  it('rejects numeric response IDs instead of rounding/stringifying them', async () => {
    http.get.mockResolvedValue({ results: [{ id: 9007199254740992, owner_user_id: null, application_id: null, conversation_id: null }], next_cursor: null });
    await expect(operationsApi.runs()).rejects.toThrow('字符串 ID');
  });
  it.each(['owner_user_id', 'application_id', 'conversation_id'])('rejects numeric %s', async key => {
    http.get.mockResolvedValue({ results: [{ id: '90071992547409931234', owner_user_id: null, application_id: null, conversation_id: null, [key]: 123 }], next_cursor: null });
    await expect(operationsApi.runs()).rejects.toThrow('字符串 ID');
  });
  it('preserves string response IDs and nullable associated IDs', async () => {
    const result = { results: [{ id: '90071992547409931234', owner_user_id: '90071992547409931235', application_id: null, conversation_id: null }], next_cursor: 'opaque+=' };
    http.get.mockResolvedValue(result); await expect(operationsApi.runs()).resolves.toEqual(result);
  });
  it.each([-1, 1.5, Infinity])('rejects an invalid optimistic concurrency value %s', async expected_max_inflight => {
    await expect(operationsApi.updateCapacity('aily', { max_inflight: 3, expected_max_inflight, reason: 'test' })).rejects.toThrow();
    expect(http.patch).not.toHaveBeenCalled();
  });
  it('accepts boundary capacities and exactly 500 Unicode characters', async () => {
    http.patch.mockImplementation(async (_url: string, change: { expected_max_inflight: number; max_inflight: number }) => ({ provider: 'aily', previous_max_inflight: change.expected_max_inflight, max_inflight: change.max_inflight, effective_for: 'new_admissions', updated_at: '2026-09-22T00:00:00Z' }));
    await operationsApi.updateCapacity('aily', { max_inflight: 1, expected_max_inflight: 10, reason: '低' });
    await operationsApi.updateCapacity('aily', { max_inflight: 10000, expected_max_inflight: 1, reason: '😀'.repeat(500) });
    expect(http.patch).toHaveBeenCalledTimes(2);
  });
});

it('accepts expected_max_inflight=0 to repair invalid previous configuration', async () => {
  const change = { max_inflight: 5, expected_max_inflight: 0, reason: '修复无效旧值' };
  http.patch.mockImplementation(async (_url: string, change: { expected_max_inflight: number; max_inflight: number }) => ({ provider: 'aily', previous_max_inflight: change.expected_max_inflight, max_inflight: change.max_inflight, effective_for: 'new_admissions', updated_at: '2026-09-22T00:00:00Z' }));
  await operationsApi.updateCapacity('aily', change);
  expect(http.patch).toHaveBeenCalledWith('/v2/admin/operations/providers/aily/capacity', change, { signal: undefined, silentError: true });
});


describe('untrusted success response boundaries', () => {
  it.each([null, {}, '<html>gateway error</html>', { sampled_at: 'now', providers: [], totals: {}, alerts: [], limitations: [] }])('rejects malformed snapshots instead of crashing or fabricating zeros', async value => {
    http.get.mockResolvedValue(value);
    await expect(operationsApi.overview()).rejects.toThrow();
  });
  it.each([null, {}, '<html>gateway error</html>', { provider: 'aily', previous_max_inflight: 20, max_inflight: 999, effective_for: 'new_admissions', updated_at: '2026-09-22T00:00:00Z' }])('does not announce a malformed or mismatched mutation response as success', async value => {
    http.patch.mockResolvedValue(value);
    await expect(operationsApi.updateCapacity('aily', { max_inflight: 10, expected_max_inflight: 20, reason: 'test' })).rejects.toThrow();
  });
});
