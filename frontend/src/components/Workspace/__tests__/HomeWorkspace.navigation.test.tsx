// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({ load: vi.fn().mockResolvedValue(undefined) }));
vi.mock('@/stores/useWorkspaceBootstrapStore', () => ({
  useWorkspaceBootstrapStore: (selector: (state: unknown) => unknown) => selector({
    isLoading: false, load: mocks.load,
    defaultApplication: { id: 7, slug: 'agent-seven', name: '默认智能体', kind: 'chat' },
  }),
}));
vi.mock('@/shell/useIsMobile', () => ({ useIsMobile: () => false }));
vi.mock('@/workbench/home/WorkbenchHome', () => ({ default: () => <h1>首页智能体</h1> }));
vi.mock('@/workbench/home/AgentWorkspaceCollections', () => ({ default: () => <section>首页任务与资源</section> }));
vi.mock('@/stores/useAuthStore', () => ({ useAuthStore: () => ({ user: { id: 1, username: 'test' } }) }));

import HomeWorkspace from '../HomeWorkspace';
import RunChatPanel from '@/components/Chat/RunChatPanel';
import { useRunChatStore } from '@/stores/useRunChatStore';
import { useWorkspaceStore } from '@/stores/useWorkspaceStore';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let host: HTMLDivElement;
let root: Root;

function Navigation() {
  const navigate = useNavigate();
  const location = useLocation();
  return <>
    <output>{location.pathname}{location.search}</output>
    <button onClick={() => navigate('/')}>首页</button>
    <button onClick={() => navigate(-1)}>后退</button>
    <button onClick={() => navigate(1)}>前进</button>
  </>;
}

beforeEach(() => {
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    matches: false, media: query, onchange: null,
    addListener: vi.fn(), removeListener: vi.fn(),
    addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn(),
  })));
  useRunChatStore.setState({
    activeConversationId: 42, isLoading: false, error: null,
    conversations: {
      42: { id: 42, title: '之前的任务', activeRunId: 'running-task', messages: [
        { id: '1', role: 'user', content: '不应残留在首页的任务内容', created_at: '2026-09-22T00:00:00Z' },
      ] },
    },
  });
  useWorkspaceStore.setState({ workspaces: {} });
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});
async function mount(initialPath = '/chat/agent-seven?conversation=42') {
  await act(async () => root.render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Navigation />
      <Routes>
        <Route path="/" element={<HomeWorkspace />} />
        <Route path="/chat/:slug" element={<RunChatPanel applicationId={7} conversationId={42} />} />
      </Routes>
    </MemoryRouter>,
  ));
}
async function click(label: string) {
  const button = Array.from(host.querySelectorAll('button')).find(item => item.textContent === label);
  expect(button).toBeTruthy();
  await act(async () => button!.click());
}
function expectHome() {
  expect(host.querySelector('output')?.textContent).toBe('/');
  expect(host.querySelector('.agent-home-surface')).not.toBeNull();
  expect(host.textContent).toContain('首页任务与资源');
  expect(host.textContent).not.toContain('不应残留在首页的任务内容');
  expect(useRunChatStore.getState().activeConversationId).toBeNull();
}

it('switches both URL and content to home without deleting the previous running task', async () => {
  await mount();
  expect(host.textContent).toContain('不应残留在首页的任务内容');
  await click('首页');
  expectHome();
  const previous = useRunChatStore.getState().conversations[42];
  expect(previous.activeRunId).toBe('running-task');
  expect(previous.messages[0].content).toBe('不应残留在首页的任务内容');
  await click('后退');
  expect(host.textContent).toContain('不应残留在首页的任务内容');
  await click('前进');
  expectHome();
});

it('clears stale active selection on direct and repeated home navigation', async () => {
  await mount('/');
  expectHome();
  await act(async () => useRunChatStore.getState().setActiveConversation(42));
  await click('首页');
  expectHome();
});
