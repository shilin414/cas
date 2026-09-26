import { create } from 'zustand';
import { enterpriseApi, type AdminMe } from '@/pages/Enterprise/enterpriseApi';
import { captureSessionGeneration, registerSessionReset, sessionStillCurrent } from '@/stores/resetSessionState';
import { subscribeAdminPermissionInvalidated } from '@/services/adminPermissionEvents';
import type { PermissionLoadStatus } from '@/router/permissionDecision';

interface AdminPermissionState {
  identity: AdminMe | null;
  loadedForUserId: string | null;
  /** 兼容字段：等价于 status === 'loading'。 */
  loading: boolean;
  error: string | null;
  /** 显式加载状态：Route Guard 不再靠 identity === null 猜语义。 */
  status: PermissionLoadStatus;
  load: (userId: string, force?: boolean) => Promise<void>;
  clear: () => void;
  has: (permission: string) => boolean;
  can: (permission: string) => boolean;
  canAny: (permissions: readonly string[]) => boolean;
  canAll: (permissions: readonly string[]) => boolean;
  canAccessConsole: () => boolean;
  invalidate: () => void;
}

const toStatus = (loading: boolean, identity: AdminMe | null, error: string | null): PermissionLoadStatus => {
  if (error) return 'error';
  if (loading) return 'loading';
  return identity ? 'ready' : 'idle';
};

export const useAdminPermissionStore = create<AdminPermissionState>((set, get) => ({
  identity: null,
  loadedForUserId: null,
  loading: false,
  error: null,
  status: 'idle',
  load: async (userId, force = false) => {
    if (!force && (get().loading || get().loadedForUserId === userId)) return;
    const generation = captureSessionGeneration();
    set({ loading: true, status: 'loading', error: null, identity: null, loadedForUserId: null });
    try {
      const identity = await enterpriseApi.adminMe();
      if (sessionStillCurrent(generation)) set({ identity, loadedForUserId: userId, loading: false, error: null, status: 'ready' });
    } catch {
      if (sessionStillCurrent(generation)) set({ identity: null, loadedForUserId: null, loading: false, error: '企业权限加载失败', status: 'error' });
    }
  },
  clear: () => set({ identity: null, loadedForUserId: null, loading: false, error: null, status: 'idle' }),
  invalidate: () => set({ identity: null, loadedForUserId: null, status: 'idle' }),
  has: (permission) => get().can(permission),
  can: (permission) => {
    const identity = get().identity;
    if (!identity) return false;
    if (identity.is_super_admin) return true;
    return identity.permissions.some((item) => item.code === permission);
  },
  canAny: (permissions) => {
    const identity = get().identity;
    if (!identity) return false;
    if (identity.is_super_admin) return true;
    return permissions.some((permission) => identity.permissions.some((item) => item.code === permission));
  },
  canAll: (permissions) => {
    const identity = get().identity;
    if (!identity) return false;
    if (identity.is_super_admin) return true;
    if (permissions.length === 0) return true;
    return permissions.every((permission) => identity.permissions.some((item) => item.code === permission));
  },
  canAccessConsole: () => Boolean(get().identity?.can_access_console),
}));

// 兼容导出：旧代码直接读状态元组时保持同源。
export const adminPermissionStatus = () => useAdminPermissionStore.getState().status;

// resetSessionState 之外，identity/loadedForUserId 等字段若被旁路 setState，
// status 可能失真；toStatus 仅供测试工具还原一致状态。
export const __resolveStatus = toStatus;

registerSessionReset(() => useAdminPermissionStore.getState().clear());

// 在线撤权（Architecture 2.0 §92）：权威 /v2/admin/** 403 → invalidate。
// RoutePermissionBoundary 看到 idle 会重新 ensureLoaded；新的（更小的）
// 权限集决定页面 403 —— 旧页面不会继续保留。
subscribeAdminPermissionInvalidated(() => {
  useAdminPermissionStore.getState().invalidate();
});
