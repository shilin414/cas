// @vitest-environment jsdom
import React from 'react';
import { act } from 'react-dom/test-utils';
import { createRoot, type Root } from 'react-dom/client';
import { Form, Input, message } from 'antd';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useScheduleEditor, type ScheduleEditorState, type UseScheduleEditorOptions } from '../useScheduleEditor';
import { ScheduleEditorFields } from '../ScheduleEditorFields';
import type { Schedule } from '@/types/schedule';

const mocks = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), preview: vi.fn(), detail: vi.fn() }));
vi.mock('@/services/scheduleApi', () => ({ createSchedule: mocks.create, updateSchedule: mocks.update, previewScheduleRuns: mocks.preview, fetchSchedule: mocks.detail }));
vi.mock('@/services/runApi', () => ({ resolveApplication: vi.fn() }));
vi.mock('@/hooks/useApplicationPage', () => ({ useApplicationPage: () => ({ items: [{ id: 7, name: 'Agent' }], loading: false, refresh: vi.fn() }) }));
vi.mock('@/hooks/useFeishuTargets', () => ({ useFeishuTargets: () => ({ items: [], loading: false, status: 'idle', refresh: vi.fn() }) }));

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true,
  matchMedia: () => ({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} }),
});
let state: ScheduleEditorState;
let root: Root;
let host: HTMLElement;
const base = {
  id: 1, name: '自动化', description: '', application_id: 7, prompt: 'Reply', schedule_type: 'daily',
  timezone: 'Asia/Shanghai', trigger: { time: '09:00' }, run_at: null,
  conversation_policy: 'reuse', overlap_policy: 'skip', misfire_policy: 'skip', deadline_policy: 'skip',
  execution_window_seconds: 120, deliveries: [
    { id: 1, target_type: 'chat', target_id: 'c', target_name: '群', content_mode: 'summary', enabled: true, condition: { operator: 'contains', text: ' ALERT ' } },
    { id: 2, target_type: 'user', target_id: 'u', target_name: '人', content_mode: 'summary', enabled: true, condition: { operator: 'not_contains', text: '正常' } },
  ],
} as Schedule;
function Harness(props: UseScheduleEditorOptions & { fields?: boolean; mobile?: boolean }) {
  state = useScheduleEditor(props);
  if (props.fields) return <ScheduleEditorFields state={state} mobile={props.mobile} />;
  // Deliberately register only visible inputs. Conditions, metadata, policies and bounds stay unregistered.
  return <Form form={state.form}><Form.Item name="name"><Input /></Form.Item><Form.Item name={['deliveries', 0, 'target_id']}><Input /></Form.Item></Form>;
}
async function render(editing: Schedule | null = base, open = true) {
  await act(async () => { root.render(<Harness open={open} editing={editing} presetApplicationId={7} onClose={() => {}} onSaved={() => {}} />); });
}
async function change(values: Parameters<ScheduleEditorState['form']['setFieldsValue']>[0]) {
  await act(async () => { state.form.setFieldsValue(values); });
}
beforeEach(() => {
  host = document.createElement('div'); document.body.appendChild(host); root = createRoot(host);
  mocks.detail.mockReset();
  mocks.create.mockReset().mockResolvedValue({ id: 3 });
  mocks.update.mockReset().mockResolvedValue({ id: 1 });
  mocks.preview.mockReset().mockResolvedValue(['2099-01-03T00:00:00Z']);
  vi.spyOn(message, 'error').mockImplementation(() => (() => {}) as ReturnType<typeof message.error>);
  vi.spyOn(message, 'success').mockImplementation(() => (() => {}) as ReturnType<typeof message.success>);
});
afterEach(async () => { await act(async () => { root.unmount(); }); host.remove(); vi.restoreAllMocks(); });

describe('automation editor state contract', () => {
  it('defaults new boundaries and submits the complete unregistered store', async () => {
    await render(null);
    expect(state.form.getFieldsValue(true)).toMatchObject({ start_mode: 'immediate', end_mode: 'never' });
    await change({ name: 'New', prompt: 'Reply', start_mode: 'specified', starts_at_local: '2099-01-01T09:00', end_mode: 'specified', ends_at_local: '2099-02-01T09:00' });
    await act(async () => { state.setDeliveryOn(true); });
    await change({ deliveries: [{ target_type: 'chat', target_id: 'c', target_name: '群', condition: { operator: 'contains', text: '  ALERT  ' } }] });
    await act(async () => { await state.handleOk(); });
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      trigger: { time: '09:00', starts_at: new Date('2099-01-01T09:00').toISOString(), ends_at: new Date('2099-02-01T09:00').toISOString() },
      misfire_policy: 'fire_once', deadline_policy: 'execute_anyway', execution_window_seconds: 0,
      deliveries: [{ target_type: 'chat', target_id: 'c', target_name: '群', content_mode: 'summary', condition: { operator: 'contains', text: '  ALERT  ' } }],
    }));
  });
  it('retains conditions, additional targets and policy settings when selecting a target', async () => {
    await render();
    await act(async () => { state.selectDeliveryTarget({ id: 'new', name: '新群', target_type: 'chat', avatar_url: '' }); });
    await act(async () => { await state.handleOk(); });
    const payload = mocks.update.mock.calls[0][1];
    expect(payload.deliveries).toHaveLength(2);
    expect(payload.deliveries[0]).toMatchObject({ target_id: 'new', target_name: '新群', target_type: 'chat', condition: { operator: 'contains', text: ' ALERT ' } });
    expect(payload.deliveries[1]).toMatchObject({ target_id: 'u', condition: { operator: 'not_contains', text: '正常' } });
    expect(payload).toMatchObject({ conversation_policy: 'reuse', overlap_policy: 'skip', misfire_policy: 'skip', deadline_policy: 'skip', execution_window_seconds: 120 });
    expect(state.targets).toContainEqual(expect.objectContaining({ id: 'new', name: '新群' }));
  });
  it('refuses silent multi-target truncation but allows explicit disabling', async () => {
    await render();
    await act(async () => { state.form.setFieldValue('deliveries', [state.form.getFieldValue('deliveries')[0]]); });
    await act(async () => { await state.handleOk(); });
    expect(mocks.update).not.toHaveBeenCalled();
    expect(message.error).toHaveBeenCalledWith(expect.stringContaining('多个投递目标'));
    await act(async () => { state.setDeliveryOn(false); });
    await act(async () => { await state.handleOk(); });
    expect(mocks.update.mock.calls[0][1].deliveries).toEqual([]);
  });
  it('ignores invalid hidden delivery conditions when notifications are off', async () => {
    await render(null);
    await change({ name: 'New', prompt: 'Reply', deliveries: [{ target_type: 'chat', target_id: 'c', target_name: '群', condition: { operator: 'contains', text: '' } }] });
    await act(async () => { await state.handleOk(); });
    expect(mocks.create.mock.calls[0][0].deliveries).toEqual([]);
  });
  it('blocks invalid bounds in both save and preview and exposes field errors', async () => {
    await render();
    const setFields = vi.spyOn(state.form, 'setFields');
    await change({ end_mode: 'specified', ends_at_local: '2000-01-01T08:00' });
    await act(async () => { await state.refreshPreview(); await state.handleOk(); });
    expect(mocks.preview).not.toHaveBeenCalled(); expect(mocks.update).not.toHaveBeenCalled();
    expect(setFields).toHaveBeenCalledWith([{ name: ['ends_at_local'], errors: ['结束时间必须晚于当前时间'] }]);
    expect(message.error).toHaveBeenCalledWith('结束时间必须晚于当前时间');
  });
  it('invalidates in-flight previews on unregistered bound changes', async () => {
    let resolve!: (v: string[]) => void;
    mocks.preview.mockReturnValueOnce(new Promise<string[]>(r => { resolve = r; }));
    await render();
    let request!: Promise<void>;
    await act(async () => { request = state.refreshPreview(); });
    expect(state.previewing).toBe(true);
    await change({ start_mode: 'specified', starts_at_local: '2099-01-01T08:00' });
    expect(state.previewing).toBe(false);
    await act(async () => { resolve(['2098-01-01T00:00:00Z']); await request; });
    expect(state.preview).toEqual([]);
    await act(async () => { await state.refreshPreview(); });
    expect(mocks.preview.mock.calls[1][0].trigger.starts_at).toBe(new Date('2099-01-01T08:00').toISOString());
  });
  it('discards previews across close/reopen even when schedule fields are identical', async () => {
    let resolve!: (v: string[]) => void;
    mocks.preview.mockReturnValueOnce(new Promise<string[]>(r => { resolve = r; }));
    await render();
    let request!: Promise<void>;
    await act(async () => { request = state.refreshPreview(); });
    await render(base, false); await render(base, true);
    await act(async () => { resolve(['2098-01-01T00:00:00Z']); await request; });
    expect(state.preview).toEqual([]);
  });
  it('does not send a stale save after async validation spans a session change', async () => {
    await render();
    let resolve!: () => void;
    vi.spyOn(state.form, 'validateFields').mockReturnValueOnce(new Promise(r => { resolve = () => r(state.form.getFieldsValue(true)); }));
    let request!: Promise<void>;
    await act(async () => { request = state.handleOk(); });
    await render({ ...base, id: 2, name: 'Other' });
    await act(async () => { resolve(); await request; });
    expect(mocks.update).not.toHaveBeenCalled();
  });
});


function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const { deliveries: _listOmittedDeliveries, ...listRow } = base;

describe('list-entrypoint detail hydration', () => {
  it('fetches a real list row before editing; title-only save keeps ALL deliveries and conditions', async () => {
    expect(listRow).not.toHaveProperty('deliveries');
    const detail = deferred<Schedule>(); mocks.detail.mockReturnValueOnce(detail.promise);
    await render(listRow);
    expect(mocks.detail).toHaveBeenCalledExactlyOnceWith(base.id);
    expect(state.hydrationLoading).toBe(true); expect(state.hydrationReady).toBe(false);
    await act(async () => { await state.handleOk(); await state.refreshPreview(); });
    expect(mocks.update).not.toHaveBeenCalled(); expect(mocks.preview).not.toHaveBeenCalled();
    await act(async () => { detail.resolve(base); });
    expect(state.hydrationReady).toBe(true); expect(state.deliveryOn).toBe(true);
    await change({ name: 'Only rename' });
    await act(async () => { await state.handleOk(); });
    expect(mocks.update).toHaveBeenCalledExactlyOnceWith(base.id, expect.objectContaining({
      name: 'Only rename', deliveries: base.deliveries!.map(({ target_type, target_id, target_name, condition }) => ({ target_type, target_id, target_name, condition, content_mode: 'summary' })),
    }));
    expect(state.targets).toContainEqual(expect.objectContaining({ id: 'c', name: '群' }));
  });
  it('leaves already-loaded detail and new-editor paths fetch-free', async () => {
    await render(base); expect(state.hydrationReady).toBe(true);
    await render({ ...base, deliveries: [] }); expect(state.hydrationReady).toBe(true);
    await render(null); expect(state.hydrationReady).toBe(true);
    expect(mocks.detail).not.toHaveBeenCalled();
  });
  it('fails closed for HTTP errors and retry loads the detail', async () => {
    mocks.detail.mockRejectedValueOnce(new Error('detail unavailable')).mockResolvedValueOnce(base);
    await render(listRow);
    expect(state.hydrationError).toBe('detail unavailable'); expect(state.hydrationReady).toBe(false);
    await act(async () => { await state.handleOk(); await state.refreshPreview(); });
    expect(mocks.update).not.toHaveBeenCalled(); expect(mocks.preview).not.toHaveBeenCalled();
    await act(async () => { state.retryHydration(); });
    expect(mocks.detail).toHaveBeenCalledTimes(2); expect(state.hydrationReady).toBe(true);
    await act(async () => { await state.handleOk(); });
    expect(mocks.update.mock.calls[0][1].deliveries).toHaveLength(2);
  });
  it('rejects partial detail responses rather than interpreting omitted recipients as empty', async () => {
    mocks.detail.mockResolvedValueOnce(listRow);
    await render(listRow);
    expect(state.hydrationError).toContain('投递配置'); expect(state.hydrationReady).toBe(false);
    await act(async () => { await state.handleOk(); await state.refreshPreview(); });
    expect(mocks.update).not.toHaveBeenCalled(); expect(mocks.preview).not.toHaveBeenCalled();
  });
  it('accepts an explicitly empty delivery array from the detail endpoint', async () => {
    mocks.detail.mockResolvedValueOnce({ ...base, deliveries: [] });
    await render(listRow);
    expect(state.hydrationReady).toBe(true); expect(state.deliveryOn).toBe(false);
    await act(async () => { await state.handleOk(); });
    expect(mocks.update.mock.calls[0][1].deliveries).toEqual([]);
  });
  it('uses hydrated detail as the multi-target deletion guard and mapping baseline', async () => {
    mocks.detail.mockResolvedValueOnce({ ...base, trigger: { time: '09:00', starts_at: '2000-01-01T00:00:00Z' } });
    await render(listRow);
    await act(async () => { state.form.setFieldValue('deliveries', [state.form.getFieldValue('deliveries')[0]]); await state.handleOk(); });
    expect(mocks.update).not.toHaveBeenCalled(); expect(message.error).toHaveBeenCalledWith(expect.stringContaining('多个投递目标'));
    await act(async () => { state.setDeliveryOn(false); });
    await act(async () => { await state.handleOk(); });
    expect(mocks.update.mock.calls[0][1].trigger.starts_at).toBe('2000-01-01T00:00:00.000Z');
  });
  it.each(['resolve', 'reject'] as const)('late A %s cannot overwrite hydrated B or its edits', async (outcome) => {
    const a = deferred<Schedule>(), b = deferred<Schedule>();
    mocks.detail.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);
    await render(listRow);
    await render({ ...listRow, id: 2, name: 'B' });
    await act(async () => { b.resolve({ ...base, id: 2, name: 'B', deliveries: [base.deliveries![1]] }); });
    await change({ name: 'B draft' });
    await act(async () => { if (outcome === 'resolve') a.resolve(base); else a.reject(new Error('old failure')); });
    expect(state.form.getFieldValue('name')).toBe('B draft'); expect(state.hydrationError).toBeNull();
    await act(async () => { await state.handleOk(); });
    expect(mocks.update).toHaveBeenCalledWith(2, expect.objectContaining({ name: 'B draft', deliveries: [expect.objectContaining({ target_id: 'u' })] }));
  });
  it('close/reopen with the same id cannot reuse stale hydration (ABA)', async () => {
    const old = deferred<Schedule>(), fresh = deferred<Schedule>();
    mocks.detail.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise);
    await render(listRow); await render(listRow, false); await render(listRow);
    await act(async () => { old.resolve(base); });
    expect(state.hydrationReady).toBe(false);
    await act(async () => { await state.handleOk(); }); expect(mocks.update).not.toHaveBeenCalled();
    await act(async () => { fresh.resolve({ ...base, name: 'Fresh' }); });
    expect(state.form.getFieldValue('name')).toBe('Fresh'); expect(state.hydrationReady).toBe(true);
  });
  it.each([false, true])('shows loading/failure/retry UI and disables inputs (mobile=%s)', async (mobile) => {
    const detail = deferred<Schedule>(); mocks.detail.mockReturnValueOnce(detail.promise).mockResolvedValueOnce(base);
    await act(async () => root.render(<Harness fields mobile={mobile} open editing={listRow} onClose={() => {}} onSaved={() => {}} />));
    expect(document.body.textContent).toContain('正在加载完整自动化配置');
    expect(document.querySelector<HTMLInputElement>('#name')!.disabled).toBe(true);
    await act(async () => { detail.reject(new Error('network failure')); });
    expect(document.body.textContent).toContain('加载自动化详情失败');
    const retry = Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(button => button.textContent?.replace(/\s/g, '') === '重试加载详情')!;
    expect(retry.disabled).toBe(false);
    await act(async () => { retry.click(); });
    expect(state.hydrationReady).toBe(true); expect(document.querySelector<HTMLInputElement>('#name')!.disabled).toBe(false);
    expect(document.body.textContent).not.toContain('加载自动化详情失败');
  });
});
