// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { V2Application } from '@/services/runApi';
import BusinessApp from '../BusinessApp';
const mock = vi.hoisted(() => ({ status: vi.fn(), execute: vi.fn(), snapshot: vi.fn(), forward: vi.fn() }));
vi.mock('@/services/businessAppApi', () => ({ businessAppApi: mock }));
vi.mock('@/components/Chat/FeishuForwardModal', () => ({ default: ({ open, shareToken, send, summary }: {open: boolean; shareToken: string; send: (token: string, targets: {target_type: 'user'; id: string}[]) => Promise<unknown>; summary: React.ReactNode}) => open ? <div data-testid="query-forward">{summary}<button onClick={() => void send(shareToken, [{target_type: 'user', id: 'ou-1'}])}>确认发送测试</button></div> : null }));
let root: Root;
let host: HTMLDivElement;
const tick = () => new Promise(resolve => setTimeout(resolve, 30));
async function mount(key: string) {
 await act(async () => { root.render(<BusinessApp application={{ id: 1, renderer_key: key } as V2Application} />); await tick(); });
}
async function input(name: string, value: string) {
 const element = host.querySelector<HTMLInputElement>(`#${name}`)!;
 await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value); element.dispatchEvent(new Event('input', { bubbles: true })); element.dispatchEvent(new Event('change', { bubbles: true })); await tick(); });
}
async function submit() { await act(async () => { host.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); await tick(); }); }
beforeEach(() => {
 vi.resetAllMocks();
 Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
 window.matchMedia = vi.fn().mockImplementation(query => ({ matches: false, media: query, onchange: null, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn() }));
 mock.status.mockResolvedValue({ configured: true, account: '001', can_manage_others: false, can_submit: true });
 mock.execute.mockResolvedValue({ data: '生产日期: 20260921', message: '操作成功' });
 host = document.createElement('div'); document.body.appendChild(host); root = createRoot(host);
});
afterEach(async () => { await act(async () => root.unmount()); host.remove(); });
describe('real business forms', () => {
 it('submits a query with the exact barcode and shows text result', async () => {
  await mount('barcode-query'); await input('barcode', '01234567890123456789'); await submit();
  expect(mock.execute).toHaveBeenCalledWith(1, { action: 'production', barcode: '01234567890123456789' }, expect.any(AbortSignal));
  expect(host.textContent).toContain('生产日期: 20260921');
 });
 it('never calls the mutation until confirmed', async () => {
  await mount('oa-unlock'); expect(host.querySelector<HTMLInputElement>('#usercode')!.readOnly).toBe(true); await submit();
  expect(mock.execute).not.toHaveBeenCalled(); expect(document.body.textContent).toContain('请确认本次账号操作');
  await act(async () => { const button = Array.from(document.querySelectorAll<HTMLButtonElement>('.ant-modal button')).find(b => b.textContent?.replace(/\s/g, '') === '确认提交')!; button.click(); await tick(); });
  expect(mock.execute).toHaveBeenCalledTimes(1); expect(mock.execute.mock.calls[0][1]).toEqual({ usercode: '001', confirmed: true });
 });
 it('explains that an unconfigured app needs server-side connector settings', async () => {
  mock.status.mockResolvedValue({ configured: false, can_submit: false, reason: '该服务尚未配置完成' }); await mount('material-query');
  expect(host.textContent).toContain('该服务尚未配置完成');
  expect(host.textContent).toContain('当前环境缺少该应用所需的接口地址或凭证');
  expect(host.textContent).not.toContain('完成接口配置和身份绑定后即可使用');
  expect(host.querySelector<HTMLButtonElement>('button[type=submit]')!.disabled).toBe(true); expect(mock.execute).not.toHaveBeenCalled();
 });
 it('explains identity binding separately from connector configuration', async () => {
  mock.status.mockResolvedValue({ configured: true, account: '', can_manage_others: false, can_submit: false, reason: '尚未绑定可信工号或无权操作该账号' }); await mount('oa-unlock');
  expect(host.textContent).toContain('尚未绑定可信工号');
  expect(host.textContent).toContain('接口已配置，但当前账号尚未绑定可信工号');
  expect(host.textContent).not.toContain('当前环境缺少该应用所需的接口地址或凭证');
 });
 it('rejects invalid barcodes without issuing a request', async () => { await mount('barcode-query'); await input('barcode', '123'); await submit(); await act(async () => { await new Promise(resolve => setTimeout(resolve, 250)); }); expect(mock.execute).not.toHaveBeenCalled(); expect(host.textContent).toContain('条码必须为20位数字'); });
 it('renders upstream errors without presenting success', async () => { mock.execute.mockRejectedValue({ response: { data: { detail: '接口暂不可用' } } }); await mount('barcode-query'); await input('barcode', '01234567890123456789'); await submit(); expect(host.textContent).toContain('接口暂不可用'); expect(host.textContent).not.toContain('生产日期'); });
});


async function confirmTwice() {
 await act(async () => { const button = Array.from(document.querySelectorAll<HTMLButtonElement>('.ant-modal button')).find(b => b.textContent?.replace(/\s/g, '') === '确认提交')!; button.click(); button.click(); await tick(); });
}
async function choose(label: string) { await act(async () => { Array.from(host.querySelectorAll<HTMLElement>('.ant-segmented-item-label')).find(el => el.textContent === label)!.click(); await tick(); }); }
describe('sensitive form regression', () => {
 it.each(['oa-password', 'ldap-password'])('%s sends registered password fields but not confirmation', async key => {
  await mount(key); await input('password', 'VerificationPass12!'); await input('password_confirm', 'VerificationPass12!'); await submit();
  expect(mock.execute).not.toHaveBeenCalled(); await confirmTwice();
  expect(mock.execute).toHaveBeenCalledTimes(1); expect(mock.execute.mock.calls[0][1]).toEqual({ usercode: '001', password: 'VerificationPass12!', confirmed: true });
  expect(host.querySelector<HTMLInputElement>('#password')!.value).toBe('');
 });
 it('mismatched passwords block confirmation', async () => {
  await mount('oa-password'); await input('password', 'VerificationPass12!'); await input('password_confirm', 'DifferentPassword12!'); await submit();
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 250)); });
  expect(mock.execute).not.toHaveBeenCalled(); expect(document.querySelector('.ant-modal')).toBeNull(); expect(host.textContent).toContain('两次输入的密码不一致');
 });
 it('phone fields survive real form registration and blank fields are omitted', async () => {
  await mount('oa-phone'); await input('mobile', '13800138000'); await input('officephone', '0595-12345678'); await submit(); await confirmTwice();
  expect(mock.execute.mock.calls[0][1]).toEqual({ usercode: '001', mobile: '13800138000', officephone: '0595-12345678', confirmed: true });
 });
 it('TPM switching from reset to lock never sends an old password', async () => {
  await mount('tpm-account'); await choose('重置密码'); await input('password', 'VerificationPass12!'); await choose('锁定账号'); await submit(); await confirmTwice();
  expect(mock.execute.mock.calls[0][1]).toEqual({ usercode: '001', action: 'lock', confirmed: true });
 });
 it('cancel clears passwords without executing', async () => {
  await mount('oa-password'); await input('password', 'VerificationPass12!'); await input('password_confirm', 'VerificationPass12!'); await submit();
  await act(async () => { Array.from(document.querySelectorAll<HTMLButtonElement>('.ant-modal button')).find(b => b.textContent?.replace(/\s/g, '') === '返回修改')!.click(); await tick(); });
  expect(mock.execute).not.toHaveBeenCalled(); expect(host.querySelector<HTMLInputElement>('#password')!.value).toBe('');
 });
 it('pending execution resists double click and aborts on unmount', async () => {
  let settle!: (value: { message: string }) => void;
  mock.execute.mockImplementation(() => new Promise(resolve => { settle = resolve; }));
  await mount('oa-unlock'); await submit(); await confirmTwice(); expect(mock.execute).toHaveBeenCalledTimes(1);
  const signal: AbortSignal = mock.execute.mock.calls[0][2]; expect(signal.aborted).toBe(false);
  await act(async () => { root.render(null); await tick(); }); expect(signal.aborted).toBe(true);
  await act(async () => { settle({ message: 'late' }); await tick(); }); expect(host.textContent).toBe('');
 });
});


const snapshotInfo = { token: 'a'.repeat(64), query_label: '生产信息 · 条码', query_value: '01234567890123456789', queried_at: '2026-09-20T01:00:00Z', expires_at: '2026-09-21T01:00:00Z', can_forward: true };
describe('query result forwarding', () => {
 it('forwards only the server snapshot token, not editable result text', async () => {
  mock.execute.mockResolvedValue({ data: 'original', message: '完成', snapshot: snapshotInfo }); mock.forward.mockResolvedValue({success_count: 1, fail_count: 0, results: []});
  await mount('barcode-query'); await input('barcode', snapshotInfo.query_value); await submit();
  await act(async () => { Array.from(host.querySelectorAll('button')).find(button => button.textContent?.includes('转发到飞书'))!.click(); await tick(); });
  expect(host.textContent).toContain('无需先打开网页'); expect(host.textContent).toContain('卡片按钮用于进入查询应用');
  await act(async () => { Array.from(host.querySelectorAll('button')).find(button => button.textContent === '确认发送测试')!.click(); await tick(); });
  expect(mock.forward).toHaveBeenCalledWith(1, snapshotInfo.token, [{target_type: 'user', id: 'ou-1'}]);
  await input('barcode', '11234567890123456789'); expect(host.querySelector('[data-testid=query-forward]')).toBeNull(); expect(host.textContent).not.toContain('转发到飞书');
 });
 it('loads a read-only snapshot without executing another query or granting forwarding', async () => {
  mock.snapshot.mockResolvedValue({ data: 'saved result', message: '快照', snapshot: {...snapshotInfo, can_forward: false} });
  await act(async () => { root.render(<BusinessApp application={{id: 1, renderer_key: 'barcode-query'} as V2Application} snapshotToken={snapshotInfo.token} />); await tick(); });
  expect(mock.status).not.toHaveBeenCalled(); expect(mock.execute).not.toHaveBeenCalled(); expect(mock.snapshot).toHaveBeenCalledWith(1, snapshotInfo.token, expect.any(AbortSignal));
  expect(host.textContent).toContain('非实时数据'); expect(host.textContent).toContain('saved result'); expect(host.querySelector('form')).toBeNull(); expect(host.textContent).not.toContain('转发到飞书');
 });
 it('shows expired snapshot errors rather than silently requerying', async () => {
  mock.snapshot.mockRejectedValue({ response: { data: { detail: '查询快照已过期' } } });
  await act(async () => { root.render(<BusinessApp application={{id: 1, renderer_key: 'material-query'} as V2Application} snapshotToken={snapshotInfo.token} />); await tick(); });
  expect(host.textContent).toContain('查询快照已过期'); expect(mock.execute).not.toHaveBeenCalled();
 });
 it('keeps successful results when forwarding cache is unavailable', async () => {
  mock.execute.mockResolvedValue({ data: 'original', message: '完成', forward_unavailable: '快照服务暂不可用' }); await mount('barcode-query'); await input('barcode', snapshotInfo.query_value); await submit();
  expect(host.textContent).toContain('original'); expect(host.textContent).toContain('快照服务暂不可用'); expect(host.textContent).not.toContain('转发到飞书');
 });
});
