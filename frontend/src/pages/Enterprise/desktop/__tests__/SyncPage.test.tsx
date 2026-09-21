/** Desktop sync page generation and label association regressions. */
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
}));

vi.mock('../../syncManagement', () => ({
  loadSyncTargets: mocks.loadSyncTargets,
  loadSyncJobs: mocks.loadSyncJobs,
  saveSyncTargetConfig: mocks.saveSyncTargetConfig,
  triggerOneSyncTarget: mocks.triggerOneSyncTarget,
  triggerAllSyncTargets: mocks.triggerAllSyncTargets,
  formatMetricLabel: (key: string) => key,
  syncTriggerLabel: (value: string) => value,
}));

vi.mock('antd', () => {
  const formApi = {
    setFields: vi.fn(),
    isFieldsTouched: vi.fn(() => false),
    validateFields: vi.fn(async () => ({
      enabled: true,
      schedule_type: 'interval',
      interval_minutes: 120,
      daily_time: { format: () => '02:00' },
      timezone: 'Asia/Shanghai',
    })),
  };
  const FormMock = ({ children, id, name }: { children?: React.ReactNode; id?: string; name?: string }) => (
    <form id={id} name={name}>{children}</form>
  );
  const FormItemMock = ({ label, htmlFor, children }: {
    label?: React.ReactNode;
    htmlFor?: string;
    children?: React.ReactNode | ((api: { getFieldValue: (name: string) => unknown }) => React.ReactNode);
  }) => (
    <label htmlFor={htmlFor}>
      {label}
      {typeof children === 'function' ? children({ getFieldValue: () => 'interval' }) : children}
    </label>
  );
  (FormMock as unknown as { Item: unknown }).Item = FormItemMock;
  const field = (type: string) => (props: React.InputHTMLAttributes<HTMLInputElement>) => (
    <input {...props} type={type} />
  );
  return {
    Alert: ({ message, description }: { message?: React.ReactNode; description?: React.ReactNode }) => <div>{message}{description}</div>,
    Button: ({ children, onClick, disabled, 'aria-label': ariaLabel }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
      <button type="button" disabled={disabled} aria-label={ariaLabel} onClick={onClick}>{children}</button>
    ),
    Card: ({ title, extra, children }: { title?: React.ReactNode; extra?: React.ReactNode; children?: React.ReactNode }) => (
      <section><header>{title}{extra}</header>{children}</section>
    ),
    Empty: ({ children, description }: { children?: React.ReactNode; description?: React.ReactNode }) => <div>{description}{children}</div>,
    Form: Object.assign(FormMock, { useForm: () => [formApi] }),
    Input: field('text'),
    InputNumber: field('number'),
    Select: ({ id, 'aria-label': ariaLabel }: { id?: string; 'aria-label'?: string }) => <select id={id} aria-label={ariaLabel} />,
    Skeleton: () => <div />,
    Space: ({ children }: { children?: React.ReactNode }) => <span>{children}</span>,
    Switch: ({ id, 'aria-label': ariaLabel }: { id?: string; 'aria-label'?: string }) => <button type="button" id={id} aria-label={ariaLabel} />,
    Table: () => <div data-testid="table" />,
    Tag: ({ children }: { children?: React.ReactNode }) => <span>{children}</span>,
    TimePicker: field('time'),
    message: { success: mocks.messageSuccess, error: mocks.messageError },
  };
});

import SyncPage from '../SyncPage';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
(globalThis as unknown as { matchMedia: unknown }).matchMedia = () => ({
  matches: false, media: '', onchange: null,
  addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {},
  dispatchEvent: () => false,
});

const CONFIG = (targetCode: string, version = 1) => ({
  target_code: targetCode,
  enabled: false,
  schedule_type: 'interval' as const,
  interval_minutes: 360,
  daily_time: '02:00',
  timezone: 'Asia/Shanghai',
  next_run_at: null,
  last_run_at: null,
  last_success_at: null,
  target_version: version,
  last_error_code: '',
  last_error_message: '',
  updated_at: '2026-09-20T02:00:00Z',
});

const TARGETS = [
  {
    code: 'directory', displayName: '组织通讯录', description: '目录', dependencies: [],
    metricKeys: ['departments'], config: CONFIG('directory'), legacyFallback: false,
  },
  {
    code: 'user_groups', displayName: '飞书用户组', description: '用户组', dependencies: ['directory'],
    metricKeys: ['groups'], config: CONFIG('user_groups'), legacyFallback: false,
  },
];

const mounted: Array<{ host: HTMLElement; root: Root }> = [];
const flush = async (ms = 0) => act(async () => { await new Promise((resolve) => setTimeout(resolve, ms)); });

async function mountPage() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  mounted.push({ host, root });
  await act(async () => { root.render(<SyncPage />); });
  await flush(20);
}

function click(element: Element) {
  return act(async () => { element.dispatchEvent(new MouseEvent('click', { bubbles: true })); });
}

beforeEach(() => {
  mocks.loadSyncTargets.mockReset().mockResolvedValue(TARGETS);
  mocks.loadSyncJobs.mockReset().mockResolvedValue([]);
  mocks.saveSyncTargetConfig.mockReset().mockImplementation(async (code: string) => CONFIG(code, 2));
  mocks.triggerOneSyncTarget.mockReset();
  mocks.triggerAllSyncTargets.mockReset();
  mocks.messageError.mockReset();
  mocks.messageSuccess.mockReset();
});

afterEach(async () => {
  while (mounted.length) {
    const { host, root } = mounted.pop()!;
    await act(async () => { root.unmount(); });
    host.remove();
  }
  document.body.innerHTML = '';
});

describe('SyncPage desktop mutation generation', () => {
  it('does not let a pre-save full refresh overwrite the saved target', async () => {
    await mountPage();
    let releaseTargets!: (value: unknown) => void;
    let releaseJobs!: (value: unknown) => void;
    mocks.loadSyncTargets.mockImplementationOnce(() => new Promise((resolve) => { releaseTargets = resolve; }));
    mocks.loadSyncJobs.mockImplementationOnce(() => new Promise((resolve) => { releaseJobs = resolve; }));

    await click(document.querySelector('button[aria-label="刷新同步管理"]')!);
    await click(document.querySelector('button[aria-label="保存组织通讯录设置"]')!);
    await flush(20);

    const directoryCard = Array.from(document.querySelectorAll('section'))
      .find((section) => section.firstElementChild?.tagName === 'HEADER'
        && section.firstElementChild.textContent?.includes('组织通讯录'));
    expect(directoryCard?.textContent).toContain('版本：2');

    await act(async () => {
      releaseTargets(TARGETS);
      releaseJobs([]);
    });
    await flush(20);

    expect(directoryCard?.textContent).toContain('版本：2');
    expect(directoryCard?.textContent).not.toContain('版本：1');
  });

  it('associates desktop Form.Item labels with the exact custom control IDs', async () => {
    await mountPage();
    const id = 'sync-target-directory-desktop-interval-minutes';
    const control = document.getElementById(id);
    const label = document.querySelector(`label[for="${id}"]`);

    expect(control).toBeTruthy();
    expect(label).toBeTruthy();
    expect(label?.getAttribute('for')).toBe(control?.id);
  });
});
