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
  RunChatPanel: () => <div>chat panel</div>,
}));
vi.mock('../ApplicationSwitcher', () => ({ default: () => <div>switcher</div> }));

import ChatRenderer from '../ChatRenderer';
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
  return <div data-testid="location">{location.pathname}{location.search}</div>;
}

async function renderChat() {
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
          <Route path="/" element={<div>home</div>} />
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

afterEach(() => {
  while (roots.length) roots.pop()?.unmount();
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
    expect(useRunChatStore.getState().activeConversationId).toBeNull();
    expect(workspaceStateOf(
      useWorkspaceStore.getState().workspaces,
      application.id,
    )).toMatchObject({ conversationId: null, draft: '', scrollTop: 0 });
  });
});

