import http from '@/services/axios';
import { assertCapacityResult, assertOverview } from './responseValidation';
import { RUN_STATUSES, type RunsQuery, type RunsPage, type OperationsOverview, type CapacityChange, type CapacityChangeResult } from './types';

const base = '/v2/admin/operations';

// IDs stay decimal strings end-to-end: never parseInt/Number, even for validation.
export function buildRunsParams(query: RunsQuery): RunsQuery {
  const status = query.status ?? 'active';
  const limit = query.limit ?? 50;
  if (!RUN_STATUSES.includes(status)) throw new Error('请选择有效的任务状态');
  if (!Number.isInteger(limit) || limit < 1 || limit > 100) throw new Error('每页条数必须为 1–100 的整数');
  const params: RunsQuery = { status, limit };
  for (const key of ['owner_user_id', 'application_id'] as const) {
    const value = query[key];
    if (value === undefined || value === '') continue;
    if (typeof value !== 'string' || !/^[1-9]\d*$/.test(value)) throw new Error('用户和应用 ID 必须为十进制正整数字符串');
    params[key] = value;
  }
  if (query.provider) params.provider = query.provider;
  if (query.cursor) params.cursor = query.cursor;
  return params;
}

export function validateCapacity(change: CapacityChange): void {
  if (!Number.isInteger(change.max_inflight) || change.max_inflight < 1 || change.max_inflight > 10000) throw new Error('新额度必须为 1–10000 的整数');
  if (!Number.isInteger(change.expected_max_inflight) || change.expected_max_inflight < 0) throw new Error('原额度无效，请刷新最新额度');
  if (!change.reason.trim()) throw new Error('请填写修改原因');
  if (Array.from(change.reason).length > 500) throw new Error('修改原因不能超过 500 个字符');
}

export const operationsApi = {
  overview: async (signal?: AbortSignal): Promise<OperationsOverview> => {
    const result: unknown = await http.get(`${base}/overview`, { signal, silentError: true });
    assertOverview(result);
    return result;
  },
  runs: async (query: RunsQuery = {}, signal?: AbortSignal): Promise<RunsPage> => {
    const result = await http.get(`${base}/runs`, { params: buildRunsParams(query), signal, silentError: true }) as unknown as RunsPage;
    // A numeric backend ID has already lost precision; reject rather than stringify it.
    if (!Array.isArray(result.results) || (result.next_cursor !== null && typeof result.next_cursor !== 'string') || result.results.some(run =>
      typeof run.id !== 'string' || ['owner_user_id', 'application_id', 'conversation_id'].some(key => {
        const id = run[key as 'owner_user_id' | 'application_id' | 'conversation_id'];
        return id !== null && typeof id !== 'string';
      }))) throw new Error('运行任务响应不符合字符串 ID 契约');
    return result;
  },
  updateCapacity: async (provider: string, change: CapacityChange, signal?: AbortSignal): Promise<CapacityChangeResult> => {
    validateCapacity(change);
    // Same cookie/CSRF instance as services/api. PATCH needs options to suppress raw error bodies.
    const result: unknown = await http.patch(`${base}/providers/${encodeURIComponent(provider)}/capacity`, change, { signal, silentError: true });
    assertCapacityResult(result, provider, change);
    return result;
  },
};
