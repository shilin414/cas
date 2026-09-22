/**
 * MobileEnterpriseConsole — 企业控制台移动端（开发执行报告 §31/§52/§88）。
 *
 * 按 pathname 切换子页面（URL 与桌面完全一致，不引入 ?tab=）。二级页面
 * 通过 useMobileHeader 把顶栏换成「← 标题」，返回固定回 /enterprise，
 * 避免浏览器历史混乱（§5 企业控制台二级页面）。
 */
import React, { lazy, Suspense, useMemo } from 'react';
import { useLocation } from 'react-router-dom';
import { useMobileHeader } from '@/shell/mobileHeader';
import { currentKey } from '../enterpriseNav';
import MobileEnterpriseHome from './MobileEnterpriseHome';
import MobileResourcePage from './MobileResourcePage';
import MobileAccessPage from './MobileAccessPage';
import MobileDirectoryPage from './MobileDirectoryPage';
import MobileSyncPage from './MobileSyncPage';
import MobileProvidersPage from './MobileProvidersPage';
import MobileAuditPage from './MobileAuditPage';
import AdminsPage from '../shared/AdminsPage';
import AccessGroupsPage from '../shared/AccessGroupsPage';
import AccessDiagnosisPage from '../shared/AccessDiagnosisPage';
import { PermissionGuard } from '../shared/PermissionGuard';

const OperationsPage = lazy(() => import('@/features/operations/OperationsPage'));

const AIModelsPage = lazy(() => import('@/features/ai-models/AIModelsPage'));

const SUBPAGE_TITLES: Record<string, string> = {
  'resources/agents': '智能体管理',
  'resources/apps': '应用管理',
  'access/agents': '智能体授权',
  'access/apps': '应用授权',
  'access/groups': '企业用户组',
  'access/diagnosis': '权限诊断',
  'admins': '管理员与角色',
  'directory': '部门与人员',
  'directory/sync': '同步管理',
  'providers': 'Provider',
  'operations': '运行中心',
  'ai-models': 'AI 模型管理',
  'audit': '审计日志',
};

export default function MobileEnterpriseConsole() {
  const location = useLocation();
  const key = currentKey(location.pathname);
  const baseKey = key.split('?')[0];

  const isHome = baseKey === 'overview' || baseKey === '';
  const title = SUBPAGE_TITLES[baseKey];

  // 首页沿用路由 handle 的 console 模式；二级页面覆盖为 detail（← 返回）。
  useMobileHeader(isHome || !title ? null : {
    mode: 'detail',
    title,
    backTo: '/enterprise',
  });

  // key：resources/agents ↔ resources/apps 是同型组件，不加 key 会复用实例，
  // 搜索词/编辑器开合等状态跨 kind 泄漏。
  const content = useMemo(() => {
    if (baseKey === 'resources/agents') return <PermissionGuard permission="resource.agent.read"><MobileResourcePage key={baseKey} kind="chat" /></PermissionGuard>;
    if (baseKey === 'resources/apps') return <PermissionGuard permission="resource.app.read"><MobileResourcePage key={baseKey} kind="fixed" /></PermissionGuard>;
    if (baseKey.startsWith('access/agents')) return <PermissionGuard permission="access.policy.read"><MobileAccessPage key={baseKey} kind="chat" /></PermissionGuard>;
    if (baseKey.startsWith('access/apps')) return <PermissionGuard permission="access.policy.read"><MobileAccessPage key={baseKey} kind="fixed" /></PermissionGuard>;
    if (baseKey === 'access/groups') return <PermissionGuard permission="access.group.read"><AccessGroupsPage /></PermissionGuard>;
    if (baseKey === 'access/diagnosis') return <PermissionGuard permission="access.diagnosis.read"><AccessDiagnosisPage /></PermissionGuard>;
    if (baseKey === 'admins') return <PermissionGuard permission={["admin.user.read", "admin.role.read"]}><AdminsPage /></PermissionGuard>;
    if (baseKey === 'directory') return <PermissionGuard permission="directory.read"><MobileDirectoryPage key={baseKey} /></PermissionGuard>;
    if (baseKey === 'directory/sync') return <PermissionGuard permission="directory.sync.read"><MobileSyncPage key={baseKey} /></PermissionGuard>;
    if (baseKey === 'ai-models') return <PermissionGuard permission={["ai.model.read", "ai.model.test", "ai.model.log.read"]}><Suspense fallback={<div role="status">正在加载 AI 模型管理…</div>}><AIModelsPage /></Suspense></PermissionGuard>;
    if (baseKey === 'operations') return <PermissionGuard permission="run.monitor.read"><Suspense fallback={<div role="status">正在加载运行中心…</div>}><OperationsPage mobile /></Suspense></PermissionGuard>;
    if (baseKey === 'providers') return <PermissionGuard permission="provider.read"><MobileProvidersPage key={baseKey} /></PermissionGuard>;
    if (baseKey === 'audit') return <PermissionGuard permission="audit.read"><MobileAuditPage key={baseKey} /></PermissionGuard>;
    return <MobileEnterpriseHome key="overview" />;
  }, [baseKey]);

  return <div className="enterprise-console-mobile">{content}</div>;
}
