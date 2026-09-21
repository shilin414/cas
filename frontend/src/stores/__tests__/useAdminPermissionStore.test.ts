import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({ adminMe: vi.fn() }));
vi.mock('@/pages/Enterprise/enterpriseApi', () => ({ enterpriseApi: { adminMe: mocks.adminMe } }));

import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { resetSessionScopedState } from '@/stores/resetSessionState';

describe('useAdminPermissionStore', () => {
  beforeEach(() => { mocks.adminMe.mockReset(); useAdminPermissionStore.getState().clear(); });
  it('keeps transport failures distinct from authoritative denial and allows retry', async () => {
    mocks.adminMe.mockRejectedValueOnce(new Error('network'));
    await useAdminPermissionStore.getState().load('7');
    expect(useAdminPermissionStore.getState()).toMatchObject({ identity: null, loadedForUserId: null, loading: false, error: '企业权限加载失败' });
    mocks.adminMe.mockResolvedValueOnce({ can_access_console: true, is_super_admin: false, roles: [], permissions: [{ id: 1, code: 'audit.read', category: 'audit', name: '审计', description: '', created_at: '' }] });
    await useAdminPermissionStore.getState().load('7', true);
    expect(useAdminPermissionStore.getState().identity?.can_access_console).toBe(true);
    expect(useAdminPermissionStore.getState().error).toBeNull();
  });
  it('clears administrator identity at the session boundary', () => {
    useAdminPermissionStore.setState({ identity: { can_access_console: true, is_super_admin: true, roles: [], permissions: [] }, loadedForUserId: '7', loading: false, error: null });
    resetSessionScopedState();
    expect(useAdminPermissionStore.getState()).toMatchObject({ identity: null, loadedForUserId: null, loading: false, error: null });
  });
});
