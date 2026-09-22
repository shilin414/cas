/** @vitest-environment jsdom */
import React from 'react';
import { act } from 'react-dom/test-utils';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import SharePage from '../SharePage';

vi.mock('react-router-dom', () => ({ useParams: () => ({ token: 'test-share' }) }));
vi.mock('@/services/shareApi', () => ({ getPublicShare: vi.fn(async () => ({
  title: '分享', shared_at: '2026-09-22T00:00:00Z', messages: [
    { role: 'assistant', content: '回复', created_at: '', agent_name: '创作助手', agent_icon: '✨' },
    { role: 'user', content: '问题', created_at: '', agent_name: '不可误用的名称', agent_icon: '🤖' },
  ],
})) }));
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

beforeEach(() => {
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: false, media: query, onchange: null, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent: () => false }));
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
});
afterEach(() => vi.unstubAllGlobals());

it('shows pinned agent name/icon without attributing user messages to the agent', async () => {
  const host = document.createElement('div'); document.body.append(host);
  const root = createRoot(host);
  try {
    await act(async () => { root.render(<SharePage />); });
    const names = [...host.querySelectorAll('.share-msg__sender')].map(node => node.textContent);
    expect(names).toEqual(['创作助手', '用户']);
    expect(host.querySelector('.share-msg__avatar')?.textContent).toContain('✨');
    expect(host.textContent).not.toContain('不可误用的名称');
    expect(host.textContent).toContain('小安工作助手');
  } finally { act(() => root.unmount()); host.remove(); }
});
