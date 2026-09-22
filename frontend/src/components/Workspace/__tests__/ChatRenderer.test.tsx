// @vitest-environment jsdom
import React from 'react';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import {
  MemoryRouter,
  Route,
  Routes,
  useLocation,
} from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/components/Chat', () => ({
  RunChatPanel: ({ application }: { application?: { name: string } }) => <div>chat panel {application?.name}</div>,
}));
vi.mock('../ApplicationSwitcher', () => ({ default: () => <div>switcher</div> }));
vi.mock('@/workbench/home/WorkbenchHome', () => ({ default: () => null }));
vi.mock('@/workbench/home/AgentWorkspaceCollections', () => ({ default: () => null }));

import ChatRenderer from '../ChatRenderer';
import HomeWorkspace from '../HomeWorkspace';
import MobileAppShell from '@/shell/MobileAppShell';
import { useWorkspaceBootstrapStore } from '@/stores/useWorkspaceBootstrapStore';
import { useApplicationEntityStore } from '@/stores/useApplicationEntityStore';
const originalLoad = useWorkspaceBootstrapStore.getState().load;
const originalEnsure = useApplicationEntityStore.getState().ensure;
import type { V2Application } from '@/services/runApi';
import { useRunChatStore } from '@/stores/useRunChatStore';
import { useWorkspaceStore, workspaceStateOf } from '@/stores/useWorkspaceStore';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const roots: Root[] = [];

const application = {
  id: 7,
  slug: 'finance-agent',
  name: '财务智能体',
  description: '',
  icon: '🤖',
  kind: 'chat',
  renderer_key: 'chat',
  runtime_type: 'agent',
} as V2Application;

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location" data-state={JSON.stringify(location.state)}>{location.pathname}{location.search}</div>;
}

async function renderChat(home: React.ReactNode = <div>home</div>) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  roots.push(root);
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/chat/finance-agent?conversation=123']}>
        <LocationProbe />
        <Routes>
          <Route path="/chat/:applicationSlug" element={<ChatRenderer application={application} />} />
          <Route path="/" element={home} />
        </Routes>
      </MemoryRouter>,
    );
    await Promise.resolve();
  });
  return host;
}

beforeEach(() => {
  useWorkspaceStore.setState({ workspaces: {} });
  useWorkspaceStore.getState().rememberConversation(application.id, 123);
  useWorkspaceStore.getState().setDraft(application.id, '未完成内容');
  useRunChatStore.getState().setActiveConversation(123);
});

afterEach(async () => {
  while (roots.length) await act(async () => roots.pop()?.unmount());
  useWorkspaceBootstrapStore.setState({load: originalLoad, defaultApplication: null});
  useApplicationEntityStore.setState({ensure: originalEnsure});
  document.body.innerHTML = '';
});

describe('ChatRenderer desktop new task', () => {
  it('clears the current task and returns to the home route', async () => {
    const host = await renderChat();
    expect(host.querySelector('[data-testid="location"]')?.textContent)
      .toBe('/chat/finance-agent?conversation=123');

    const button = Array.from(host.querySelectorAll('button'))
      .find((item) => item.textContent?.includes('新任务'));
    expect(button).toBeTruthy();
    await act(async () => { button!.click(); await Promise.resolve(); });

    expect(host.querySelector('[data-testid="location"]')?.textContent).toBe('/');
    expect(host.textContent).toContain('home');
    expect(JSON.parse(host.querySelector('[data-testid="location"]')!.getAttribute('data-state')!)).toEqual({ newTaskApplicationId: application.id });
    expect(useRunChatStore.getState().activeConversationId).toBeNull();
    expect(workspaceStateOf(
      useWorkspaceStore.getState().workspaces,
      application.id,
    )).toMatchObject({ conversationId: null, draft: '', scrollTop: 0 });
  });
});


it('carries the agent through the real chat-to-home transition when the default is different', async () => {
  useWorkspaceBootstrapStore.setState({defaultApplication:{...application,id:99,name:'其他默认智能体',slug:'other'},load:vi.fn().mockResolvedValue(undefined)});
  const ensure=vi.fn().mockResolvedValue(application);
  useApplicationEntityStore.setState({ensure});
  const host=await renderChat(<HomeWorkspace />);
  const button=Array.from(host.querySelectorAll('button')).find(item=>item.textContent?.includes('新任务'))!;
  await act(async()=>button.click());
  expect(host.querySelector('[data-testid="location"]')?.textContent).toBe('/');
  expect(ensure).toHaveBeenCalledWith(application.id, expect.anything());
  expect(host.textContent).toContain('chat panel 财务智能体');
  expect(host.textContent).not.toContain('其他默认智能体');
  expect(useRunChatStore.getState().activeConversationId).toBeNull();
});

it('mobile shell and chat renderer do not race the return-home navigation', async () => {
  useWorkspaceBootstrapStore.setState({defaultApplication:{...application,id:99,name:'其他默认智能体'},load:vi.fn().mockResolvedValue(undefined)});
  useApplicationEntityStore.setState({ensure:vi.fn().mockResolvedValue(application)});
  useWorkspaceStore.getState().openApplication(application.id);
  const host=document.createElement('div');document.body.appendChild(host);
  const root=createRoot(host);roots.push(root);
  await act(async()=>root.render(<MemoryRouter initialEntries={['/chat/finance-agent?conversation=123']}>
    <LocationProbe />
    <Routes><Route element={<MobileAppShell chrome={{hideHeader:false,hideSidebar:false,padded:false,mobile:{showAgentSwitcher:false}}} />}>
      <Route path="/chat/:applicationSlug" element={<ChatRenderer application={application} />} />
      <Route path="/" element={<HomeWorkspace />} />
    </Route></Routes>
  </MemoryRouter>));
  await act(async()=>(host.querySelector('.mobile-shell__task-btn') as HTMLButtonElement).click());
  expect(host.querySelector('[data-testid="location"]')?.textContent).toBe('/');
  expect(host.textContent).toContain('chat panel 财务智能体');
  expect(host.textContent).not.toContain('其他默认智能体');
  expect(workspaceStateOf(useWorkspaceStore.getState().workspaces,application.id)).toMatchObject({conversationId:null,draft:'',scrollTop:0});
});
