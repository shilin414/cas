import React from 'react';
import { Result } from 'antd';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { useAuthStore } from '@/stores/useAuthStore';

export function PermissionGuard({ permission, children }: { permission: string | string[]; children: React.ReactNode }) {
  const isStaff = useAuthStore((state) => Boolean(state.user?.is_staff));
  const identity = useAdminPermissionStore((state) => state.identity);
  const required = Array.isArray(permission) ? permission : [permission];
  const allowed = Boolean(isStaff || identity?.is_super_admin || identity?.permissions.some((item) => required.includes(item.code)));
  return allowed ? <>{children}</> : <Result status="403" title="无权限" subTitle={`需要权限：${required.join(" 或 ")}`} />;
}
