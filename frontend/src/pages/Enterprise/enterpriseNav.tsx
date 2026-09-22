/**
 * enterpriseNav — the ONE navigation map of the enterprise console
 * (开发执行报告 §88): Desktop renders it as the Sider Menu, Mobile renders
 * it as the home menu hub, but the keys — and therefore the URLs — are
 * identical, so 刷新 / 分享 / 前进后退 behave the same on both.
 */
import React from 'react';
import {
  ApartmentOutlined,
  AppstoreOutlined,
  AuditOutlined,
  CloudSyncOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  TeamOutlined,
  UserSwitchOutlined,
  ClusterOutlined,
  SearchOutlined,
} from '@ant-design/icons';

export interface EnterpriseNavItem {
  /** URL path segment under /enterprise/ (may carry a query, e.g. access?app=). */
  key: string;
  label: string;
  icon: React.ReactNode;
  requiredPermission: string;
  requiredAnyPermissions?: string[];
}

export interface EnterpriseNavSection {
  title: string;
  items: EnterpriseNavItem[];
}

export const ENTERPRISE_SECTIONS: EnterpriseNavSection[] = [
  {
    title: '资源管理',
    items: [
      { key: 'resources/agents', label: '智能体管理', icon: <RobotOutlined />, requiredPermission: 'resource.agent.read' },
      { key: 'resources/apps', label: '应用管理', icon: <AppstoreOutlined />, requiredPermission: 'resource.app.read' },
    ],
  },
  {
    title: '权限管理',
    items: [
      { key: 'access/agents', label: '智能体授权', icon: <SafetyCertificateOutlined />, requiredPermission: 'access.policy.read' },
      { key: 'access/apps', label: '应用授权', icon: <SafetyCertificateOutlined />, requiredPermission: 'access.policy.read' },
      { key: 'access/groups', label: '企业用户组', icon: <ClusterOutlined />, requiredPermission: 'access.group.read' },
      { key: 'access/diagnosis', label: '权限诊断', icon: <SearchOutlined />, requiredPermission: 'access.diagnosis.read' },
      { key: 'admins', label: '管理员与角色', icon: <UserSwitchOutlined />, requiredPermission: 'admin.user.read', requiredAnyPermissions: ['admin.user.read', 'admin.role.read'] },
    ],
  },
  {
    title: '组织架构',
    items: [
      { key: 'directory', label: '部门与人员', icon: <TeamOutlined />, requiredPermission: 'directory.read' },
      { key: 'directory/sync', label: '同步管理', icon: <CloudSyncOutlined />, requiredPermission: 'directory.sync.read' },
    ],
  },
  {
    title: '平台管理',
    items: [
      { key: 'ai-models', label: 'AI 模型管理', icon: <RobotOutlined />, requiredPermission: 'ai.model.read', requiredAnyPermissions: ['ai.model.read', 'ai.model.test', 'ai.model.log.read'] },
      { key: 'operations', label: '运行中心', icon: <ClusterOutlined />, requiredPermission: 'run.monitor.read' },
      { key: 'providers', label: 'Provider', icon: <ApartmentOutlined />, requiredPermission: 'provider.read' },
      { key: 'audit', label: '审计日志', icon: <AuditOutlined />, requiredPermission: 'audit.read' },
    ],
  },
];

export const pagePath = (key: string) => `/enterprise/${key}`;

export const currentKey = (pathname: string) =>
  pathname.replace(/^\/enterprise\/?/, '') || 'overview';

export const fmt = (v?: string | null) => (v ? new Date(v).toLocaleString() : '—');

export function filterEnterpriseSections(permissions: Set<string>, isSuperAdmin: boolean) {
  return ENTERPRISE_SECTIONS.map((section) => ({ ...section, items: section.items.filter((item) => isSuperAdmin || (item.requiredAnyPermissions ? item.requiredAnyPermissions.some((code) => permissions.has(code)) : permissions.has(item.requiredPermission))) })).filter((section) => section.items.length > 0);
}
