// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
const api = vi.hoisted(() => ({ overview: vi.fn(), runs: vi.fn(), updateCapacity: vi.fn() }));
vi.mock('../operationsApi', async (original) => ({ ...await original<object>(), operationsApi: api }));
// Block the shared transport even when indirectly imported by auth/permission stores.
vi.mock('@/services/axios', () => ({ default: { get: vi.fn(() => { throw new Error('Unexpected HTTP'); }), post: vi.fn(), patch: vi.fn() } }));
import OperationsPage from '../OperationsPage';
import { useAuthStore } from '@/stores/useAuthStore';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { resetSessionScopedState } from '@/stores/resetSessionState';
import { filterEnterpriseSections } from '@/pages/Enterprise/enterpriseNav';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
Object.defineProperty(window, 'matchMedia', { writable: true, value: () => ({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} }) });
// jsdom has no pseudo-element layout; AntD only probes it to measure scrollbar width.
const nativeComputedStyle = window.getComputedStyle.bind(window);
window.getComputedStyle = (element) => nativeComputedStyle(element);
globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
const sampled = '2026-09-22T08:00:00Z';
const provider = { key: 'aily', name: 'Aily', status: 'active', max_inflight: 10, effective_inflight: 7, controlled_inflight: 5, uncontrolled_inflight: 4, queued: 2, running: 6, waiting_input: 1, waiting_external: 0, cancelling: 0, oldest_queued_at: sampled, oldest_queued_age_seconds: 80 };
const snapshot = { sampled_at: sampled, providers: [provider], totals: { queued: 2, running: 6, waiting_input: 1, waiting_external: 0, cancelling: 0, pending_occurrences: 3, pending_deliveries: 4, sending_deliveries: 1, pending_outbox: 5 }, alerts: [{ code: 'queue_backlog', severity: 'warning', provider: 'aily', message: '存在排队积压' }], limitations: ['快照不代表实时队列位置'] };
const run = { id: '90071992547409931234', provider: 'aily', runtime_type: 'chat', status: 'queued', priority: 'normal', trigger_type: 'manual', owner_user_id: '90071992547409931235', application_id: '90071992547409931236', conversation_id: null, queued_at: sampled, available_at: null, started_at: null, finished_at: null, created_at: sampled, attempt: 0, max_attempts: 3, error_code: 'QUOTA', queue_age_seconds: 80, wait_reason: 'provider_paused', prompt: 'PRIVATE PROMPT', output: 'PRIVATE OUTPUT', error: 'PRIVATE ERROR' };
let root: Root; let host: HTMLDivElement;
function identity(permissions = ['run.monitor.read'], userId = '1', staff = false) {
  useAuthStore.setState({ user: { id: userId, username: 'tester', email: '', role: '', created_at: '', is_staff: staff } });
  useAdminPermissionStore.setState({ loadedForUserId: userId, identity: { can_access_console: true, is_super_admin: false, roles: [], permissions: permissions.map((code, i) => ({ id: i, code, category: '', name: '', description: '', created_at: '' })) } });
}
async function render(mobile = false) { await act(async () => root.render(<OperationsPage mobile={mobile} />)); }
function button(text: string) { const result = Array.from(document.querySelectorAll('button')).find((item) => item.textContent?.replace(/\s/g, '') === text.replace(/\s/g, '')); expect(result, `button ${text}`).toBeTruthy(); return result!; }
async function click(text: string) { await act(async () => button(text).click()); }
async function input(label: string, value: string) {
  const element = document.querySelector(`[aria-label="${label}"]`) as HTMLInputElement | HTMLTextAreaElement;
  expect(element).toBeTruthy();
  await act(async () => { Object.getOwnPropertyDescriptor(element.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype, 'value')!.set!.call(element, value); element.dispatchEvent(new Event('input', { bubbles: true })); });
}
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (error: unknown) => void; const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; }); return { promise, resolve, reject }; }
beforeEach(() => {
  vi.resetAllMocks(); identity();
  api.overview.mockResolvedValue(snapshot); api.runs.mockResolvedValue({ results: [run], next_cursor: null });
  api.updateCapacity.mockResolvedValue({ provider: 'aily', previous_max_inflight: 10, max_inflight: 3, effective_for: 'new_admissions', updated_at: sampled });
  host = document.createElement('div'); document.body.appendChild(host); root = createRoot(host);
});
afterEach(async () => { await act(async () => root.unmount()); host.remove(); vi.useRealTimers(); });

describe('operations console', () => {
  it('gates navigation and direct page access, not by is_staff', async () => {
    expect(filterEnterpriseSections(new Set(), false).flatMap(s => s.items).some(i => i.key === 'operations')).toBe(false);
    expect(filterEnterpriseSections(new Set(['run.monitor.read']), false).flatMap(s => s.items).some(i => i.key === 'operations')).toBe(true);
    identity([], '1', true); await render();
    expect(host.textContent).toContain('run.monitor.read'); expect(api.overview).not.toHaveBeenCalled(); expect(api.runs).not.toHaveBeenCalled();
  });
  it.each([false, true])('renders capacity, alerts and metadata only (mobile=%s)', async (mobile) => {
    await render(mobile);
    expect(host.textContent).toContain('存在排队积压'); expect(host.textContent).toContain('快照不代表实时队列位置');
    expect(host.textContent).toContain(run.id); expect(host.textContent).toContain(run.owner_user_id);
    expect(host.textContent).toContain('Provider 已暂停');
    expect(host.querySelector('[data-testid="effective-capacity"]')?.textContent).toContain('7 / 10');
    expect(host.textContent).not.toMatch(/PRIVATE|精确队列位置|预计完成/);
    expect(host.textContent).not.toContain('修改额度');
    expect(host.querySelector('[data-testid="run-cards"]') !== null).toBe(mobile);
  });
  it('shows loading, snapshot error without zero fallback, and retry/empty', async () => {
    const pending = deferred<typeof snapshot>(); api.overview.mockReturnValueOnce(pending.promise); api.runs.mockResolvedValue({ results: [], next_cursor: null });
    await render(); expect(host.textContent).toContain('正在加载运行快照'); expect(host.textContent).toContain('暂无匹配任务');
    await act(async () => pending.reject({ response: { data: { detail: 'PRIVATE ERROR' } } }));
    expect(host.textContent).toContain('运行快照加载失败'); expect(host.querySelector('[data-testid="effective-capacity"]')).toBeNull();
    expect(host.textContent).not.toContain('PRIVATE'); await click('手动刷新'); expect(host.textContent).toContain('7 / 10');
  });
  it('handles task failure independently without rendering a false empty state', async () => {
    api.runs.mockRejectedValueOnce(new Error('PRIVATE ERROR')); await render();
    expect(host.textContent).toContain('任务加载失败'); expect(host.textContent).not.toContain('暂无匹配任务');
    expect(host.textContent).toContain('7 / 10'); await click('重新加载任务'); expect(host.textContent).toContain(run.id);
  });
  it('paginates with opaque cursors and resets cursor on filtering and refresh', async () => {
    api.runs.mockResolvedValueOnce({ results: [run], next_cursor: 'opaque+/=' }).mockResolvedValue({ results: [{ ...run, id: '99' }], next_cursor: null });
    await render(); await click('下一页'); expect(api.runs.mock.calls[api.runs.mock.calls.length - 1]?.[0].cursor).toBe('opaque+/='); expect(host.textContent).not.toContain(run.id);
    await input('所属用户 ID', '90071992547409939999'); await input('应用 ID', '90071992547409938888'); await click('应用筛选');
    expect(api.runs.mock.calls[api.runs.mock.calls.length - 1]?.[0]).toMatchObject({ owner_user_id: '90071992547409939999', application_id: '90071992547409938888' });
    expect(api.runs.mock.calls[api.runs.mock.calls.length - 1]?.[0].cursor).toBeUndefined();
    await click('手动刷新'); expect(api.runs.mock.calls[api.runs.mock.calls.length - 1]?.[0].cursor).toBeUndefined();
  });
  it('validates filters before sending a request', async () => {
    await render(); await input('所属用户 ID', '1e9'); await click('应用筛选');
    expect(host.textContent).toContain('十进制正整数字符串'); expect(api.runs).toHaveBeenCalledTimes(1);
  });
  it('never polls automatically', async () => {
    await render(); vi.useFakeTimers(); await act(async () => { await vi.advanceTimersByTimeAsync(120000); });
    expect(api.overview).toHaveBeenCalledTimes(1); expect(api.runs).toHaveBeenCalledTimes(1);
  });
  it('requires reason and confirmation before PATCH, then refreshes overview', async () => {
    identity(['run.monitor.read', 'provider.manage']); await render(); await click('修改额度');
    await click('继续确认'); expect(api.updateCapacity).not.toHaveBeenCalled(); expect(document.body.textContent).toContain('请填写修改原因');
    await input('新额度', '3'); await input('修改原因', '降低并发'); await click('继续确认');
    expect(api.updateCapacity).not.toHaveBeenCalled(); expect(document.body.textContent).toContain('不强杀'); expect(document.body.textContent).toContain('容量验收');
    await click('确认修改');
    expect(api.updateCapacity.mock.calls[0].slice(0, 2)).toEqual(['aily', { max_inflight: 3, expected_max_inflight: 10, reason: '降低并发' }]);
    expect(api.overview).toHaveBeenCalledTimes(2); expect(host.textContent).toContain('额度已更新');
  });
  it('handles 409 conflict without success or silent overwrite', async () => {
    identity(['run.monitor.read', 'provider.manage']); api.updateCapacity.mockRejectedValue({ response: { status: 409, data: { detail: 'PRIVATE ERROR' } } });
    await render(); await click('修改额度'); await input('修改原因', '调整并发'); await click('继续确认'); await click('确认修改');
    expect(document.body.textContent).toContain('409'); expect(document.body.textContent).not.toContain('PRIVATE'); expect(document.body.textContent).not.toContain('额度已更新');
    expect(button('确认修改').disabled).toBe(true); await click('刷新最新额度'); expect(api.overview).toHaveBeenCalledTimes(2);
  });
  it('discards late old-account reads and clears rendered data on session reset', async () => {
    const old = deferred<typeof snapshot>(); const oldRuns = deferred<{ results: typeof run[]; next_cursor: null }>();
    api.overview.mockReturnValueOnce(old.promise); api.runs.mockReturnValueOnce(oldRuns.promise); await render();
    await act(async () => { resetSessionScopedState(); identity(['run.monitor.read'], '2'); });
    expect(host.textContent).toContain('7 / 10');
    await act(async () => { old.resolve({ ...snapshot, providers: [{ ...provider, name: 'PRIVATE OLD PROVIDER' }] }); oldRuns.resolve({ results: [{ ...run, id: 'OLD-ACCOUNT-ID' }], next_cursor: null }); });
    expect(host.textContent).not.toContain('PRIVATE OLD'); expect(host.textContent).not.toContain('OLD-ACCOUNT-ID');
    await act(async () => resetSessionScopedState()); expect(host.textContent).not.toContain(run.id);
  });
  it('discards stale mutation success after account switch', async () => {
    const pending = deferred<unknown>(); api.updateCapacity.mockReturnValue(pending.promise); identity(['run.monitor.read', 'provider.manage']);
    await render(); await click('修改额度'); await input('修改原因', '测试'); await click('继续确认'); await click('确认修改');
    await act(async () => { resetSessionScopedState(); identity(['run.monitor.read'], '2'); });
    const count = api.overview.mock.calls.length; await act(async () => pending.resolve({}));
    expect(api.overview).toHaveBeenCalledTimes(count); expect(document.body.textContent).not.toContain('额度已更新');
  });
});

async function select(label: string, value: string) {
  const element = document.querySelector(`[aria-label="${label}"]`) as HTMLSelectElement;
  await act(async () => { element.value = value; element.dispatchEvent(new Event('change', { bubbles: true })); });
}
describe('race conditions and boundary states', () => {
  it('filters status/provider/limit and resets back to the contract defaults', async () => {
    await render(); await select('任务状态', 'failed'); await select('每页条数', '25'); await input('Provider 筛选', 'coze'); await click('应用筛选');
    expect(api.runs.mock.calls[1][0]).toEqual({ status: 'failed', provider: 'coze', limit: 25 });
    await click('重置筛选'); expect(api.runs.mock.calls[2][0]).toEqual({ status: 'active', limit: 50 });
  });
  it('offers previous page without offset or an invented total page count', async () => {
    api.runs.mockResolvedValueOnce({ results: [run], next_cursor: 'next' }).mockResolvedValueOnce({ results: [], next_cursor: null }).mockResolvedValue({ results: [run], next_cursor: 'next' });
    await render(); expect(button('上一页').disabled).toBe(true); await click('下一页'); expect(button('下一页').disabled).toBe(true);
    await click('上一页'); expect(api.runs.mock.calls[2][0].cursor).toBeUndefined(); expect(host.textContent).toContain(run.id);
  });
  it('does not allow an old filtered request to overwrite the new list', async () => {
    const old = deferred<{ results: typeof run[]; next_cursor: null }>(); api.runs.mockReturnValueOnce(old.promise);
    await render(); expect(host.textContent).toContain('正在加载任务'); await input('Provider 筛选', 'coze'); await click('应用筛选');
    expect(api.runs.mock.calls[0][1].aborted).toBe(true);
    await act(async () => old.resolve({ results: [{ ...run, id: 'STALE-FILTER-ROW' }], next_cursor: null }));
    expect(host.textContent).not.toContain('STALE-FILTER-ROW'); expect(host.textContent).toContain(run.id);
  });
  it('does not allow a superseded snapshot error to replace a successful refresh', async () => {
    const old = deferred<typeof snapshot>(); api.overview.mockReturnValueOnce(old.promise); await render(); await click('手动刷新');
    expect(api.overview.mock.calls[0][0].aborted).toBe(true); await act(async () => old.reject(new Error('stale error')));
    expect(host.textContent).toContain('7 / 10'); expect(host.textContent).not.toContain('运行快照加载失败');
  });
  it('clears a previously successful snapshot after a failed refresh', async () => {
    await render(); api.overview.mockRejectedValueOnce(new Error('PRIVATE')); await click('手动刷新');
    expect(host.querySelector('[data-testid="effective-capacity"]')).toBeNull(); expect(host.textContent).not.toContain('存在排队积压');
  });
  it('shows empty provider/alert states without inventing capacity', async () => {
    api.overview.mockResolvedValue({ ...snapshot, providers: [], alerts: [], limitations: [] }); await render(true);
    expect(host.textContent).toContain('暂无 Provider 容量数据'); expect(host.textContent).toContain('暂无状态告警');
    expect(host.querySelector('[data-testid="effective-capacity"]')?.textContent).toBe('—');
  });
  it('ignores unknown wait reason and error text rather than exposing raw content', async () => {
    api.runs.mockResolvedValue({ results: [{ ...run, wait_reason: 'PRIVATE WAIT DETAILS', error_code: 'PRIVATE ERROR DETAILS' }], next_cursor: null }); await render();
    expect(host.textContent).toContain('未提供可验证等待原因'); expect(host.textContent).not.toContain('PRIVATE');
  });
  it('does not admit manage-only or an identity snapshot belonging to another account', async () => {
    identity(['provider.manage']); await render(); expect(api.overview).not.toHaveBeenCalled();
    await act(async () => { identity(['run.monitor.read']); useAdminPermissionStore.setState({ loadedForUserId: 'previous-user' }); });
    expect(api.overview).not.toHaveBeenCalled();
  });
  it('removes the open mutation dialog when manage permission is revoked', async () => {
    identity(['run.monitor.read', 'provider.manage']); await render(); await click('修改额度');
    await act(async () => identity(['run.monitor.read']));
    expect(document.querySelector('[aria-label="修改原因"]')).toBeNull(); expect(host.textContent).not.toContain('修改额度');
  });
  it.each(['0', '10001', '1.5'])('blocks invalid capacity %s before confirmation', async value => {
    identity(['run.monitor.read', 'provider.manage']); await render(); await click('修改额度'); await input('新额度', value); await input('修改原因', 'reason'); await click('继续确认');
    expect(document.body.textContent).toContain('1–10000 的整数'); expect(api.updateCapacity).not.toHaveBeenCalled();
  });
  it('blocks oversized reasons and leaves cancellation without a PATCH', async () => {
    identity(['run.monitor.read', 'provider.manage']); await render(); await click('修改额度'); await input('修改原因', 'x'.repeat(501)); await click('继续确认');
    expect(document.body.textContent).toContain('不能超过 500'); await click('取消'); expect(api.updateCapacity).not.toHaveBeenCalled();
  });
  it('shows ordinary mutation failure safely, never claims success', async () => {
    identity(['run.monitor.read', 'provider.manage']); api.updateCapacity.mockRejectedValue({ response: { status: 500, data: { detail: 'PRIVATE ERROR' } } });
    await render(true); await click('修改额度'); await input('修改原因', '测试'); await click('继续确认'); await click('确认修改');
    expect(document.body.textContent).toContain('额度修改失败'); expect(document.body.textContent).not.toContain('PRIVATE'); expect(document.body.textContent).not.toContain('额度已更新');
    expect(api.overview).toHaveBeenCalledTimes(1);
  });
  it('deduplicates concurrent confirmation clicks', async () => {
    identity(['run.monitor.read', 'provider.manage']); const pending = deferred<unknown>(); api.updateCapacity.mockReturnValue(pending.promise);
    await render(); await click('修改额度'); await input('修改原因', '测试'); await click('继续确认');
    await act(async () => { button('确认修改').click(); button('确认修改').click(); }); expect(api.updateCapacity).toHaveBeenCalledTimes(1);
    await act(async () => pending.resolve({}));
  });
  it('ignores old-account mutation failure as well as success', async () => {
    const pending = deferred<unknown>(); api.updateCapacity.mockReturnValue(pending.promise); identity(['run.monitor.read', 'provider.manage']);
    await render(); await click('修改额度'); await input('修改原因', '测试'); await click('继续确认'); await click('确认修改');
    await act(async () => { resetSessionScopedState(); identity(['run.monitor.read'], '2'); });
    await act(async () => pending.reject({ response: { status: 409 } }));
    expect(document.body.textContent).not.toContain('额度已被其他管理员修改'); expect(api.updateCapacity.mock.calls[0][2].aborted).toBe(true);
  });
});

describe('pagination interaction regression', () => {
  it('does not append the same cursor twice on rapid next-page clicks', async () => {
    api.runs.mockResolvedValueOnce({ results: [run], next_cursor: 'page-two' }).mockResolvedValue({ results: [{ ...run, id: 'second-page' }], next_cursor: null });
    await render(); await act(async () => { button('下一页').click(); button('下一页').click(); });
    expect(host.textContent).toContain('第 2 页'); expect(host.textContent).not.toContain('第 3 页');
  });
});

it('moves back only one page on rapid previous-page clicks', async () => {
  api.runs.mockResolvedValueOnce({ results: [run], next_cursor: 'page-two' }).mockResolvedValueOnce({ results: [run], next_cursor: 'page-three' }).mockResolvedValueOnce({ results: [run], next_cursor: null }).mockResolvedValue({ results: [run], next_cursor: 'page-three' });
  await render(); await click('下一页'); await click('下一页');
  await act(async () => { button('上一页').click(); button('上一页').click(); });
  expect(host.textContent).toContain('第 2 页');
});

describe('backend contract clarifications', () => {
  it.each([false, true])('disables editing missing providers (mobile=%s)', async mobile => {
    identity(['run.monitor.read', 'provider.manage']);
    api.overview.mockResolvedValue({ ...snapshot, providers: [{ ...provider, status: 'missing', max_inflight: 0 }] });
    await render(mobile); expect(host.textContent).toContain('配置缺失'); expect(button('修改额度').disabled).toBe(true);
    expect(api.updateCapacity).not.toHaveBeenCalled();
  });
  it('shows provider_unavailable as a verified waiting fact', async () => {
    api.runs.mockResolvedValue({ results: [{ ...run, wait_reason: 'provider_unavailable' }], next_cursor: null });
    await render(); expect(host.textContent).toContain('Provider 不可用');
  });
  it('can repair a present provider whose old max is zero', async () => {
    identity(['run.monitor.read', 'provider.manage']);
    api.overview.mockResolvedValue({ ...snapshot, providers: [{ ...provider, max_inflight: 0 }] });
    await render(); await click('修改额度'); await input('新额度', '5'); await input('修改原因', '修复旧值'); await click('继续确认'); await click('确认修改');
    expect(api.updateCapacity.mock.calls[0][1]).toEqual({ max_inflight: 5, expected_max_inflight: 0, reason: '修复旧值' });
  });
});
