/**
 * adminPermissionEvents — 403 在线撤权事件桥测试（Architecture 2.0 §91–§93）。
 *
 *   /v2/admin/** 的 403 → emit → store invalidate → status idle
 *   /app/* 的 ACL 403 → 不 emit，Enterprise 权限不受影响
 */
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  emitAdminPermissionInvalidated,
  isAdminAuthorityPath,
  subscribeAdminPermissionInvalidated,
} from '@/services/adminPermissionEvents';

describe('adminPermissionEvents', () => {
  it('round-trips a subscription', () => {
    const listener = vi.fn();
    const unsubscribe = subscribeAdminPermissionInvalidated(listener);

    emitAdminPermissionInvalidated({ url: '/api/v2/admin/me' });
    expect(listener).toHaveBeenCalledWith({ url: '/api/v2/admin/me' });

    unsubscribe();
    emitAdminPermissionInvalidated({ url: '/api/v2/admin/users' });
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it('recognizes only authoritative admin paths', () => {
    expect(isAdminAuthorityPath('/api/v2/admin/me')).toBe(true);
    expect(isAdminAuthorityPath('/api/v2/admin/users/7/roles')).toBe(true);
    // /app/* 的 ACL 403 绝不能清 Enterprise Admin 权限（§93）。
    expect(isAdminAuthorityPath('/api/v2/app/foo/run')).toBe(false);
    expect(isAdminAuthorityPath(undefined)).toBe(false);
  });
});
