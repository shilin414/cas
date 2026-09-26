// @vitest-environment jsdom

import React, { act, useMemo, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import {
  createMemoryRouter,
  MemoryRouter,
  Route,
  RouterProvider,
  Routes,
  useLocation,
} from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  fetchWorkspaceBootstrap: vi.fn(),
}));

vi.mock('@/services/runApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/services/runApi')>()),
  fetchWorkspaceBootstrap: mocks.fetchWorkspaceBootstrap,
}));

vi.mock('antd', () => ({
  Button: ({ children, icon, onClick, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { icon?: React.ReactNode }) => (
    <button type="button" onClick={onClick} {...props}>{icon}{children}</button>
  ),
  Drawer: ({ open, children, title, onClose, afterOpenChange }: {
    open: boolean;
    children: React.ReactNode;
    title?: React.ReactNode;
    onClose: () => void;
    afterOpenChange?: (open: boolean) => void;
  }) => {
    if (typeof afterOpenChange === 'function') {
      // 真实 Drawer 在动画结束后回调；测试里同步通知关闭完成，导航在
      // afterOpenChange(false) 之后才发生（Architecture 2.0 §21）。
      (globalThis as unknown as { __drawerAfterOpenChange?: (open: boolean) => void }).__drawerAfterOpenChange = afterOpenChange;
    }
    return open ? (
      <aside data-testid="drawer">
        <div>{title}</div>
        <button type="button" onClick={onClose}>关闭抽屉</button>
        {children}
      </aside>
    ) : null;
  },
}));
vi.mock('@/components/ConversationHistory/ConversationHistory', () => ({
  default: ({ onNewConversation }: { onNewConversation: () => void }) => (
    <button type="button" onClick={onNewConversation}>最近会话</button>
  ),
}));
vi.mock('@/components/AccountMenu/AccountMenu', () => ({
  default: () => <div>账号</div>,
}));
vi.mock('@/components/Mobile/MobileAgentSwitcher', () => ({
  default: () => <div>智能体切换</div>,
}));
vi.mock('@/components/Theme', () => ({
  ThemePicker: () => <div>主题</div>,
}));

import MobileAppShell from '@/shell/MobileAppShell';
import { useMobileHeaderAction } from '@/shell/mobileHeader';
import { useAuthStore } from '@/stores/useAuthStore';
import { useApplicationEntityStore } from '@/stores/useApplicationEntityStore';
import type { V2Application } from '@/services/runApi';
import { useNavigationPreferencesStore } from '@/stores/useNavigationPreferencesStore';
import { useWorkspaceBootstrapStore } from '@/stores/useWorkspaceBootstrapStore';
import { useWorkspaceStore } from '@/stores/useWorkspaceStore';
import { useRunChatStore } from '@/stores/useRunChatStore';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const roots: Array<{ host: HTMLElement; root: Root }> = [];

const LocationProbe = () => {
  const location = useLocation();
  return <output data-testid="location" data-state={JSON.stringify(location.state)}>{location.pathname}</output>;
};

function buttonByText(text: string): HTMLButtonElement {
  const button = Array.from(document.querySelectorAll<HTMLButtonElement>('button'))
    .find((item) => item.textContent?.includes(text));
  if (!button) throw new Error(`missing button: ${text}`);
  return button;
}

/** 关闭后驱动 Drawer 的 afterOpenChange（模拟动画结束）再断言导航。 */
async function closeDrawerAndFlush() {
  await act(async () => {
    const hook = (globalThis as unknown as { __drawerAfterOpenChange?: (open: boolean) => void }).__drawerAfterOpenChange;
    hook?.(false);
  });
}

// jsdom 的 matchMedia 默认不匹配 → useIsMobile=false（桌面）。
// 本文件全部场景是 Mobile Shell，统一 stub 成移动端（Commit 05 Root Replace）。
(globalThis as unknown as { matchMedia: unknown }).matchMedia = (query: string) => ({
  matches: true, media: query, onchange: null,
  addListener() {}, removeListener() {},
  addEventListener() {}, removeEventListener() {},
  dispatchEvent: () => false,
});

async function mountShell(initialPath: string) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  roots.push({ host, root });
  // data router 挂载（useAppNavigation/useRouteMeta 依赖 useMatches）。
  const router = createMemoryRouter([
    {
      path: '*',
      element: (
        <>
          <MobileAppShell chrome={{ hideHeader: false, hideSidebar: false, padded: false }} />
          <LocationProbe />
        </>
      ),
    },
  ], { initialEntries: [initialPath] });
  await act(async () => {
    root.render(<RouterProvider router={router} />);
  });
}

beforeEach(() => {
  mocks.fetchWorkspaceBootstrap.mockReset().mockResolvedValue({
    default_application: null,
    favorites: [],
    frequent: [],
    recent: [],
    recommended: [],
    recent_fixed_apps: [],
    recent_capabilities: [],
    recent_tasks: [],
    agent_categories: [],
    app_categories: [],
  });
  useWorkspaceBootstrapStore.getState().clear();
  useApplicationEntityStore.setState({ byId: {}, bySlug: {}, validatedAtById: {} });
  useWorkspaceStore.setState({ mobileNavOpen: false, activeApplicationId: null });
  useNavigationPreferencesStore.setState({ iconMode: 'outline', icons: {} });
  useAuthStore.setState({
    user: {
      id: '1', username: 'tester', email: '', role: 'user',
      display_name: '测试用户', created_at: '', is_staff: true,
    },
    isAuthenticated: true,
  });
});

afterEach(async () => {
  while (roots.length) {
    const { host, root } = roots.pop()!;
    await act(async () => root.unmount());
    host.remove();
  }
  document.body.innerHTML = '';
});

describe('MobileAppShell navigation regression', () => {
  it('loads workspace bootstrap when a direct mobile route mounts', async () => {
    await mountShell('/chat/main-agent');

    expect(mocks.fetchWorkspaceBootstrap).toHaveBeenCalledTimes(1);
    expect(useWorkspaceBootstrapStore.getState().dirty).toBe(false);
  });

  it('reloads a dirty workspace bootstrap once without a render loop', async () => {
    await mountShell('/agents');
    expect(mocks.fetchWorkspaceBootstrap).toHaveBeenCalledTimes(1);

    await act(async () => {
      useWorkspaceBootstrapStore.getState().invalidate();
    });

    expect(mocks.fetchWorkspaceBootstrap).toHaveBeenCalledTimes(2);
    expect(useWorkspaceBootstrapStore.getState().dirty).toBe(false);

    await act(async () => {
      await Promise.resolve();
    });
    expect(mocks.fetchWorkspaceBootstrap).toHaveBeenCalledTimes(2);
  });

  it('forces a fresh request when invalidated during the initial load', async () => {
    let resolveInitial!: (value: any) => void;
    mocks.fetchWorkspaceBootstrap
      .mockReset()
      .mockImplementationOnce(() => new Promise((resolve) => { resolveInitial = resolve; }))
      .mockResolvedValueOnce({
        default_application: null, favorites: [], frequent: [], recent: [],
        recommended: [], recent_fixed_apps: [], recent_capabilities: [],
        recent_tasks: [], agent_categories: [], app_categories: [],
      });

    await mountShell('/agents');
    expect(mocks.fetchWorkspaceBootstrap).toHaveBeenCalledTimes(1);
    await act(async () => {
      useWorkspaceBootstrapStore.getState().invalidate();
      resolveInitial({
        default_application: null, favorites: [], frequent: [], recent: [],
        recommended: [], recent_fixed_apps: [], recent_capabilities: [],
        recent_tasks: [], agent_categories: [], app_categories: [],
      });
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(mocks.fetchWorkspaceBootstrap).toHaveBeenCalledTimes(2);
    expect(useWorkspaceBootstrapStore.getState().dirty).toBe(false);
  });

  it('opens the drawer, keeps supporting surfaces, marks chat active, and closes after navigation', async () => {
    await mountShell('/chat/main-agent');

    const menuButton = document.querySelector<HTMLButtonElement>('[aria-label="打开导航"]');
    if (!menuButton) throw new Error('missing mobile menu button');
    await act(async () => menuButton.click());
    const drawer = document.querySelector('[data-testid="drawer"]');
    expect(drawer).toBeTruthy();
    expect(drawer?.textContent).toContain('最近任务');
    expect(drawer?.textContent).toContain('最近使用');
    expect(drawer?.textContent).toContain('账号');

    const home = buttonByText('首页');
    expect(home.getAttribute('aria-current')).toBe('page');
    expect(home.querySelector('.mobile-shell__nav-icon')).toBeTruthy();

    await act(async () => buttonByText('应用').click());
    // 点击后抽屉立即开始关闭；导航发生在动画结束（afterOpenChange false）后。
    expect(document.querySelector('[data-testid="location"]')?.textContent).toBe('/chat/main-agent');
    await closeDrawerAndFlush();
    expect(document.querySelector('[data-testid="drawer"]')).toBeNull();
    expect(document.querySelector('[data-testid="location"]')?.textContent).toBe('/apps');
  });

  it('mobile root navigation replaces browser history instead of stacking entries (Commit 05)', async () => {
    // MemoryRouter 起始为 POP；root 切换必须 REPLACE —— / → /agents → /apps
    // 不形成连续返回栈。外壳通过 data router 挂载（useMatches 需要）。
    const host = document.createElement('div');
    document.body.appendChild(host);
    const root = createRoot(host);
    roots.push({ host, root });
    const router = createMemoryRouter([
      {
        path: '*',
        element: (
          <>
            <MobileAppShell chrome={{ hideHeader: false, hideSidebar: false, padded: false }} />
            <LocationProbe />
          </>
        ),
      },
    ], { initialEntries: ['/'] });
    await act(async () => {
      root.render(<RouterProvider router={router} />);
    });

    await act(async () => {
      document.querySelector<HTMLButtonElement>('[aria-label="打开导航"]')!.click();
    });
    await act(async () => buttonByText('应用').click());
    await closeDrawerAndFlush();
    expect(router.state.location.pathname).toBe('/apps');
    expect(router.state.historyAction).toBe('REPLACE');

    await act(async () => {
      document.querySelector<HTMLButtonElement>('[aria-label="打开导航"]')!.click();
    });
    await act(async () => buttonByText('自动化').click());
    await closeDrawerAndFlush();
    expect(router.state.location.pathname).toBe('/schedules');
    expect(router.state.historyAction).toBe('REPLACE');
  });

  it('returns to the mobile home route when 新任务 is clicked from a chat', async () => {
    const application = {
      id: 1,
      slug: 'main-agent',
      name: '主智能体',
      description: '',
      icon: 'AI',
      kind: 'chat',
      runtime_type: 'agent',
      provider_key: 'feishu_aily',
      capabilities: {},
    } satisfies V2Application;
    useApplicationEntityStore.setState({
      byId: { [application.id]: application },
      bySlug: { [application.slug]: application },
    });
    useWorkspaceStore.setState({ activeApplicationId: application.id });

    await mountShell('/chat/main-agent');
    await act(async () => buttonByText('新任务').click());

    expect(document.querySelector('[data-testid="location"]')?.textContent).toBe('/');
  });
});

// ── Route-aware header (开发执行报告 §5/§78) ───────────────────────────

/** A page that binds the shell's trailing action, like MobileScheduleCenter. */
const HeaderActionProbe = () => {
  const [count, setCount] = useState(0);
  const options = useMemo(() => ({ onAction: () => setCount((c) => c + 1) }), []);
  useMobileHeaderAction(options);
  return <output data-testid="probe">{count}</output>;
};

/**
 * The enterprise resource page scenario: the page declares the create action
 * over a route-meta-driven detail header（审查 MAJOR 回归：层合并不 clobber）。
 */
const EnterpriseResourceProbe = () => {
  const [count, setCount] = useState(0);
  const create = useMemo(() => ({
    action: 'create' as const,
    onAction: () => setCount((c) => c + 1),
  }), []);
  useMobileHeaderAction(create);
  return <output data-testid="probe">{count}</output>;
};

async function mountShellWithChrome(
  chrome: Record<string, unknown>,
  child: React.ReactElement = <LocationProbe />,
  initialPath = '/schedules',
) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  roots.push({ host, root });
  const router = createMemoryRouter([
    {
      path: '*',
      element: <MobileAppShell chrome={chrome as never} />,
      children: [{ path: '*', element: child }],
    },
  ], { initialEntries: [initialPath] });
  await act(async () => {
    root.render(<RouterProvider router={router} />);
  });
}

describe('MobileAppShell route-aware header', () => {
  it('hides the top agent switcher on the home workspace', async () => {
    await mountShellWithChrome({
      hideHeader: false, hideSidebar: false, padded: false,
      mobile: { mode: 'workspace', action: 'new-task', showAgentSwitcher: false },
    }, undefined, '/');

    expect(document.body.textContent).not.toContain('智能体切换');
    expect(document.querySelector('.mobile-shell__workspace-spacer')).toBeTruthy();
    expect(buttonByText('新任务')).toBeTruthy();
  });
  it('page mode swaps the switcher for the page title (§5 智能体中心)', async () => {
    await mountShellWithChrome({
      hideHeader: false, hideSidebar: false, padded: true,
      mobile: { mode: 'page', title: '智能体中心' },
    }, undefined, '/agents');

    expect(document.querySelector('.mobile-shell__title')?.textContent)
      .toBe('智能体中心');
    expect(document.body.textContent).not.toContain('智能体切换');
    expect(document.body.textContent).not.toContain('新任务');
    // ☰ still opens the drawer.
    expect(document.querySelector('[aria-label="打开导航"]')).toBeTruthy();
  });

  it('detail mode backs to the console root instead of history (§5 企业二级页面)', async () => {
    // Direct Link 进入企业二级页：route meta parent = /enterprise，
    // 顶栏返回必须 replace 回父页面，而不是 PUSH 新的 /enterprise。
    const host = document.createElement('div');
    document.body.appendChild(host);
    const root = createRoot(host);
    roots.push({ host, root });
    const router = createMemoryRouter([
      {
        path: '/enterprise',
        element: <output data-testid="location">/enterprise</output>,
      },
      {
        path: '/enterprise/resources/agents',
        element: (
          <MobileAppShell chrome={{ hideHeader: false, hideSidebar: false, padded: true, mobile: { mode: 'console', title: '企业控制台' } }} />
        ),
        handle: {
          app: {
            id: 'enterprise-resource-agents',
            level: 'detail',
            root: '/enterprise',
            parent: '/enterprise',
            mobile: { mode: 'detail', title: '智能体管理' },
          },
        },
        children: [
          { index: true, element: <LocationProbe /> },
        ],
      },
    ], { initialEntries: ['/enterprise/resources/agents'] });
    await act(async () => {
      root.render(<RouterProvider router={router} />);
    });
    await act(async () => { await Promise.resolve(); });

    expect(document.querySelector('.mobile-shell__title')?.textContent)
      .toBe('智能体管理');
    const back = document.querySelector<HTMLButtonElement>('[aria-label="返回企业控制台"]');
    expect(back).toBeTruthy();
    await act(async () => back!.click());
    expect(router.state.location.pathname).toBe('/enterprise');
    expect(router.state.historyAction).toBe('REPLACE');
  });

  it('schedules create action fires the page-bound onAction (§5 自动化 ＋)', async () => {
    await mountShellWithChrome({
      hideHeader: false, hideSidebar: false, padded: true,
      mobile: { mode: 'page', title: '自动化', action: 'create' },
    }, <HeaderActionProbe />, '/schedules');

    const create = document.querySelector<HTMLButtonElement>('[aria-label="新建"]');
    expect(create).toBeTruthy();
    await act(async () => create!.click());
    expect(document.querySelector('[data-testid="probe"]')?.textContent).toBe('1');
  });

  it('keeps the create action disabled until a page binds onAction', async () => {
    await mountShellWithChrome({
      hideHeader: false, hideSidebar: false, padded: true,
      mobile: { mode: 'page', title: '自动化', action: 'create' },
    }, undefined, '/schedules');

    const create = document.querySelector<HTMLButtonElement>('[aria-label="新建"]');
    expect(create?.disabled).toBe(true);
  });

  it('merges nested header layers instead of clobbering (企业资源页 ＋)', async () => {
    // Commit 06 起：静态 mode/title 由 Route Meta 决定；动态层只覆盖 action。
    // 无 meta 时 chrome 静态值保持；这里验证页面层 action 与 chrome 层合并
    // （不 clobber）—— mode/title 的 meta 驱动由 detail-mode 测试单独覆盖。
    await mountShellWithChrome({
      hideHeader: false, hideSidebar: false, padded: true,
      mobile: { mode: 'console', title: '企业控制台' },
    }, <EnterpriseResourceProbe />, '/enterprise/resources/agents');

    // chrome 静态 title 保留（detail 标题此后由 enterprise 子路由 meta 提供）。
    expect(document.querySelector('.mobile-shell__title')?.textContent)
      .toBe('企业控制台');
    // …AND the create action from the page inside it.
    const create = document.querySelector<HTMLButtonElement>('[aria-label="新建"]');
    expect(create).toBeTruthy();
    expect(create?.disabled).toBe(false);
    await act(async () => create!.click());
    expect(document.querySelector('[data-testid="probe"]')?.textContent).toBe('1');
  });

  it('hides 企业控制台 from the drawer for non-staff users (§78)', async () => {
    useAuthStore.setState({
      user: {
        id: '2', username: 'user', email: '', role: 'user',
        display_name: '普通用户', created_at: '', is_staff: false,
      },
      isAuthenticated: true,
    });
    await mountShell('/');

    await act(async () => {
      document.querySelector<HTMLButtonElement>('[aria-label="打开导航"]')!.click();
    });
    const drawer = document.querySelector('[data-testid="drawer"]');
    expect(drawer?.textContent).not.toContain('企业控制台');
  });
});

it('mobile new task carries the current chat agent and clears its old context', async () => {
  useWorkspaceStore.setState({activeApplicationId:7});
  useWorkspaceStore.getState().rememberConversation(7,123);
  useWorkspaceStore.getState().setDraft(7,'旧草稿');
  useRunChatStore.getState().setActiveConversation(123);
  await mountShell('/chat/it-agent');
  await act(async()=>buttonByText('新任务').click());
  expect(JSON.parse(document.querySelector('[data-testid="location"]')!.getAttribute('data-state')!)).toEqual({newTaskApplicationId:7});
  expect(useWorkspaceStore.getState().workspaces[7]).toMatchObject({conversationId:null,draft:''});
  expect(useRunChatStore.getState().activeConversationId).toBeNull();
});
