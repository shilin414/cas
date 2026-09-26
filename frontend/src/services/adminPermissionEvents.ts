/**
 * adminPermissionEvents — Admin 403 → 权限失效事件桥（Architecture 2.0 §91）。
 *
 * services/axios.ts 不能 import useAdminPermissionStore（会形成
 * api.ts → store → enterpriseApi → api.ts 循环依赖），所以用无依赖的
 * 发布/订阅解耦：axios 层只 emit，store 层订阅后 invalidate()。
 *
 * 仅 /v2/admin/** 的权威 403 才 emit（§92–§93）：/app/* 的 ACL 403
 * 不能清 Enterprise Admin 权限。
 */
type AdminPermissionInvalidatedListener = (context: { url: string }) => void;

const listeners = new Set<AdminPermissionInvalidatedListener>();

export function emitAdminPermissionInvalidated(context: { url: string }) {
  for (const listener of listeners) listener(context);
}

export function subscribeAdminPermissionInvalidated(listener: AdminPermissionInvalidatedListener): () => void {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** 仅权威 Admin 端点的 403 视为在线撤权信号。 */
export function isAdminAuthorityPath(url?: string): boolean {
  if (!url) return false;
  return url.includes('/v2/admin/');
}
