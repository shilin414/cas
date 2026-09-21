// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ sessions: vi.fn(), createSession: vi.fn(), session: vi.fn(), deleteSession: vi.fn(), upload: vi.fn(), deleteAttachment: vi.fn(), send: vi.fn(), invocation: vi.fn(), cancel: vi.fn(), attachment: vi.fn() }));
vi.mock('@/services/aiModels', () => ({ aiModelsApi: mocks }));
vi.mock('@/stores/useAuthStore', () => ({ useAuthStore: (select: (state: unknown) => unknown) => select({ user: { id: 'owner' } }) }));
import RemoteTestPanel from '../RemoteTestPanel';
import type { AIConnection, AIModel, Invocation, TestSessionDetail } from '@/services/aiModels';
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
Object.defineProperty(window, 'matchMedia', { writable: true, value: () => ({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} }) });
globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
const connection: AIConnection = { id: 'c', name: 'Connection', adapter: 'gemini', base_url: 'https://example.com/v1', has_credential: true, enabled: true, timeout_seconds: 77, max_concurrency: 3, version: 1, created_at: '', updated_at: '' };
const model: AIModel = { id: 'm', name: 'Chat', model_id: 'real-model', execution_location: 'server_remote', capability_kind: 'chat', connection_id: 'c', capabilities: { image: true, video: true, pdf: true, streaming: true }, default_parameters: { temperature: 0, max_output_tokens: 42 }, enabled: true, version: 1, created_at: '', updated_at: '' };
const session: TestSessionDetail = { id: 's', model_id: 'm', title: 'my session', created_at: '2026-09-21T00:00:00Z', expires_at: '2026-09-22T00:00:00Z', messages: [] };
const snapshot = (status: Invocation['status'], extra: Partial<Invocation> = {}): Invocation => ({ id: 'i', model_id: 'm', session_id: 's', status, output_text: '', created_at: '', cancel_requested: false, cancellation_confirmed: false, input_tokens: null, ...extra });
let root: Root; let host: HTMLDivElement;
beforeEach(() => { vi.resetAllMocks(); sessionStorage.clear(); mocks.sessions.mockResolvedValue([]); mocks.createSession.mockResolvedValue(session); mocks.session.mockResolvedValue(session); mocks.send.mockResolvedValue({ invocation_id: 'i' }); mocks.invocation.mockResolvedValue(snapshot('succeeded')); mocks.cancel.mockResolvedValue(undefined); host = document.createElement('div'); document.body.appendChild(host); root = createRoot(host); });
afterEach(async () => { await act(async () => root.unmount()); host.remove(); });
function button(text: string) { const found = Array.from(host.querySelectorAll('button')).find((item) => item.textContent?.replace(/\s/g, '') === text.replace(/\s/g, '')); if (!found) throw new Error('Missing button: ' + text); return found; }
async function click(text: string) { await act(async () => button(text).click()); }
async function typeMessage(value: string) { const input = host.querySelector('[aria-label="测试消息"]') as HTMLTextAreaElement; await act(async () => { Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(input, value); input.dispatchEvent(new Event('input', { bubbles: true })); }); }
async function start() { await act(async () => root.render(<RemoteTestPanel model={model} connection={connection} onBusyChange={() => {}} />)); await click('新建会话'); }
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (error: unknown) => void; const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
async function selectExistingSession() {
  await act(async () => root.render(<RemoteTestPanel model={model} connection={connection} onBusyChange={() => {}} />));
  const input = host.querySelector('[role="combobox"]') as HTMLElement;
  await act(async () => { input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })); });
  const option = Array.from(document.querySelectorAll('.ant-select-item-option')).find((item) => item.textContent?.includes('my session')) as HTMLElement;
  if (!option) throw new Error('Session option not rendered');
  await act(async () => option.click());
}
describe('remote invocation lifecycle', () => {
  it('allows stop immediately after receipt while the history GET is still pending', async () => {
    const history = deferred<TestSessionDetail>(); mocks.session.mockReturnValue(history.promise); mocks.invocation.mockResolvedValue(snapshot('running'));
    await start(); await typeMessage('do not duplicate'); await click('发送消息');
    expect(mocks.send).toHaveBeenCalledTimes(1); expect(button('请求停止').disabled).toBe(false);
    expect((host.querySelector('[aria-label="测试消息"]') as HTMLTextAreaElement).disabled).toBe(true);
    await click('请求停止'); expect(mocks.cancel).toHaveBeenCalledWith('i'); expect(mocks.send).toHaveBeenCalledTimes(1);
    await act(async () => history.resolve(session));
  });
  it('blocks duplicate submissions without clearing a draft before the receipt arrives', async () => {
    const receipt = deferred<{ invocation_id: string }>(); const history = deferred<TestSessionDetail>();
    mocks.send.mockReturnValue(receipt.promise); mocks.session.mockReturnValue(history.promise); mocks.invocation.mockResolvedValue(snapshot('running'));
    await start(); await typeMessage('keep until accepted');
    const sendButton = button('发送消息'); await act(async () => { sendButton.click(); sendButton.click(); });
    expect(mocks.send).toHaveBeenCalledTimes(1); expect((host.querySelector('[aria-label="测试消息"]') as HTMLTextAreaElement).value).toBe('keep until accepted');
    await act(async () => receipt.resolve({ invocation_id: 'i' }));
    expect((host.querySelector('[aria-label="测试消息"]') as HTMLTextAreaElement).value).toBe(''); expect(button('请求停止').disabled).toBe(false);
    await act(async () => history.resolve(session));
  });
  it('never lets an older accepted-history response overwrite the terminal history or the next draft', async () => {
    const older = deferred<TestSessionDetail>(); const newer = deferred<TestSessionDetail>(); const outcome = deferred<Invocation>();
    mocks.session.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise); mocks.invocation.mockReturnValue(outcome.promise);
    await start(); await typeMessage('round one'); await click('发送消息');
    await act(async () => outcome.resolve(snapshot('succeeded', { output_text: 'latest answer' })));
    expect(mocks.session).toHaveBeenCalledTimes(2);
    const message = { id: 'latest', role: 'assistant' as const, text: 'latest answer', created_at: '', attachments: [] };
    await act(async () => newer.resolve({ ...session, messages: [message] }));
    await typeMessage('next unsent draft');
    await act(async () => older.resolve({ ...session, messages: [{ ...message, id: 'old', text: 'obsolete response' }] }));
    expect(host.textContent).not.toContain('obsolete response'); expect(host.textContent).toContain('latest answer');
    expect((host.querySelector('[aria-label="测试消息"]') as HTMLTextAreaElement).value).toBe('next unsent draft'); expect(mocks.send).toHaveBeenCalledTimes(1);
  });
  it('reports history failure separately and retries history without blocking stop or repolling', async () => {
    mocks.session.mockRejectedValueOnce(new Error('history unavailable')).mockResolvedValue(session); mocks.invocation.mockResolvedValue(snapshot('running'));
    await start(); await typeMessage('hello'); await click('发送消息');
    expect(host.textContent).toContain('对话历史刷新失败：history unavailable'); expect(host.textContent).not.toContain('状态查询中断'); expect(button('请求停止').disabled).toBe(false);
    const polls = mocks.invocation.mock.calls.length; await click('刷新对话');
    expect(mocks.invocation).toHaveBeenCalledTimes(polls); expect(mocks.session).toHaveBeenCalledTimes(2); expect(host.textContent).not.toContain('history unavailable');
    await click('请求停止'); expect(mocks.cancel).toHaveBeenCalledWith('i');
  });
  it('uses a separate synchronous cancellation lock for repeated clicks', async () => {
    const history = deferred<TestSessionDetail>(); const stopped = deferred<void>();
    mocks.session.mockReturnValue(history.promise); mocks.invocation.mockResolvedValue(snapshot('running')); mocks.cancel.mockReturnValue(stopped.promise);
    await start(); await typeMessage('hello'); await click('发送消息');
    const stop = button('请求停止'); await act(async () => { stop.click(); stop.click(); });
    expect(mocks.cancel).toHaveBeenCalledTimes(1); expect(mocks.send).toHaveBeenCalledTimes(1);
    await act(async () => stopped.resolve()); expect(host.textContent).toContain('上游是否停止及是否计费仍待确认');
    await act(async () => history.resolve(session));
  });
  it('does not apply a previous session history after creating a new session', async () => {
    const older = deferred<TestSessionDetail>(); const terminalHistory = deferred<TestSessionDetail>();
    mocks.createSession.mockResolvedValueOnce(session).mockResolvedValueOnce({ ...session, id: 's2', title: 'another session' });
    mocks.session.mockReturnValueOnce(older.promise).mockReturnValueOnce(terminalHistory.promise);
    await start(); await typeMessage('first'); await click('发送消息'); await click('新建会话');
    await act(async () => terminalHistory.resolve({ ...session, messages: [{ id: 'old', role: 'user', text: 'old session body', created_at: '', attachments: [] }] }));
    await act(async () => older.resolve(session));
    expect(host.textContent).not.toContain('old session body'); expect(host.textContent).toContain('another session');
    await typeMessage('new session message'); mocks.session.mockResolvedValue({ ...session, id: 's2', messages: [] }); await click('发送消息');
    expect(mocks.send.mock.calls[1][0]).toBe('s2');
  });
  it('recovers a server-owned active invocation with empty sessionStorage and blocks delete/send', async () => {
    mocks.sessions.mockResolvedValue([session]); mocks.session.mockResolvedValue({ ...session, active_invocation_id: 'server-active' }); mocks.invocation.mockResolvedValue(snapshot('running', { id: 'server-active' }));
    await selectExistingSession();
    expect(mocks.invocation).toHaveBeenCalledWith('server-active', expect.any(AbortSignal));
    expect((host.querySelector('[aria-label="删除当前会话"]') as HTMLButtonElement).disabled).toBe(true);
    expect((host.querySelector('[aria-label="测试消息"]') as HTMLTextAreaElement).disabled).toBe(true); expect(button('新建会话').disabled).toBe(true);
    expect(mocks.send).not.toHaveBeenCalled(); expect(button('请求停止').disabled).toBe(false);
    await click('请求停止'); expect(mocks.cancel).toHaveBeenCalledWith('server-active');
  });
  it('prefers server active invocation over a stale sessionStorage hint', async () => {
    sessionStorage.setItem('aim:invocation:owner:s', 'stale-local');
    mocks.sessions.mockResolvedValue([session]); mocks.session.mockResolvedValue({ ...session, active_invocation_id: 'server-active' }); mocks.invocation.mockResolvedValue(snapshot('running', { id: 'server-active' }));
    await selectExistingSession(); expect(mocks.invocation).toHaveBeenCalledWith('server-active', expect.any(AbortSignal));
    expect(sessionStorage.getItem('aim:invocation:owner:s')).toBe('server-active');
  });
  it('treats an explicitly empty server active id as authoritative over the local hint', async () => {
    sessionStorage.setItem('aim:invocation:owner:s', 'stale-local');
    mocks.sessions.mockResolvedValue([session]); mocks.session.mockResolvedValue({ ...session, active_invocation_id: '' });
    await selectExistingSession(); expect(mocks.invocation).not.toHaveBeenCalled(); expect(sessionStorage.getItem('aim:invocation:owner:s')).toBeNull();
    expect((host.querySelector('[aria-label="删除当前会话"]') as HTMLButtonElement).disabled).toBe(false);
  });

  it('surfaces polling failure; retry reads the same invocation and shows unknown usage', async () => {
    mocks.invocation.mockRejectedValueOnce(new Error('network down')).mockResolvedValue(snapshot('succeeded', { output_text: 'real complete snapshot' }));
    await start(); await typeMessage('hello'); await click('发送消息');
    expect(host.textContent).toContain('network down'); expect(host.textContent).not.toContain('已完成');
    await click('重新查询'); expect(mocks.send).toHaveBeenCalledTimes(1); expect(mocks.invocation).toHaveBeenCalledTimes(2);
    expect(host.textContent).toContain('已完成'); expect(host.textContent).toContain('输入 — tokens'); expect(host.textContent).toContain('real complete snapshot');
  });
  it('does not claim cancellation confirmed merely because the stop endpoint returns', async () => {
    mocks.invocation.mockResolvedValue(snapshot('running'));
    await start(); await typeMessage('hello'); await click('发送消息');
    await click('请求停止'); expect(mocks.cancel).toHaveBeenCalledWith('i'); expect(host.textContent).toContain('上游是否停止及是否计费仍待确认'); expect(host.textContent).not.toContain('服务端已确认取消');
  });
  it('keeps cancellation failures visible and allows retry', async () => {
    mocks.invocation.mockResolvedValue(snapshot('running')); mocks.cancel.mockRejectedValue(new Error('cancel failed'));
    await start(); await typeMessage('hello'); await click('发送消息'); await click('请求停止');
    expect(host.textContent).toContain('cancel failed'); expect(button('请求停止').disabled).toBe(false);
  });
  it('never replays a timed-out submit with a new idempotency key', async () => {
    mocks.send.mockRejectedValueOnce(new Error('timeout')).mockResolvedValue({ invocation_id: 'i' });
    await start(); await typeMessage('same body'); await click('发送消息');
    const first = mocks.send.mock.calls[0][1]; expect(host.textContent).toContain('提交结果尚未确认');
    await click('确认同一次提交'); expect(mocks.send.mock.calls[1][1]).toEqual(first); expect(first.request_id).toBeTruthy();
  });
  it('shows server failed/indeterminate states, never success', async () => {
    mocks.invocation.mockResolvedValue(snapshot('indeterminate', { output_text: 'partial', error_code: 'upstream_unknown', error_message: 'connection lost' }));
    await start(); await typeMessage('hello'); await click('发送消息');
    expect(host.textContent).toContain('上游结果不确定'); expect(host.textContent).toContain('connection lost'); expect(host.textContent).toContain('partial'); expect(host.textContent).not.toContain('已完成');
  });
  it('keeps historical attachments across the next round instead of rebuilding history client-side', async () => {
    mocks.session.mockResolvedValue({ ...session, messages: [{ id: 'msg', role: 'user', text: 'round one', created_at: '', attachments: [{ id: 'file', name: 'history.pdf', kind: 'pdf', mime_type: 'application/pdf', size_bytes: 12 }] }] });
    await start(); await typeMessage('first'); await click('发送消息'); expect(host.textContent).toContain('history.pdf');
    await typeMessage('next round'); await click('发送消息'); expect(host.textContent).toContain('history.pdf');
    expect(mocks.send.mock.calls[1][1]).toMatchObject({ text: 'next round', attachment_ids: [] });
    expect(mocks.send.mock.calls[1][1]).not.toHaveProperty('messages');
  });
  it('removes GIF from the picker and rejects it even when file filtering is bypassed', async () => {
    await start();
    const input = host.querySelector('[aria-label="添加测试附件"]') as HTMLInputElement;
    expect(input.accept).not.toContain('image/gif'); expect(input.accept).toContain('image/jpeg');
    Object.defineProperty(input, 'files', { configurable: true, value: [new File(['gif'], 'photo.gif', { type: 'image/gif' })] });
    await act(async () => input.dispatchEvent(new Event('change', { bubbles: true })));
    expect(mocks.upload).not.toHaveBeenCalled(); expect(host.textContent).toContain('photo.gif：当前模型或协议不支持此输入类型');
  });
  it('does not cancel upstream just because the component unmounts', async () => {
    mocks.invocation.mockResolvedValue(snapshot('running')); await start(); await typeMessage('hello'); await click('发送消息');
    await act(async () => root.render(<div />)); expect(mocks.cancel).not.toHaveBeenCalled();
    expect(sessionStorage.getItem('aim:invocation:owner:s')).toBe('i');
  });
});
