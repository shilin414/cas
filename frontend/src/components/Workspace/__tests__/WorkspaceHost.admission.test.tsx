// @vitest-environment jsdom

/**
 * WorkspaceHost POP 快速恢复测试（Architecture 2.0 §70–§75）。
 *
 *   consume snapshot → renderer 同步显示（不阻塞等待 resolve）
 *   manage cache only → 不同步 admit（宁 loading 不旧页面）
 *   background resolve success → 更新
 *   background transport error + fresh consume snapshot → 保留页面
 */
import React from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  resolveApplication: vi.fn(),
}));

vi.mock('@/services/runApi', async (importOriginal) => ({
  ...(await importOriginal<object>()),
  resolveApplication: mocks.resolveApplication,
}));

import WorkspaceHost from '@/components/Workspace/WorkspaceHost';
import { useApplicationEntityStore } from '@/stores/useApplicationEntityStore';
import { useWorkspaceStore } from '@/stores/useWorkspaceStore';
import type { V2Application } from '@/services/runApi';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as unknown as { matchMedia: unknown }).matchMedia = (query: string) => ({
  matches: false, media: query, onchange: null,
  addListener() {}, removeListener() {},
  addEventListener() {}, removeEventListener() {},
  dispatchEvent: () => false,
});

const application = (id: number, slug: string): V2Application => ({
  id, slug, name: `应用${id}`, description: '', icon: 'AI', kind: 'chat',
  runtime_type: 'agent', provider_key: 'feishu_aily', capabilities: {},
} as never);

const roots: Array<{ host: HTMLElement; root: Root }> = [];

async function mountHostAt(path: string) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  roots.push({ host, root });
  const router = createMemoryRouter([
    { path: '/', element: <WorkspaceHost kind="home" /> },
    { path: '/chat/:applicationSlug', element: <WorkspaceHost kind="chat" /> },
  ], { initialEntries: [path] });
  await act(async () => {
    root.render(<RouterProvider router={router} />);
  });
  return { host, root, router };
}

beforeEach(() => {
  mocks.resolveApplication.mockReset();
  useApplicationEntityStore.setState({ byId: {}, bySlug: {}, validatedAtById: {} });
  useWorkspaceStore.setState({ activeApplicationId: null });
});

afterEach(async () => {
  while (roots.length) {
    const { host, root } = roots.pop()!;
    await act(async () => root.unmount());
    host.remove();
  }
  document.body.innerHTML = '';
});

describe('WorkspaceHost consume-validated instant admission (§70–§75)', () => {
  it('renders synchronously from a fresh consume snapshot while revalidating', async () => {
    const app = application(7, 'main-agent');
    useApplicationEntityStore.setState({
      byId: { 7: app },
      bySlug: { 'main-agent': app },
      validatedAtById: { 7: Date.now() },
    });
    // resolve 挂起：consume 快照已足够同步显示。
    let resolve!: (value: V2Application) => void;
    mocks.resolveApplication.mockImplementationOnce(
      () => new Promise((res) => { resolve = res as typeof resolve; }));

    const { host } = await mountHostAt('/chat/main-agent');
    // 同步帧就有 renderer（chat panel 渲染需要 ChatRenderer 被 mock？——
    // ChatRenderer 内部有依赖；这里仅断言不再停留在 loading）。
    expect(host.querySelector('.workspace-host__loading')).toBeNull();
    expect(mocks.resolveApplication).toHaveBeenCalledWith({ slug: 'main-agent' });

    await act(async () => { resolve(app); });
  });

  it('a manage-only cache entry does NOT admit synchronously', async () => {
    const app = application(8, 'manage-only');
    // upsertManage：validatedAtById 不写（manage 数据不产生 consume trust）。
    useApplicationEntityStore.setState({
      byId: { 8: app },
      bySlug: { 'manage-only': app },
      validatedAtById: {},
    });
    let resolve!: (value: V2Application) => void;
    mocks.resolveApplication.mockImplementationOnce(
      () => new Promise((res) => { resolve = res as typeof resolve; }));

    const { host } = await mountHostAt('/chat/manage-only');
    expect(host.querySelector('.workspace-host__loading')).toBeTruthy();

    await act(async () => { resolve(app); });
    expect(host.querySelector('.workspace-host__loading')).toBeNull();
  });

  it('background resolve failure keeps the page when a fresh consume snapshot exists (§74)', async () => {
    const app = application(9, 'fresh-app');
    useApplicationEntityStore.setState({
      byId: { 9: app },
      bySlug: { 'fresh-app': app },
      validatedAtById: { 9: Date.now() },
    });
    mocks.resolveApplication.mockRejectedValueOnce(new Error('network down'));

    const { host } = await mountHostAt('/chat/fresh-app');
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });
    // 网络失败 + fresh 快照：暂时继续显示，不是错误页。
    expect(host.querySelector('.workspace-host__missing')).toBeNull();
    expect(host.querySelector('.workspace-host__loading')).toBeNull();
  });

  it('background resolve failure without any snapshot shows the retry surface (§74)', async () => {
    mocks.resolveApplication.mockRejectedValueOnce(new Error('network down'));
    const { host } = await mountHostAt('/chat/ghost-app');
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });
    expect(host.textContent).toContain('应用加载失败');
  });
});
