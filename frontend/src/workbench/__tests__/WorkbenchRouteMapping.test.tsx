// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => {
  const workflow = {
    id: 101,
    slug: 'monthly-report',
    name: 'Monthly report',
    description: '',
    icon: 'W',
    kind: 'workflow',
  };
  const rendererChat = {
    id: 102,
    slug: 'legacy-chat',
    name: 'Legacy chat',
    description: '',
    icon: 'C',
    kind: 'custom',
    renderer_key: 'chat',
  };
  return {
    workflow,
    rendererChat,
    navigate: vi.fn(),
    close: vi.fn(),
    openApplication: vi.fn(),
    rememberConversation: vi.fn(),
    setSearchParams: vi.fn(),
    loadBootstrap: vi.fn().mockResolvedValue(undefined),
    ensureApplication: vi.fn(),
    recentCapabilities: [workflow, rendererChat],
  };
});

vi.mock('react-router-dom', () => ({
  useNavigate: () => mocks.navigate,
  useLocation: () => ({ pathname: '/' }),
  useSearchParams: () => [new URLSearchParams(), mocks.setSearchParams],
}));
vi.mock('@ant-design/icons', () => ({
  LeftOutlined: () => null,
  RightOutlined: () => null,
  SearchOutlined: () => null,
}));
vi.mock('@/shell/useIsMobile', () => ({ useIsMobile: () => false }));
vi.mock('@/stores/useAuthStore', () => ({
  useAuthStore: (selector: (state: any) => unknown) => selector({ user: { is_staff: false } }),
}));
vi.mock('@/stores/useWorkbenchUiStore', () => ({
  useWorkbenchUiStore: (selector: (state: any) => unknown) => selector({
    sidebarCollapsed: false,
    toggleSidebar: vi.fn(),
  }),
}));
vi.mock('@/stores/useWorkspaceBootstrapStore', () => ({
  useWorkspaceBootstrapStore: (selector: (state: any) => unknown) => selector({
    load: mocks.loadBootstrap,
    dirty: false,
    recentTasks: [],
    isLoading: false,
    defaultApplication: mocks.rendererChat,
    recommended: [],
  }),
}));
vi.mock('@/stores/useWorkspaceStore', () => ({
  useWorkspaceStore: (selector: (state: any) => unknown) => selector({
    openApplication: mocks.openApplication,
    rememberConversation: mocks.rememberConversation,
  }),
}));
vi.mock('@/stores/useApplicationEntityStore', () => ({
  useApplicationEntityStore: (selector: (state: any) => unknown) => selector({
    ensure: mocks.ensureApplication,
  }),
}));
vi.mock('@/components/Navigation', () => ({
  NavigationItemIcon: () => null,
  getVisibleNavigationItems: () => [],
  isNavigationItemActive: () => false,
}));
vi.mock('@/components/AccountMenu/AccountMenu', () => ({ default: () => null }));
vi.mock('@/components/Agents/AgentAvatar', () => ({ default: () => null }));
vi.mock('@/components/Navigation/RecentNavigationIcon', () => ({ default: () => null }));
vi.mock('@/workbench/tasks/RecentTaskList', () => ({ default: () => null }));
vi.mock('@/workbench/capability/useRecentCapabilities', () => ({
  useRecentCapabilities: () => mocks.recentCapabilities,
}));
vi.mock('@/workbench/capability/CapabilityPickerDialog', () => ({
  default: ({ open, onSelect }: { open: boolean; onSelect: (item: any) => void }) => open ? (
    <div>
      <button type="button" onClick={() => onSelect(mocks.workflow)}>picker-workflow</button>
      <button type="button" onClick={() => onSelect(mocks.rendererChat)}>picker-renderer-chat</button>
    </div>
  ) : null,
}));
vi.mock('@/workbench/capability/CapabilityPickerSheet', () => ({ default: () => null }));
vi.mock('@/components/Chat', () => ({
  RunChatPanel: ({ emptyState }: { emptyState: React.ReactNode }) => <>{emptyState}</>,
}));
vi.mock('@/workbench/home/WorkbenchHome', () => ({
  default: ({ onOpen }: { onOpen: (item: any) => void }) => (
    <div>
      <button type="button" onClick={() => onOpen(mocks.workflow)}>home-workflow</button>
      <button type="button" onClick={() => onOpen(mocks.rendererChat)}>home-renderer-chat</button>
    </div>
  ),
}));

import CapabilityPicker from '@/workbench/capability/CapabilityPicker';
import DesktopSidebar from '@/workbench/shell/DesktopSidebar';
import MobileWorkbenchDrawer from '@/workbench/shell/MobileWorkbenchDrawer';
import HomeWorkspace from '@/components/Workspace/HomeWorkspace';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

async function mount(node: React.ReactNode): Promise<{ host: HTMLDivElement; root: Root }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  await act(async () => { root.render(node); });
  return { host, root };
}

function clickButton(host: HTMLElement, label: string) {
  const button = [...host.querySelectorAll('button')]
    .find((candidate) => candidate.textContent?.includes(label));
  if (!button) throw new Error(`missing button: ${label}`);
  act(() => { button.click(); });
}

async function unmount(root: Root, host: HTMLElement) {
  await act(async () => { root.unmount(); });
  host.remove();
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('Workbench application route mapping', () => {
  it('routes capability-picker selections through workflow and renderer-key chat routes', async () => {
    const { host, root } = await mount(<CapabilityPicker open onClose={mocks.close} />);

    clickButton(host, 'picker-workflow');
    expect(mocks.openApplication).toHaveBeenCalledWith(mocks.workflow.id);
    expect(mocks.close).toHaveBeenCalledTimes(1);
    expect(mocks.navigate).toHaveBeenLastCalledWith('/workflow/monthly-report');

    clickButton(host, 'picker-renderer-chat');
    expect(mocks.openApplication).toHaveBeenCalledWith(mocks.rendererChat.id);
    expect(mocks.navigate).toHaveBeenLastCalledWith('/chat/legacy-chat');

    await unmount(root, host);
  });

  it('uses the workflow route for desktop recent capabilities', async () => {
    const { host, root } = await mount(<DesktopSidebar />);

    clickButton(host, mocks.workflow.name);
    expect(mocks.navigate).toHaveBeenLastCalledWith('/workflow/monthly-report');

    await unmount(root, host);
  });

  it('uses renderer_key=chat and pushes a page route from the mobile drawer', async () => {
    const { host, root } = await mount(
      <MobileWorkbenchDrawer
        onRootNavigate={(path) => { mocks.close(); mocks.navigate(path); }}
        onPageNavigate={(path) => { mocks.close(); mocks.navigate(path); }}
        close={mocks.close}
      />,
    );

    clickButton(host, mocks.rendererChat.name);
    expect(mocks.close).toHaveBeenCalledTimes(1);
    expect(mocks.navigate).toHaveBeenLastCalledWith('/chat/legacy-chat');

    await unmount(root, host);
  });

  it('routes home capability cards through the canonical mapper', async () => {
    const { host, root } = await mount(<HomeWorkspace />);

    clickButton(host, 'home-workflow');
    expect(mocks.openApplication).toHaveBeenCalledWith(mocks.workflow.id);
    expect(mocks.navigate).toHaveBeenLastCalledWith('/workflow/monthly-report');

    clickButton(host, 'home-renderer-chat');
    expect(mocks.openApplication).toHaveBeenCalledWith(mocks.rendererChat.id);
    expect(mocks.navigate).toHaveBeenLastCalledWith('/chat/legacy-chat');

    await unmount(root, host);
  });
});
