import { create } from 'zustand';
import { enterpriseApi, type AdminMe } from '@/pages/Enterprise/enterpriseApi';
import { captureSessionGeneration, registerSessionReset, sessionStillCurrent } from '@/stores/resetSessionState';
import { subscribeAdminPermissionInvalidated } from '@/services/adminPermissionEvents';
import { useAuthStore } from '@/stores/useAuthStore';
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

registerSessionReset(() => useAdminPermissionStore.getState().clear());

// 在线撤权（Architecture 2.0 §92）：权威 /v2/admin/** 403 → invalidate 后
// 主动重发 load（审查 M-4）—— 用户可能停留在没有 boundary 的页面
// （概览/非企业页），不能只依赖挂载点触发 ensureLoaded。load 内部有
// loading/loadedForUserId 去重；401 会话边界由 clear() 走 resetSessionState，
// 不会进入这里循环。
subscribeAdminPermissionInvalidated(() => {
  const store = useAdminPermissionStore.getState();
  store.invalidate();
  const userId = useAuthStore.getState().user?.id;
  if (userId && !useAuthStore.getState().user?.is_staff) {
    void store.load(String(userId), true);
  }
});
