// @vitest-environment jsdom
import React from 'react';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  fetchApplicationPage: vi.fn(),
  fetchTasks: vi.fn(),
}));

vi.mock('@/services/runApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/services/runApi')>()),
  fetchApplicationPage: mocks.fetchApplicationPage,
}));
vi.mock('@/services/taskApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/services/taskApi')>()),
  fetchTasks: mocks.fetchTasks,
}));

import CapabilityPickerCore from '../capability/CapabilityPickerCore';
import type { ApplicationSummary } from '@/services/runApi';
import { useWorkspaceBootstrapStore } from '@/stores/useWorkspaceBootstrapStore';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const roots: Root[] = [];

function app(id: number, name: string, kind: string): ApplicationSummary {
  return { id, slug: `app-${id}`, name, kind, description: '', icon: kind === 'chat' ? '🤖' : '▦' };
}

async function renderPicker() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  roots.push(root);
  await act(async () => {
    root.render(<CapabilityPickerCore onSelect={() => {}} onTaskSelect={() => {}} />);
    await Promise.resolve();
  });
  return host;
}

async function clickTab(host: HTMLElement, label: string) {
  const button = Array.from(host.querySelectorAll('button')).find((item) => item.textContent === label);
  expect(button).toBeTruthy();
  await act(async () => { button!.click(); await Promise.resolve(); });
}

beforeEach(() => {
  mocks.fetchApplicationPage.mockReset();
  mocks.fetchTasks.mockReset().mockResolvedValue({ items: [], nextCursor: '' });
  useWorkspaceBootstrapStore.setState({
    recentCapabilities: [app(1, '最近智能体', 'chat'), app(2, '最近应用', 'custom')],
  });
});

afterEach(() => {
  while (roots.length) roots.pop()?.unmount();
  document.body.innerHTML = '';
});

describe('CapabilityPickerCore', () => {
  it('prefetches both capability groups once and switches tabs locally without a loading flash', async () => {
    mocks.fetchApplicationPage.mockImplementation(async ({ kind }: { kind?: string }) => ({
      items: kind === 'chat'
        ? [app(11, '财务智能体', 'chat')]
        : [app(22, '报表应用', 'dashboard')],
      next_cursor: '',
      has_more: false,
    }));

    const host = await renderPicker();
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });

    expect(mocks.fetchApplicationPage).toHaveBeenCalledTimes(2);
    expect(mocks.fetchApplicationPage).toHaveBeenCalledWith(expect.objectContaining({ kind: 'chat', q: '' }));
    expect(mocks.fetchApplicationPage).toHaveBeenCalledWith(expect.objectContaining({ kind: 'fixed', q: '' }));
    expect(host.textContent).toContain('财务智能体');
    expect(host.textContent).toContain('报表应用');

    await clickTab(host, '智能体');
    expect(host.textContent).toContain('财务智能体');
    expect(host.textContent).not.toContain('报表应用');
    expect(host.textContent).not.toContain('正在搜索');

    await clickTab(host, '应用');
    expect(host.textContent).toContain('报表应用');
    expect(host.textContent).not.toContain('财务智能体');
    expect(host.textContent).not.toContain('正在搜索');
    expect(mocks.fetchApplicationPage).toHaveBeenCalledTimes(2);
  });
});
