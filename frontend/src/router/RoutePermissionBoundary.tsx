import { useEffect } from 'react';
import { Outlet, useNavigate } from 'react-router-dom';
import { Button, Result, Spin } from 'antd';
import { useRouteMeta } from './useRouteMeta';
import { useAuthStore } from '@/stores/useAuthStore';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { evaluatePermission } from './permissionDecision';

/**
 * RoutePermissionBoundary — 页面进入权限边界（Architecture 2.0 §35）。
 *
 * 读取当前 route meta 的 permission rule，用 evaluatePermission 统一判定：
 *   loading → 确保加载并显示 Loading（绝不能提前 403）
 *   error   → 错误 + 重试（绝不解释成无权限）
 *   denied  → 403
 *   allowed → Outlet
 * 后端 requireAdminPermission 不在此层 —— 前端只是 UX 边界。
 */
export const RoutePermissionBoundary: React.FC = () => {
  const navigate = useNavigate();
  const meta = useRouteMeta();
  const userId = useAuthStore((state) => state.user?.id);
  const isStaff = useAuthStore((state) => Boolean(state.user?.is_staff));
  const status = useAdminPermissionStore((state) => state.status);
  const load = useAdminPermissionStore((state) => state.load);
  const can = useAdminPermissionStore((state) => state.can);
  const canAny = useAdminPermissionStore((state) => state.canAny);
  const canAll = useAdminPermissionStore((state) => state.canAll);
  const canAccessConsole = useAdminPermissionStore((state) => Boolean(state.identity?.can_access_console));

  const rule = meta?.permission;

  // idle 意味着还没人触发加载：这里负责 ensureLoaded。
  useEffect(() => {
    if (!rule || rule.type === 'none') return;
    if (isStaff) return;
    if (userId && status === 'idle') void load(String(userId));
  }, [rule, isStaff, userId, status, load]);

  const decision = evaluatePermission(rule, {
    isStaff,
    permissionStatus: status,
    canAccessConsole,
    can,
    canAny,
    canAll,
  });

  if (decision === 'loading') {
    return (
      <div style={{ padding: 48, display: 'flex', justifyContent: 'center' }}>
        <Spin fullscreen tip="正在加载企业权限…" />
      </div>
    );
  }

  if (decision === 'error') {
    return (
      <Result
        status="warning"
        title="企业权限加载失败"
        subTitle="无法确认当前账号的企业权限，请重试。"
        extra={(
          <Button type="primary" onClick={() => userId && void load(String(userId), true)}>
            重试
          </Button>
        )}
      />
    );
  }

  if (decision === 'denied') {
    return (
      <Result
        status="403"
        title="无权限"
        subTitle="当前账号没有访问该页面的权限。"
        extra={(
          <Button type="primary" onClick={() => navigate('/enterprise', { replace: true })}>
            返回企业控制台
          </Button>
        )}
      />
    );
  }

  return <Outlet />;
};

export default RoutePermissionBoundary;
