/**
 * permissions — 路由权限规则的声明式形状（Architecture 2.0 §5）。
 *
 * 路由通过 handle.app.permission 声明一条 PermissionRule，由
 * permissionDecision / RoutePermissionBoundary 统一解释。
 * 现有路由绝大多数是 ANY 语义；不要因为存在 all API 就擅自改语义。
 */

export type PermissionRule =
  | { type: 'none' }
  | { type: 'enterprise' }
  | {
      type: 'any';
      permissions: readonly string[];
    }
  | {
      type: 'all';
      permissions: readonly string[];
    };

export const noPermission = (): PermissionRule => ({
  type: 'none',
});

/** 企业控制台总体资格：is_staff 直通，其余看 can_access_console。 */
export const enterprisePermission = (): PermissionRule => ({
  type: 'enterprise',
});

/** 任一权限满足即允许（现有路由数组的语义）。 */
export const anyPermission = (
  ...permissions: string[]
): PermissionRule => ({
  type: 'any',
  permissions,
});

/** 全部权限满足才允许。目前路由基本不用，仅供需要 AND 的场景。 */
export const allPermissions = (
  ...permissions: string[]
): PermissionRule => ({
  type: 'all',
  permissions,
});
