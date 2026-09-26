/**
 * MobileResourcePage — 错误态回归（二次复审 P2-10）。
 *
 * useApplicationPage deliberately keeps loaded rows when loadMore fails; the
 * page used to drop the whole list behind `{!error && …}`. Pin the split:
 *   · fatal (first page failed, no data) → full error state + retry;
 *   · partial (a later page failed, rows loaded) → inline alert + rows survive.
 */
// @vitest-environment jsdom
import React from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react-dom/test-utils';

const mocks = vi.hoisted(() => ({
  loadMore: vi.fn(),
  refresh: vi.fn(),
  patchItem: vi.fn(),
  updateAgentApplication: vi.fn(),
  fetchWorkspaceBootstrap: vi.fn(),
}));

const pageState = {
  items: [] as Array<{
    id: number; name: string; slug: string; provider_key?: string;
    runtime_type?: string; enabled?: boolean; color?: string; is_default_agent?: boolean;
  }>,
  loading: false,
  loadingMore: false,
  hasMore: false,
  error: null as string | null,
  loadMore: mocks.loadMore,
  refresh: mocks.refresh,
  patchItem: mocks.patchItem,
};

vi.mock('@/hooks/useApplicationPage', () => ({
  useApplicationPage: vi.fn(() => pageState),
}));

vi.mock('@/services/runApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/services/runApi')>()),
  updateAgentApplication: mocks.updateAgentApplication,
  fetchWorkspaceBootstrap: mocks.fetchWorkspaceBootstrap,
}));

vi.mock('react-router-dom', () => ({
  useNavigate: () => vi.fn(),
  useLocation: () => ({ search: '', pathname: '/enterprise/resources/agents' }),
}));

vi.mock('@/shell/mobileHeader', () => ({
  useMobileHeaderAction: () => {},
}));

vi.mock('@/components/Agents/AgentEditorModal', () => ({
  default: () => <div data-testid="agent-editor" />,
}));
vi.mock('@/components/Agents/AgentAvatar', () => ({
  default: ({ application }: { application: { name: string } }) => (
    <span data-testid="avatar">{application.name}</span>
  ),
}));

vi.mock('antd', () => ({
  Alert: ({ message }: { message?: React.ReactNode }) => (
    <div role="alert">{message}</div>
  ),
  Button: ({ children, onClick }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" onClick={onClick}>{children}</button>
  ),
  Form: Object.assign(
    ({ children }: { children?: React.ReactNode }) => <form>{children}</form>,
    { useForm: () => [{ resetFields: vi.fn(), setFieldsValue: vi.fn() }] },
  ),
  Input: Object.assign(
    (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
    { TextArea: () => <textarea /> },
  ),
  Modal: Object.assign(
    ({ open }: { open?: boolean }) => (open ? <div data-testid="modal" /> : null),
    { confirm: vi.fn() },
  ),
  Select: () => <select />,
  Skeleton: () => <div data-testid="skeleton" />,
  message: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@/components/MobileConsole', () => ({
  MobileActionSheet: ({ open, actions }: { open?: boolean; actions?: Array<{ key: string; label: string; onClick: () => void }> }) => (
    open ? (
      <div data-testid="action-sheet">
        {actions?.map((action) => (
          <button type="button" key={action.key} onClick={action.onClick}>{action.label}</button>
        ))}
      </div>
    ) : null
  ),
  MobileEmptyState: ({ title, hint, action }: {
    title?: string; hint?: string; action?: React.ReactNode;
  }) => (
    <div data-testid="empty-state">
      <strong>{title}</strong>
      {hint}
      {action}
    </div>
  ),
  MobileEntityRow: ({ title, onMore }: { title: string; onMore?: () => void }) => (
    <div className="mobile-console-row">
      <span className="mobile-console-row__title">{title}</span>
      {onMore && <button type="button" className="mobile-console-row__more" onClick={onMore}>•••</button>}
    </div>
  ),
  MobilePage: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  MobileSearchBar: ({ placeholder }: { placeholder?: string }) => (
    <input className="mobile-console-search" placeholder={placeholder} readOnly />
  ),
}));

import MobileResourcePage from '../MobileResourcePage';

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
async function mountPage() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  mounted.push({ host, root });
  await act(async () => {
    root.render(<MobileResourcePage kind="chat" />);
  });
  await flush(20);
}

function click(el: Element) {
  return act(async () => {
    el.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    el.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
}

afterEach(async () => {
  while (mounted.length) {
    const { host, root } = mounted.pop()!;
    await act(async () => { root.unmount(); });
    host.remove();
  }
  document.body.innerHTML = '';
});

const app = (id: number, name: string) => ({
  id, name, slug: `slug-${id}`, provider_key: 'aily', runtime_type: 'agent',
});

// 权限语义收紧（Architecture 2.0 §10/§48）：identity 未加载时不再默认放行
// mutation；需要 manage 权限的用例必须显式注入 identity。
const grantIdentity = (codes: string[]) => {
  useAdminPermissionStore.setState({
    identity: {
      can_access_console: true,
      is_super_admin: false,
      roles: [],
      permissions: codes.map((code, index) => ({ id: index, code, category: '', name: code, description: '', created_at: '' })),
    },
    status: 'ready',
  });
};

beforeEach(() => {
  grantIdentity(['resource.agent.read', 'resource.agent.manage', 'access.policy.manage']);
  pageState.items = [];
  pageState.loading = false;
  pageState.loadingMore = false;
  pageState.hasMore = false;
  pageState.error = null;
  mocks.loadMore.mockReset();
  mocks.refresh.mockReset();
  mocks.patchItem.mockReset();
  mocks.updateAgentApplication.mockReset();
  mocks.fetchWorkspaceBootstrap.mockReset().mockResolvedValue({
    default_application: null, favorites: [], frequent: [], recent: [], recommended: [],
    recent_fixed_apps: [], recent_capabilities: [], recent_tasks: [],
    agent_categories: [], app_categories: [],
  });
});

describe('MobileResourcePage — error split (P2-10)', () => {
  it('fatal: first-page failure with no data renders the full error state + retry', async () => {
    pageState.error = '网络错误';
    await mountPage();

    const empty = document.querySelector('[data-testid="empty-state"]')!;
    expect(empty.textContent).toContain('加载资源失败');
    expect(Array.from(document.querySelectorAll('button'))
      .find((b) => b.textContent === '重试')).toBeTruthy();
  });

  it('partial: a failed loadMore keeps every rendered row', async () => {
    pageState.items = [app(1, '财务助手'), app(2, 'IT助手')];
    pageState.hasMore = true;
    pageState.error = '加载更多失败';
    await mountPage();

    // Rows survive…
    const titles = Array.from(document.querySelectorAll('.mobile-console-row__title'))
      .map((el) => el.textContent);
    expect(titles).toEqual(['财务助手', 'IT助手']);
    // …the inline alert surfaces…
    expect(document.body.textContent).toContain('加载失败');
    // …and exactly ONE retry CTA exists (§47–§48): the Alert explains, the
    // bottom button acts — never two retry entrypoints.
    const ctaTexts = Array.from(document.querySelectorAll('button'))
      .map((b) => b.textContent);
    expect(ctaTexts.filter((t) => t === '重试')).toHaveLength(0);
    expect(ctaTexts.filter((t) => t === '加载失败，点击重试')).toHaveLength(1);
    // …and the retry button re-runs loadMore (not a full refresh).
    const retry = Array.from(document.querySelectorAll('button'))
      .find((b) => b.textContent === '加载失败，点击重试');
    expect(retry).toBeTruthy();
    await click(retry!);
    expect(mocks.loadMore).toHaveBeenCalledTimes(1);
    expect(mocks.refresh).not.toHaveBeenCalled();
  });

  it('success + empty renders the real empty state', async () => {
    pageState.items = [];
    await mountPage();
    expect(document.querySelector('[data-testid="empty-state"]')!.textContent)
      .toContain('暂无智能体');
  });

  it('uses the server response when disabling the default agent', async () => {
    pageState.items = [{ ...app(7, '默认助手'), enabled: true, is_default_agent: true }];
    mocks.updateAgentApplication.mockResolvedValue({
      ...pageState.items[0],
      enabled: false,
      is_default_agent: false,
    });
    await mountPage();

    await click(document.querySelector('.mobile-console-row__more')!);
    const disable = Array.from(document.querySelectorAll('button'))
      .find((button) => button.textContent === '停用');
    expect(disable).toBeTruthy();
    await click(disable!);
    await flush();

    expect(mocks.patchItem).toHaveBeenCalledWith(7, expect.objectContaining({
      enabled: false,
      is_default_agent: false,
    }));
  });
});
