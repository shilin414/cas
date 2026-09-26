import { useEffect, useRef } from 'react';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { Button, Drawer } from 'antd';
import {
  ArrowLeftOutlined,
  MenuOutlined,
  MoreOutlined,
  PlusOutlined,
  SearchOutlined,
} from '@ant-design/icons';
import MobileAgentSwitcher from '@/components/Mobile/MobileAgentSwitcher';
import MobileWorkbenchDrawer from '@/workbench/shell/MobileWorkbenchDrawer';
import { useWorkspaceStore } from '@/stores/useWorkspaceStore';
import { useRunChatStore } from '@/stores/useRunChatStore';
import { useWorkspaceBootstrapStore } from '@/stores/useWorkspaceBootstrapStore';
import { useAppNavigation } from '@/router/useAppNavigation';
import type { ShellChrome } from './useShellChrome';
import { MobileHeaderProvider, useMobileHeaderState } from './mobileHeader';
import './shell.css';

const MobileShellContent: React.FC<{ chrome: ShellChrome }> = ({ chrome }) => {
  const mobileNavOpen = useWorkspaceStore((state) => state.mobileNavOpen);
  const setMobileNavOpen = useWorkspaceStore((state) => state.setMobileNavOpen);
  const loadBootstrap = useWorkspaceBootstrapStore((state) => state.load);
  const bootstrapDirty = useWorkspaceBootstrapStore((state) => state.dirty);
  const navigate = useNavigate();
  const location = useLocation();
  const navigation = useAppNavigation();
  const pageOverride = useMobileHeaderState();
  const mobile = { ...chrome.mobile, ...pageOverride };
  const mode = mobile.mode ?? 'workspace';
  const showBack = mobile.showBack ?? mode === 'detail';
  const showMenu = mobile.showMenu ?? !showBack;
  const showAgentSwitcher = mobile.showAgentSwitcher ?? true;

  // Drawer 导航时序（Architecture 2.0 §21）：先记录目标并关抽屉，
  // 等关闭动画真正结束（afterOpenChange(false)）再导航 —— 不用 setTimeout 猜。
  const pendingNavigationRef = useRef<string | null>(null);
  const pendingNavigationTypeRef = useRef<'root' | 'page'>('root');

  useEffect(() => { void loadBootstrap(); }, [loadBootstrap]);
  useEffect(() => {
    if (bootstrapDirty) void loadBootstrap(true);
  }, [bootstrapDirty, loadBootstrap]);

  // 顶栏返回：应用内 PUSH 过 → POP；Direct Link → route meta parent/root replace。
  const go = () => {
    setMobileNavOpen(false);
    navigation.back();
  };

  const requestDrawerNavigation = (path: string, type: 'root' | 'page') => {
    pendingNavigationRef.current = path;
    pendingNavigationTypeRef.current = type;
    setMobileNavOpen(false);
  };

  const handleNewTask = () => {
    setMobileNavOpen(false);
    if (pageOverride?.onAction) { pageOverride.onAction(); return; }
    const applicationId = location.pathname.startsWith('/chat/')
      ? useWorkspaceStore.getState().activeApplicationId : null;
    if (applicationId != null) {
      useWorkspaceStore.getState().startNewConversation(applicationId);
      useRunChatStore.getState().setActiveConversation(null);
      navigate('/', { state: { newTaskApplicationId: applicationId } });
    } else {
      navigate('/');
    }
  };

  const renderAction = () => {
    const action = mobile.action ?? (mode === 'workspace' ? 'new-task' : 'none');
    if (action === 'none') return <span className="mobile-shell__bar-spacer" aria-hidden />;
    if (action === 'new-task') {
      return (
        <Button
          type="primary"
          size="small"
          icon={<PlusOutlined />}
          className="mobile-shell__task-btn"
          onClick={handleNewTask}
        >
          新任务
        </Button>
      );
    }
    const labels = { create: '新建', search: '搜索', more: '更多' } as const;
    const icons = {
      create: <PlusOutlined />,
      search: <SearchOutlined />,
      more: <MoreOutlined />,
    } as const;
    return (
      <button
        type="button"
        className="mobile-shell__icon-btn"
        aria-label={labels[action]}
        onClick={pageOverride?.onAction}
        disabled={!pageOverride?.onAction}
      >
        {icons[action]}
      </button>
    );
  };

  return (
    <div className="mobile-shell">
      {!chrome.hideHeader && (
        <header className="mobile-shell__bar">
          {showBack ? (
            <button
              type="button"
              className="mobile-shell__icon-btn"
              aria-label="返回企业控制台"
              onClick={go}
            >
              <ArrowLeftOutlined />
            </button>
          ) : showMenu ? (
            <button
              type="button"
              className="mobile-shell__icon-btn"
              aria-label="打开导航"
              onClick={() => setMobileNavOpen(true)}
            >
              <MenuOutlined />
            </button>
          ) : <span className="mobile-shell__bar-spacer" aria-hidden />}

          {mode === 'workspace' ? (
            showAgentSwitcher
              ? <MobileAgentSwitcher />
              : <span className="mobile-shell__workspace-spacer" aria-hidden />
          ) : (
            <div className="mobile-shell__title" title={mobile.title}>{mobile.title}</div>
          )}
          {renderAction()}
        </header>
      )}

      {/* No `--padded` modifier: MobilePage owns its own padding (二次复审
          P3-4 — a class with no CSS behind it only misleads). */}
      <main className="mobile-shell__main">
        <Outlet />
      </main>

      <Drawer
        placement="left"
        open={mobileNavOpen}
        onClose={() => setMobileNavOpen(false)}
        afterOpenChange={(open) => {
          if (open) return;
          const target = pendingNavigationRef.current;
          if (!target) return;
          pendingNavigationRef.current = null;
          if (pendingNavigationTypeRef.current === 'root') navigation.switchRoot(target);
          else navigation.pushPage(target);
        }}
        width="82vw"
        title="小安工作助手"
        rootClassName="mobile-shell__drawer"
        styles={{ body: { padding: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' } }}
      >
        <MobileWorkbenchDrawer
          onRootNavigate={(path) => requestDrawerNavigation(path, 'root')}
          onPageNavigate={(path) => requestDrawerNavigation(path, 'page')}
          close={() => setMobileNavOpen(false)}
        />
      </Drawer>
    </div>
  );
};

const MobileAppShell: React.FC<{ chrome: ShellChrome }> = ({ chrome }) => (
  <MobileHeaderProvider>
    <MobileShellContent chrome={chrome} />
  </MobileHeaderProvider>
);

export default MobileAppShell;
