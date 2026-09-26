/**
 * Enterprise 子路由 Adapter 集合（Architecture 2.0 §46）。
 *
 * 每个 route 一个 Adapter：只做 PC/Mobile Presentation 切换；
 * 权限由子路由 meta + RoutePermissionBoundary 负责（不在页面内重复）。
 */
import { lazy, Suspense } from 'react';
import { useIsMobile } from '@/shell/useIsMobile';
import Overview from './desktop/Overview';
import AuditPage from './desktop/AuditPage';
import MobileAuditPage from './mobile/MobileAuditPage';
import ProvidersPage from './desktop/ProvidersPage';
import MobileProvidersPage from './mobile/MobileProvidersPage';
import ResourcePage from './desktop/ResourcePage';
import MobileResourcePage from './mobile/MobileResourcePage';
import AccessPage from './desktop/AccessPage';
import MobileAccessPage from './mobile/MobileAccessPage';
import DirectoryPage from './desktop/DirectoryPage';
import MobileDirectoryPage from './mobile/MobileDirectoryPage';
import SyncPage from './desktop/SyncPage';
import MobileSyncPage from './mobile/MobileSyncPage';
import AdminsPage from './shared/AdminsPage';
import AccessGroupsPage from './shared/AccessGroupsPage';
import AccessDiagnosisPage from './shared/AccessDiagnosisPage';
import MobileEnterpriseHome from './mobile/MobileEnterpriseHome';

const OperationsPage = lazy(() => import('@/features/operations/OperationsPage'));
const AIModelsPage = lazy(() => import('@/features/ai-models/AIModelsPage'));

const lazyFallback = (label: string) => (
  <div role="status">{`正在加载${label}…`}</div>
);

export function EnterpriseOverviewRoute() {
  const isMobile = useIsMobile();
  return isMobile ? <MobileEnterpriseHome /> : <Overview />;
}

export function EnterpriseAuditRoute() {
  const isMobile = useIsMobile();
  return isMobile ? <MobileAuditPage /> : <AuditPage />;
}

export function EnterpriseProvidersRoute() {
  const isMobile = useIsMobile();
  return isMobile ? <MobileProvidersPage /> : <ProvidersPage />;
}

export function EnterpriseAgentResourceRoute() {
  const isMobile = useIsMobile();
  // key prop：resources/agents ↔ resources/apps 同型组件防止状态跨 kind 泄漏。
  return isMobile
    ? <MobileResourcePage key="resources/agents" kind="chat" />
    : <ResourcePage kind="chat" />;
}

export function EnterpriseAppResourceRoute() {
  const isMobile = useIsMobile();
  return isMobile
    ? <MobileResourcePage key="resources/apps" kind="fixed" />
    : <ResourcePage kind="fixed" />;
}

export function EnterpriseAgentAccessRoute() {
  const isMobile = useIsMobile();
  return isMobile
    ? <MobileAccessPage key="access/agents" kind="chat" />
    : <AccessPage kind="chat" />;
}

export function EnterpriseAppAccessRoute() {
  const isMobile = useIsMobile();
  return isMobile
    ? <MobileAccessPage key="access/apps" kind="fixed" />
    : <AccessPage kind="fixed" />;
}

export function EnterpriseAccessGroupsRoute() {
  return <AccessGroupsPage />;
}

export function EnterpriseAccessDiagnosisRoute() {
  return <AccessDiagnosisPage />;
}

export function EnterpriseAdminsRoute() {
  return <AdminsPage />;
}

export function EnterpriseDirectoryRoute() {
  const isMobile = useIsMobile();
  return isMobile ? <MobileDirectoryPage key="directory" /> : <DirectoryPage />;
}

export function EnterpriseSyncRoute() {
  const isMobile = useIsMobile();
  return isMobile ? <MobileSyncPage key="directory/sync" /> : <SyncPage />;
}

export function EnterpriseAIModelsRoute() {
  return (
    <Suspense fallback={lazyFallback('AI 模型管理')}>
      <AIModelsPage />
    </Suspense>
  );
}

export function EnterpriseOperationsRoute() {
  const isMobile = useIsMobile();
  return (
    <Suspense fallback={lazyFallback('运行中心')}>
      <OperationsPage mobile={isMobile} />
    </Suspense>
  );
}
