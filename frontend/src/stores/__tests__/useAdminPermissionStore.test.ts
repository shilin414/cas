import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({ adminMe: vi.fn() }));
vi.mock('@/pages/Enterprise/enterpriseApi', () => ({ enterpriseApi: { adminMe: mocks.adminMe } }));

import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { resetSessionScopedState } from '@/stores/resetSessionState';

const permission = (code: string) => ({ id: code.length, code, category: '', name: code, description: '', created_at: '' });

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

  describe('can / canAny / canAll (Commit 02)', () => {
    beforeEach(() => {
      useAdminPermissionStore.setState({
        identity: {
          can_access_console: false,
          is_super_admin: false,
          roles: [],
          permissions: [permission('admin.user.read'), permission('admin.role.read')],
        },
      });
    });

    it('can() matches held permission codes', () => {
      const { can } = useAdminPermissionStore.getState();
      expect(can('admin.user.read')).toBe(true);
      expect(can('audit.read')).toBe(false);
    });

    it('can() is false while identity is unknown — never default-allow', () => {
      useAdminPermissionStore.setState({ identity: null });
      expect(useAdminPermissionStore.getState().can('admin.user.read')).toBe(false);
    });

    it('canAny() is ANY semantics: one held permission is enough', () => {
      expect(useAdminPermissionStore.getState().canAny(['admin.user.read', 'admin.role.read'])).toBe(true);
      expect(useAdminPermissionStore.getState().canAny(['admin.user.read', 'audit.read'])).toBe(true);
      expect(useAdminPermissionStore.getState().canAny(['audit.read', 'access.group.read'])).toBe(false);
    });

    it('canAll() requires every permission', () => {
      expect(useAdminPermissionStore.getState().canAll(['admin.user.read', 'admin.role.read'])).toBe(true);
      expect(useAdminPermissionStore.getState().canAll(['admin.user.read', 'audit.read'])).toBe(false);
    });

    it('is_super_admin bypasses can/canAny/canAll', () => {
      useAdminPermissionStore.setState({
        identity: { can_access_console: true, is_super_admin: true, roles: [], permissions: [] },
      });
      const { can, canAny, canAll } = useAdminPermissionStore.getState();
      expect(can('anything.read')).toBe(true);
      expect(canAny(['anything.read'])).toBe(true);
      expect(canAll(['a', 'b'])).toBe(true);
    });

    it('canAccessConsole() reflects identity.can_access_console', () => {
      expect(useAdminPermissionStore.getState().canAccessConsole()).toBe(false);
      useAdminPermissionStore.setState({
        identity: { can_access_console: true, is_super_admin: false, roles: [], permissions: [] },
      });
      expect(useAdminPermissionStore.getState().canAccessConsole()).toBe(true);
    });

    it('has() stays compatible with can()', () => {
      expect(useAdminPermissionStore.getState().has('admin.user.read')).toBe(true);
      expect(useAdminPermissionStore.getState().has('audit.read')).toBe(false);
    });
  });

  describe('status lifecycle (Commit 02 §7)', () => {
    it('idle → loading → ready on success', async () => {
      expect(useAdminPermissionStore.getState().status).toBe('idle');
      let resolveMe!: (value: unknown) => void;
      mocks.adminMe.mockImplementationOnce(() => new Promise((resolve) => { resolveMe = resolve; }));
      const pending = useAdminPermissionStore.getState().load('7');
      expect(useAdminPermissionStore.getState().status).toBe('loading');
      resolveMe({ can_access_console: true, is_super_admin: false, roles: [], permissions: [] });
      await pending;
      expect(useAdminPermissionStore.getState().status).toBe('ready');
    });

    it('idle → loading → error on failure, clear() back to idle', async () => {
      mocks.adminMe.mockRejectedValueOnce(new Error('network'));
      await useAdminPermissionStore.getState().load('7');
      expect(useAdminPermissionStore.getState().status).toBe('error');
      useAdminPermissionStore.getState().clear();
      expect(useAdminPermissionStore.getState().status).toBe('idle');
    });

    it('invalidate() drops the identity and returns to idle for a fresh load', () => {
      useAdminPermissionStore.setState({
        identity: { can_access_console: true, is_super_admin: false, roles: [], permissions: [] },
        loadedForUserId: '7',
      });
      useAdminPermissionStore.getState().invalidate();
      expect(useAdminPermissionStore.getState()).toMatchObject({ identity: null, loadedForUserId: null, status: 'idle' });
    });
  });
});

