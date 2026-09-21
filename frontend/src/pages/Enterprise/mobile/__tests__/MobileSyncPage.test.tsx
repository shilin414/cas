/**
 * MobileSyncPage — independent target mutations and dirty-form-safe refreshes.
 */
// @vitest-environment jsdom
import React from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react-dom/test-utils';

const mocks = vi.hoisted(() => ({
  loadSyncTargets: vi.fn(),
  loadSyncJobs: vi.fn(),
  saveSyncTargetConfig: vi.fn(),
  triggerOneSyncTarget: vi.fn(),
  triggerAllSyncTargets: vi.fn(),
  messageError: vi.fn(),
  messageSuccess: vi.fn(),
  setFields: vi.fn(),
  isFieldsTouched: vi.fn(() => false),
}));

vi.mock('../../syncManagement', () => ({
  loadSyncTargets: mocks.loadSyncTargets,
  loadSyncJobs: mocks.loadSyncJobs,
  saveSyncTargetConfig: mocks.saveSyncTargetConfig,
  triggerOneSyncTarget: mocks.triggerOneSyncTarget,
  triggerAllSyncTargets: mocks.triggerAllSyncTargets,
  formatMetricLabel: (key: string) => ({ departments: '部门', groups: '用户组' })[key] || key,
  syncTriggerLabel: (value: string) => value === 'scheduled' ? '自动' : '手动',
}));

vi.mock('antd', () => {
  const FormMock = ({ children, id, name }: { children?: React.ReactNode; id?: string; name?: string }) => (
    <form id={id} name={name}>{children}</form>
  );
  const FormItemMock = ({ label, htmlFor, children }: {
    label?: React.ReactNode;
    htmlFor?: string;
    children?: React.ReactNode | ((api: { getFieldValue: (name: string) => unknown }) => React.ReactNode);
  }) => {
    const content = typeof children === 'function'
      ? children({ getFieldValue: () => 'interval' })
      : children;
    return <label htmlFor={htmlFor}>{label}{content}</label>;
  };
  (FormMock as unknown as { Item: unknown }).Item = FormItemMock;
  const formApi = {
    setFields: mocks.setFields,
    isFieldsTouched: mocks.isFieldsTouched,
    validateFields: vi.fn(async () => ({
      enabled: true,
      schedule_type: 'interval',
      interval_minutes: 120,
      daily_time: { format: () => '02:00' },
      timezone: 'Asia/Shanghai',
    })),
  };
  const field = (type: string) => (props: React.InputHTMLAttributes<HTMLInputElement>) => (
    <input {...props} type={type} />
  );
  return {
    Alert: ({ message, description, action }: {
      message?: React.ReactNode;
      description?: React.ReactNode;
      action?: React.ReactNode;
    }) => <div role="alert">{message}{description}{action}</div>,
    Button: ({ children, onClick, disabled, className, 'aria-label': ariaLabel }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
      <button type="button" className={className} disabled={disabled} aria-label={ariaLabel} onClick={onClick}>{children}</button>
    ),
    Form: Object.assign(FormMock, { useForm: () => [formApi] }),
    Input: field('text'),
    InputNumber: field('number'),
    Select: ({ id, 'aria-label': ariaLabel }: { id?: string; 'aria-label'?: string }) => <select id={id} aria-label={ariaLabel} />,
    Skeleton: () => <div data-testid="skeleton" />,
    Switch: ({ id, 'aria-label': ariaLabel }: { id?: string; 'aria-label'?: string }) => <button type="button" id={id} aria-label={ariaLabel} />,
    Tag: ({ children }: { children?: React.ReactNode }) => <span>{children}</span>,
    TimePicker: field('time'),
    message: { success: mocks.messageSuccess, error: mocks.messageError },
  };
});

vi.mock('@/components/MobileConsole', () => ({
  MobilePage: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  MobileSection: ({ title, children }: { title?: React.ReactNode; children?: React.ReactNode }) => (
    <section data-testid="section"><h3>{title}</h3>{children}</section>
  ),
}));

import MobileSyncPage from '../MobileSyncPage';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
(globalThis as unknown as { matchMedia: unknown }).matchMedia = (query: string) => ({
  matches: false, media: query, onchange: null,
  addListener() {}, removeListener() {},
  addEventListener() {}, removeEventListener() {},
  dispatchEvent: () => false,
});

const flush = async (ms = 0) => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, ms));
  });
};

const mounted: Array<{ host: HTMLElement; root: Root }> = [];
async function mountPage(wait = 30) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  mounted.push({ host, root });
  await act(async () => { root.render(<MobileSyncPage />); });
  await flush(wait);
}

function click(el: Element) {
  return act(async () => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
}

const CONFIG = (targetCode: string, version = 1) => ({
  target_code: targetCode,
  enabled: false,
  schedule_type: 'interval' as const,
  interval_minutes: 360,
  daily_time: '02:00',
  timezone: 'Asia/Shanghai',
  next_run_at: null,
  last_run_at: null,
  last_success_at: '2026-09-18T02:00:00Z',
  target_version: version,
  last_error_code: '',
  last_error_message: '',
  updated_at: '2026-09-18T02:00:00Z',
});

const TARGETS = [
  {
    code: 'directory',
    displayName: '组织通讯录',
    description: '同步部门、用户和所属部门关系',
    dependencies: [],
    metricKeys: ['departments', 'users'],
    config: CONFIG('directory'),
    legacyFallback: false,
  },
  {
    code: 'user_groups',
    displayName: '飞书用户组',
    description: '同步普通组、动态组和成员',
    dependencies: ['directory'],
    metricKeys: ['groups', 'group_members'],
    config: CONFIG('user_groups'),
    legacyFallback: false,
  },
];

const JOB = (id: string, targetCode: string, status = 'success') => ({
  id,
  targetCode,
  batchId: 'batch-1',
  triggerType: 'scheduled',
  status,
  metrics: targetCode === 'directory' ? { departments: 10 } : { groups: 4 },
  warnings: [],
  targetVersion: 2,
  startedAt: '2026-09-18T02:00:00Z',
  finishedAt: '2026-09-18T02:05:14Z',
  errorCode: '',
  errorMessage: '',
  createdAt: '2026-09-18T02:00:00Z',
  legacyFallback: false,
});

beforeEach(() => {
  mocks.loadSyncTargets.mockReset().mockResolvedValue(TARGETS);
  mocks.loadSyncJobs.mockReset().mockResolvedValue([
    JOB('directory-1', 'directory'),
    JOB('groups-1', 'user_groups'),
  ]);
  mocks.saveSyncTargetConfig.mockReset().mockImplementation(async (code: string) => CONFIG(code, 2));
  mocks.triggerOneSyncTarget.mockReset().mockResolvedValue(undefined);
  mocks.triggerAllSyncTargets.mockReset().mockResolvedValue(undefined);
  mocks.messageError.mockReset();
  mocks.messageSuccess.mockReset();
  mocks.setFields.mockReset();
  mocks.isFieldsTouched.mockReset().mockReturnValue(false);
});

afterEach(async () => {
  while (mounted.length) {
    const { host, root } = mounted.pop()!;
    await act(async () => { root.unmount(); });
    host.remove();
  }
  document.body.innerHTML = '';
});

describe('MobileSyncPage — independent target cards', () => {
  it('renders the real metric contract with unique form names, IDs, and aria labels', async () => {
    await mountPage();

    expect(document.body.textContent).toContain('部门 10');
    expect(document.body.textContent).toContain('用户组 4');
    expect(document.querySelector('form[name="sync-target-directory-mobile"]')).toBeTruthy();
    expect(document.querySelector('form[name="sync-target-user_groups-mobile"]')).toBeTruthy();
    const intervalControl = document.querySelector('#sync-target-directory-mobile-interval-minutes');
    const intervalLabel = document.querySelector('label[for="sync-target-directory-mobile-interval-minutes"]');
    expect(intervalControl).toBeTruthy();
    expect(intervalLabel).toBeTruthy();
    expect(intervalLabel?.getAttribute('for')).toBe(intervalControl?.id);
    expect(document.querySelector('button[aria-label="立即同步组织通讯录"]')).toBeTruthy();
    expect(document.querySelector('button[aria-label="保存飞书用户组设置"]')).toBeTruthy();
  });

  it('saving target A merges only A and never performs a target-list reload that can reset dirty B', async () => {
    await mountPage();
    const initialTargetLoads = mocks.loadSyncTargets.mock.calls.length;

    await click(document.querySelector('button[aria-label="保存组织通讯录设置"]')!);
    await flush(20);

    expect(mocks.saveSyncTargetConfig).toHaveBeenCalledWith('directory', expect.objectContaining({
      enabled: true,
      interval_minutes: 120,
    }));
    expect(mocks.loadSyncTargets).toHaveBeenCalledTimes(initialTargetLoads);
  });

  it('triggering target A refreshes jobs fresh without reloading target forms', async () => {
    await mountPage();
    const initialTargetLoads = mocks.loadSyncTargets.mock.calls.length;

    await click(document.querySelector('button[aria-label="立即同步组织通讯录"]')!);
    await flush(20);

    expect(mocks.triggerOneSyncTarget).toHaveBeenCalledWith('directory');
    expect(mocks.loadSyncTargets).toHaveBeenCalledTimes(initialTargetLoads);
    expect(mocks.loadSyncJobs).toHaveBeenLastCalledWith(50, { fresh: true });
  });

  it('an older full refresh resolving after save cannot overwrite the saved target', async () => {
    await mountPage();
    let releaseTargets!: (value: unknown) => void;
    let releaseJobs!: (value: unknown) => void;
    mocks.loadSyncTargets.mockImplementationOnce(() => new Promise((resolve) => { releaseTargets = resolve; }));
    mocks.loadSyncJobs.mockImplementationOnce(() => new Promise((resolve) => { releaseJobs = resolve; }));

    await click(document.querySelector('button[aria-label="刷新同步管理"]')!);
    await click(document.querySelector('button[aria-label="保存组织通讯录设置"]')!);
    await flush(20);

    const directorySection = Array.from(document.querySelectorAll('section'))
      .find((section) => section.querySelector('h3')?.textContent === '组织通讯录');
    expect(directorySection?.textContent).toContain('版本：2');

    await act(async () => {
      releaseTargets(TARGETS);
      releaseJobs([JOB('stale', 'directory')]);
    });
    await flush(20);

    expect(directorySection?.textContent).toContain('版本：2');
    expect(directorySection?.textContent).not.toContain('版本：1');
  });

  it('manual refresh explicitly requests fresh target and job reads', async () => {
    await mountPage();
    await click(document.querySelector('button[aria-label="刷新同步管理"]')!);
    await flush(20);

    expect(mocks.loadSyncTargets).toHaveBeenLastCalledWith({ fresh: true });
    expect(mocks.loadSyncJobs).toHaveBeenLastCalledWith(50, { fresh: true });
  });
});

describe('MobileSyncPage — safe fallback and failures', () => {
  it('makes legacy combined capability explicit and disables every mutation', async () => {
    mocks.loadSyncTargets.mockResolvedValue(TARGETS.map((target) => ({
      ...target,
      legacyFallback: true,
    })));
    await mountPage();

    expect(document.body.textContent).toContain('旧接口只支持通讯录与用户组组合配置、组合执行');
    expect((document.querySelector('button[aria-label="立即同步组织通讯录"]') as HTMLButtonElement).disabled).toBe(true);
    expect((document.querySelector('button[aria-label="保存飞书用户组设置"]') as HTMLButtonElement).disabled).toBe(true);
    expect((document.querySelector('button[aria-label="同步全部目标"]') as HTMLButtonElement).disabled).toBe(true);
  });

  it('keeps forms available when job history alone fails', async () => {
    mocks.loadSyncJobs.mockRejectedValue(new Error('network down'));
    await mountPage();

    expect(document.querySelectorAll('button[aria-label^="保存"]')).toHaveLength(2);
    expect(document.body.textContent).toContain('加载同步记录失败');
  });

  it('surfaces mutation failures without invoking another target mutation', async () => {
    await mountPage();
    mocks.saveSyncTargetConfig.mockRejectedValueOnce(new Error('save down'));

    await click(document.querySelector('button[aria-label="保存组织通讯录设置"]')!);
    await flush(20);

    expect(mocks.messageError).toHaveBeenCalledWith('保存同步设置失败');
    expect(mocks.saveSyncTargetConfig).toHaveBeenCalledTimes(1);
  });
});

describe('MobileSyncPage — out-of-order loads', () => {
  it('allows only the newest load generation to update cards and history', async () => {
    let releaseTargets!: (value: unknown) => void;
    let releaseJobs!: (value: unknown) => void;
    let targetCall = 0;
    mocks.loadSyncTargets.mockImplementation(() => {
      targetCall += 1;
      if (targetCall === 1) return new Promise((resolve) => { releaseTargets = resolve; });
      return Promise.resolve([{ ...TARGETS[0], displayName: '新一代通讯录' }, TARGETS[1]]);
    });
    let jobCall = 0;
    mocks.loadSyncJobs.mockImplementation(() => {
      jobCall += 1;
      if (jobCall === 1) return new Promise((resolve) => { releaseJobs = resolve; });
      return Promise.resolve([{ ...JOB('new', 'directory', 'failed'), errorMessage: '新一代失败原因' }]);
    });

    await mountPage(0);
    await click(document.querySelector('button[aria-label="刷新同步管理"]')!);
    await flush(30);
    expect(document.body.textContent).toContain('新一代通讯录');
    expect(document.body.textContent).toContain('新一代失败原因');

    await act(async () => {
      releaseTargets(TARGETS);
      releaseJobs([JOB('old', 'directory')]);
    });
    await flush(20);

    expect(document.body.textContent).toContain('新一代通讯录');
    expect(document.body.textContent).toContain('新一代失败原因');
  });
});
