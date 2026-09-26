// @vitest-environment jsdom

/**
 * PermissionGuard 回归基线（Architecture 2.0 §3.3）。
 *
 * 固定现有 PermissionGuard 的语义：
 *   - 单权限：staff / super admin / 持有者允许
 *   - 多权限数组：任一满足即允许（ANY），绝不误改成 AND
 */
import React from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { PermissionGuard } from '@/pages/Enterprise/shared/PermissionGuard';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { useAuthStore } from '@/stores/useAuthStore';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const roots: Array<{ host: HTMLElement; root: Root }> = [];

async function mount(node: React.ReactElement) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  roots.push({ host, root });
  await act(async () => { root.render(node); });
  return host;
}

beforeEach(() => {
  useAdminPermissionStore.setState({
    identity: null,
    loadedForUserId: null,
    loading: false,
    error: null,
  });
  useAuthStore.setState({
    user: {
      id: '1', username: 'tester', email: '', role: 'user',
      display_name: '测试用户', created_at: '', is_staff: false,
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

const identityWith = (...codes: string[]) => ({
  user_id: 1,
  is_super_admin: false,
  can_access_console: true,
  permissions: codes.map((code) => ({ code, name: code })),
}) as never;

describe('PermissionGuard regression semantics', () => {
  it('allows a single permission holder (resource.agent.read)', async () => {
    useAdminPermissionStore.setState({ identity: identityWith('resource.agent.read') });
    await mount(<PermissionGuard permission="resource.agent.read"><div>内容</div></PermissionGuard>);
    expect(document.body.textContent).toContain('内容');
  });

  it('denies when the single permission is missing', async () => {
    useAdminPermissionStore.setState({ identity: identityWith('resource.app.read') });
    await mount(<PermissionGuard permission="resource.agent.read"><div>内容</div></PermissionGuard>);
    expect(document.body.textContent).toContain('无权限');
  });

  it('array permission means ANY: one of [admin.user.read, admin.role.read] is enough', async () => {
    useAdminPermissionStore.setState({ identity: identityWith('admin.role.read') });
    await mount(
      <PermissionGuard permission={['admin.user.read', 'admin.role.read']}>
        <div>内容</div>
      </PermissionGuard>,
    );
    expect(document.body.textContent).toContain('内容');
  });

  it('array permission denies when none is held (stays ANY, never silently AND)', async () => {
    useAdminPermissionStore.setState({ identity: identityWith('audit.read') });
    await mount(
      <PermissionGuard permission={['admin.user.read', 'admin.role.read']}>
        <div>内容</div>
      </PermissionGuard>,
    );
    expect(document.body.textContent).toContain('无权限');
  });

  it('staff bypasses without any enterprise identity', async () => {
    useAuthStore.setState({
      user: {
        id: '1', username: 'staff', email: '', role: 'admin',
        display_name: '管理员', created_at: '', is_staff: true,
      },
      isAuthenticated: true,
    });
    await mount(
      <PermissionGuard permission={['admin.user.read', 'admin.role.read']}>
        <div>内容</div>
      </PermissionGuard>,
    );
    expect(document.body.textContent).toContain('内容');
  });

  it('super admin bypasses any listed permission', async () => {
    useAdminPermissionStore.setState({
      identity: {
        user_id: 1, is_super_admin: true, can_access_console: true, permissions: [],
      } as never,
    });
    await mount(<PermissionGuard permission="resource.agent.read"><div>内容</div></PermissionGuard>);
    expect(document.body.textContent).toContain('内容');
  });
});
