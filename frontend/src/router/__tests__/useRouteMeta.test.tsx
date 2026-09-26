// @vitest-environment jsdom

/**
 * useRouteMeta — handle.app 读取测试（Architecture 2.0 §13/Commit 03）。
 *
 * 最深层 matched route 的 handle.app 优先；没有 app meta 的路由返回 null。
 * 组件不解析 pathname —— 页面身份完全由路由声明。
 */
import React from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';

import { useRouteMeta } from '@/router/useRouteMeta';
import { anyPermission, enterprisePermission } from '@/router/permissions';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const roots: Array<{ host: HTMLElement; root: Root }> = [];

const MetaProbe = () => {
  const meta = useRouteMeta();
  return <output data-testid="meta">{meta ? JSON.stringify(meta) : 'null'}</output>;
};

async function mountAt(path: string, withAppHandle: boolean) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  roots.push({ host, root });
  const handle = withAppHandle
    ? {
        shell: { padded: true },
        app: {
          id: 'enterprise-resource-agents',
          level: 'detail' as const,
          root: '/enterprise',
          parent: '/enterprise',
          permission: anyPermission('resource.agent.read'),
          mobile: { mode: 'detail' as const, title: '智能体管理' },
        },
      }
    : { shell: { padded: true } };
  const router = createMemoryRouter([
    {
      path: '/enterprise/resources/agents',
      element: <MetaProbe />,
      handle: handle as never,
    },
  ], { initialEntries: [path] });
  await act(async () => {
    root.render(<RouterProvider router={router} />);
  });
}

afterEach(async () => {
  while (roots.length) {
    const { host, root } = roots.pop()!;
    await act(async () => root.unmount());
    host.remove();
  }
  document.body.innerHTML = '';
});

describe('useRouteMeta', () => {
  it('reads handle.app from the deepest matched route', async () => {
    await mountAt('/enterprise/resources/agents', true);
    const raw = document.querySelector('[data-testid="meta"]')?.textContent!;
    expect(raw).not.toBe('null');
    const meta = JSON.parse(raw);
    expect(meta).toMatchObject({
      id: 'enterprise-resource-agents',
      level: 'detail',
      root: '/enterprise',
      parent: '/enterprise',
    });
    expect(meta.permission).toEqual(anyPermission('resource.agent.read'));
    expect(meta.mobile).toEqual({ mode: 'detail', title: '智能体管理' });
  });

  it('returns null when no route declares handle.app', async () => {
    await mountAt('/enterprise/resources/agents', false);
    expect(document.querySelector('[data-testid="meta"]')?.textContent).toBe('null');
  });
});

describe('enterprisePermission factory shape', () => {
  it('produces the enterprise rule consumed by route boundaries', () => {
    expect(enterprisePermission()).toEqual({ type: 'enterprise' });
  });
});
