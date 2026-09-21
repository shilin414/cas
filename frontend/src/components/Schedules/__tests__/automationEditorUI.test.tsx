// @vitest-environment jsdom
import React from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react-dom/test-utils';
import { ScheduleEditorModal } from '../ScheduleEditorModal';
import { MobileScheduleEditor } from '../MobileScheduleEditor';
import { createSchedule, updateSchedule, previewScheduleRuns } from '@/services/scheduleApi';
import type { Schedule } from '@/types/schedule';
import type { ScheduleEditorState } from '../useScheduleEditor';
import type { ScheduleFormValues } from '@/lib/scheduleFormat';

let editorState: ScheduleEditorState;
vi.mock('../useScheduleEditor', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../useScheduleEditor')>();
  return { ...actual, useScheduleEditor: (options: Parameters<typeof actual.useScheduleEditor>[0]) => {
    editorState = actual.useScheduleEditor(options);
    return editorState;
  } };
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
async function rerender(node: React.ReactNode) {
  await act(async () => root.render(node)); await flush();
}


vi.mock('@/services/runApi', () => ({
  fetchApplicationPage: vi.fn(async () => ({ items: [{ id: 7, name: '日报智能体', enabled: true, is_bound: true }], next_cursor: '', has_more: false })),
  resolveApplication: vi.fn(async () => ({ id: 7, name: '日报智能体' })),
}));
vi.mock('@/services/shareApi', () => ({
  fetchFeishuTargets: vi.fn(async (type: string) => ({ items: type === 'chat' ? [{ id: 'oc_1', name: '运营群', target_type: 'chat' }, { id: 'oc_new', name: '新运营群', target_type: 'chat' }] : [], next_cursor: '', has_more: false })),
}));
vi.mock('@/services/scheduleApi', () => ({
  createSchedule: vi.fn(async () => ({ id: 1 })),
  updateSchedule: vi.fn(async () => ({ id: 1 })),
  previewScheduleRuns: vi.fn(async () => ['2027-10-01T01:00:00Z']),
}));

beforeAll(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true,
    ResizeObserver: class { observe() {} unobserve() {} disconnect() {} },
    matchMedia: (query: string) => ({ matches: false, media: query, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} }),
  });
  HTMLElement.prototype.scrollIntoView = vi.fn();
});
let root: Root;
let host: HTMLDivElement;
afterEach(async () => {
  if (root) await act(async () => root.unmount());
  host?.remove();
  vi.restoreAllMocks();
  vi.clearAllMocks();
});
async function mount(node: React.ReactNode) {
  host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  await act(async () => root.render(node)); await flush();
}
async function flush() { await act(async () => { await new Promise((r) => setTimeout(r, 30)); }); }
function button(text: string) {
  const el = [...document.querySelectorAll<HTMLButtonElement>('button')].find((e) => e.textContent?.replace(/\s/g, '') === text.replace(/\s/g, ''));
  expect(el, `button ${text}`).toBeTruthy(); return el!;
}
async function click(el: Element) { await act(async () => { el.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })); el.dispatchEvent(new MouseEvent('click', { bubbles: true })); }); await flush(); }
async function input(selector: string, value: string) {
  const el = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector)!; expect(el).toBeTruthy();
  await act(async () => { Object.getOwnPropertyDescriptor(el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype, 'value')!.set!.call(el, value); el.dispatchEvent(new Event('input', { bubbles: true })); });
}
const base: Schedule = {
  id: 9, name: '每日日报', description: '', application_id: 7, prompt: '总结今天的数据', schedule_type: 'daily', cron_expression: '', timezone: 'Asia/Shanghai', run_at: null,
  trigger: { time: '09:00' }, enabled: true, conversation_policy: 'reuse', overlap_policy: 'skip', misfire_policy: 'skip', deadline_policy: 'skip', execution_window_seconds: 600,
  next_run_at: null, last_run_at: null, created_at: '', updated_at: '', deliveries: [
    { id: 1, target_type: 'chat', target_id: 'oc_1', target_name: '运营群', content_mode: 'summary', enabled: true },
    { id: 2, target_type: 'user', target_id: 'ou_2', target_name: '另一位成员', content_mode: 'summary', enabled: true },
  ],
};
const props = { open: true, editing: null, presetApplicationId: 7, onClose: vi.fn(), onSaved: vi.fn() };

describe('automation editor UI', () => {
  it('desktop uses composer/config columns and a footer without unsupported controls', async () => {
    await mount(<ScheduleEditorModal {...props} />);
    expect(document.querySelector('.automation-editor__composer')).toBeTruthy();
    expect(document.querySelector('.automation-editor__configuration')).toBeTruthy();
    expect(document.querySelector('.ant-modal-title')?.textContent).toBe('新建自动化');
    expect(document.querySelector('.ant-modal-footer')?.textContent?.replace(/\s/g, '')).toContain('创建');
    expect(document.body.textContent).not.toMatch(/定时任务|添加附件|项目选择|空闲时/);
  });
  it('mobile navigation keeps the same mounted inputs and composer draft', async () => {
    await mount(<MobileScheduleEditor {...props} />);
    expect(document.querySelector('[aria-label="配置推送"]')?.textContent).toContain('推送配置');
    await input('#name', '保留草稿'); const title = document.querySelector('#name');
    await click(document.querySelector('[aria-label="配置触发器"]')!);
    expect(document.querySelector('.mobile-fs-drawer__title')?.textContent).toBe('触发配置');
    expect(document.querySelector('#name')).toBe(title);
    await click(button('完成'));
    expect((document.querySelector('#name') as HTMLInputElement).value).toBe('保留草稿');
    await click(document.querySelector('[aria-label="配置推送"]')!);
    expect(document.querySelector('.mobile-fs-drawer__title')?.textContent).toBe('推送配置');
    await click(document.querySelector('[aria-label="返回"]')!);
    expect(document.querySelector('.mobile-fs-drawer__title')?.textContent).toBe('新建自动化');
  });
  it('mobile start sheet cancel discards draft; done commits an accessible date/time', async () => {
    await mount(<MobileScheduleEditor {...props} />);
    await click(document.querySelector('[aria-label="配置触发器"]')!);
    await click(document.querySelector('[aria-label="设置开始时间"]')!);
    await click(document.querySelector('input[value="specified"]')!);
    await input('[aria-label="开始日期和时间"]', '2027-10-01T09:30');
    await click(button('取消'));
    expect(document.querySelector('[aria-label="设置开始时间"]')?.textContent).toContain('立即生效');
    await click(document.querySelector('[aria-label="设置开始时间"]')!);
    await click(document.querySelector('input[value="specified"]')!);
    await input('[aria-label="开始日期和时间"]', '2027-10-01T09:30');
    await click(document.querySelector('.automation-boundary-sheet button[data-action="done"]')!);
    expect(document.querySelector('[aria-label="设置开始时间"]')?.textContent).toContain('2027-10-01 09:30');
  });
  it('required composer validation prevents saving; valid mobile draft creates', async () => {
    await mount(<MobileScheduleEditor {...props} />);
    await click(button('创建')); expect(createSchedule).not.toHaveBeenCalled();
    await input('#name', '自动化测试'); await input('#prompt', '总结今天的数据');
    await click(button('创建')); expect(createSchedule).toHaveBeenCalledTimes(1);
  });
  it('editing keeps additional delivery targets and policies while adding a condition', async () => {
    await mount(<ScheduleEditorModal {...props} editing={base} />);
    const select = document.querySelector('#deliveries_0_condition_operator')!.closest('.ant-select')!;
    await click(select.querySelector('.ant-select-selector')!);
    await click([...document.querySelectorAll('.ant-select-item-option')].find((e) => e.textContent === '回复包含指定文本')!);
    await click(button('保存')); expect(updateSchedule).not.toHaveBeenCalled();
    await input('#deliveries_0_condition_text', '发现需要关注的异常');
    await click(button('保存'));
    const payload = vi.mocked(updateSchedule).mock.calls[0][1];
    expect(payload.deliveries).toHaveLength(2);
    expect(payload.deliveries?.[0]).toMatchObject({ target_id: 'oc_1', condition: { operator: 'contains', text: '发现需要关注的异常' } });
    expect(payload.deliveries?.[1]).toMatchObject({ target_id: 'ou_2', target_name: '另一位成员' });
    expect(payload).toMatchObject({ conversation_policy: 'reuse', overlap_policy: 'skip', misfire_policy: 'skip', deadline_policy: 'skip', execution_window_seconds: 600 });
  });
  it('desktop cancel closes without saving', async () => {
    const onClose = vi.fn();
    await mount(<ScheduleEditorModal {...props} onClose={onClose} />);
    await input('#name', '尚未保存');
    await click(button('取消'));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(createSchedule).not.toHaveBeenCalled();
  });
  it('mobile end sheet requires a date, rejects a reversed range, and cancels cleanly', async () => {
    await mount(<MobileScheduleEditor {...props} editing={{ ...base, trigger: { time: '09:00', starts_at: '2027-10-02T01:00:00Z' } }} />);
    await click(document.querySelector('[aria-label="配置触发器"]')!);
    await click(document.querySelector('[aria-label="设置结束时间"]')!);
    await click(document.querySelector('.automation-boundary-sheet input[type="radio"][value="specified"]')!);
    await click(document.querySelector('.automation-boundary-sheet button[data-action="done"]')!);
    expect(document.querySelector('.automation-boundary-sheet [role="alert"]')?.textContent).toContain('请选择结束时间');
    await input('[aria-label="结束日期和时间"]', '2027-10-01T09:30');
    await click(document.querySelector('.automation-boundary-sheet button[data-action="done"]')!);
    expect(document.querySelector('.automation-boundary-sheet [role="alert"]')?.textContent).toContain('不得早于');
    await click(button('取消'));
    expect(document.querySelector('[aria-label="设置结束时间"]')?.textContent).toContain('永不结束');
  });
  it('mobile save reveals a missing target in the push panel rather than losing hidden fields', async () => {
    await mount(<MobileScheduleEditor {...props} />);
    await input('#name', '验证推送'); await input('#prompt', '测试正文');
    await click(document.querySelector('[aria-label="配置推送"]')!);
    await click(document.querySelector('[aria-label="开启飞书投递"]')!);
    await click(document.querySelector('[aria-label="返回"]')!);
    await click(button('创建'));
    expect(createSchedule).not.toHaveBeenCalled();
    expect(document.querySelector('.mobile-fs-drawer__title')?.textContent).toBe('推送配置');
    expect(document.querySelector('#deliveries_0_target_id_help')?.textContent).toContain('请选择');
    expect((document.querySelector('#prompt') as HTMLTextAreaElement).value).toBe('测试正文');
  });
  it('changing the first recipient preserves its condition and every extra target', async () => {
    const editing = { ...base, deliveries: base.deliveries!.map((d, i) => i === 0 ? { ...d, condition: { operator: 'not_contains' as const, text: '无需人工处理' } } : d) };
    await mount(<ScheduleEditorModal {...props} editing={editing} />);
    await click(document.querySelector('#deliveries_0_target_id')!.closest('.ant-select')!.querySelector('.ant-select-selector')!);
    await click([...document.querySelectorAll('.ant-select-item-option')].find((e) => e.textContent === '[群聊] 新运营群')!);
    await click(button('保存'));
    const deliveries = vi.mocked(updateSchedule).mock.calls[0][1].deliveries!;
    expect(deliveries).toHaveLength(2);
    expect(deliveries[0]).toMatchObject({ target_id: 'oc_new', target_name: '新运营群', condition: { operator: 'not_contains', text: '无需人工处理' } });
    expect(deliveries[1]).toMatchObject({ target_id: 'ou_2' });
  });
  it('single-run editors still expose effective-date bounds and all advanced policies', async () => {
    await mount(<ScheduleEditorModal {...props} editing={{ ...base, schedule_type: 'once', run_at: '2027-10-01T01:00:00Z' }} />);
    const details = document.querySelector<HTMLDetailsElement>('.automation-editor__advanced')!;
    await act(async () => { details.open = true; details.dispatchEvent(new Event('toggle')); });
    expect(document.querySelector('.automation-editor__boundaries')?.hasAttribute('hidden')).toBe(false);
    expect(document.querySelector('#run_at_local')?.getAttribute('type')).toBe('datetime-local');
    for (const id of ['start_mode', 'end_mode', 'conversation_policy', 'overlap_policy', 'misfire_policy', 'deadline_policy', 'execution_window_seconds']) expect(document.getElementById(id), id).toBeTruthy();
  });

  it.each(['A-to-B', 'close-reopen-A'] as const)('mobile save ignores deferred validation from %s', async (transition) => {
    await mount(<MobileScheduleEditor {...props} editing={base} />);
    const pending = deferred<ScheduleFormValues>();
    vi.spyOn(editorState.form, 'validateFields').mockImplementationOnce(() => pending.promise);
    await click(button('保存'));
    const next = transition === 'A-to-B' ? { ...base, id: 10, name: '自动化 B', prompt: 'B 的内容' } : base;
    if (transition === 'close-reopen-A') await rerender(<MobileScheduleEditor {...props} open={false} editing={base} />);
    await rerender(<MobileScheduleEditor {...props} editing={next} />);
    await act(async () => pending.resolve(editorState.form.getFieldsValue(true)));
    await flush();
    expect(updateSchedule).not.toHaveBeenCalled();
    expect(props.onSaved).not.toHaveBeenCalled();
    expect(props.onClose).not.toHaveBeenCalled();
    await click(button('保存'));
    expect(updateSchedule).toHaveBeenCalledTimes(1);
    expect(vi.mocked(updateSchedule).mock.calls[0]).toEqual([next.id, expect.objectContaining({ name: next.name, prompt: next.prompt })]);
  });
  it('old subpanel completion cannot navigate the new editor after a deferred validation', async () => {
    await mount(<MobileScheduleEditor {...props} editing={base} />);
    await click(document.querySelector('[aria-label="配置触发器"]')!);
    const pending = deferred<ScheduleFormValues>();
    vi.spyOn(editorState.form, 'validateFields').mockImplementationOnce(() => pending.promise);
    await click(button('完成'));
    await rerender(<MobileScheduleEditor {...props} editing={{ ...base, id: 10 }} />);
    await click(document.querySelector('[aria-label="配置智能体"]')!);
    await act(async () => pending.resolve(editorState.form.getFieldsValue(true)));
    await flush();
    expect(document.querySelector('.mobile-fs-drawer__title')?.textContent).toBe('选择智能体');
  });
  it('observes unregistered delivery names and all targets without dropping metadata', async () => {
    await mount(<MobileScheduleEditor {...props} editing={base} />);
    expect(document.querySelector('[aria-label="配置推送"]')?.textContent).toContain('运营群');
    expect(document.body.textContent).toContain('已保留全部 2 个推送目标');
    await act(async () => editorState.form.setFieldValue(['deliveries', 0, 'target_name'], '新的目标显示名称'));
    expect(document.querySelector('[aria-label="配置推送"]')?.textContent).toContain('新的目标显示名称');
  });
  it('mobile preview reveals required composer fields instead of leaving errors hidden', async () => {
    await mount(<MobileScheduleEditor {...props} />);
    await click(document.querySelector('[aria-label="配置触发器"]')!);
    await click(button('预览未来执行时间'));
    expect(previewScheduleRuns).not.toHaveBeenCalled();
    expect(document.querySelector('.mobile-fs-drawer__title')?.textContent).toBe('新建自动化');
    expect(document.querySelector('#name_help')?.textContent).toContain('请输入自动化名称');
  });
  it('mobile preview routes invalid delivery text to its panel then succeeds after correction', async () => {
    const editing = { ...base, deliveries: base.deliveries!.map((d, i) => i === 0 ? { ...d, condition: { operator: 'contains' as const, text: '' } } : d) };
    await mount(<MobileScheduleEditor {...props} editing={editing} />);
    await click(document.querySelector('[aria-label="配置触发器"]')!);
    await click(button('预览未来执行时间'));
    expect(previewScheduleRuns).not.toHaveBeenCalled();
    expect(document.querySelector('.mobile-fs-drawer__title')?.textContent).toBe('推送配置');
    await input('#deliveries_0_condition_text', '指定的一段文本');
    await click(button('完成'));
    await click(document.querySelector('[aria-label="配置触发器"]')!);
    await click(button('预览未来执行时间'));
    expect(previewScheduleRuns).toHaveBeenCalledTimes(1);
    expect(document.querySelectorAll('.schedule-preview-list li')).toHaveLength(1);
  });
  it.each(['resolve', 'reject'] as const)('old preview validation %s cannot run or navigate editor B', async (outcome) => {
    await mount(<MobileScheduleEditor {...props} editing={base} />);
    await click(document.querySelector('[aria-label="配置触发器"]')!);
    const pending = deferred<ScheduleFormValues>();
    vi.spyOn(editorState.form, 'validateFields').mockImplementationOnce(() => pending.promise);
    await click(button('预览未来执行时间'));
    await rerender(<MobileScheduleEditor {...props} editing={{ ...base, id: 10 }} />);
    await click(document.querySelector('[aria-label="配置智能体"]')!);
    await act(async () => {
      if (outcome === 'resolve') pending.resolve(editorState.form.getFieldsValue(true));
      else pending.reject({ errorFields: [{ name: ['deliveries', 0, 'target_id'], errors: ['请选择目标'] }] });
    });
    await flush();
    expect(previewScheduleRuns).not.toHaveBeenCalled();
    expect(document.querySelector('.mobile-fs-drawer__title')?.textContent).toBe('选择智能体');
  });

  it('unmounting during validation prevents any later mobile save', async () => {
    await mount(<MobileScheduleEditor {...props} editing={base} />);
    const pending = deferred<ScheduleFormValues>();
    const values = editorState.form.getFieldsValue(true);
    vi.spyOn(editorState.form, 'validateFields').mockImplementationOnce(() => pending.promise);
    await click(button('保存'));
    await rerender(null);
    await act(async () => pending.resolve(values));
    await flush();
    expect(updateSchedule).not.toHaveBeenCalled();
    expect(props.onClose).not.toHaveBeenCalled();
  });
  it.each(['save', 'preview'] as const)('late %s completion does not reveal unrelated errors in the new editor', async (kind) => {
    await mount(<MobileScheduleEditor {...props} editing={base} />);
    const savePending = deferred<Schedule>();
    const previewPending = deferred<string[]>();
    if (kind === 'save') {
      vi.mocked(updateSchedule).mockImplementationOnce(() => savePending.promise);
      await click(button('保存'));
      expect(updateSchedule).toHaveBeenCalledTimes(1);
    } else {
      vi.mocked(previewScheduleRuns).mockImplementationOnce(() => previewPending.promise);
      await click(document.querySelector('[aria-label="配置触发器"]')!);
      await click(button('预览未来执行时间'));
      expect(previewScheduleRuns).toHaveBeenCalledTimes(1);
    }
    await rerender(<MobileScheduleEditor {...props} editing={{ ...base, id: 10 }} />);
    await click(document.querySelector('[aria-label="配置智能体"]')!);
    await act(async () => editorState.form.setFields([{ name: ['deliveries', 0, 'target_id'], errors: ['B 的待处理错误'] }]));
    await act(async () => {
      if (kind === 'save') savePending.resolve(base);
      else previewPending.resolve(['2027-10-01T01:00:00Z']);
    });
    await flush();
    expect(document.querySelector('.mobile-fs-drawer__title')?.textContent).toBe('选择智能体');
    expect(props.onClose).not.toHaveBeenCalled();
    expect(props.onSaved).not.toHaveBeenCalled();
  });

});
