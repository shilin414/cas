import { useEffect } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { useAuthStore } from "@/stores/useAuthStore";
import { useAdminPermissionStore } from "@/stores/useAdminPermissionStore";

import { appPath, routerPath } from '@/lib/deploymentPaths';
import { sessionLoginRoute, safeLoginReturnTo } from '@/services/authRedirect';

interface ProtectedRouteProps {
  children: React.ReactNode;
}

/**
 * 未登录的普通用户不进入任何「登录页」：直接引导到 /login，
 * 由该路由自动发起飞书 OAuth（架构准则 §43：Feishu SSO Only，
 * 用户甚至不应该看到登录页）。
 */
export const ProtectedRoute: React.FC<ProtectedRouteProps> = ({ children }) => {
  const location = useLocation();
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  const explicitlyLoggedOut = useAuthStore(
    (state) => state.explicitlyLoggedOut,
  );

  if (!isAuthenticated) {
    return (
      <Navigate
        to={sessionLoginRoute(appPath(location.pathname + location.search + location.hash), explicitlyLoggedOut)}
        replace
      />
    );
  }

  return <>{children}</>;
};

export const AdminRoute: React.FC<ProtectedRouteProps> = ({ children }) => {
  const user = useAuthStore((state) => state.user);
  if (!user?.is_staff) return <Navigate to="/" replace />;
  return <>{children}</>;
};

export const EnterpriseRoute: React.FC<ProtectedRouteProps> = ({ children }) => {
  const user = useAuthStore((state) => state.user);
  const identity = useAdminPermissionStore((state) => state.identity);
  const loading = useAdminPermissionStore((state) => state.loading);
  const error = useAdminPermissionStore((state) => state.error);
  useEffect(() => { if (user?.id && !user.is_staff) void useAdminPermissionStore.getState().load(String(user.id)); }, [user?.id, user?.is_staff]);
  if (!user) return <Navigate to="/login" replace />;
  if (user.is_staff) return <>{children}</>;
  if (loading || (!identity && !error)) return <div style={{ padding: 32 }}>正在加载企业权限…</div>;
  if (error) return <div style={{ padding: 32 }}>企业权限加载失败。<button type="button" onClick={() => void useAdminPermissionStore.getState().load(String(user.id), true)}>重试</button></div>;
  if (!identity?.can_access_console) return <Navigate to="/" replace />;
  return <>{children}</>;
};

export const PublicRoute: React.FC<ProtectedRouteProps> = ({ children }) => {
  const location = useLocation();
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);

  if (isAuthenticated) {
    return <Navigate to={routerPath(safeLoginReturnTo(new URLSearchParams(location.search).get('return_to')))} replace />;
  }

  return <>{children}</>;
};
