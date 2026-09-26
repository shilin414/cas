// @vitest-environment jsdom

/**
 * useAppNavigation — switch/push/replace/back 语义测试（Architecture 2.0
 * §15–§19 / Commit 04）。
 *
 *   switchRoot  Mobile=replace（带 root state），PC=push
 *   pushPage    始终 push，state 记录来源
 *   back        push 来源 → POP(-1)；否则 parent/root replace fallback
 */
import React from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react';
import { createMemoryRouter, RouterProvider, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useAppNavigation, type AppNavigationState } from '@/router/useAppNavigation';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// useIsMobile 读取 shell/useIsMobile；用 mock 直接控制。
vi.mock('@/shell/useIsMobile', () => ({
  useIsMobile: () => (globalThis as unknown as { __isMobile: boolean }).__isMobile,
}));

const roots: Array<{ host: HTMLElement; root: Root }> = [];

interface ProbeHandle {
  navigation: ReturnType<typeof useAppNavigation>;
  locationPath: string;
  locationState: AppNavigationState | null;
}

let probe: ProbeHandle;

const Probe = () => {
  const navigation = useAppNavigation();
  const location = useLocation();
  probe = { navigation, locationPath: location.pathname, locationState: location.state as AppNavigationState | null };
  return null;
};

const Entry = () => <output data-testid="entry">{probe?.locationPath}</output>;

async function mountRouter(
  routes: Array<{ path: string; app?: unknown }>,
  initialPath: string,
) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  roots.push({ host, root });
  const router = createMemoryRouter(
    routes.map(({ path, app }) => ({
      path,
      element: (
        <>
          <Probe />
          <Entry />
        </>
      ),
      handle: app ? { app } : undefined,
    })),
    { initialEntries: [initialPath] },
  );
  router.subscribe(() => {});
  await act(async () => {
    root.render(<RouterProvider router={router} />);
  });
  return router;
}

beforeEach(() => {
  (globalThis as unknown as { __isMobile: boolean }).__isMobile = true;
});

afterEach(async () => {
  while (roots.length) {
    const { host, root } = roots.pop()!;
    await act(async () => root.unmount());
    host.remove();
  }
  document.body.innerHTML = '';
  probe = undefined as unknown as ProbeHandle;
});

describe('useAppNavigation', () => {
  it('switchRoot replaces on mobile and stamps a root state', async () => {
    const router = await mountRouter(
      [{ path: '/' }, { path: '/agents' }],
      '/',
    );
    expect(router.state.location.pathname).toBe('/');
    expect(router.state.historyAction).toBe('POP');

    await act(async () => { probe.navigation.switchRoot('/agents'); });

    expect(router.state.location.pathname).toBe('/agents');
    // replace：history action 不产生新的 PUSH entry。
    expect(router.state.historyAction).toBe('REPLACE');
    expect(router.state.location.state).toEqual({ appNavigation: { type: 'root' } });
  });

  it('switchRoot pushes on desktop (PC experience unchanged)', async () => {
    (globalThis as unknown as { __isMobile: boolean }).__isMobile = false;
    const router = await mountRouter(
      [{ path: '/' }, { path: '/agents' }],
      '/',
    );
    await act(async () => { probe.navigation.switchRoot('/agents'); });
    expect(router.state.location.pathname).toBe('/agents');
    expect(router.state.historyAction).toBe('PUSH');
    expect(router.state.location.state).toBeNull();
  });

  it('pushPage pushes with the source recorded in state', async () => {
    const router = await mountRouter(
      [{ path: '/schedules' }, { path: '/schedules/:scheduleId' }],
      '/schedules?tab=done',
    );
    await act(async () => { probe.navigation.pushPage('/schedules/1'); });
    expect(router.state.location.pathname).toBe('/schedules/1');
    expect(router.state.historyAction).toBe('PUSH');
    expect(router.state.location.state).toEqual({
      appNavigation: { type: 'push', from: '/schedules?tab=done' },
    });
  });

  it('back() POPs an app-pushed entry (navigate -1)', async () => {
    const router = await mountRouter(
      [{ path: '/schedules' }, { path: '/schedules/:scheduleId' }],
      '/schedules',
    );
    await act(async () => { probe.navigation.pushPage('/schedules/1'); });
    expect(router.state.location.pathname).toBe('/schedules/1');

    await act(async () => { probe.navigation.back(); });
    expect(router.state.location.pathname).toBe('/schedules');
    expect(router.state.historyAction).toBe('POP');
  });

  it('back() on a direct-link detail replaces to the route parent', async () => {
    const router = await mountRouter(
      [
        { path: '/enterprise' },
        {
          path: '/enterprise/resources/agents',
          app: { id: 'enterprise-resource-agents', level: 'detail', root: '/enterprise', parent: '/enterprise' },
        },
      ],
      '/enterprise/resources/agents',
    );
    expect(router.state.historyAction).toBe('POP'); // MemoryRouter 初始
    await act(async () => { probe.navigation.back(); });
    expect(router.state.location.pathname).toBe('/enterprise');
    expect(router.state.historyAction).toBe('REPLACE');
  });

  it('back() without parent falls back to the route root', async () => {
    const router = await mountRouter(
      [
        { path: '/' },
        {
          path: '/schedules/:scheduleId',
          app: { id: 'schedule-detail', level: 'detail', root: '/schedules' },
        },
      ],
      '/schedules/7',
    );
    await act(async () => { probe.navigation.back(); });
    expect(router.state.location.pathname).toBe('/schedules');
  });

  it('back() without any meta falls back to /', async () => {
    const router = await mountRouter(
      [{ path: '/' }, { path: '/agents' }, { path: '/agents/:id' }],
      '/agents/9',
    );
    await act(async () => { probe.navigation.back(); });
    expect(router.state.location.pathname).toBe('/');
  });

  it('replacePage replaces without pushing history', async () => {
    const router = await mountRouter(
      [{ path: '/schedules/new' }, { path: '/schedules/:scheduleId' }],
      '/schedules/new',
    );
    await act(async () => { probe.navigation.replacePage('/schedules/123'); });
    expect(router.state.location.pathname).toBe('/schedules/123');
    expect(router.state.historyAction).toBe('REPLACE');
  });
});
