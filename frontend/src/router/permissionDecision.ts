/**
 * permissionDecision — 把 PermissionRule + 登录/权限上下文解释成
 * loading / allowed / denied / error 的纯函数（Architecture 2.0 §8）。
 *
 * 兼容不变量（禁止改动语义）：
 *   is_staff        → 企业权限直接允许（含 enterprise rule 与任何列表规则）
 *   is_super_admin  → permission 直接允许（经 can/canAny/canAll 短路）
 *   can_access_console → 非 staff 用户进入 Enterprise 的必要条件
 * 权限尚未加载（idle/loading）不得判 denied —— 那是“未知”，不是“无权限”。
 */
import type { PermissionRule } from './permissions';

export type PermissionDecision =
  | 'loading'
  | 'allowed'
  | 'denied'
  | 'error';

export type PermissionLoadStatus =
  | 'idle'
  | 'loading'
  | 'ready'
  | 'error';

export interface PermissionDecisionContext {
  isStaff: boolean;
  permissionStatus: PermissionLoadStatus;
  canAccessConsole: boolean;
  can: (permission: string) => boolean;
  canAny: (permissions: readonly string[]) => boolean;
  canAll: (permissions: readonly string[]) => boolean;
}

export function evaluatePermission(
  rule: PermissionRule | undefined,
  context: PermissionDecisionContext,
): PermissionDecision {
  // 无规则 = 页面不要求企业权限。
  if (!rule || rule.type === 'none') return 'allowed';
  // staff 拥有企业域全部权限，永远直通。
  if (context.isStaff) return 'allowed';

  if (rule.type === 'enterprise') {
    // 非 staff 必须先知道权限加载结果，未知不算拒绝。
    if (context.permissionStatus === 'idle' || context.permissionStatus === 'loading') return 'loading';
    if (context.permissionStatus === 'error') return 'error';
    return context.canAccessConsole ? 'allowed' : 'denied';
  }

  // any / all：需要权限清单就绪；idle 意味着还没人触发加载，同样视作加载中。
  if (context.permissionStatus === 'idle' || context.permissionStatus === 'loading') return 'loading';
  if (context.permissionStatus === 'error') return 'error';

  if (rule.type === 'any') {
    return context.canAny(rule.permissions) ? 'allowed' : 'denied';
  }
  return context.canAll(rule.permissions) ? 'allowed' : 'denied';
}
