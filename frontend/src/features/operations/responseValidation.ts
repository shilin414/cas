import type { CapacityChange, CapacityChangeResult, OperationsOverview } from './types';

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('运行管理响应格式无效，请刷新核对');
  return value as Record<string, unknown>;
}
function timestamp(value: unknown): boolean { return typeof value === 'string' && Number.isFinite(Date.parse(value)); }
function counts(value: Record<string, unknown>, fields: string[]): void {
  if (fields.some(key => typeof value[key] !== 'number' || !Number.isSafeInteger(value[key]) || (value[key] as number) < 0)) throw new Error('运行管理数值不可确认，请刷新核对');
}
const runCounts = ['queued', 'running', 'waiting_input', 'waiting_external', 'cancelling'];
export function assertOverview(value: unknown): asserts value is OperationsOverview {
  const snapshot = record(value);
  if (!timestamp(snapshot.sampled_at) || !Array.isArray(snapshot.providers) || !Array.isArray(snapshot.alerts) || !Array.isArray(snapshot.limitations) || snapshot.limitations.some(item => typeof item !== 'string')) throw new Error('运行快照响应无效');
  counts(record(snapshot.totals), [...runCounts, 'pending_occurrences', 'pending_deliveries', 'sending_deliveries', 'pending_outbox']);
  for (const item of snapshot.providers) {
    const provider = record(item);
    if (['key', 'name', 'status'].some(key => typeof provider[key] !== 'string') || (provider.oldest_queued_at !== null && !timestamp(provider.oldest_queued_at))) throw new Error('Provider 快照响应无效');
    counts(provider, [...runCounts, 'max_inflight', 'effective_inflight', 'controlled_inflight', 'uncontrolled_inflight', 'oldest_queued_age_seconds']);
  }
  for (const item of snapshot.alerts) {
    const alert = record(item);
    if (typeof alert.code !== 'string' || typeof alert.message !== 'string' || !['critical', 'warning'].includes(String(alert.severity)) || (alert.provider !== undefined && typeof alert.provider !== 'string')) throw new Error('告警快照响应无效');
  }
}
export function assertCapacityResult(value: unknown, provider: string, input: CapacityChange): asserts value is CapacityChangeResult {
  const result = record(value);
  if (result.provider !== provider || result.max_inflight !== input.max_inflight || result.previous_max_inflight !== input.expected_max_inflight || result.effective_for !== 'new_admissions' || !timestamp(result.updated_at)) throw new Error('无法确认额度变更结果，请刷新核对实际额度');
}
