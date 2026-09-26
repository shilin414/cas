// @vitest-environment jsdom

/**
 * RoutePermissionBoundary — 路由权限边界测试（Architecture 2.0 §35/§三十七–§四十）。
 *
 *   loading → 绝不提前 403 / redirect
 *   error   → 错误 + 重试（不是无权限）
 *   denied  → 403（Direct Link 无法绕过）
 *   allowed → Outlet 渲染
 *   撤权（identity 失效）→ 重新加载后 403，旧页面不保留
 */
import React from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({ adminMe: vi.fn() }));
vi.mock('@/pages/Enterprise/enterpriseApi', () => ({
  enterpriseApi: { adminMe: mocks.adminMe },
}));

import { RoutePermissionBoundary } from '@/router/RoutePermissionBoundary';
import { anyPermission, enterprisePermission } from '@/router/permissions';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { useAuthStore } from '@/stores/useAuthStore';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const roots: Array<{ host: HTMLElement; root: Root }> = [];
(globalThis as unknown as { matchMedia: unknown }).matchMedia = (query: string) => ({
  matches: false, media: query, onchange: null,
  addListener() {}, removeListener() {},
  addEventListener() {}, removeEventListener() {},
  dispatchEvent: () => false,
});

async function mountWithRule(rule: unknown, initialPath = '/enterprise/audit') {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  roots.push({ host, root });
  const router = createMemoryRouter([
    {
      path: '/enterprise',
      element: <div>console-home</div>,
    },
    {
      path: '/enterprise/audit',
      element: <RoutePermissionBoundary />,
      handle: { app: { id: 'enterprise-audit', level: 'detail', root: '/enterprise', parent: '/enterprise', permission: rule } },
      children: [{ index: true, element: <div data-testid="page">audit-page</div> }],
    },
  ], { initialEntries: [initialPath] });
  await act(async () => {
    root.render(<RouterProvider router={router} />);
  });
  return { host, router };
}

beforeEach(() => {
  mocks.adminMe.mockReset();
  useAdminPermissionStore.getState().clear();
  useAuthStore.setState({
    user: {
      id: '7', username: 'user', email: '', role: 'user',
      display_name: '普通用户', created_at: '', is_staff: false,
    },
    isAuthenticated: true,
  });
});

afterEach(async () => {
  while (roots.length) {
    const { host, root } = roots.pop()!;
    await act(async () => root.unmount());
    host.remove();
  }
  document.body.innerHTML = '';
});

describe('RoutePermissionBoundary', () => {
  it('shows loading while permissions load — never a premature 403 (§三十八)', async () => {
    let resolveMe!: (value: unknown) => void;
    mocks.adminMe.mockImplementationOnce(() => new Promise((resolve) => { resolveMe = resolve; }));
    const { host } = await mountWithRule(anyPermission('audit.read'));

    expect(host.textContent).toContain('正在加载企业权限');
    expect(host.textContent).not.toContain('无权限');
    // 不是 redirect 到首页。
    expect(host.textContent).not.toContain('console-home');

    await act(async () => {
      resolveMe({ can_access_console: true, is_super_admin: false, roles: [], permissions: [{ id: 1, code: 'audit.read', category: '', name: '', description: '', created_at: '' }] });
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(host.textContent).toContain('audit-page');
  });

  it('direct link without the permission gets 403, not home redirect (§三十七)', async () => {
    mocks.adminMe.mockResolvedValueOnce({
      can_access_console: true, is_super_admin: false, roles: [],
      permissions: [{ id: 1, code: 'resource.agent.read', category: '', name: '', description: '', created_at: '' }],
    });
    const { host } = await mountWithRule(anyPermission('audit.read'));
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });

    expect(host.textContent).toContain('无权限');
    expect(host.textContent).not.toContain('console-home');
  });

  it('admin 500 shows error + retry, not denial (§三十九)', async () => {
    mocks.adminMe.mockRejectedValueOnce(new Error('server down'));
    const { host } = await mountWithRule(anyPermission('audit.read'));
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });

    expect(host.textContent).toContain('企业权限加载失败');
    expect(host.textContent).not.toContain('无权限');

    mocks.adminMe.mockResolvedValueOnce({
      can_access_console: true, is_super_admin: false, roles: [],
      permissions: [{ id: 1, code: 'audit.read', category: '', name: '', description: '', created_at: '' }],
    });
    const retry = Array.from(host.querySelectorAll('button')).find((b) => b.textContent?.replace(/\s/g, '') === '重试');
    expect(retry).toBeTruthy();
    await act(async () => retry!.click());
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });
    expect(host.textContent).toContain('audit-page');
  });

  it('is_staff bypasses without loading enterprise permissions', async () => {
    useAuthStore.setState({
      user: {
        id: '1', username: 'staff', email: '', role: 'admin',
        display_name: '管理员', created_at: '', is_staff: true,
      },
      isAuthenticated: true,
    });
    const { host } = await mountWithRule(enterprisePermission());
    expect(host.textContent).toContain('audit-page');
    expect(mocks.adminMe).not.toHaveBeenCalled();
  });

  it('revoked permission turns the live page into 403 (§四十)', async () => {
    mocks.adminMe.mockResolvedValueOnce({
      can_access_console: true, is_super_admin: false, roles: [],
      permissions: [{ id: 1, code: 'audit.read', category: '', name: '', description: '', created_at: '' }],
    });
    const { host } = await mountWithRule(anyPermission('audit.read'));
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });
    expect(host.textContent).toContain('audit-page');

    // 管理员撤权 → invalidate → boundary 重新加载得到空权限。
    mocks.adminMe.mockResolvedValueOnce({
      can_access_console: true, is_super_admin: false, roles: [], permissions: [],
    });
    await act(async () => {
      useAdminPermissionStore.getState().invalidate();
    });
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });
    expect(host.textContent).toContain('无权限');
  });
});
