import React from 'react';
import { Result } from 'antd';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { useAuthStore } from '@/stores/useAuthStore';

/**
 * 页面级（进入）权限守卫。语义：permission 数组 = ANY（任一满足），
 * is_staff / is_super_admin 直通。
 *
 * 注意：新路由体系（RoutePermissionBoundary）已逐步接管“页面进入”权限；
 * 本组件保留给尚未迁移的页面与“操作按钮”场景 —— 但操作权限建议改用
 * useAdminPermissionStore 的 can/canAny（Commit 14）。
 */
export function PermissionGuard({ permission, children }: { permission: string | string[]; children: React.ReactNode }) {
  const isStaff = useAuthStore((state) => Boolean(state.user?.is_staff));
  const canAny = useAdminPermissionStore((state) => state.canAny);
  const required = Array.isArray(permission) ? permission : [permission];
  const allowed = Boolean(isStaff || canAny(required));
  return allowed ? <>{children}</> : <Result status="403" title="无权限" subTitle={`需要权限：${required.join(" 或 ")}`} />;
}
