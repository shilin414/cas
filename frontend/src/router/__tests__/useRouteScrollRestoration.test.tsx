// @vitest-environment jsdom

/**
 * useRouteScrollRestoration — 滚动恢复测试（Architecture 2.0 §78–§80）。
 *
 *   离开保存 scrollTop（按 location.key / root）
 *   PUSH → 0
 *   POP → 恢复
 *   root REPLACE → 恢复该 root 最近位置
 */
import React from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { useRouteScrollRestoration, __resetScrollMemoryForTests } from '@/router/useRouteScrollRestoration';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// rAF 在 jsdom 中存在；直接同步执行。
(globalThis as unknown as { requestAnimationFrame: unknown }).requestAnimationFrame = (cb: FrameRequestCallback) => {
  cb(0);
  return 0;
};

const roots: Array<{ host: HTMLElement; root: Root }> = [];

// jsdom 没有 document.scrollingElement：用 hook 支持的
// [data-scroll-container] 容器（真实浏览器两者都覆盖）。
const Probe = () => {
  useRouteScrollRestoration();
  return <div data-testid="page" data-scroll-container style={{ height: 400, overflowY: 'auto' }} />;
};

async function mountRouter(routes: Array<{ path: string; app?: unknown }>, initial: string) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  roots.push({ host, root });
  const router = createMemoryRouter(
    routes.map(({ path, app }) => ({
      path,
      element: <Probe />,
      handle: app ? { app } : undefined,
    })),
    { initialEntries: [initial] },
  );
  await act(async () => {
    root.render(<RouterProvider router={router} />);
  });
  return router;
}

const container = () =>
  document.querySelector<HTMLElement>('[data-scroll-container]')!;

const scrollTo = async (router: ReturnType<typeof createMemoryRouter>, top: number) => {
  await act(async () => {
    container().scrollTop = top;
    container().dispatchEvent(new Event('scroll'));
  });
  void router;
};

afterEach(async () => {
  while (roots.length) {
    const { host, root } = roots.pop()!;
    await act(async () => root.unmount());
    host.remove();
  }
  document.body.innerHTML = '';
  __resetScrollMemoryForTests();
});

describe('useRouteScrollRestoration', () => {
  it('PUSH resets scroll to top', async () => {
    const router = await mountRouter(
      [
        { path: '/agents', app: { id: 'agents', level: 'root', root: '/agents' } },
        { path: '/agents/:id', app: { id: 'agents-detail', level: 'detail', root: '/agents', parent: '/agents' } },
      ],
      '/agents',
    );
    await scrollTo(router, 1200);
    await act(async () => { router.navigate('/agents/5'); });
    expect(container().scrollTop).toBe(0);
  });

  it('POP restores the saved position for that location key', async () => {
    const router = await mountRouter(
      [
        { path: '/agents', app: { id: 'agents', level: 'root', root: '/agents' } },
        { path: '/agents/:id', app: { id: 'agents-detail', level: 'detail', root: '/agents', parent: '/agents' } },
      ],
      '/agents',
    );
    await scrollTo(router, 800);
    await act(async () => { router.navigate('/agents/5'); });
    expect(container().scrollTop).toBe(0);
    // POP 回列表：恢复 800。
    await act(async () => { router.navigate(-1); });
    expect(container().scrollTop).toBe(800);
  });

  it('root REPLACE restores the root\'s last position', async () => {
    const router = await mountRouter(
      [
        { path: '/', app: { id: 'home', level: 'root', root: '/' } },
        { path: '/agents', app: { id: 'agents', level: 'root', root: '/agents' } },
      ],
      '/agents',
    );
    await scrollTo(router, 600);
    await act(async () => { router.navigate('/', { replace: true }); });
    // 切走再切回 /agents（replace，root 语义）：恢复 600。
    await act(async () => { router.navigate('/agents', { replace: true }); });
    expect(container().scrollTop).toBe(600);
  });
});
