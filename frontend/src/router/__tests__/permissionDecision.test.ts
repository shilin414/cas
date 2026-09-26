/**
 * permissionDecision — evaluatePermission 纯函数测试（Architecture 2.0 §8/Commit 02）。
 *
 * 固定的语义：
 *   none → allowed
 *   is_staff → 一律 allowed（企业权限 bypass）
 *   enterprise → 非 staff 需要 can_access_console；未知状态是 loading/error
 *   any → 任一满足（is_super_admin 经 canAny 短路）
 *   all → 全部满足
 * 权限尚未加载不得默认允许或拒绝。
 */
import { describe, expect, it } from 'vitest';
import {
  evaluatePermission,
  type PermissionDecisionContext,
} from '@/router/permissionDecision';
import {
  allPermissions,
  anyPermission,
  enterprisePermission,
  noPermission,
} from '@/router/permissions';

const codes = (...list: string[]) => list;

function context(overrides: Partial<PermissionDecisionContext> = {}): PermissionDecisionContext {
  const held: string[] = [];
  const can = (permission: string) => held.includes(permission);
  return {
    isStaff: false,
    permissionStatus: 'ready',
    canAccessConsole: true,
    can,
    canAny: (permissions) => permissions.some(can),
    canAll: (permissions) => permissions.every(can),
    ...overrides,
  };
}

/** 构造一个持有指定权限的上下文（模拟 store 的 can/canAny/canAll）。 */
function holderContext(held: string[], overrides: Partial<PermissionDecisionContext> = {}) {
  const can = (permission: string) => held.includes(permission);
  return context({
    can,
    canAny: (permissions) => permissions.some(can),
    canAll: (permissions) => permissions.every(can),
    ...overrides,
  });
}

describe('evaluatePermission', () => {
  it('undefined and none rules are always allowed', () => {
    expect(evaluatePermission(undefined, context())).toBe('allowed');
    expect(evaluatePermission(noPermission(), context())).toBe('allowed');
  });

  it('is_staff bypasses every rule to allowed', () => {
    const staff = holderContext([], { isStaff: true, permissionStatus: 'idle', canAccessConsole: false });
    expect(evaluatePermission(enterprisePermission(), staff)).toBe('allowed');
    expect(evaluatePermission(anyPermission('audit.read'), staff)).toBe('allowed');
    expect(evaluatePermission(allPermissions('a', 'b'), staff)).toBe('allowed');
  });

  describe('enterprise rule', () => {
    it('allowed when can_access_console is true', () => {
      expect(evaluatePermission(enterprisePermission(), context({ canAccessConsole: true }))).toBe('allowed');
    });

    it('denied when can_access_console is false', () => {
      expect(evaluatePermission(enterprisePermission(), context({ canAccessConsole: false }))).toBe('denied');
    });

    it('loading while permissions are still loading or idle — never a premature 403', () => {
      expect(evaluatePermission(enterprisePermission(), context({ permissionStatus: 'loading' }))).toBe('loading');
      expect(evaluatePermission(enterprisePermission(), context({ permissionStatus: 'idle' }))).toBe('loading');
    });

    it('error surfaces as error, not as denied', () => {
      expect(evaluatePermission(enterprisePermission(), context({ permissionStatus: 'error' }))).toBe('error');
    });
  });

  describe('any rule', () => {
    const rule = anyPermission('admin.user.read', 'admin.role.read');

    it('allowed when exactly one listed permission is held', () => {
      expect(evaluatePermission(rule, holderContext(codes('admin.role.read')))).toBe('allowed');
    });

    it('denied when none is held', () => {
      expect(evaluatePermission(rule, holderContext(codes('audit.read')))).toBe('denied');
    });

    it('is_super_admin is satisfied because canAny short-circuits to true', () => {
      // 模拟 store：super admin 的 canAny 恒真。
      const superAdmin = holderContext([], {
        canAny: () => true,
      });
      expect(evaluatePermission(rule, superAdmin)).toBe('allowed');
    });

    it('loading while status is idle or loading', () => {
      expect(evaluatePermission(rule, holderContext([], { permissionStatus: 'loading' }))).toBe('loading');
      expect(evaluatePermission(rule, holderContext([], { permissionStatus: 'idle' }))).toBe('loading');
    });

    it('error status yields error', () => {
      expect(evaluatePermission(rule, holderContext([], { permissionStatus: 'error' }))).toBe('error');
    });
  });

  describe('all rule', () => {
    const rule = allPermissions('a.read', 'b.read');

    it('allowed only when every permission is held', () => {
      expect(evaluatePermission(rule, holderContext(codes('a.read', 'b.read')))).toBe('allowed');
      expect(evaluatePermission(rule, holderContext(codes('a.read')))).toBe('denied');
    });

    it('denied when none is held', () => {
      expect(evaluatePermission(rule, holderContext([]))).toBe('denied');
    });
  });
});
