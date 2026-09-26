import { lazy, Suspense, type ReactNode } from 'react';
import { createBrowserRouter, Navigate } from 'react-router-dom';
import { AuthLayout } from '@/layouts';
import AppShell from '@/shell/AppShell';
import WorkspaceHost from '@/components/Workspace/WorkspaceHost';
import { AdminRoute, EnterpriseRoute, ProtectedRoute, PublicRoute } from './guards';
import { enterprisePermission } from './permissions';
import { ENTERPRISE_ROUTE_DEFINITIONS } from '@/pages/Enterprise/enterpriseRoutes';
import LegacyAppRunRedirect from './LegacyAppRunRedirect';

const ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY = Object.fromEntries(
  ENTERPRISE_ROUTE_DEFINITIONS.map((definition) => [definition.key, definition]),
);

// Console pages (application management) are route-level lazy (三次复审
// §55–§58): 首页 / Chat 首屏不再下载用户可能永远不会进入的管理页 ——
// Enterprise 已用同一模式证明可行。只切分次级 console 路由：
// WorkspaceHost / 主 Chat / Shell / 登录页 保持同步加载。
const TaskCenterPage = lazy(() => import('@/workbench/tasks/TaskCenterPage'));
const AgentsPage = lazy(() => import('@/pages/Agents/AgentsPage'));
const AgentDetailPage = lazy(() => import('@/pages/Agents/AgentDetailPage'));
const TemplatesPage = lazy(() => import('@/pages/Templates/TemplatesPage'));
const TemplateDetailPage = lazy(() => import('@/pages/Templates/TemplateDetailPage'));
const AppsPage = lazy(() => import('@/pages/Apps/AppsPage'));
const AppDetailPage = lazy(() => import('@/pages/Apps/AppDetailPage'));
const ChatApplicationEditPage = lazy(() => import('@/pages/Apps/ChatApplicationEditPage'));
const WorkspacePage = lazy(() => import('@/pages/Workspace/WorkspacePage'));
const WorkflowsPage = lazy(() => import('@/pages/Workflows/WorkflowsPage'));
const WorkflowEditorPage = lazy(() => import('@/pages/Workflows/WorkflowEditorPage'));
const WorkflowRunnerPage = lazy(() => import('@/pages/Workflows/WorkflowRunnerPage'));
const SkillsPage = lazy(() => import('@/pages/Skills/SkillsPage'));
// Schedules route surfaces（Architecture 2.0 §51）：详情/编辑有真实 URL。
const ScheduleListRoute = lazy(() => import('@/pages/Schedules/ScheduleListRoute'));
const ScheduleDetailRoute = lazy(() => import('@/pages/Schedules/ScheduleDetailRoute'));
const ScheduleEditorCreateRoute = lazy(() => import('@/pages/Schedules/ScheduleEditorRoute')
  .then((m) => ({ default: (props: { mode?: 'create' | 'edit' }) => <m.default mode={props.mode ?? 'create'} /> })));
const ScheduleEditorEditRoute = lazy(() => import('@/pages/Schedules/ScheduleEditorRoute')
  .then((m) => ({ default: () => <m.default mode="edit" /> })));

// Auth pages stay outside the shell entirely.
// 普通用户 = 飞书 SSO Only（/login 自动发起 OAuth）；管理员 = /login/admin。
import LoginPage from '@/pages/Auth/LoginPage';
import FeishuAutoLoginPage from '@/pages/Auth/FeishuAutoLoginPage';
import AdminLoginPage from '@/pages/Auth/AdminLoginPage';
import SsoCallbackPage from '@/pages/Auth/SsoCallbackPage';
import FeishuCallbackPage from '@/pages/Auth/FeishuCallbackPage';

// Public share snapshot (no shell, no auth — the backend returns only the
// snapshotted messages for the token).
import SharePage from '@/pages/Share/SharePage';

const EnterpriseOverviewRoute = lazy(() => import('@/pages/Enterprise/enterpriseRouteAdapters')
  .then((m) => ({ default: m.EnterpriseOverviewRoute })));

// Enterprise 嵌套路由（Architecture 2.0 §32/§34）：URL 不变，页面由子路由
// 渲染；每条子路由在 handle.app 声明权限与移动 Header 元数据，由
// RoutePermissionBoundary 统一执行 —— 不在每个 route JSX 手写 Guard。
const EnterpriseRouteLayout = lazy(() => import('@/pages/Enterprise/EnterpriseRouteLayout'));
// RoutePermissionBoundary 内部渲染 <Outlet/>，作为 layout route element 使用。
const RoutePermissionBoundary = lazy(() => import('@/router/RoutePermissionBoundary')
  .then((m) => ({ default: m.RoutePermissionBoundary })));
const enterpriseAdapters = () => import('@/pages/Enterprise/enterpriseRouteAdapters');

const enterpriseChild = (
  definition: { id: string; path: string; key: string; permission: unknown; mobileTitle: string },
  load: (m: Awaited<ReturnType<typeof enterpriseAdapters>>) => React.ComponentType,
) => {
  const Component = lazy(async () => {
    const m = await enterpriseAdapters();
    return { default: load(m) };
  });
  return {
    path: definition.path,
    element: (
      <Suspense fallback={<div style={{ padding: 32 }}>正在加载…</div>}>
        <RoutePermissionBoundary />
      </Suspense>
    ),
    children: [
      {
        index: true,
        element: (
          <Suspense fallback={<div style={{ padding: 32 }}>正在加载…</div>}>
            <Component />
          </Suspense>
        ),
      },
    ],
    handle: {
      app: {
        id: definition.id,
        level: 'detail' as const,
        root: '/enterprise',
        parent: '/enterprise',
        permission: definition.permission as never,
        mobile: { mode: 'detail' as const, title: definition.mobileTitle },
      },
    },
  };
};

/** 次级 console 路由的懒加载壳：短 fallback，不打断布局。 */
const lazyConsole = (node: ReactNode) => (
  <Suspense fallback={<div style={{ padding: 32 }}>正在加载…</div>}>{node}</Suspense>
);

/** Console pages scroll and pad; workspaces lay themselves out (§17/§33). */
const consolePage = { shell: { padded: true } };
const fullWidthConsole = { shell: { padded: true } };
const fullscreenConsole = { shell: { hideSidebar: true, hideHeader: true } };

// Mobile shell handles: the header swaps the workspace switcher for a page
// title; Schedules gains a create action wired by the page itself via
// useMobileHeaderAction. Enterprise sub-page titles come from each child
// route's handle.app (mode: 'detail').
//
// Architecture 2.0 (§12/§14): every top-level route also declares handle.app
// (route meta) alongside the legacy handle.shell — useShellChrome keeps
// reading `shell`, useRouteMeta reads `app` — until all routes are migrated.
const homeWorkspaceHandle = {
  shell: {
    mobile: {
      mode: 'workspace' as const,
      action: 'new-task' as const,
      showAgentSwitcher: false,
    },
  },
  app: {
    id: 'home',
    level: 'root' as const,
    root: '/',
    mobile: {
      mode: 'workspace' as const,
      action: 'new-task' as const,
      showAgentSwitcher: false,
    },
  },
};

const fixedWorkspaceHandle = {
  shell: { mobile: { mode: 'page' as const, title: '应用', action: 'none' as const } },
  app: { id: 'workspace-fixed', level: 'root' as const, root: '/', mobile: { mode: 'page' as const, title: '应用' } },
};
const taskPageHandle = {
  shell: {
    padded: false,
    mobile: { mode: 'page' as const, title: '任务', action: 'none' as const },
  },
  app: { id: 'tasks', level: 'root' as const, root: '/tasks', mobile: { mode: 'page' as const, title: '任务' } },
};
const agentsPageHandle = {
  shell: {
    padded: true,
    mobile: { mode: 'page' as const, title: '智能体中心' },
  },
  app: { id: 'agents', level: 'root' as const, root: '/agents', mobile: { mode: 'page' as const, title: '智能体中心' } },
};
const appsPageHandle = {
  shell: {
    padded: true,
    mobile: { mode: 'page' as const, title: '应用中心' },
  },
  app: { id: 'apps', level: 'root' as const, root: '/apps', mobile: { mode: 'page' as const, title: '应用中心' } },
};
const schedulesPageHandle = {
  shell: {
    padded: true,
    mobile: { mode: 'page' as const, title: '自动化', action: 'create' as const },
  },
  app: {
    id: 'schedules',
    level: 'root' as const,
    root: '/schedules',
    mobile: { mode: 'page' as const, title: '自动化', action: 'create' as const },
  },
};
const enterprisePageHandle = {
  shell: {
    padded: false,
    mobile: { mode: 'console' as const, title: '企业控制台' },
  },
  app: {
    id: 'enterprise',
    level: 'root' as const,
    root: '/enterprise',
    permission: enterprisePermission(),
    mobile: { mode: 'console' as const, title: '企业控制台' },
  },
};

/**
 * One AppShell wraps every authenticated route (§33).
 *
 * The shell is a *layout* route, so switching between the main workspace and
 * the console never unmounts it — only <Outlet/> swaps. Workspace routes
 * (`/`, `/chat/:slug`, `/app/:slug`, `/workflow/:slug`) all render the same
 * WorkspaceHost, which is why a fixed application opens inside the shell
 * instead of navigating away (§32).
 */
const router = createBrowserRouter([
  {
    path: '/',
    element: (
      <ProtectedRoute>
        <AppShell />
      </ProtectedRoute>
    ),
    children: [
      // ── Workspaces (§18/§33) ──────────────────────────────────────────
      { index: true, element: <WorkspaceHost kind="home" />, handle: homeWorkspaceHandle },
      { path: 'tasks', element: lazyConsole(<TaskCenterPage />), handle: taskPageHandle },
      { path: 'chat/:applicationSlug', element: <WorkspaceHost kind="chat" /> },
      { path: 'app/:applicationSlug', element: <WorkspaceHost kind="page" />, handle: fixedWorkspaceHandle },
      {
        path: 'workflow/:applicationSlug',
        element: <WorkspaceHost kind="workflow" />,
        handle: fixedWorkspaceHandle,
      },

      // ── Console (application management, route-level lazy) ────────────
      { path: 'agents', element: lazyConsole(<AgentsPage />), handle: agentsPageHandle },
      {
        path: 'agents/:id',
        element: lazyConsole(<AgentDetailPage />),
        handle: consolePage,
      },
      {
        path: 'templates',
        element: lazyConsole(<TemplatesPage />),
        handle: consolePage,
      },
      {
        path: 'templates/:id',
        element: lazyConsole(<TemplateDetailPage />),
        handle: consolePage,
      },
      { path: 'apps', element: lazyConsole(<AppsPage />), handle: appsPageHandle },
      {
        path: 'apps/:id',
        element: lazyConsole(
          <AdminRoute><AppDetailPage /></AdminRoute>,
        ),
        handle: consolePage,
      },
      // Launched apps now live in the shell's application workspace (§32).
      { path: 'apps/:id/run', element: <LegacyAppRunRedirect /> },
      {
        path: 'apps/:id/edit',
        element: lazyConsole(
          <AdminRoute><ChatApplicationEditPage /></AdminRoute>,
        ),
        handle: consolePage,
      },
      { path: 'skills', element: lazyConsole(<SkillsPage />), handle: fullWidthConsole },
      {
        path: 'schedules',
        element: lazyConsole(<ScheduleListRoute />),
        handle: schedulesPageHandle,
      },
      {
        path: 'schedules/new',
        element: lazyConsole(<ScheduleEditorCreateRoute />),
        handle: {
          shell: schedulesPageHandle.shell,
          app: {
            id: 'schedule-editor-new',
            level: 'detail' as const,
            root: '/schedules',
            parent: '/schedules',
            mobile: { mode: 'detail' as const, title: '新建自动化' },
          },
        },
      },
      {
        path: 'schedules/:scheduleId',
        element: lazyConsole(<ScheduleDetailRoute />),
        handle: {
          shell: schedulesPageHandle.shell,
          app: {
            id: 'schedule-detail',
            level: 'detail' as const,
            root: '/schedules',
            parent: '/schedules',
            mobile: { mode: 'detail' as const, title: '自动化详情' },
          },
        },
      },
      {
        path: 'schedules/:scheduleId/edit',
        element: lazyConsole(<ScheduleEditorEditRoute />),
        handle: {
          shell: schedulesPageHandle.shell,
          app: {
            id: 'schedule-editor-edit',
            level: 'detail' as const,
            root: '/schedules',
            parent: '/schedules/:scheduleId',
            mobile: { mode: 'detail' as const, title: '编辑自动化' },
          },
        },
      },
      {
        path: 'workflows',
        element: lazyConsole(<WorkflowsPage />),
        handle: fullWidthConsole,
      },
      {
        path: 'workflows/:id/edit',
        element: lazyConsole(<WorkflowEditorPage />),
        handle: fullWidthConsole,
      },
      {
        path: 'workflow-runs/:runId',
        element: lazyConsole(<WorkflowRunnerPage />),
        handle: fullscreenConsole,
      },
      // Template-workflow workspace: still on the legacy agent engine.
      {
        path: 'workspace',
        element: lazyConsole(<WorkspacePage />),
        handle: consolePage,
      },
      {
        path: 'workspace/:id',
        element: lazyConsole(<WorkspacePage />),
        handle: fullWidthConsole,
      },
      {
        path: 'enterprise',
        element: (
          <Suspense fallback={<div style={{ padding: 32 }}>正在加载企业控制台…</div>}>
            <EnterpriseRoute>
              <EnterpriseRouteLayout />
            </EnterpriseRoute>
          </Suspense>
        ),
        handle: {
          shell: enterprisePageHandle.shell,
          app: enterprisePageHandle.app,
        },
        children: [
          {
            index: true,
            element: (
              <Suspense fallback={<div style={{ padding: 32 }}>正在加载…</div>}>
                {/* 概览也经 boundary（审查 M-3）：useRouteMeta 冒泡读取父路由
                    /enterprise 的 enterprisePermission —— 与其余 13 个子页一致。 */}
                <RoutePermissionBoundary>
                  <EnterpriseOverviewRoute />
                </RoutePermissionBoundary>
              </Suspense>
            ),
          },
          // 子路由全部由 ENTERPRISE_ROUTE_DEFINITIONS 派生（§46 Adapter）。
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['resources/agents'], (m) => m.EnterpriseAgentResourceRoute),
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['resources/apps'], (m) => m.EnterpriseAppResourceRoute),
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['access/agents'], (m) => m.EnterpriseAgentAccessRoute),
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['access/apps'], (m) => m.EnterpriseAppAccessRoute),
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['access/groups'], (m) => m.EnterpriseAccessGroupsRoute),
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['access/diagnosis'], (m) => m.EnterpriseAccessDiagnosisRoute),
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['admins'], (m) => m.EnterpriseAdminsRoute),
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['directory'], (m) => m.EnterpriseDirectoryRoute),
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['directory/sync'], (m) => m.EnterpriseSyncRoute),
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['ai-models'], (m) => m.EnterpriseAIModelsRoute),
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['operations'], (m) => m.EnterpriseOperationsRoute),
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['providers'], (m) => m.EnterpriseProvidersRoute),
          enterpriseChild(ENTERPRISE_ROUTE_DEFINITIONS_BY_KEY['audit'], (m) => m.EnterpriseAuditRoute),
          { path: '*', element: <Navigate to="/enterprise" replace /> },
        ],
      },

      { path: '*', element: <Navigate to="/" replace /> },
    ],
  },
  // ── Auth (no shell) ───────────────────────────────────────────────────
  // /login：普通用户入口，自动发起飞书 OAuth（Feishu SSO Only）。
  {
    path: '/login',
    element: <AuthLayout><PublicRoute><FeishuAutoLoginPage /></PublicRoute></AuthLayout>,
  },
  // /login/admin：管理员本地账号口令登录（唯一出现用户名密码的入口）。
  {
    path: '/login/admin',
    element: <AuthLayout><AdminLoginPage /></AuthLayout>,
  },
  {
    path: '/auth/sso/callback',
    element: <AuthLayout><SsoCallbackPage /></AuthLayout>,
  },
  {
    path: '/auth/feishu/callback',
    element: <AuthLayout><FeishuCallbackPage /></AuthLayout>,
  },
  // /auth/login：飞书登录 fallback（OAuth 失败时的重试入口，无密码/注册）。
  {
    path: '/auth/login',
    element: <AuthLayout><PublicRoute><LoginPage /></PublicRoute></AuthLayout>,
  },
  // ── Public share snapshot (no shell, works logged-out) ─────────────────
  {
    path: '/share/:token',
    element: <SharePage />,
  },
  { path: '*', element: <Navigate to="/" replace /> },
], { basename: import.meta.env.BASE_URL });

export default router;
