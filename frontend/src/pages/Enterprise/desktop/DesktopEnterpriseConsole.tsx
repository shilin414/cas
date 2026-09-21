/**
 * DesktopEnterpriseConsole — the desktop console exactly as before the
 * mobile split (开发执行报告 §31/§51/§54): AntD Layout + 220px Sider + Menu,
 * content routed by the shared `currentKey`. Logic moved verbatim from
 * EnterprisePage.tsx; Desktop 不允许回归.
 */
import React, { lazy, Suspense, useMemo } from 'react';
import { Layout, Menu } from 'antd';
import { ControlOutlined } from '@ant-design/icons';
import { useLocation, useNavigate } from 'react-router-dom';
import { filterEnterpriseSections, currentKey, pagePath } from '../enterpriseNav';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { PermissionGuard } from '../shared/PermissionGuard';
import AdminsPage from '../shared/AdminsPage';
import AccessGroupsPage from '../shared/AccessGroupsPage';
import AccessDiagnosisPage from '../shared/AccessDiagnosisPage';
import Overview from './Overview';
import ResourcePage from './ResourcePage';
import AccessPage from './AccessPage';
import DirectoryPage from './DirectoryPage';
import SyncPage from './SyncPage';
import ProvidersPage from './ProvidersPage';
import AuditPage from './AuditPage';
import '../EnterprisePage.css';

const AIModelsPage = lazy(() => import('@/features/ai-models/AIModelsPage'));

const { Sider, Content } = Layout;

export default function DesktopEnterpriseConsole() {
  const location = useLocation();
  const navigate = useNavigate();
  const key = currentKey(location.pathname);
  const identity = useAdminPermissionStore((state) => state.identity);
  const permissions = useMemo(() => new Set(identity?.permissions.map((item) => item.code) ?? []), [identity]);
  const sections = useMemo(() => filterEnterpriseSections(permissions, Boolean(identity?.is_super_admin)), [permissions, identity?.is_super_admin]);
  const content = useMemo(() => {
    if (key === 'resources/agents') return <PermissionGuard permission="resource.agent.read"><ResourcePage kind="chat" /></PermissionGuard>;
    if (key === 'resources/apps') return <PermissionGuard permission="resource.app.read"><ResourcePage kind="fixed" /></PermissionGuard>;
    if (key.startsWith('access/agents')) return <PermissionGuard permission="access.policy.read"><AccessPage kind="chat" /></PermissionGuard>;
    if (key.startsWith('access/apps')) return <PermissionGuard permission="access.policy.read"><AccessPage kind="fixed" /></PermissionGuard>;
    if (key === 'access/groups') return <PermissionGuard permission="access.group.read"><AccessGroupsPage /></PermissionGuard>;
    if (key === 'access/diagnosis') return <PermissionGuard permission="access.diagnosis.read"><AccessDiagnosisPage /></PermissionGuard>;
    if (key === 'admins') return <PermissionGuard permission={["admin.user.read", "admin.role.read"]}><AdminsPage /></PermissionGuard>;
    if (key === 'directory') return <PermissionGuard permission="directory.read"><DirectoryPage /></PermissionGuard>;
    if (key === 'directory/sync') return <PermissionGuard permission="directory.sync.read"><SyncPage /></PermissionGuard>;
    if (key === 'ai-models') return <PermissionGuard permission={["ai.model.read", "ai.model.test", "ai.model.log.read"]}><Suspense fallback={<div role="status">正在加载 AI 模型管理…</div>}><AIModelsPage /></Suspense></PermissionGuard>;
    if (key === 'providers') return <PermissionGuard permission="provider.read"><ProvidersPage /></PermissionGuard>;
    if (key === 'audit') return <PermissionGuard permission="audit.read"><AuditPage /></PermissionGuard>;
    return <Overview />;
  }, [key]);
  return (
    <Layout className="enterprise-console">
      <Sider width={220} theme="light" className="enterprise-sider">
        <div className="enterprise-brand">企业控制台</div>
        <Menu
          mode="inline"
          selectedKeys={[key.split('?')[0]]}
          items={[
            { key: 'overview', icon: <ControlOutlined />, label: '概览' },
            ...sections.map((section) => ({
              type: 'group' as const,
              label: section.title,
              children: section.items.map(({ key: k, icon, label }) => ({
                key: k,
                icon,
                label,
              })),
            })),
          ]}
          onClick={({ key: k }) => navigate(pagePath(k))}
        />
      </Sider>
      <Content className="enterprise-content">{content}</Content>
    </Layout>
  );
}
