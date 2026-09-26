import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import AccountMenu from '@/components/AccountMenu/AccountMenu';
import RecentNavigationIcon from '@/components/Navigation/RecentNavigationIcon';
import { NavigationItemIcon, getVisibleNavigationItems, isNavigationItemActive } from '@/components/Navigation';
import { routeForApplication } from '@/lib/applicationRoute';
import { useAuthStore } from '@/stores/useAuthStore';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { useWorkspaceBootstrapStore } from '@/stores/useWorkspaceBootstrapStore';
import RecentTaskList from '../tasks/RecentTaskList';
import { useRecentCapabilities } from '../capability/useRecentCapabilities';
import './workbench-shell.css';

export interface MobileWorkbenchDrawerProps {
  /**
   * 一级菜单（首页/任务/智能体/应用/自动化/企业管理）→ root 切换，
   * Mobile 用 replace 不污染浏览器历史。
   */
  onRootNavigate: (path: string) => void;
  /** 打开具体能力（最近使用 / 最近任务）→ 详情 PUSH。 */
  onPageNavigate: (path: string) => void;
  /** 关闭抽屉但不导航（账号面板、取消等）。 */
  close: () => void;
}

export default function MobileWorkbenchDrawer({ onRootNavigate, onPageNavigate, close }: MobileWorkbenchDrawerProps) {
  const location = useLocation();
  const isStaff = useAuthStore((state) => Boolean(state.user?.is_staff));
  const userId = useAuthStore((state) => state.user?.id);
  const canAccessEnterprise = useAdminPermissionStore((state) => Boolean(state.identity?.can_access_console));
  useEffect(() => { if (userId) void useAdminPermissionStore.getState().load(String(userId)); }, [userId]);
  const capabilities = useRecentCapabilities(6);
  const tasks = useWorkspaceBootstrapStore((state) => state.recentTasks);
  return (
    <div className="mobile-workbench-drawer">
      <div className="mobile-workbench-drawer__scroll">
      <nav className="mobile-shell__nav" aria-label="主导航">
        {getVisibleNavigationItems({ isStaff, canAccessEnterprise }).map((item) => {
          const active = isNavigationItemActive(item.id, location.pathname);
          return (
            <button key={item.id} type="button" className={`mobile-shell__nav-item${active ? ' active' : ''}`}
              aria-current={active ? 'page' : undefined} onClick={() => onRootNavigate(item.path)}>
              <NavigationItemIcon item={item} surface="mobile" className="mobile-shell__nav-icon" /><span>{item.mobileLabel}</span>
            </button>
          );
        })}
      </nav>
      <section className="mobile-workbench-drawer__section"><h2>最近使用</h2>{capabilities.slice(0, 6).map((item) => (
        <button type="button" key={item.id} onClick={() => onPageNavigate(routeForApplication(item))}>
          <RecentNavigationIcon kind={item.kind} emoji={item.icon} /><span>{item.name}</span>
        </button>
      ))}</section>
      <section className="mobile-workbench-drawer__section"><h2>最近任务</h2><RecentTaskList items={tasks} limit={6} />
        <button type="button" className="mobile-workbench-drawer__all" onClick={() => onRootNavigate('/tasks')}>查看全部任务 ›</button>
      </section>
      </div>
      <div className="mobile-workbench-drawer__account"><AccountMenu variant="panel" onLogoutComplete={close} /></div>
    </div>
  );
}
