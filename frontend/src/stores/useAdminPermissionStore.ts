import { create } from 'zustand';
import { enterpriseApi, type AdminMe } from '@/pages/Enterprise/enterpriseApi';
import { captureSessionGeneration, registerSessionReset, sessionStillCurrent } from '@/stores/resetSessionState';

interface AdminPermissionState {
  identity: AdminMe | null;
  loadedForUserId: string | null;
  loading: boolean;
  error: string | null;
  load: (userId: string, force?: boolean) => Promise<void>;
  clear: () => void;
  has: (permission: string) => boolean;
}

export const useAdminPermissionStore = create<AdminPermissionState>((set, get) => ({
  identity: null,
  loadedForUserId: null,
  loading: false,
  error: null,
  load: async (userId, force = false) => {
    if (!force && (get().loading || get().loadedForUserId === userId)) return;
    const generation = captureSessionGeneration();
    set({ loading: true, error: null, identity: null, loadedForUserId: null });
    try {
      const identity = await enterpriseApi.adminMe();
      if (sessionStillCurrent(generation)) set({ identity, loadedForUserId: userId, loading: false, error: null });
    } catch {
      if (sessionStillCurrent(generation)) set({ identity: null, loadedForUserId: null, loading: false, error: '企业权限加载失败' });
    }
  },
  clear: () => set({ identity: null, loadedForUserId: null, loading: false, error: null }),
  has: (permission) => Boolean(get().identity?.is_super_admin || get().identity?.permissions.some((item) => item.code === permission)),
}));

registerSessionReset(() => useAdminPermissionStore.getState().clear());
